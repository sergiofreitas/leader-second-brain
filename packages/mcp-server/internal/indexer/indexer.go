// Package indexer embeds memory chunks in the background.
//
// Embedding a long transcript can take from seconds (a cloud API) to minutes
// (a local model), longer than an MCP tool call should block. Ingest only
// stores the chunks; the indexer picks up the chunks that have no embedding
// for the current model, embeds them in batches and saves the vectors. The
// work queue is the data itself, so nothing is lost on restart and a model
// change re-embeds everything.
package indexer

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// Indexer embeds pending chunks with one embedding provider
type Indexer struct {
	store     *sqlite.Store
	embedder  providers.EmbeddingProvider
	model     string
	batchSize int
	idle      time.Duration // how often to look for work without a Notify
	wake      chan struct{}
	now       func() time.Time
}

// New creates an indexer for embedder and loads its model's vectors into the
// store's search index (dropping vectors of other models)
func New(store *sqlite.Store, embedder providers.EmbeddingProvider, batchSize int) (*Indexer, error) {
	model := embedder.Model()
	if model == "" {
		return nil, fmt.Errorf("indexer: embedding provider has no model name")
	}
	if batchSize <= 0 {
		batchSize = 32
	}
	if err := store.UseEmbeddingModel(model); err != nil {
		return nil, fmt.Errorf("indexer: %w", err)
	}
	return &Indexer{
		store:     store,
		embedder:  embedder,
		model:     model,
		batchSize: batchSize,
		idle:      time.Minute,
		wake:      make(chan struct{}, 1),
		now:       time.Now,
	}, nil
}

// Model returns the embedding model the indexer uses
func (ix *Indexer) Model() string { return ix.model }

// Notify tells the indexer there may be new chunks; it never blocks
func (ix *Indexer) Notify() {
	select {
	case ix.wake <- struct{}{}:
	default:
	}
}

// RunOnce embeds pending chunks, batch by batch, until there are none left.
// It stops at the first failed batch (after recording the failure, so those
// chunks are retried later with backoff) and returns its error.
func (ix *Indexer) RunOnce(ctx context.Context) (embedded int, err error) {
	for ctx.Err() == nil {
		chunks, err := ix.store.PendingChunks(ix.model, ix.now(), ix.batchSize)
		if err != nil {
			return embedded, fmt.Errorf("pending chunks: %w", err)
		}
		if len(chunks) == 0 {
			return embedded, nil
		}
		ids := make([]int64, len(chunks))
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			ids[i], texts[i] = c.ID, c.Content
		}

		vectors, err := ix.embedder.EmbedBatch(texts)
		if err == nil && len(vectors) != len(texts) {
			err = fmt.Errorf("provider returned %d vectors for %d texts", len(vectors), len(texts))
		}
		if err != nil {
			if recErr := ix.store.RecordEmbeddingFailure(ix.model, ids, err, ix.now()); recErr != nil {
				return embedded, fmt.Errorf("embed: %w (recording the failure: %v)", err, recErr)
			}
			return embedded, fmt.Errorf("embed: %w", err)
		}
		if err := ix.store.SaveEmbeddings(ix.model, ids, vectors); err != nil {
			return embedded, err
		}
		embedded += len(chunks)
	}
	return embedded, ctx.Err()
}

// Run embeds pending chunks until ctx is cancelled, waking up on Notify, when
// a failed chunk is due for a retry, and periodically.
func (ix *Indexer) Run(ctx context.Context) {
	for {
		if n, err := ix.RunOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("indexer: %v (embedded %d chunks before the error)", err, n)
		}
		wait := ix.idle
		if next, ok, err := ix.store.NextRetryAt(ix.model); err == nil && ok {
			if d := next.Sub(ix.now()); d < wait {
				wait = max(d, time.Second)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ix.wake:
		case <-time.After(wait):
		}
	}
}

// Status returns the indexing progress for the indexer's model
func (ix *Indexer) Status() (sqlite.EmbeddingStatus, error) {
	return ix.store.GetEmbeddingStatus(ix.model)
}
