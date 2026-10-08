package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// TestTeamContext reviews a team like the one of the example run: who shares
// which development point, the last feedback and assessment of each person,
// what needs attention and what the leader has pending
func TestTeamContext(t *testing.T) {
	srv := newTestServer(t)
	day := func(daysAgo int) string { return time.Now().AddDate(0, 0, -daysAgo).Format(dateLayout) }
	ingest := func(args map[string]interface{}) map[string]interface{} {
		args["modality"] = "text"
		return call(t, "ingest", srv.HandleIngest, args)
	}

	team := []string{"Leandro", "Roberto", "Bernardo", "Natiele", "Evandro"}
	var rels []providers.ExtractedRel
	for _, name := range team {
		rels = append(rels, providers.ExtractedRel{From: name, To: "Sergio Freitas", Type: "REPORTS_TO"})
	}
	ingest(map[string]interface{}{
		"content": "Estrutura do time.", "about_person": "Sergio Freitas",
		"extraction": &providers.EntityExtraction{Relationships: rels},
	})

	feedback := func(name string, daysAgo int, topics ...string) {
		ingest(map[string]interface{}{
			"content": "Feedback do " + name + ".", "about_person": name, "occurred_at": day(daysAgo),
			"extraction": &providers.EntityExtraction{
				MemoryType: "feedback", Summary: "Feedback do " + name, Topics: topics,
				FeedbackItems: []providers.ExtractedFeedbackItem{
					{Category: "stop", Content: "Parar " + name}, {Category: "continue", Content: "Continuar " + name},
				},
			},
		})
	}
	feedback("Leandro", 37, "work in progress", "antecipar problemas")
	ingest(map[string]interface{}{ // older, stored after: not the last feedback
		"content": "Feedback antigo do Leandro.", "about_person": "Leandro", "occurred_at": day(100),
		"extraction": &providers.EntityExtraction{
			MemoryType: "feedback", Summary: "Feedback antigo",
			FeedbackItems: []providers.ExtractedFeedbackItem{{Category: "start", Content: "Antigo"}},
		},
	})
	feedback("Roberto", 36, "work in progress", "PDI", "antecipar problemas")
	feedback("Bernardo", 70, "work in progress", "PDI")
	feedback("Evandro", 35, "PDI", "aprendizado")
	for _, name := range []string{"Leandro", "Natiele"} {
		ingest(map[string]interface{}{
			"content": "Score de cultura do " + name + ".", "about_person": name, "occurred_at": day(36),
			"extraction": &providers.EntityExtraction{MemoryType: "assessment", Summary: "Score do " + name, Topics: []string{"avaliação de cultura", "antecipar problemas"}},
		})
	}
	ingest(map[string]interface{}{
		"content": "Evandro ainda não teve avaliação; marcar a rodada de 1:1.", "about_person": "Evandro",
		"extraction": &providers.EntityExtraction{Tasks: []providers.ExtractedTask{
			{Description: "Avaliar o Evandro", Owner: "Sergio Freitas"},
			{Description: "Marcar a rodada de 1:1", Owner: "Sergio Freitas", AboutPerson: "Sergio Freitas"},
		}},
	})
	// The evaluation was asked for 40 days ago
	srv.store.DB().Exec(`UPDATE graph_nodes SET props = json_set(props, '$.created_at', ?) WHERE label = 'Task' AND json_extract(props, '$.description') = 'Avaliar o Evandro'`,
		time.Now().AddDate(0, 0, -40).Format(time.RFC3339))

	view := call(t, "get_team_context", srv.HandleGetTeamContext, map[string]interface{}{"leader_name": "sergio"})
	if view["leader"] != "Sergio Freitas" || view["team_size"] != float64(5) {
		t.Fatalf("leader = %v, team_size = %v", view["leader"], view["team_size"])
	}

	// Shared topics: who has each development point, the most shared first
	if got := toJSON(view["shared_topics"]); got != `[`+
		`{"count":3,"people":["Leandro","Natiele","Roberto"],"topic":"antecipar problemas"},`+
		`{"count":3,"people":["Bernardo","Evandro","Roberto"],"topic":"PDI"},`+
		`{"count":3,"people":["Bernardo","Leandro","Roberto"],"topic":"work in progress"},`+
		`{"count":2,"people":["Leandro","Natiele"],"topic":"avaliação de cultura"}]` {
		t.Errorf("shared_topics = %s", got)
	}

	members := map[string]map[string]interface{}{}
	for _, m := range view["members"].([]interface{}) {
		member := m.(map[string]interface{})
		members[member["name"].(string)] = member
	}
	leandro := members["Leandro"]
	if got := toJSON(leandro["last_feedback"]); got != `{"created_at":"`+leandro["last_feedback"].(map[string]interface{})["created_at"].(string)+
		`","items":[{"category":"stop","content":"Parar Leandro"},{"category":"continue","content":"Continuar Leandro"}],"occurred_at":"`+day(37)+`"}` {
		t.Errorf("Leandro's last_feedback = %s", got)
	}
	if a := leandro["last_assessment"].(map[string]interface{}); a["summary"] != "Score do Leandro" || a["occurred_at"] != day(36) {
		t.Errorf("Leandro's last_assessment = %s", toJSON(a))
	}
	if leandro["manager"] != "Sergio Freitas" || leandro["memories"] != float64(3) || leandro["signals"] != nil {
		t.Errorf("Leandro = %s", toJSON(leandro))
	}

	signals := func(name string) string {
		var s []string
		for _, sig := range members[name]["signals"].([]interface{}) {
			s = append(s, sig.(map[string]interface{})["signal"].(string))
		}
		return strings.Join(s, ",")
	}
	if got := signals("Natiele"); got != "no_feedback" {
		t.Errorf("Natiele's signals = %s", got)
	}
	if got := signals("Bernardo"); got != "no_recent_memory" {
		t.Errorf("Bernardo's signals = %s", got)
	}
	if got := signals("Evandro"); got != "stale_task" {
		t.Errorf("Evandro's signals = %s", got)
	}
	pending, _ := members["Evandro"]["pending_tasks"].([]interface{})
	if len(pending) != 1 || pending[0].(map[string]interface{})["id"] == nil {
		t.Errorf("Evandro's pending_tasks = %s", toJSON(pending))
	}

	var leaderTasks []string
	for _, task := range view["leader_pending_tasks"].([]interface{}) {
		leaderTasks = append(leaderTasks, task.(map[string]interface{})["description"].(string))
	}
	if got := strings.Join(leaderTasks, " | "); got != "Marcar a rodada de 1:1 | Avaliar o Evandro" {
		t.Errorf("leader_pending_tasks = %s", got)
	}

	// recall lists the assessments stored as memories
	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Natiele", "context": "pdi"})
	assessments, _ := brief["assessments"].([]interface{})
	if len(assessments) != 1 || assessments[0].(map[string]interface{})["summary"] != "Score do Natiele" {
		t.Errorf("recall assessments = %s", toJSON(brief["assessments"]))
	}
	if !strings.Contains(brief["recommendation"].(string), "1 registro") {
		t.Errorf("recall recommendation = %v", brief["recommendation"])
	}
}
