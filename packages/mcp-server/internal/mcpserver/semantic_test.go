package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers/embedding"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// filler returns n words of small talk, with none of the test keywords
func filler(n int) string {
	return strings.TrimSpace(strings.Repeat("e aí a gente conversou sobre o dia a dia ", n/10))
}

// TestLongTranscriptSearch ingests a long 1:1 transcript split by subject by
// the host, and checks semantic search finds the right passage
func TestLongTranscriptSearch(t *testing.T) {
	srv := newTestServer(t)

	segments := []string{
		"Abrimos a 1:1 falando das entregas da sprint. " + filler(120),
		"Sobre delegação: ele ainda centraliza as decisões e não delega as revisões de código. " + filler(120),
		"Sobre férias: quer tirar férias em dezembro e vai organizar a cobertura. " + filler(120),
	}
	result := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality":     "audio",
		"content":      strings.Join(segments, "\n\n"),
		"about_person": "Sérgio",
		"segments":     segments,
	})
	if result["chunks"] != float64(3) || result["semantic_index"] != "queued" {
		t.Errorf("ingest result = %s, want 3 chunks queued", toJSON(result))
	}

	indexNow(t, srv)
	found := call(t, "search_memories", srv.HandleSearchMemories, map[string]interface{}{
		"query": "como anda a delegação dele?", "semantic": true,
	})
	results, _ := found["results"].([]interface{})
	if len(results) != 1 {
		t.Fatalf("search = %s, want the transcript once", toJSON(found))
	}
	excerpt, _ := results[0].(map[string]interface{})["excerpt"].(string)
	if !strings.HasPrefix(excerpt, "Sobre delegação") {
		t.Errorf("excerpt = %.60q..., want the delegation passage", excerpt)
	}
}

// TestUnsegmentedTranscriptIsSplit checks a long text without segments is
// split by size, so each subject still gets its own passage
func TestUnsegmentedTranscriptIsSplit(t *testing.T) {
	srv := newTestServer(t)
	content := filler(300) + " ele pediu feedback sobre a apresentação. " + filler(300)
	result := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "audio", "content": content, "about_person": "Evandro",
	})
	if n, _ := result["chunks"].(float64); n < 3 {
		t.Errorf("chunks = %v, want the text split in several passages", result["chunks"])
	}
	indexNow(t, srv)
	found := call(t, "search_memories", srv.HandleSearchMemories, map[string]interface{}{
		"query": "feedback", "semantic": true,
	})
	results, _ := found["results"].([]interface{})
	if len(results) != 1 || !strings.Contains(results[0].(map[string]interface{})["excerpt"].(string), "pediu feedback") {
		t.Errorf("search = %s, want the passage mentioning feedback", toJSON(found))
	}
}

