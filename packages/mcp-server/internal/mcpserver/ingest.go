package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/graph"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// Modalities are the accepted values of the ingest "modality" argument
var Modalities = []string{"text", "audio", "image", "video"}

// HandleIngest processes an ingest tool call.
//
// All content types — observations, feedback, 1:1 transcripts, voice notes —
// enter through this single tool. The MCP host (the leader's own assistant)
// does the understanding: it transcribes or describes media, and passes the
// text in "content" plus the entities it extracted in "extraction"
// (*providers.EntityExtraction). Without an extraction, the server falls back
// to its own LLM provider. The server validates the extraction, resolves
// names against the people it already knows and stores everything in a
// single transaction.
func (s *Server) HandleIngest(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	modality, _ := args["modality"].(string)
	content, _ := args["content"].(string)
	filePath, _ := args["file_path"].(string)
	aboutPersonHint, _ := args["about_person"].(string)
	hostExtraction, _ := args["extraction"].(*providers.EntityExtraction)

	// Step 1: The text to store. Media is transcribed/described by the host.
	if modality == "" {
		modality = "text"
	}
	if !contains(Modalities, modality) {
		return nil, fmt.Errorf("invalid modality %q (valid: %s)", modality, strings.Join(Modalities, ", "))
	}
	content = strings.TrimSpace(content)
	if content == "" {
		if modality == "text" {
			return nil, errors.New("content is required")
		}
		return nil, fmt.Errorf("content is required for %s: transcribe or describe the file and pass the text in content (this server doesn't process media files itself)", modality)
	}

	// Step 2: Entities — from the host, or from the server's LLM provider
	extraction := hostExtraction
	if extraction == nil {
		var err error
		extraction, err = s.llm.ExtractEntities(content, providers.ExtractionConfig{
			FeedbackCategories: s.cfg.FeedbackCategoryIDs(),
			FeedbackEnabled:    s.cfg.Skills.Feedback,
		})
		if err != nil || extraction == nil {
			extraction = &providers.EntityExtraction{}
		}
	}
	if strings.TrimSpace(extraction.AboutPerson) == "" {
		extraction.AboutPerson = aboutPersonHint
	}
	if err := s.normalizeExtraction(extraction); err != nil {
		return nil, err
	}

	// Step 3: Embedding (outside the transaction: it may call a provider).
	// A failure only costs semantic search for this memory.
	embedding, err := s.embedding.Embed(content)
	if err != nil {
		log.Printf("warning: could not embed memory: %v", err)
		embedding = nil
	}

	memID := generateID("mem")
	now := time.Now().Format(time.RFC3339)
	out := newIngestOutcome()

	// Steps 4-9 write the memory, its embedding and its graph in a single
	// transaction: either everything is stored or nothing is.
	err = s.store.InTx(func(tx *sql.Tx, st *sqlite.Store) error {
		g := s.graph.WithTx(tx)
		person := func(name, role string) (string, error) {
			id, storedName, created, err := ensurePerson(st, g, name, role)
			if err != nil {
				return "", err
			}
			out.addPerson(storedName, created)
			return id, nil
		}

		// Step 4: People, so names are resolved before anything refers to them
		roles := map[string]string{}
		for _, p := range extraction.Persons {
			roles[p.Name] = p.Role
		}
		aboutID, aboutName := "", ""
		if extraction.AboutPerson != "" {
			if aboutID, err = person(extraction.AboutPerson, roles[extraction.AboutPerson]); err != nil {
				return err
			}
			aboutName = out.lastPerson
		}
		mentioned := []string{}
		for _, p := range extraction.Persons {
			id, err := person(p.Name, p.Role)
			if err != nil {
				return err
			}
			if id != aboutID {
				mentioned = append(mentioned, id)
			}
		}

		// Step 5: The memory, its embedding and its node
		if err := st.InsertMemory(memID, extraction.MemoryType, content, modality, "mcp", filePath, aboutName, 1.0); err != nil {
			return fmt.Errorf("store memory: %w", err)
		}
		if embedding != nil {
			if err := st.InsertVector(memID, embedding); err != nil {
				return fmt.Errorf("store vector: %w", err)
			}
			out.Embedded = true
		}
		memProps := map[string]interface{}{
			"type": extraction.MemoryType, "content": content,
			"modality": modality, "created_at": now,
		}
		if extraction.Summary != "" {
			memProps["summary"] = extraction.Summary
		}
		if filePath != "" {
			memProps["file_path"] = filePath
		}
		if err := g.AddNode("Memory", memID, memProps); err != nil {
			return fmt.Errorf("add memory node: %w", err)
		}
		if aboutID != "" {
			if err := g.AddEdge(memID, aboutID, "ABOUT", nil); err != nil {
				return err
			}
		}
		for _, id := range mentioned {
			if err := g.AddEdge(memID, id, "MENTIONS", nil); err != nil {
				return err
			}
		}

		// Step 6: Topics, shared across memories so patterns can be counted
		for _, topic := range extraction.Topics {
			topicID := "topic_" + strings.ReplaceAll(sqlite.FoldName(topic), " ", "-")
			if err := g.AddNode("Topic", topicID, map[string]interface{}{"name": topic}); err != nil {
				return fmt.Errorf("add topic node: %w", err)
			}
			if err := g.AddEdge(memID, topicID, "DISCUSSES", nil); err != nil {
				return err
			}
		}

		// Step 7: Person-to-person relationships (hierarchy, mentoring, ...)
		for _, rel := range extraction.Relationships {
			fromID, err := person(rel.From, roles[rel.From])
			if err != nil {
				return err
			}
			toID, err := person(rel.To, roles[rel.To])
			if err != nil {
				return err
			}
			if err := g.AddEdge(fromID, toID, rel.Type, nil); err != nil {
				return err
			}
		}

		// Step 8: Tasks — owned by someone, concerning someone
		for _, t := range extraction.Tasks {
			taskID := generateID("task")
			props := map[string]interface{}{
				"description": t.Description, "status": "pending", "created_at": now,
			}
			ownerID := ""
			if t.Owner != "" {
				if ownerID, err = person(t.Owner, roles[t.Owner]); err != nil {
					return err
				}
				props["owner"] = out.lastPerson
			}
			targetID := aboutID
			if t.AboutPerson != "" {
				if targetID, err = person(t.AboutPerson, roles[t.AboutPerson]); err != nil {
					return err
				}
			}
			if err := st.InsertTask(taskID, t.Description, ownerID, "pending"); err != nil {
				return fmt.Errorf("store task: %w", err)
			}
			if err := g.AddNode("Task", taskID, props); err != nil {
				return fmt.Errorf("add task node: %w", err)
			}
			if err := g.AddEdge(taskID, memID, "DERIVED_FROM", nil); err != nil {
				return err
			}
			if targetID != "" {
				if err := g.AddEdge(taskID, targetID, "TARGETS", nil); err != nil {
					return err
				}
			}
			if ownerID != "" {
				if err := g.AddEdge(taskID, ownerID, "OWNED_BY", nil); err != nil {
					return err
				}
			}
			out.Tasks++
		}

		// Step 9: Feedback — one Feedback node per person it is about, with
		// its items in the configured format and who gave it
		giverID, giverName := "", ""
		if extraction.FeedbackFrom != "" && len(extraction.FeedbackItems) > 0 {
			if giverID, err = person(extraction.FeedbackFrom, roles[extraction.FeedbackFrom]); err != nil {
				return err
			}
			giverName = out.lastPerson
		}
		groups := map[string][]providers.ExtractedFeedbackItem{}
		var order []string
		for _, item := range extraction.FeedbackItems {
			about := item.AboutPerson
			if about == "" {
				about = extraction.AboutPerson
			}
			if _, seen := groups[about]; !seen {
				order = append(order, about)
			}
			groups[about] = append(groups[about], item)
		}
		for _, about := range order {
			fbID := generateID("fb")
			props := map[string]interface{}{
				"type": "detected", "format": s.cfg.Feedback.Format,
				"created_at": now, "source_memory": memID,
			}
			if giverName != "" {
				props["from"] = giverName
			}
			if err := g.AddNode("Feedback", fbID, props); err != nil {
				return fmt.Errorf("add feedback node: %w", err)
			}
			if err := g.AddEdge(memID, fbID, "FORMALIZED_IN", nil); err != nil {
				return err
			}
			if about != "" {
				aboutFbID, err := person(about, roles[about])
				if err != nil {
					return err
				}
				if err := g.AddEdge(fbID, aboutFbID, "ABOUT", nil); err != nil {
					return err
				}
			}
			if giverID != "" {
				if err := g.AddEdge(fbID, giverID, "GIVEN_BY", nil); err != nil {
					return err
				}
			}
			for _, item := range groups[about] {
				itemID := generateID("fi")
				if err := g.AddNode("FeedbackItem", itemID, map[string]interface{}{
					"category": item.Category, "content": item.Content,
				}); err != nil {
					return fmt.Errorf("add feedback item node: %w", err)
				}
				if err := g.AddEdge(fbID, itemID, "CONTAINS", nil); err != nil {
					return err
				}
				out.FeedbackItems++
			}
		}
		out.AboutPerson = aboutName
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	resultJSON, _ := json.MarshalIndent(map[string]interface{}{
		"status":         "stored",
		"memory_id":      memID,
		"memory_type":    extraction.MemoryType,
		"about_person":   out.AboutPerson,
		"summary":        extraction.Summary,
		"persons":        out.Persons,
		"topics":         extraction.Topics,
		"relationships":  len(extraction.Relationships),
		"tasks":          out.Tasks,
		"feedback_items": out.FeedbackItems,
		"embedded":       out.Embedded,
	}, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// normalizeExtraction trims and validates an extraction in place. Errors name
// the valid values, so the host can fix its call and retry.
func (s *Server) normalizeExtraction(ex *providers.EntityExtraction) error {
	ex.MemoryType = strings.ToLower(strings.TrimSpace(ex.MemoryType))
	if ex.MemoryType == "" {
		ex.MemoryType = "observation"
	}
	if !contains(providers.MemoryTypes, ex.MemoryType) {
		return fmt.Errorf("invalid memory_type %q (valid: %s)", ex.MemoryType, strings.Join(providers.MemoryTypes, ", "))
	}
	ex.Summary = strings.TrimSpace(ex.Summary)
	ex.AboutPerson = strings.TrimSpace(ex.AboutPerson)
	ex.FeedbackFrom = strings.TrimSpace(ex.FeedbackFrom)

	persons := ex.Persons[:0]
	for _, p := range ex.Persons {
		p.Name, p.Role = strings.TrimSpace(p.Name), strings.TrimSpace(p.Role)
		if p.Name != "" {
			persons = append(persons, p)
		}
	}
	ex.Persons = persons

	topics := ex.Topics[:0]
	for _, t := range ex.Topics {
		if t = strings.TrimSpace(t); t != "" {
			topics = append(topics, t)
		}
	}
	ex.Topics = topics

	for i := range ex.Relationships {
		rel := &ex.Relationships[i]
		rel.From, rel.To = strings.TrimSpace(rel.From), strings.TrimSpace(rel.To)
		rel.Type = strings.ToUpper(strings.Join(strings.Fields(rel.Type), "_"))
		if rel.From == "" || rel.To == "" {
			return fmt.Errorf("relationship %d: from and to are required (person names)", i+1)
		}
		if !contains(providers.PersonRelationshipTypes, rel.Type) {
			return fmt.Errorf("relationship %d: invalid type %q (valid: %s)", i+1, rel.Type, strings.Join(providers.PersonRelationshipTypes, ", "))
		}
	}

	for i := range ex.Tasks {
		t := &ex.Tasks[i]
		t.Description = strings.TrimSpace(t.Description)
		t.Owner, t.AboutPerson = strings.TrimSpace(t.Owner), strings.TrimSpace(t.AboutPerson)
		if t.Description == "" {
			return fmt.Errorf("task %d: description is required", i+1)
		}
	}

	categories := s.cfg.FeedbackCategoryIDs()
	for i := range ex.FeedbackItems {
		item := &ex.FeedbackItems[i]
		item.Category = strings.ToLower(strings.TrimSpace(item.Category))
		item.Content = strings.TrimSpace(item.Content)
		item.AboutPerson = strings.TrimSpace(item.AboutPerson)
		if item.Content == "" {
			return fmt.Errorf("feedback item %d: content is required", i+1)
		}
		if len(categories) > 0 && !contains(categories, item.Category) {
			return fmt.Errorf("feedback item %d: invalid category %q (valid: %s)", i+1, item.Category, strings.Join(categories, ", "))
		}
		if item.AboutPerson == "" && ex.AboutPerson == "" {
			return fmt.Errorf("feedback item %d: about_person is required (on the item or the memory)", i+1)
		}
	}
	return nil
}

// ensurePerson returns the ID and stored name of the person with the given
// name (ignoring case and accents), creating them in SQLite and in the graph
// if they don't exist yet. A non-empty role updates the person's role.
func ensurePerson(st *sqlite.Store, g graph.GraphEngine, name, role string) (id, storedName string, created bool, err error) {
	id, storedName, err = st.FindPerson(name)
	if err == nil {
		if role != "" {
			if err := st.SetPersonRole(id, role); err != nil {
				return "", "", false, fmt.Errorf("set role of %q: %w", storedName, err)
			}
			if err := g.UpdateNode("Person", id, map[string]interface{}{"role": role}); err != nil {
				return "", "", false, fmt.Errorf("set role of %q: %w", storedName, err)
			}
		}
		return id, storedName, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", false, fmt.Errorf("find person %q: %w", name, err)
	}
	id = generateID("person")
	if err := st.UpsertPerson(id, name, role, "", "", 0); err != nil {
		return "", "", false, fmt.Errorf("upsert person: %w", err)
	}
	if err := g.AddNode("Person", id, map[string]interface{}{
		"name": name, "role": role,
	}); err != nil {
		return "", "", false, fmt.Errorf("add person node: %w", err)
	}
	return id, name, true, nil
}

// ingestOutcome collects what an ingest stored, for the tool result
type ingestOutcome struct {
	AboutPerson   string
	Persons       []personOutcome
	Tasks         int
	FeedbackItems int
	Embedded      bool
	lastPerson    string // stored name of the last person resolved
	seen          map[string]int
}

type personOutcome struct {
	Name   string `json:"name"`
	Status string `json:"status"` // created | existing
}

func newIngestOutcome() *ingestOutcome {
	return &ingestOutcome{Persons: []personOutcome{}, seen: map[string]int{}}
}

func (o *ingestOutcome) addPerson(name string, created bool) {
	o.lastPerson = name
	if _, ok := o.seen[name]; ok {
		return
	}
	status := "existing"
	if created {
		status = "created"
	}
	o.seen[name] = len(o.Persons)
	o.Persons = append(o.Persons, personOutcome{name, status})
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
