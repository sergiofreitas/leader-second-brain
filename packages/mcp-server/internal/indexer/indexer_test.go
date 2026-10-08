package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// fakeEmbedder embeds a text as [len, 1, 0] and can be told to fail
type fakeEmbedder struct {
	mu      sync.Mutex
	model   string
	fail    error
	short   bool // return one vector too few
	batches [][]string
}

func (f *fakeEmbedder) Embed(text string) ([]float32, error) {
	v, err := f.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}

func (f *fakeEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, texts)
	if f.fail != nil {
		return nil, f.fail
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t)), 1, 0}
	}
	if f.short {
		out = out[1:]
	}
	return out, nil
}

func (f *fakeEmbedder) Dimensions() int { return 3 }
func (f *fakeEmbedder) Model() string   { return f.model }

func (f *fakeEmbedder) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.New(t.TempDir() + "/indexer.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addMemory(t *testing.T, s *sqlite.Store, id string, chunks ...string) {
	t.Helper()
	if err := s.InsertMemory(id, "observation", strings.Join(chunks, " "), "text", "test", "", "", 1); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	if err := s.InsertChunks(id, chunks); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}
}

func TestRunOnceEmbedsInBatches(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 5; i++ {
		addMemory(t, s, fmt.Sprintf("m%d", i), fmt.Sprintf("chunk %d", i))
	}
	emb := &fakeEmbedder{model: "fake-v1"}
	ix, err := New(s, emb, 2)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	n, err := ix.RunOnce(context.Background())
	if err != nil || n != 5 {
		t.Fatalf("RunOnce = %d, %v; want 5 chunks", n, err)
	}
	if emb.calls() != 3 {
		t.Errorf("provider called %d times, want 3 batches of up to 2", emb.calls())
	}
	if st, _ := ix.Status(); st != (sqlite.EmbeddingStatus{Chunks: 5, Embedded: 5}) {
		t.Errorf("status = %+v", st)
	}
	if results, _ := s.SearchVector([]float32{7, 1, 0}, 10); len(results) != 5 {
		t.Errorf("search found %d memories, want 5", len(results))
	}
	// Nothing left: no more provider calls
	if n, _ := ix.RunOnce(context.Background()); n != 0 || emb.calls() != 3 {
		t.Errorf("second RunOnce embedded %d chunks with %d calls", n, emb.calls())
	}
}

func TestFailuresAreRetriedLater(t *testing.T) {
	s := newStore(t)
	addMemory(t, s, "m1", "um trecho", "outro trecho")
	emb := &fakeEmbedder{model: "fake-v1", fail: errors.New("503 service unavailable")}
	ix, err := New(s, emb, 32)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	now := time.Unix(1_800_000_000, 0)
	ix.now = func() time.Time { return now }

	if _, err := ix.RunOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("RunOnce error = %v, want the provider's error", err)
	}
	// Before the backoff expires the chunks aren't tried again
	emb.fail = nil
	if n, _ := ix.RunOnce(context.Background()); n != 0 || emb.calls() != 1 {
		t.Errorf("retried before the backoff: %d chunks, %d calls", n, emb.calls())
	}
	// After it, they are, and succeed
	now = now.Add(time.Minute)
	if n, err := ix.RunOnce(context.Background()); n != 2 || err != nil {
		t.Errorf("retry after backoff = %d, %v; want 2 chunks", n, err)
	}

	// A provider returning the wrong number of vectors counts as a failure
	addMemory(t, s, "m2", "mais um")
	emb.short = true
	if _, err := ix.RunOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "0 vectors for 1 texts") {
		t.Errorf("RunOnce with a short response = %v", err)
	}
}

func TestRunWakesUpOnNotify(t *testing.T) {
	s := newStore(t)
	emb := &fakeEmbedder{model: "fake-v1"}
	ix, err := New(s, emb, 32)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ix.idle = time.Hour // only Notify can wake it up in time

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { ix.Run(ctx); close(done) }()

	addMemory(t, s, "m1", "anotação nova")
	ix.Notify()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st, _ := ix.Status(); st.Embedded == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the indexer didn't embed the new chunk after Notify")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop after cancel")
	}
}

func TestModelChangeReembeds(t *testing.T) {
	s := newStore(t)
	addMemory(t, s, "m1", "trecho")
	ix, _ := New(s, &fakeEmbedder{model: "fake-v1"}, 32)
	ix.RunOnce(context.Background())

	emb2 := &fakeEmbedder{model: "fake-v2"}
	ix2, err := New(s, emb2, 32)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if st, _ := ix2.Status(); st.Embedded != 0 || st.Pending != 1 {
		t.Errorf("status with a new model = %+v, want the chunk pending again", st)
	}
	if n, _ := ix2.RunOnce(context.Background()); n != 1 || emb2.calls() != 1 {
		t.Errorf("new model embedded %d chunks", n)
	}
}

func TestModelNameIsRequired(t *testing.T) {
	if _, err := New(newStore(t), &fakeEmbedder{}, 32); err == nil {
		t.Error("New accepted a provider without a model name")
	}
}
