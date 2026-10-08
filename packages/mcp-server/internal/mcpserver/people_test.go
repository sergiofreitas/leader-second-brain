package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// textOf returns a tool's plain-text answer (not-found, ambiguous name)
func textOf(t *testing.T, res *ToolResult, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	return res.Content[0].Text
}

func seedTeam(t *testing.T, srv *Server) {
	t.Helper()
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Estrutura do time.", "about_person": "Sergio Freitas",
		"extraction": &providers.EntityExtraction{
			Persons: []providers.ExtractedPerson{
				{Name: "Natiele Bastião da Silva", Role: "QA Pleno"},
				{Name: "Bruna Guimarães"}, {Name: "Bruna Costa"},
			},
			Relationships: []providers.ExtractedRel{
				{From: "Natiele Bastião da Silva", To: "Sergio Freitas", Type: "REPORTS_TO"},
				{From: "Bruna Guimarães", To: "Sergio Freitas", Type: "REPORTS_TO"},
			},
		},
	})
}

// TestFindPersonByPartOfTheName looks people up by first name or part of the
// name, as the leader calls them
func TestFindPersonByPartOfTheName(t *testing.T) {
	srv := newTestServer(t)
	seedTeam(t, srv)
	ctx := context.Background()

	for _, name := range []string{"Natiele", "natiele silva", "NATIELE DA SILVA", "Natiele Bastiao da Silva"} {
		brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": name})
		if brief["person"] != "Natiele Bastião da Silva" {
			t.Errorf("recall(%q) person = %v", name, brief["person"])
		}
	}

	// Connectors (da, de, do...) don't count
	if brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Bruna da Costa"}); brief["person"] != "Bruna Costa" {
		t.Errorf("recall(Bruna da Costa) person = %v", brief["person"])
	}

	// Several matches: the answer lists them, so the host can ask which one
	res, err := srv.HandleRecall(ctx, map[string]interface{}{"person_name": "Bruna"})
	text := textOf(t, res, err)
	if !strings.Contains(text, "matches several people") || !strings.Contains(text, `"Bruna Guimarães"`) || !strings.Contains(text, `"Bruna Costa"`) {
		t.Errorf("recall(Bruna) = %s", text)
	}
	// No match, or a word that isn't in the name
	for _, name := range []string{"Carla", "Natiele Souza", "Nat", "da", " "} {
		res, err := srv.HandleRecall(ctx, map[string]interface{}{"person_name": name})
		if text := textOf(t, res, err); !strings.Contains(text, "not found") {
			t.Errorf("recall(%q) = %s, want not found", name, text)
		}
	}

	team := call(t, "get_team_context", srv.HandleGetTeamContext, map[string]interface{}{"leader_name": "sergio"})
	if team["leader"] != "Sergio Freitas" || team["team_size"] != float64(2) {
		t.Errorf("get_team_context(sergio) = %s", toJSON(team))
	}
	res, err = srv.HandleGetTeamContext(ctx, map[string]interface{}{"leader_name": "Bruna"})
	if text := textOf(t, res, err); !strings.Contains(text, "matches several people") {
		t.Errorf("get_team_context(Bruna) = %s", text)
	}

	renamed := call(t, "rename_person", srv.HandleRenamePerson, map[string]interface{}{"name": "Costa", "new_name": "Bruna Costa Lima"})
	if renamed["from"] != "Bruna Costa" {
		t.Errorf("rename_person(Costa) = %s", toJSON(renamed))
	}
	if _, err := srv.HandleRenamePerson(ctx, map[string]interface{}{"name": "Bruna", "new_name": "Bruna X"}); err == nil || !strings.Contains(err.Error(), "matches several people") {
		t.Errorf("rename_person(Bruna) error = %v", err)
	}
}

// TestIngestRefusesLookalikeNewPerson checks a new name that may be someone
// already stored is refused, until the host confirms it's someone else
func TestIngestRefusesLookalikeNewPerson(t *testing.T) {
	srv := newTestServer(t)
	seedTeam(t, srv)
	ctx := context.Background()
	memories := func() (n int) {
		srv.store.DB().QueryRow(`SELECT count(*) FROM memories`).Scan(&n)
		return n
	}
	before := memories()

	cases := []struct {
		about   string
		persons []providers.ExtractedPerson
		want    string // a stored name the error must suggest
	}{
		{"Natiele", nil, "Natiele Bastião da Silva"},                     // part of a stored name
		{"Bruna Guimarães Souza", nil, "Bruna Guimarães"},                // a stored name, and more
		{"Leo", []providers.ExtractedPerson{{Name: "Leo Souza"}}, "Leo"}, // two new lookalikes in one call
	}
	for _, c := range cases {
		_, err := srv.HandleIngest(ctx, map[string]interface{}{
			"modality": "text", "content": "Conversa com " + c.about + ".", "about_person": c.about,
			"extraction": &providers.EntityExtraction{Summary: "x", Persons: c.persons},
		})
		if err == nil || !strings.Contains(err.Error(), `"`+c.want+`"`) || !strings.Contains(err.Error(), "new_persons") {
			t.Errorf("ingest about %q: err = %v, want it to suggest %q and new_persons", c.about, err, c.want)
		}
	}
	if memories() != before {
		t.Errorf("refused ingests stored memories: %d, want %d", memories(), before)
	}

	// The user confirmed Natiele is someone else: she is created
	stored := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Conversa com a Natiele do financeiro.", "about_person": "Natiele",
		"new_persons": []string{"natiele"},
	})
	if got := toJSON(stored["persons"]); got != `[{"name":"Natiele","status":"created"}]` {
		t.Errorf("confirmed new person: persons = %s", got)
	}
	// A stored name is still reused, accents and case aside
	stored = call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Bruna fechou o chamado.", "about_person": "bruna guimaraes",
	})
	if got := toJSON(stored["persons"]); got != `[{"name":"Bruna Guimarães","status":"existing"}]` {
		t.Errorf("stored name: persons = %s", got)
	}
}
