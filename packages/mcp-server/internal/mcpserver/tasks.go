package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// HandleCompleteTask processes a complete_task tool call: it marks a task
// done. Task ids are listed by recall and get_team_context. Completing a task
// that is already done changes nothing.
func (s *Server) HandleCompleteTask(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	taskID, _ := args["task_id"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task_id is required (task ids are listed by recall and get_team_context)")
	}

	status := "completed"
	var description string
	err := s.store.InTx(func(tx *sql.Tx, st *sqlite.Store) error {
		g := s.graph.WithTx(tx)
		previous, err := st.CompleteTask(taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("task %q not found (task ids are listed by recall and get_team_context)", taskID)
		}
		if err != nil {
			return fmt.Errorf("complete task: %w", err)
		}
		task, err := g.GetNode("Task", taskID)
		if err != nil {
			return fmt.Errorf("task node %s: %w", taskID, err)
		}
		description, _ = task["description"].(string)
		if previous == "done" {
			status = "already_completed"
			return nil
		}
		return g.UpdateNode("Task", taskID, map[string]interface{}{
			"status": "done", "completed_at": time.Now().Format(time.RFC3339),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("complete_task: %w", err)
	}

	resultJSON, _ := json.MarshalIndent(map[string]interface{}{
		"status":      status,
		"task_id":     taskID,
		"description": description,
	}, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}
