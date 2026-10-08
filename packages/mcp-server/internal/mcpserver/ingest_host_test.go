package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

func call(t *testing.T, name string, f func(context.Context, map[string]interface{}) (*ToolResult, error), args map[string]interface{}) map[string]interface{} {
	t.Helper()
	res, err := f(context.Background(), args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("%s: result is not JSON: %s", name, res.Content[0].Text)
	}
	return out
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestHostExtraction runs the pitch's scenario: the head talks to Bernardo,
// who gives feedback about his manager Sérgio. The host extracts the
// entities; the server stores them and recall/team context return them.
func TestHostExtraction(t *testing.T) {
	srv := newTestServer(t)

	stored := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality":     "audio",
		"content":      "Conversei com o Bernardo. Ele disse que o Sérgio anda microgerenciando as tarefas e não dá feedback construtivo, mas elogiou a disponibilidade dele. Preciso trabalhar isso com o Sérgio na próxima 1:1.",
		"file_path":    "/tmp/nota-de-voz.m4a",
		"about_person": "Sérgio",
		"extraction": &providers.EntityExtraction{
			MemoryType:   "feedback",
			Summary:      "Bernardo relatou microgestão do Sérgio",
			Persons:      []providers.ExtractedPerson{{Name: "Bernardo", Role: "Dev"}, {Name: "Sérgio", Role: "Tech Lead"}, {Name: "Ana"}},
			Topics:       []string{"Microgestão", "feedback construtivo"},
			FeedbackFrom: "Bernardo",
			Relationships: []providers.ExtractedRel{
				{From: "Bernardo", To: "Sérgio", Type: "reports to"},
				{From: "Sérgio", To: "Ana", Type: "REPORTS_TO"},
			},
			Tasks: []providers.ExtractedTask{
				{Description: "Trabalhar feedback construtivo e delegação com o Sérgio", Owner: "Ana"},
			},
			FeedbackItems: []providers.ExtractedFeedbackItem{
				{Category: "Stop", Content: "Microgerenciar as tarefas técnicas do time"},
				{Category: "start", Content: "Dar feedback construtivo"},
				{Category: "continue", Content: "Manter a disponibilidade"},
			},
		},
	})
	if stored["about_person"] != "Sérgio" || stored["memory_type"] != "feedback" ||
		stored["tasks"] != float64(1) || stored["feedback_items"] != float64(3) {
		t.Errorf("ingest result = %s", toJSON(stored))
	}
	if got := toJSON(stored["persons"]); got != `[{"name":"Sérgio","status":"created"},{"name":"Bernardo","status":"created"},{"name":"Ana","status":"created"}]` {
		t.Errorf("persons = %s", got)
	}

	// A second memory names Sérgio without the accent: he must be reused
	stored = call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality":     "text",
		"content":      "Na daily o Sergio refez a tarefa do Bernardo de novo.",
		"about_person": "sergio",
		"extraction": &providers.EntityExtraction{
			Topics:  []string{"microgestao"},
			Persons: []providers.ExtractedPerson{{Name: "bernardo"}},
		},
	})
	if got := toJSON(stored["persons"]); got != `[{"name":"Sérgio","status":"existing"},{"name":"Bernardo","status":"existing"}]` {
		t.Errorf("second ingest persons = %s", got)
	}

	people := call(t, "list_people", srv.HandleListPeople, nil)
	if people["count"] != float64(3) {
		t.Errorf("list_people = %s, want 3 people", toJSON(people))
	}

	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "SERGIO", "context": "1:1"})
	checks := map[string]string{
		"person":        `"Sérgio"`,
		"managers":      `["Ana"]`,
		"reports":       `["Bernardo"]`,
		"pending_tasks": `[{"created_at":`,
		"topics":        `[{"mentions":2,"topic":`,
		"feedbacks":     `"from":"Bernardo","items":[{"category":"stop","content":"Microgerenciar`,
	}
	for key, want := range checks {
		if got := toJSON(brief[key]); !strings.Contains(got, want) {
			t.Errorf("recall %s = %s, want it to contain %s", key, got, want)
		}
	}
	if got := toJSON(brief["pending_tasks"]); !strings.Contains(got, `"owner":"Ana"`) {
		t.Errorf("pending task owner = %s, want Ana", got)
	}
	if memories, _ := brief["memories"].([]interface{}); len(memories) != 2 {
		t.Errorf("recall memories = %d, want 2", len(memories))
	}

	team := call(t, "get_team_context", srv.HandleGetTeamContext, map[string]interface{}{"leader_name": "Ana"})
	members, _ := team["members"].([]interface{})
	if len(members) != 2 {
		t.Fatalf("team context = %s, want 2 members", toJSON(team))
	}
	for i, want := range []struct {
		name, role string
		depth      float64
	}{{"Sérgio", "Tech Lead", 1}, {"Bernardo", "Dev", 2}} {
		m := members[i].(map[string]interface{})
		if m["name"] != want.name || m["role"] != want.role || m["depth"] != want.depth {
			t.Errorf("team member %d = %s, want %s (%s) at depth %v", i, toJSON(m), want.name, want.role, want.depth)
		}
	}
}

// TestIngestValidation checks that invalid calls are rejected with an error
// the host can act on, and that nothing is stored
func TestIngestValidation(t *testing.T) {
	srv := newTestServer(t)
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"media without text", map[string]interface{}{"modality": "audio", "file_path": "/tmp/a.m4a"},
			"transcribe or describe the file"},
		{"unknown modality", map[string]interface{}{"modality": "pdf", "content": "x"},
			"valid: text, audio, image, video"},
		{"unknown memory type", map[string]interface{}{"content": "x", "extraction": &providers.EntityExtraction{MemoryType: "gossip"}},
			"invalid memory_type"},
		{"unknown feedback category", map[string]interface{}{"content": "x", "about_person": "Ana", "extraction": &providers.EntityExtraction{
			FeedbackItems: []providers.ExtractedFeedbackItem{{Category: "keep", Content: "y"}}}},
			"valid: stop, start, continue"},
		{"feedback about nobody", map[string]interface{}{"content": "x", "extraction": &providers.EntityExtraction{
			FeedbackItems: []providers.ExtractedFeedbackItem{{Category: "stop", Content: "y"}}}},
			"about_person is required"},
		{"unknown relationship", map[string]interface{}{"content": "x", "extraction": &providers.EntityExtraction{
			Relationships: []providers.ExtractedRel{{From: "A", To: "B", Type: "LIKES"}}}},
			"valid: REPORTS_TO, MENTORS, WORKS_WITH"},
		{"task without description", map[string]interface{}{"content": "x", "extraction": &providers.EntityExtraction{
			Tasks: []providers.ExtractedTask{{Owner: "Ana"}}}},
			"description is required"},
	}
	for _, c := range cases {
		_, err := srv.HandleIngest(context.Background(), c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", c.name, err, c.want)
		}
	}
	var memories int
	if err := srv.store.DB().QueryRow(`SELECT count(*) FROM memories`).Scan(&memories); err != nil || memories != 0 {
		t.Errorf("memories after rejected ingests = %d (err %v), want 0", memories, err)
	}
}
