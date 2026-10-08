package mcpserver

import (
	"context"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// TestRenamePerson renames people known by their first name and checks that
// the new name reaches everywhere a name is stored
func TestRenamePerson(t *testing.T) {
	srv := newTestServer(t)
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality":     "text",
		"content":      "O Osmar me contou que a Bruna elogiou o Bernardo como padrinho dela na sustentação.",
		"about_person": "Bernardo",
		"extraction": &providers.EntityExtraction{
			MemoryType:    "feedback",
			Persons:       []providers.ExtractedPerson{{Name: "Osmar", Role: "Tech Lead"}, {Name: "Bruna"}},
			Relationships: []providers.ExtractedRel{{From: "Bruna", To: "Osmar", Type: "REPORTS_TO"}},
			FeedbackFrom:  "Bruna",
			FeedbackItems: []providers.ExtractedFeedbackItem{{Category: "continue", Content: "Apoiar a Bruna na sustentação"}},
			Tasks:         []providers.ExtractedTask{{Description: "Agradecer o Bernardo pelo apadrinhamento", Owner: "Osmar"}},
		},
	})
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality":     "text",
		"content":      "O Osmar é tech lead de operações cross.",
		"about_person": "Osmar",
	})

	renamed := call(t, "rename_person", srv.HandleRenamePerson, map[string]interface{}{
		"name": "osmar", "new_name": "Osmar de Morais Junior",
	})
	if got := toJSON(renamed); got != `{"feedbacks":0,"from":"Osmar","memories":1,"status":"renamed","tasks":1,"to":"Osmar de Morais Junior"}` {
		t.Errorf("rename Osmar = %s", got)
	}
	renamed = call(t, "rename_person", srv.HandleRenamePerson, map[string]interface{}{
		"name": "Bruna", "new_name": "Bruna Guimaraes",
	})
	if renamed["feedbacks"] != float64(1) {
		t.Errorf("rename Bruna = %s, want 1 feedback", toJSON(renamed))
	}
	// Changing only the accents of one's own name is allowed
	call(t, "rename_person", srv.HandleRenamePerson, map[string]interface{}{
		"name": "Bruna Guimaraes", "new_name": "Bruna Guimarães",
	})

	people := call(t, "list_people", srv.HandleListPeople, nil)
	if got := toJSON(people["people"]); got != `[{"name":"Bernardo"},{"name":"Bruna Guimarães"},{"name":"Osmar de Morais Junior","role":"Tech Lead"}]` {
		t.Errorf("people = %s", got)
	}

	db := srv.store.DB()
	var about int
	db.QueryRow(`SELECT count(*) FROM memories WHERE about_person = 'Osmar de Morais Junior'`).Scan(&about)
	if about != 1 {
		t.Errorf("memories about Osmar de Morais Junior = %d, want 1", about)
	}
	// The FTS index follows: "Morais" is only in the new about_person
	if results, err := srv.store.SearchFTS("Morais", nil, 10); err != nil || len(results) != 1 {
		t.Errorf("FTS for the new name = %v (err %v), want 1 memory", results, err)
	}
	var owner, giver string
	db.QueryRow(`SELECT json_extract(props, '$.owner') FROM graph_nodes WHERE label = 'Task'`).Scan(&owner)
	db.QueryRow(`SELECT json_extract(props, '$.from') FROM graph_nodes WHERE label = 'Feedback'`).Scan(&giver)
	if owner != "Osmar de Morais Junior" || giver != "Bruna Guimarães" {
		t.Errorf("task owner = %q, feedback from = %q", owner, giver)
	}

	// Relationships are kept, and the new name is reused by later ingests
	team := call(t, "get_team_context", srv.HandleGetTeamContext, map[string]interface{}{"leader_name": "Osmar de Morais Junior"})
	if members, _ := team["members"].([]interface{}); len(members) != 1 ||
		members[0].(map[string]interface{})["name"] != "Bruna Guimarães" {
		t.Errorf("Osmar's team = %s, want Bruna Guimarães", toJSON(team))
	}
	stored := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Bruna fechou o primeiro chamado sozinha.", "about_person": "bruna guimaraes",
	})
	if got := toJSON(stored["persons"]); got != `[{"name":"Bruna Guimarães","status":"existing"}]` {
		t.Errorf("ingest after rename: persons = %s", got)
	}
}

func TestRenamePersonRefuses(t *testing.T) {
	srv := newTestServer(t)
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Ana e Bia estão no mesmo time.", "about_person": "Ana",
		"extraction": &providers.EntityExtraction{Persons: []providers.ExtractedPerson{{Name: "Bia"}}},
	})

	for _, tc := range []struct{ name, newName string }{
		{"Bia", "ana"},     // another person's name
		{"Carla", "Clara"}, // unknown person
		{"Bia", " "},       // no new name
	} {
		args := map[string]interface{}{"name": tc.name, "new_name": tc.newName}
		if _, err := srv.HandleRenamePerson(context.Background(), args); err == nil {
			t.Errorf("rename %q to %q succeeded, want an error", tc.name, tc.newName)
		}
	}
	people := call(t, "list_people", srv.HandleListPeople, nil)
	if got := toJSON(people["people"]); got != `[{"name":"Ana"},{"name":"Bia"}]` {
		t.Errorf("people after refused renames = %s", got)
	}
}
