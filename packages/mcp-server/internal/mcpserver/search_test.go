package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
)

func TestFuse(t *testing.T) {
	r := func(id string, extra ...string) map[string]interface{} {
		m := map[string]interface{}{"memory_id": id, "type": "observation"}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	keyword := []map[string]interface{}{r("k1", "snippet", "…k1…"), r("both", "snippet", "…both…"), r("k3")}
	semantic := []map[string]interface{}{r("s1", "excerpt", "s1 passage"), r("both", "excerpt", "both passage")}

	got := fuse(keyword, semantic, 10)
	var order []string
	for _, x := range got {
		order = append(order, x["memory_id"].(string))
	}
	// "both" is second in each list: 2/(60+2) beats 1/(60+1)
	if fmt.Sprint(order) != "[both k1 s1 k3]" {
		t.Errorf("order = %v, want [both k1 s1 k3]", order)
	}
	both := got[0]
	if both["excerpt"] != "both passage" || both["snippet"] != "…both…" || fmt.Sprint(both["matched_by"]) != "[keyword semantic]" {
		t.Errorf("fused result = %v", both)
	}
	if len(fuse(keyword, semantic, 2)) != 2 {
		t.Error("limit not applied")
	}
	if len(fuse(nil, nil, 5)) != 0 {
		t.Error("empty inputs should give no results")
	}
}

// flakyQueryEmbedding embeds passages normally but fails to embed queries,
// like a gateway that went down after indexing
type flakyQueryEmbedding struct{ keywordEmbedding }

func (flakyQueryEmbedding) Embed(string) ([]float32, error) {
	return nil, errors.New("gateway unavailable")
}

func TestSearchModes(t *testing.T) {
	srv := newTestServer(t) // semantic search enabled (keyword embedding)
	for _, content := range []string{
		"O Sérgio anda microgerenciando as tarefas técnicas do time.",
		"Ele ainda não delega as revisões de código.",
		"O Evandro vai tirar férias em dezembro.",
	} {
		call(t, "ingest", srv.HandleIngest, map[string]interface{}{"modality": "text", "content": content, "about_person": "Sérgio"})
	}
	indexNow(t, srv)

	// Default is hybrid when semantic search is enabled
	found := call(t, "search", srv.HandleSearchMemories, map[string]interface{}{
		"query": "microgestão", "terms": []string{"microger*", "não delega"},
	})
	if found["mode"] != "hybrid" {
		t.Fatalf("mode = %v, want hybrid", found["mode"])
	}
	// Semantic search always returns the nearest memories, even unrelated
	// ones; the memories also found by keyword come first, the unrelated one
	// (found by meaning only) last
	results := found["results"].([]interface{})
	if len(results) != 3 {
		t.Fatalf("hybrid search = %s, want 3 results", toJSON(found))
	}
	for i, r := range results {
		matchedBy := toJSON(r.(map[string]interface{})["matched_by"])
		wantBoth := i < 2
		if gotBoth := strings.Contains(matchedBy, "keyword"); gotBoth != wantBoth {
			t.Errorf("result %d matched_by %s; want the keyword matches ranked first", i, matchedBy)
		}
	}
	if last := toJSON(results[2]); !strings.Contains(last, "férias") {
		t.Errorf("last result = %s, want the unrelated memory", last)
	}

	// keyword only: without terms the inflected word isn't found
	found = call(t, "search", srv.HandleSearchMemories, map[string]interface{}{"query": "microgestão", "mode": "keyword"})
	if found["count"] != float64(0) {
		t.Errorf("keyword search without terms = %s, want nothing", toJSON(found))
	}

	if _, err := srv.HandleSearchMemories(context.Background(), map[string]interface{}{"query": "x", "mode": "fuzzy"}); err == nil ||
		!strings.Contains(err.Error(), "valid: hybrid, keyword, semantic") {
		t.Errorf("invalid mode error = %v", err)
	}
	if _, err := srv.HandleSearchMemories(context.Background(), map[string]interface{}{"query": " "}); err == nil {
		t.Error("empty query accepted")
	}

	// When the provider fails, hybrid degrades to keyword and says why
	srv.embedding = flakyQueryEmbedding{}
	found = call(t, "search", srv.HandleSearchMemories, map[string]interface{}{
		"query": "férias", "terms": []string{"feria*"},
	})
	if found["count"] != float64(1) || !strings.Contains(toJSON(found["semantic_error"]), "gateway unavailable") {
		t.Errorf("degraded hybrid search = %s, want the keyword result and the semantic error", toJSON(found))
	}
	if _, err := srv.HandleSearchMemories(context.Background(), map[string]interface{}{"query": "férias", "mode": "semantic"}); err == nil {
		t.Error("semantic mode hid the provider error")
	}
}

func TestSearchDefaultsToKeywordWithoutEmbedding(t *testing.T) {
	cfg := &config.Config{Graph: config.GraphConfig{Engine: "sqlite"}}
	cfg.Storage.SQLite.Path = t.TempDir() + "/kw.db"
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer srv.Close()
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{"modality": "text", "content": "Na 1:1 falamos do PDI."})

	found := call(t, "search", srv.HandleSearchMemories, map[string]interface{}{"query": "1:1"})
	if found["mode"] != "keyword" || found["count"] != float64(1) {
		t.Errorf("search = %s, want keyword mode with 1 result", toJSON(found))
	}
	if _, ok := found["index_status"]; ok {
		t.Error("index_status without semantic search")
	}
}