// TestSemanticSearchOverHTTP runs ingest → indexer → OpenAI-compatible
// provider → semantic search against a fake embeddings API
func TestSemanticSearchOverHTTP(t *testing.T) {
	var mu sync.Mutex // the background indexer and indexNow may call concurrently
	var inputs []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		inputs = append(inputs, req.Input...)
		mu.Unlock()
		vectors, _ := keywordEmbedding{}.EmbedBatch(req.Input)
		data := make([]map[string]interface{}, len(vectors))
		for i, v := range vectors {
			data[i] = map[string]interface{}{"index": i, "embedding": v}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	defer api.Close()

	cfg := &config.Config{Graph: config.GraphConfig{Engine: "sqlite"}}
	cfg.Storage.SQLite.Path = t.TempDir() + "/http.db"
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer srv.Close()
	provider, err := embedding.NewOpenAI(embedding.OpenAIConfig{
		BaseURL: api.URL + "/v1", Model: "multilingual-e5-small",
		QueryPrefix: "query: ", DocumentPrefix: "passage: ",
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if err := srv.EnableSemanticSearch(provider); err != nil {
		t.Fatalf("enable: %v", err)
	}

	for _, content := range []string{
		"O Sérgio não delega as revisões de código.",
		"O Evandro vai tirar férias em dezembro.",
	} {
		call(t, "ingest", srv.HandleIngest, map[string]interface{}{"modality": "text", "content": content})
	}
	indexNow(t, srv)

	found := call(t, "search_memories", srv.HandleSearchMemories, map[string]interface{}{
		"query": "férias do time", "semantic": true, "limit": float64(1),
	})
	results, _ := found["results"].([]interface{})
	if len(results) != 1 || !strings.Contains(toJSON(results[0]), "dezembro") {
		t.Errorf("search = %s, want the vacation memory", toJSON(found))
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(inputs, "|"), "passage: O Sérgio") || inputs[len(inputs)-1] != "query: férias do time" {
		t.Errorf("inputs sent = %q, want passages then the prefixed query", inputs)
	}
}

// TestEmbeddingFromConfig checks New enables semantic search from the config,
// and refuses to start with an invalid provider
func TestEmbeddingFromConfig(t *testing.T) {
	cfg := &config.Config{Graph: config.GraphConfig{Engine: "sqlite"}}
	cfg.Storage.SQLite.Path = t.TempDir() + "/cfg.db"
	cfg.Providers.Embedding = config.EmbeddingConfig{
		Provider: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1", Model: "text-embedding-3-small",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if srv.indexer == nil || srv.indexer.Model() != "text-embedding-3-small" {
		t.Errorf("semantic search not enabled from the config")
	}
	srv.Close()

	cfg.Storage.SQLite.Path = t.TempDir() + "/bad.db"
	cfg.Providers.Embedding = config.EmbeddingConfig{Provider: "local:sentence_transformers"}
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "unknown embedding provider") {
		t.Errorf("New with an unknown provider = %v", err)
	}
}

// TestSemanticSearchDisabled checks the server works without an embedding
// provider and says so when semantic search is asked for
func TestSemanticSearchDisabled(t *testing.T) {
	cfg := &config.Config{Graph: config.GraphConfig{Engine: "sqlite"}}
	cfg.Storage.SQLite.Path = t.TempDir() + "/plain.db"
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer srv.Close()

	result := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Evandro resolveu um bug de TEF sozinho.", "about_person": "Evandro",
	})
	if !strings.HasPrefix(result["semantic_index"].(string), "disabled") {
		t.Errorf("semantic_index = %v, want disabled", result["semantic_index"])
	}
	_, err = srv.HandleSearchMemories(context.Background(), map[string]interface{}{"query": "bug", "semantic": true})
	if err == nil || !strings.Contains(err.Error(), "semantic search is disabled") {
		t.Errorf("semantic search error = %v", err)
	}
	found := call(t, "search_memories", srv.HandleSearchMemories, map[string]interface{}{"query": "TEF"})
	if found["count"] != float64(1) {
		t.Errorf("keyword search = %s, want 1 result", toJSON(found))
	}
}

// TestOldMemoriesAreBackfilled checks memories stored before chunking
// existed get chunks (and so embeddings) when the server starts
func TestOldMemoriesAreBackfilled(t *testing.T) {
	dbPath := t.TempDir() + "/old.db"
	st, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.InsertMemory("m_old", "observation", "Ele não delega as revisões.", "text", "mcp", "", "Sérgio", 1); err != nil {
		t.Fatalf("insert: %v", err)
	}
	st.Close()

	cfg := &config.Config{Graph: config.GraphConfig{Engine: "sqlite"}}
	cfg.Storage.SQLite.Path = dbPath
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer srv.Close()
	if err := srv.EnableSemanticSearch(keywordEmbedding{}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	indexNow(t, srv)
	if results, _ := srv.store.SearchVector([]float32{1, 0, 0, 0, 0}, 5); len(results) != 1 || results[0]["memory_id"] != "m_old" {
		t.Errorf("search after backfill = %v, want m_old", results)
	}
}
