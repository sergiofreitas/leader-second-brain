package mcpserver

import (
	"testing"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// freezeClock makes generateID see the same instant on every call, as on
// Windows when several IDs are generated within one clock tick
func freezeClock(t *testing.T) {
	t.Helper()
	frozen := time.Now()
	now = func() time.Time { return frozen }
	t.Cleanup(func() { now = time.Now })
}

func TestGenerateIDIsUniqueWithinOneClockTick(t *testing.T) {
	freezeClock(t)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := generateID("person")
		if seen[id] {
			t.Fatalf("generateID repeated %q after %d calls", id, i)
		}
		seen[id] = true
	}
}

// TestFeedbackItemsKeepTheirOrder checks recall lists feedback items in the
// order they were given, even when their IDs share a timestamp
func TestFeedbackItemsKeepTheirOrder(t *testing.T) {
	freezeClock(t)
	srv := newTestServer(t)
	items := []providers.ExtractedFeedbackItem{
		{Category: "stop", Content: "Parar 1"}, {Category: "start", Content: "Começar 1"},
		{Category: "start", Content: "Começar 2"}, {Category: "continue", Content: "Continuar 1"},
		{Category: "continue", Content: "Continuar 2"},
	}
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Feedback da Ana.", "about_person": "Ana",
		"extraction": &providers.EntityExtraction{MemoryType: "feedback", FeedbackItems: items},
	})
	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Ana", "context": "feedback"})
	feedbacks, _ := brief["feedbacks"].([]interface{})
	if len(feedbacks) != 1 {
		t.Fatalf("feedbacks = %s", toJSON(brief["feedbacks"]))
	}
	got := feedbacks[0].(map[string]interface{})["items"].([]interface{})
	for i, item := range got {
		if content := item.(map[string]interface{})["content"]; content != items[i].Content {
			t.Errorf("item %d = %v, want %q (items in the order given)", i, content, items[i].Content)
		}
	}
}

// TestIngestCreatesEveryNewPerson ingests a team structure naming several
// new people at once: each of them must be stored, none replacing another
func TestIngestCreatesEveryNewPerson(t *testing.T) {
	freezeClock(t)
	srv := newTestServer(t)

	team := []string{"Leandro", "Roberto", "Bernardo", "Evandro", "Natiele"}
	extraction := &providers.EntityExtraction{
		Persons:       []providers.ExtractedPerson{{Name: "Sérgio", Role: "Tech Lead"}, {Name: "Marcelo", Role: "Head"}},
		Relationships: []providers.ExtractedRel{{From: "Sérgio", To: "Marcelo", Type: "REPORTS_TO"}},
	}
	for _, name := range team {
		extraction.Persons = append(extraction.Persons, providers.ExtractedPerson{Name: name, Role: "Dev"})
		extraction.Relationships = append(extraction.Relationships, providers.ExtractedRel{From: name, To: "Sérgio", Type: "REPORTS_TO"})
	}
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality":     "text",
		"content":      "Sérgio é tech lead, reporta ao Marcelo e lidera Leandro, Roberto, Bernardo, Evandro e Natiele.",
		"about_person": "Sérgio",
		"extraction":   extraction,
	})

	people := call(t, "list_people", srv.HandleListPeople, nil)
	if people["count"] != float64(len(team)+2) {
		t.Errorf("list_people = %s, want %d people", toJSON(people), len(team)+2)
	}
	ctx := call(t, "get_team_context", srv.HandleGetTeamContext, map[string]interface{}{"leader_name": "Marcelo"})
	if ctx["team_size"] != float64(len(team)+1) {
		t.Errorf("Marcelo's team = %s, want %d people", toJSON(ctx), len(team)+1)
	}
}
