package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/graph"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// HandleDeleteMemory processes a delete_memory tool call: it deletes a memory
// stored by mistake with what was derived from it — its feedbacks and their
// items, and the tasks that came from it. People, their relationships and
// topics are kept: other memories may share them.
//
// Deleting can't be undone, so it takes two calls: without confirm it only
// describes what would be deleted, for the host to show the user; with
// confirm: true, after the user agreed, it deletes.
func (s *Server) HandleDeleteMemory(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	memoryID, _ := args["memory_id"].(string)
	memoryID = strings.TrimSpace(memoryID)
	confirm, _ := args["confirm"].(bool)
	if memoryID == "" {
		return nil, errors.New("memory_id is required (memory ids are in recall, search_memories and ingest's result)")
	}

	result := map[string]interface{}{"memory_id": memoryID}
	err := s.store.InTx(func(tx *sql.Tx, st *sqlite.Store) error {
		g := s.graph.WithTx(tx)
		memory, err := st.GetMemory(memoryID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("memory %q not found (memory ids are in recall, search_memories and ingest's result)", memoryID)
		}
		if err != nil {
			return fmt.Errorf("get memory: %w", err)
		}
		derived, err := derivedFrom(g, memoryID)
		if err != nil {
			return err
		}

		// What is (or would be) deleted
		result["type"] = memory["type"]
		result["about_person"] = memory["about_person"]
		if node, err := g.GetNode("Memory", memoryID); err == nil {
			for _, key := range []string{"summary", "occurred_at", "created_at"} {
				if v, ok := node[key].(string); ok && v != "" {
					result[key] = v
				}
			}
		}
		result["feedback_items"] = len(derived.items)
		tasks := make([]string, len(derived.tasks))
		for i, t := range derived.tasks {
			tasks[i] = t.description
		}
		result["tasks"] = tasks

		if !confirm {
			result["status"] = "preview"
			result["next"] = "Nothing was deleted. Show this to the user; if they confirm, call delete_memory again with confirm: true."
			return nil
		}

		// The derived nodes first; each node's edges go with it
		for _, id := range derived.items {
			if err := g.DeleteNode("FeedbackItem", id); err != nil {
				return fmt.Errorf("delete feedback item: %w", err)
			}
		}
		for _, id := range derived.feedbacks {
			if err := g.DeleteNode("Feedback", id); err != nil {
				return fmt.Errorf("delete feedback: %w", err)
			}
		}
		for _, t := range derived.tasks {
			if err := st.DeleteTask(t.id); err != nil {
				return fmt.Errorf("delete task: %w", err)
			}
			if err := g.DeleteNode("Task", t.id); err != nil {
				return fmt.Errorf("delete task node: %w", err)
			}
		}
		if err := g.DeleteNode("Memory", memoryID); err != nil {
			return fmt.Errorf("delete memory node: %w", err)
		}
		if _, err := st.DeleteMemory(memoryID); err != nil {
			return fmt.Errorf("delete memory: %w", err)
		}
		result["status"] = "deleted"
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("delete_memory: %w", err)
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// derived is what ingest created from a memory, besides the memory itself
type derived struct {
	feedbacks []string // Feedback node ids
	items     []string // FeedbackItem node ids
	tasks     []struct{ id, description string }
}

// derivedFrom finds the feedbacks (Memory -[FORMALIZED_IN]-> Feedback), their
// items (Feedback -[CONTAINS]-> FeedbackItem) and the tasks
// (Task -[DERIVED_FROM]-> Memory) of a memory
func derivedFrom(g graph.GraphEngine, memoryID string) (derived, error) {
	var d derived
	feedbacks, err := g.GetEdges(memoryID, "FORMALIZED_IN")
	if err != nil {
		return d, err
	}
	for _, f := range feedbacks {
		d.feedbacks = append(d.feedbacks, f.To)
		items, err := g.GetEdges(f.To, "CONTAINS")
		if err != nil {
			return d, err
		}
		for _, item := range items {
			d.items = append(d.items, item.To)
		}
	}
	tasks, err := g.GetEdgesTo(memoryID, "DERIVED_FROM")
	if err != nil {
		return d, err
	}
	for _, t := range tasks {
		node, err := g.GetNode("Task", t.From)
		if err != nil {
			return d, fmt.Errorf("task %s: %w", t.From, err)
		}
		description, _ := node["description"].(string)
		d.tasks = append(d.tasks, struct{ id, description string }{t.From, description})
	}
	return d, nil
}
