package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// newMethodServer is a test server with an organization's method: a rubric
// of two criteria and a short quiet period
func newMethodServer(t *testing.T) *Server {
	t.Helper()
	srv := newTestServer(t)
	srv.cfg.Assessment.Name = "Score de Cultura"
	srv.cfg.Assessment.Criteria = []config.Criterion{
		{ID: "ter-coragem", Name: "Ter coragem", Group: "Protagonismo"},
		{ID: "antecipar-os-problemas", Name: "Antecipar os problemas", Group: "Sonhar Grande"},
	}
	return srv
}

func score(v int) *int { return &v }

func TestGetPlaybook(t *testing.T) {
	srv := newMethodServer(t)

	all := call(t, "get_playbook", srv.HandleGetPlaybook, map[string]interface{}{})
	rites, _ := all["rites"].(map[string]interface{})
	for _, rite := range []string{"one_on_one", "weekly", "skip_level", "feedback"} {
		if rites[rite] == nil {
			t.Errorf("rites lack %s: %s", rite, toJSON(all["rites"]))
		}
	}
	criteria := all["assessment"].(map[string]interface{})["criteria"].([]interface{})
	if len(criteria) != 2 || all["feedback"] == nil || all["alerts"] == nil || all["memory_types"] == nil {
		t.Errorf("get_playbook = %s", toJSON(all))
	}

	feedback := call(t, "get_playbook", srv.HandleGetPlaybook, map[string]interface{}{"rite": "feedback"})
	if len(feedback["rites"].(map[string]interface{})) != 1 || feedback["feedback"] == nil {
		t.Errorf("get_playbook(rite: feedback) = %s", toJSON(feedback))
	}

	for _, args := range []map[string]interface{}{{"rite": "retro"}, {"section": "everything"}} {
		if _, err := srv.HandleGetPlaybook(context.Background(), args); err == nil {
			t.Errorf("get_playbook(%v): want an error", args)
		}
	}
}

