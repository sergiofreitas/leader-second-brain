package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// HandleRenamePerson processes a rename_person tool call: it gives a known
// person a new name (e.g. "Osmar" → "Osmar de Morais Junior"). The name is
// copied into memories, tasks and feedbacks when they are stored, so all of
// them are updated in one transaction.
func (s *Server) HandleRenamePerson(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, _ := args["name"].(string)
	newName, _ := args["new_name"].(string)
	name, newName = strings.TrimSpace(name), strings.TrimSpace(newName)
	if name == "" || newName == "" {
		return nil, errors.New("name and new_name are required")
	}

	var oldName string
	var memories, tasks, feedbacks int
	err := s.store.InTx(func(tx *sql.Tx, st *sqlite.Store) error {
		g := s.graph.WithTx(tx)
		var id string
		var err error
		id, oldName, err = st.FindPerson(name)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("person %q not found (see list_people)", name)
		}
		if err != nil {
			return fmt.Errorf("find person %q: %w", name, err)
		}
		// The new name may differ only in case or accents ("Sergio" →
		// "Sérgio"), but must not be someone else's
		otherID, otherName, err := st.FindPerson(newName)
		if err == nil && otherID != id {
			return fmt.Errorf("%q is already another person (%q): merging two people isn't supported", newName, otherName)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find person %q: %w", newName, err)
		}

		if err := st.SetPersonName(id, newName); err != nil {
			return fmt.Errorf("rename person: %w", err)
		}
		if err := g.UpdateNode("Person", id, map[string]interface{}{"name": newName}); err != nil {
			return fmt.Errorf("rename person node: %w", err)
		}

		// Memories about the person (feedbacks have ABOUT edges too: they
		// aren't memories, so nothing changes for them here)
		about, err := g.GetEdgesTo(id, "ABOUT")
		if err != nil {
			return err
		}
		for _, e := range about {
			updated, err := st.SetMemoryAboutPerson(e.From, newName)
			if err != nil {
				return fmt.Errorf("update memory %s: %w", e.From, err)
			}
			if updated {
				memories++
			}
		}

		// Tasks the person owns and feedbacks the person gave
		names := []struct {
			edge, label, prop string
			count             *int
		}{
			{"OWNED_BY", "Task", "owner", &tasks},
			{"GIVEN_BY", "Feedback", "from", &feedbacks},
		}
		for _, n := range names {
			edges, err := g.GetEdgesTo(id, n.edge)
			if err != nil {
				return err
			}
			for _, e := range edges {
				if err := g.UpdateNode(n.label, e.From, map[string]interface{}{n.prop: newName}); err != nil {
					return fmt.Errorf("update %s %s: %w", strings.ToLower(n.label), e.From, err)
				}
				*n.count++
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rename_person: %w", err)
	}

	resultJSON, _ := json.MarshalIndent(map[string]interface{}{
		"status":    "renamed",
		"from":      oldName,
		"to":        newName,
		"memories":  memories,
		"tasks":     tasks,
		"feedbacks": feedbacks,
	}, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}
