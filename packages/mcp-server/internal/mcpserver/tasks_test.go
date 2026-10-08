package mcpserver

import (
	"context"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

func TestCompleteTask(t *testing.T) {
	srv := newTestServer(t)
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "O Evandro ainda não teve a avaliação. Preciso fazer a avaliação dele e marcar a rodada de 1:1.",
		"about_person": "Evandro",
		"extraction": &providers.EntityExtraction{
			Persons: []providers.ExtractedPerson{{Name: "Sérgio"}},
			Tasks: []providers.ExtractedTask{
				{Description: "Fazer a avaliação do Evandro", Owner: "Sérgio"},
				{Description: "Marcar a rodada de 1:1", Owner: "Sérgio"},
			},
		},
	})

	brief := call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Evandro", "context": "1:1"})
	pending, _ := brief["pending_tasks"].([]interface{})
	if len(pending) != 2 {
		t.Fatalf("pending_tasks = %s", toJSON(brief["pending_tasks"]))
	}
	var taskID string
	for _, p := range pending {
		if task := p.(map[string]interface{}); task["description"] == "Fazer a avaliação do Evandro" {
			taskID, _ = task["id"].(string)
		}
	}
	if taskID == "" {
		t.Fatalf("pending task without id: %s", toJSON(pending))
	}

	done := call(t, "complete_task", srv.HandleCompleteTask, map[string]interface{}{"task_id": taskID})
	if done["status"] != "completed" || done["description"] != "Fazer a avaliação do Evandro" {
		t.Errorf("complete_task = %s", toJSON(done))
	}
	var status string
	var completedAt *string
	srv.store.DB().QueryRow(`SELECT status, completed_at FROM tasks WHERE id = ?`, taskID).Scan(&status, &completedAt)
	if status != "done" || completedAt == nil {
		t.Errorf("tasks row: status %q, completed_at %v", status, completedAt)
	}
	// Completing it again changes nothing, not even when it was completed
	srv.store.DB().Exec(`UPDATE tasks SET completed_at = '2026-01-01 10:00:00' WHERE id = ?`, taskID)
	again := call(t, "complete_task", srv.HandleCompleteTask, map[string]interface{}{"task_id": taskID})
	if again["status"] != "already_completed" {
		t.Errorf("complete_task again = %s, want already_completed", toJSON(again))
	}
	var firstCompletion string
	srv.store.DB().QueryRow(`SELECT completed_at FROM tasks WHERE id = ?`, taskID).Scan(&firstCompletion)
	if firstCompletion != "2026-01-01 10:00:00" {
		t.Errorf("completed_at after completing again = %q, want it unchanged", firstCompletion)
	}

	brief = call(t, "recall", srv.HandleRecall, map[string]interface{}{"person_name": "Evandro", "context": "1:1"})
	pending, _ = brief["pending_tasks"].([]interface{})
	completed, _ := brief["completed_tasks"].([]interface{})
	if len(pending) != 1 || pending[0].(map[string]interface{})["description"] != "Marcar a rodada de 1:1" {
		t.Errorf("pending after completing = %s", toJSON(pending))
	}
	if len(completed) != 1 {
		t.Fatalf("completed_tasks = %s", toJSON(brief["completed_tasks"]))
	}
	task := completed[0].(map[string]interface{})
	if task["id"] != taskID || task["status"] != "done" || task["completed_at"] == nil || task["owner"] != "Sérgio" {
		t.Errorf("completed task = %s", toJSON(task))
	}

	for _, id := range []string{"task_nope", " "} {
		if _, err := srv.HandleCompleteTask(context.Background(), map[string]interface{}{"task_id": id}); err == nil {
			t.Errorf("complete_task(%q) succeeded, want an error", id)
		}
	}
}
