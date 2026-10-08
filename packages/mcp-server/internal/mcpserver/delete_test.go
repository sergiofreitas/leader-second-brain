package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// TestDeleteMemory deletes a feedback recorded about the wrong person, in two
// steps (preview, then confirmed), and checks what goes and what stays
func TestDeleteMemory(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	wrong := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "O Bernardo anda microgerenciando o time.", "about_person": "Ana",
		"extraction": &providers.EntityExtraction{
			MemoryType: "feedback", Summary: "Microgestão (pessoa errada)", Topics: []string{"microgestão"},
			Persons:       []providers.ExtractedPerson{{Name: "Bernardo"}},
			Relationships: []providers.ExtractedRel{{From: "Ana", To: "Bernardo", Type: "REPORTS_TO"}},
			FeedbackItems: []providers.ExtractedFeedbackItem{{Category: "stop", Content: "Microgerenciar"}, {Category: "start", Content: "Delegar"}},
			Tasks:         []providers.ExtractedTask{{Description: "Conversar sobre microgestão", Owner: "Bernardo"}},
		},
	})
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "A Ana tirou férias e voltou animada.", "about_person": "Ana",
		"extraction": &providers.EntityExtraction{Summary: "Férias", Topics: []string{"microgestão"}},
	})
	indexNow(t, srv)
	wrongID := wrong["memory_id"].(string)

	// recall lists memory ids, for delete_memory
	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Ana"})
	var ids []string
	for _, m := range brief["memories"].([]interface{}) {
		ids = append(ids, m.(map[string]interface{})["memory_id"].(string))
	}
	if len(ids) != 2 || (ids[0] != wrongID && ids[1] != wrongID) {
		t.Fatalf("recall memory ids = %v, want %s among them", ids, wrongID)
	}

	count := func(query string) (n int) {
		srv.store.DB().QueryRow(query).Scan(&n)
		return n
	}
	nodes := func() int { return count(`SELECT count(*) FROM graph_nodes`) }
	before := nodes()

	// Without confirm: a preview, nothing deleted
	preview := call(t, "delete_memory", srv.HandleDeleteMemory, map[string]interface{}{"memory_id": wrongID})
	if preview["status"] != "preview" || preview["summary"] != "Microgestão (pessoa errada)" || preview["about_person"] != "Ana" ||
		preview["feedback_items"] != float64(2) || toJSON(preview["tasks"]) != `["Conversar sobre microgestão"]` {
		t.Errorf("preview = %s", toJSON(preview))
	}
	if nodes() != before || count(`SELECT count(*) FROM memories`) != 2 {
		t.Fatalf("the preview deleted something")
	}

	deleted := call(t, "delete_memory", srv.HandleDeleteMemory, map[string]interface{}{"memory_id": wrongID, "confirm": true})
	if deleted["status"] != "deleted" {
		t.Errorf("delete = %s", toJSON(deleted))
	}
	// Gone: the memory, its feedback and 2 items, its task (node and row)
	if got := before - nodes(); got != 5 {
		t.Errorf("%d nodes deleted, want 5 (memory, feedback, 2 items, task)", got)
	}
	if n := count(`SELECT count(*) FROM graph_nodes WHERE label IN ('Feedback', 'FeedbackItem', 'Task')`); n != 0 {
		t.Errorf("%d feedback/item/task nodes left", n)
	}
	if n := count(`SELECT count(*) FROM tasks`); n != 0 {
		t.Errorf("%d task rows left", n)
	}
	if n := count(`SELECT count(*) FROM graph_edges WHERE from_id = '` + wrongID + `' OR to_id = '` + wrongID + `'`); n != 0 {
		t.Errorf("%d edges of the deleted memory left", n)
	}
	// Not found by keyword or by meaning
	found := call(t, "search_memories", srv.HandleSearchMemories, map[string]interface{}{"query": "microgerenciando", "mode": "keyword"})
	if found["count"] != float64(0) {
		t.Errorf("keyword search finds the deleted memory: %s", toJSON(found["results"]))
	}
	found = call(t, "search_memories", srv.HandleSearchMemories, map[string]interface{}{"query": "microgestão", "mode": "semantic"})
	for _, r := range found["results"].([]interface{}) {
		if r.(map[string]interface{})["memory_id"] == wrongID {
			t.Errorf("semantic search finds the deleted memory")
		}
	}

	// Kept: the people, their relationship, the topic, the other memory
	brief = call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Ana"})
	if got := toJSON(brief["managers"]); got != `["Bernardo"]` {
		t.Errorf("Ana's managers = %s, want the relationship kept", got)
	}
	if memories := brief["memories"].([]interface{}); len(memories) != 1 || memories[0].(map[string]interface{})["summary"] != "Férias" {
		t.Errorf("Ana's memories = %s", toJSON(memories))
	}
	if brief["feedbacks"] != nil || brief["pending_tasks"] != nil {
		t.Errorf("feedbacks = %s, pending_tasks = %s", toJSON(brief["feedbacks"]), toJSON(brief["pending_tasks"]))
	}
	if got := toJSON(brief["topics"]); got != `[{"mentions":1,"topic":"microgestão"}]` {
		t.Errorf("topics = %s", got)
	}

	for _, args := range []map[string]interface{}{
		{"memory_id": wrongID, "confirm": true}, // already deleted
		{"memory_id": "mem_nope"},
		{"memory_id": " "},
	} {
		if _, err := srv.HandleDeleteMemory(ctx, args); err == nil || !strings.Contains(err.Error(), "memory") {
			t.Errorf("delete_memory(%v) error = %v", args, err)
		}
	}
}