// TestAgreementsWithDueDates records a 1:1 with agreements: the overdue one
// is flagged in recall and in the team view
func TestAgreementsWithDueDates(t *testing.T) {
	srv := newMethodServer(t)
	day := func(days int) string { return time.Now().AddDate(0, 0, days).Format(dateLayout) }

	out := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "1:1 com o Bernardo.", "about_person": "Bernardo",
		"source_url": "https://docs.example.com/1a1-bernardo",
		"extraction": &providers.EntityExtraction{
			MemoryType: "one_on_one", Summary: "1:1 de outubro",
			Tasks: []providers.ExtractedTask{
				{Description: "Reestruturar o PDI", Owner: "Bernardo", DueDate: day(-3)},
				{Description: "Documentar as decisões", Owner: "Bernardo", DueDate: day(10) + "T12:00:00Z"},
				{Description: "Sem prazo", Owner: "Sergio"},
			},
			Relationships: []providers.ExtractedRel{{From: "Bernardo", To: "Sergio", Type: "REPORTS_TO"}},
		},
	})
	if out["source_url"] != "https://docs.example.com/1a1-bernardo" || out["tasks"] != float64(3) {
		t.Errorf("ingest = %s", toJSON(out))
	}
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Semanal do time.", "about_person": "Bernardo",
		"extraction": &providers.EntityExtraction{MemoryType: "weekly"},
	})

	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Bernardo", "context": "1:1"})
	due := map[string]string{}
	overdue := map[string]bool{}
	for _, task := range brief["pending_tasks"].([]interface{}) {
		task := task.(map[string]interface{})
		d, _ := task["due_date"].(string)
		due[task["description"].(string)] = d
		overdue[task["description"].(string)] = task["overdue"] == true
	}
	if due["Reestruturar o PDI"] != day(-3) || due["Documentar as decisões"] != day(10) || due["Sem prazo"] != "" {
		t.Errorf("due dates = %v", due)
	}
	if !overdue["Reestruturar o PDI"] || overdue["Documentar as decisões"] || overdue["Sem prazo"] {
		t.Errorf("overdue = %v", overdue)
	}
	types := map[string]bool{}
	for _, m := range brief["memories"].([]interface{}) {
		m := m.(map[string]interface{})
		types[m["type"].(string)] = true
		if m["type"] == "one_on_one" && m["source_url"] != "https://docs.example.com/1a1-bernardo" {
			t.Errorf("1:1 memory = %s", toJSON(m))
		}
	}
	if !types["weekly"] || !types["one_on_one"] {
		t.Errorf("memory types = %v", types)
	}

	team := call(t, "get_team_context", srv.HandleGetTeamContext, map[string]interface{}{"leader_name": "Sergio"})
	signals := toJSON(team["members"].([]interface{})[0].(map[string]interface{})["signals"])
	if !strings.Contains(signals, `"overdue_task"`) || !strings.Contains(signals, "Reestruturar o PDI") {
		t.Errorf("signals = %s", signals)
	}

	_, err := srv.HandleIngest(context.Background(), map[string]interface{}{
		"modality": "text", "content": "x", "about_person": "Bernardo",
		"extraction": &providers.EntityExtraction{Tasks: []providers.ExtractedTask{{Description: "x", DueDate: "15/11/2026"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "due_date") {
		t.Errorf("a due date out of format: err = %v", err)
	}
}

func TestStructuredFeedbackItems(t *testing.T) {
	srv := newMethodServer(t)
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Feedback do Bernardo.", "about_person": "Bernardo",
		"extraction": &providers.EntityExtraction{
			MemoryType: "feedback",
			FeedbackItems: []providers.ExtractedFeedbackItem{{
				Category: "stop", Content: "Na sprint 42, percebi muitas tarefas em paralelo.",
				Situation: "Sprint 42", Behavior: "Cinco tarefas em andamento ao mesmo tempo",
				Impact: "Duas entregas atrasaram", ChangeSignal: "No máximo duas tarefas em andamento",
			}},
		},
	})
	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Bernardo", "context": "feedback"})
	item := brief["feedbacks"].([]interface{})[0].(map[string]interface{})["items"].([]interface{})[0].(map[string]interface{})
	if item["situation"] != "Sprint 42" || item["behavior"] == nil || item["impact"] == nil || item["change_signal"] != "No máximo duas tarefas em andamento" {
		t.Errorf("feedback item = %s", toJSON(item))
	}
}

// TestAssessment records a culture score by criterion: it is checked
// against the rubric and the scale, the divergences are flagged, recall
// lists it as the leader's private notes and delete_memory removes it
func TestAssessment(t *testing.T) {
	srv := newMethodServer(t)
	ingest := func(about string, a *providers.ExtractedAssessment) (*ToolResult, error) {
		return srv.HandleIngest(context.Background(), map[string]interface{}{
			"modality": "text", "content": "Score de cultura de outubro.", "about_person": about,
			"occurred_at": "2026-10-01",
			"extraction":  &providers.EntityExtraction{Summary: "Score de outubro", Assessment: a},
		})
	}

	for name, tc := range map[string]struct {
		about string
		a     *providers.ExtractedAssessment
		want  string
	}{
		"unknown criterion": {"Bernardo", &providers.ExtractedAssessment{Scores: []providers.ExtractedScore{{Criterion: "Ser rápido", Leader: score(3)}}}, "unknown criterion"},
		"outside the scale": {"Bernardo", &providers.ExtractedAssessment{Scores: []providers.ExtractedScore{{Criterion: "ter-coragem", Self: score(6)}}}, "outside the scale"},
		"no score":          {"Bernardo", &providers.ExtractedAssessment{Scores: []providers.ExtractedScore{{Criterion: "ter-coragem"}}}, "leader, self or both"},
		"scored twice":      {"Bernardo", &providers.ExtractedAssessment{Scores: []providers.ExtractedScore{{Criterion: "Ter coragem", Leader: score(3)}, {Criterion: "ter-coragem", Leader: score(4)}}}, "twice"},
		"nobody assessed":   {"", &providers.ExtractedAssessment{Scores: []providers.ExtractedScore{{Criterion: "ter-coragem", Leader: score(3)}}}, "about_person"},
		"no scores":         {"Bernardo", &providers.ExtractedAssessment{}, "scores are required"},
	} {
		if _, err := ingest(tc.about, tc.a); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}

	res, err := ingest("Bernardo", &providers.ExtractedAssessment{Cycle: "2026-10", Scores: []providers.ExtractedScore{
		{Criterion: "TER CORAGEM", Leader: score(4), Self: score(2), Evidence: "Propôs o piloto do novo deploy."},
		{Criterion: "Antecipar os problemas", Leader: score(3), Self: score(4)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	out := call(t, "ingest", func(context.Context, map[string]interface{}) (*ToolResult, error) { return res, nil }, nil)
	if out["memory_type"] != "assessment" {
		t.Errorf("memory_type = %v, want assessment by default", out["memory_type"])
	}
	divergences := out["assessment"].(map[string]interface{})["divergences"].([]interface{})
	if len(divergences) != 1 || divergences[0].(map[string]interface{})["criterion"] != "Ter coragem" || divergences[0].(map[string]interface{})["gap"] != float64(2) {
		t.Errorf("divergences = %s", toJSON(divergences))
	}

	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Bernardo", "context": "pdi", "time_range": "all"})
	assessments := brief["assessments"].([]interface{})
	if len(assessments) != 1 {
		t.Fatalf("assessments = %s", toJSON(assessments))
	}
	a := assessments[0].(map[string]interface{})
	scores := a["scores"].([]interface{})
	first := scores[0].(map[string]interface{})
	if a["name"] != "Score de Cultura" || a["cycle"] != "2026-10" || a["date"] != "2026-10-01" || a["visibility"] != "leader_only" ||
		len(scores) != 2 || first["criterion_id"] != "ter-coragem" || first["group"] != "Protagonismo" || first["evidence"] == nil {
		t.Errorf("assessment = %s", toJSON(a))
	}
	if avg := a["averages"].(map[string]interface{}); avg["leader"] != 3.5 || avg["self"] != float64(3) {
		t.Errorf("averages = %v", avg)
	}
	if brief["assessments_note"] == nil {
		t.Error("recall has no note that the scores are private")
	}

	preview := call(t, "delete_memory", srv.HandleDeleteMemory, map[string]interface{}{"memory_id": out["memory_id"]})
	if preview["assessments"] != float64(1) {
		t.Errorf("delete preview = %s", toJSON(preview))
	}
	call(t, "delete_memory", srv.HandleDeleteMemory, map[string]interface{}{"memory_id": out["memory_id"], "confirm": true})
	brief = call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Bernardo", "time_range": "all"})
	if brief["assessments"] != nil {
		t.Errorf("assessments after delete = %s", toJSON(brief["assessments"]))
	}
}

// TestAssessmentWithoutRubric accepts any criterion when the method lists
// none
func TestAssessmentWithoutRubric(t *testing.T) {
	srv := newTestServer(t)
	out := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Avaliação.", "about_person": "Natiele",
		"extraction": &providers.EntityExtraction{MemoryType: "one_on_one", Assessment: &providers.ExtractedAssessment{
			Scores: []providers.ExtractedScore{{Criterion: "Comunicação Clara", Leader: score(5)}},
		}},
	})
	if out["memory_type"] != "one_on_one" || out["assessment"] == nil {
		t.Errorf("ingest = %s", toJSON(out))
	}
	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Natiele"})
	a := brief["assessments"].([]interface{})[0].(map[string]interface{})
	if s := a["scores"].([]interface{})[0].(map[string]interface{}); s["criterion_id"] != "comunicacao-clara" {
		t.Errorf("score = %s", toJSON(s))
	}
}
