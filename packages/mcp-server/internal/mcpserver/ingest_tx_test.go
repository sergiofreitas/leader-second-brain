package mcpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/graph"
)

// failingGraph fails AddEdge for one edge label, to break an ingest midway
type failingGraph struct {
	graph.GraphEngine
	failLabel string
}

func (f failingGraph) WithTx(tx *sql.Tx) graph.GraphEngine {
	return failingGraph{f.GraphEngine.WithTx(tx), f.failLabel}
}

func (f failingGraph) AddEdge(from, to, label string, props map[string]interface{}) error {
	if label == f.failLabel {
		return errors.New("injected failure")
	}
	return f.GraphEngine.AddEdge(from, to, label, props)
}

// fixedEmbedding returns the same non-zero vector for any text
type fixedEmbedding struct{}

func (fixedEmbedding) Embed(string) ([]float32, error) { return []float32{1, 0, 0}, nil }
func (fixedEmbedding) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}
func (fixedEmbedding) Dimensions() int { return 3 }

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{Profile: "test", Graph: config.GraphConfig{Engine: "sqlite"}}
	cfg.Storage.SQLite.Path = t.TempDir() + "/ingest.db"
	cfg.Feedback = config.FeedbackConfig{
		Format: "stop_start_continue",
		Categories: []config.FeedbackCategory{
			{ID: "stop", Label: "Parar"}, {ID: "start", Label: "Começar"}, {ID: "continue", Label: "Continuar"},
		},
		TargetSystem: "markdown",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	srv.embedding = fixedEmbedding{}
	return srv
}

// TestIngestIsAtomic checks that a failure in the last graph write of an
// ingest rolls back the memory, its embedding, the person and the graph
func TestIngestIsAtomic(t *testing.T) {
	srv := newTestServer(t)
	realGraph := srv.graph
	srv.graph = failingGraph{realGraph, "ABOUT"}

	args := map[string]interface{}{
		"modality":     "text",
		"content":      "Bernardo comentou que o Sérgio anda microgerenciando o time.",
		"about_person": "Sérgio",
	}
	if _, err := srv.HandleIngest(context.Background(), args); err == nil {
		t.Fatal("ingest succeeded despite the injected failure")
	}

	if _, err := srv.store.GetPersonByName("Sérgio"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("person after failed ingest: err = %v, want ErrNoRows", err)
	}
	if results, err := srv.store.SearchFTS("microgerenciando", 10); err != nil || len(results) != 0 {
		t.Errorf("FTS after failed ingest = %v (err %v), want no memories", results, err)
	}
	if results, _ := srv.store.SearchVector([]float32{1, 0, 0}, 10); len(results) != 0 {
		t.Errorf("vector search after failed ingest = %v, want nothing", results)
	}
	var nodes int
	if err := srv.store.DB().QueryRow(`SELECT count(*) FROM graph_nodes`).Scan(&nodes); err != nil || nodes != 0 {
		t.Errorf("graph nodes after failed ingest = %d (err %v), want 0", nodes, err)
	}

	// The same ingest succeeds once the failure is gone, and stores everything
	srv.graph = realGraph
	if _, err := srv.HandleIngest(context.Background(), args); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	personID, err := srv.store.GetPersonByName("Sérgio")
	if err != nil {
		t.Fatalf("person after ingest: %v", err)
	}
	pc, err := srv.graph.GetPersonContext(personID)
	if err != nil || len(pc.Memories) != 1 {
		t.Errorf("person context after ingest = %+v (err %v), want 1 memory", pc, err)
	}
	if results, _ := srv.store.SearchVector([]float32{1, 0, 0}, 10); len(results) != 1 {
		t.Errorf("vector search after ingest = %v, want 1 memory", results)
	}
}

// TestConcurrentIngests checks that parallel ingests about the same new
// person all succeed and create that person only once
func TestConcurrentIngests(t *testing.T) {
	srv := newTestServer(t)
	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := srv.HandleIngest(context.Background(), map[string]interface{}{
				"modality":     "text",
				"content":      fmt.Sprintf("Observação %d sobre o Evandro", i),
				"about_person": "Evandro",
			})
			errs <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent ingest: %v", err)
		}
	}

	var persons int
	if err := srv.store.DB().QueryRow(`SELECT count(*) FROM persons WHERE name = 'Evandro'`).Scan(&persons); err != nil || persons != 1 {
		t.Errorf("persons named Evandro = %d (err %v), want 1", persons, err)
	}
	personID, _ := srv.store.GetPersonByName("Evandro")
	if pc, err := srv.graph.GetPersonContext(personID); err != nil || len(pc.Memories) != n {
		t.Errorf("memories about Evandro = %d (err %v), want %d", len(pc.Memories), err, n)
	}
}
