package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/retrieve"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/graph"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/adapters"
)

// Server holds all dependencies for the MCP server
type Server struct {
	cfg       *config.Config
	store     *sqlite.Store
	graph     graph.GraphEngine
	embedding providers.EmbeddingProvider
	llm       providers.LLMProvider
	retriever *retrieve.HybridRetriever
	adapter   providers.OutputAdapter
}

// New creates a new MCP server with all dependencies wired
func New(cfg *config.Config) (*Server, error) {
	// Initialize SQLite store
	store, err := sqlite.New(cfg.Storage.SQLite.Path)
	if err != nil {
		return nil, fmt.Errorf("init sqlite store: %w", err)
	}

	// Initialize graph engine on the same database as the store
	var graphEngine graph.GraphEngine
	switch cfg.Graph.Engine {
	case "", "sqlite":
	case "graphlite":
		log.Printf("warning: graph engine 'graphlite' was removed; using 'sqlite' (same database file)")
	default:
		store.Close()
		return nil, fmt.Errorf("unknown graph engine %q (supported: sqlite)", cfg.Graph.Engine)
	}
	graphEngine, err = graph.NewSQLiteEngine(store.DB())
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("init graph: %w", err)
	}

	// Providers (stubs for now — replaced by real implementations)
	embedding := &providers.StubEmbedding{}
	llm := &providers.StubLLM{}

	// Initialize hybrid retriever
	retriever := retrieve.NewHybridRetriever(store, graphEngine, embedding)

	// Initialize output adapter (Qulture, Lattice, Markdown, JSON)
	adapter := adapters.NewAdapter(cfg.Feedback.TargetSystem)

	return &Server{
		cfg:       cfg,
		store:      store,
		graph:      graphEngine,
		embedding:  embedding,
		llm:        llm,
		retriever:  retriever,
		adapter:   adapter,
	}, nil
}

// ============================================================
// MCP Tool definitions
// ============================================================

type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// GetToolDefinitions returns all registered MCP tools
func (s *Server) GetToolDefinitions() []ToolDefinition {
	tools := []ToolDefinition{
		{
			Name:        "ingest",
			Description: "Capture a memory from text, audio, image, or video. The system transcribes/OCR's/describes the input, extracts entities (persons, topics, tasks), detects content type (observation, feedback, 1:1, etc.), and stores everything in the local knowledge graph + vector index. Feedback items are structured according to the configured feedback format.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"modality": map[string]interface{}{
						"type": "string",
						"enum": []string{"text", "audio", "image", "video"},
						"description": "The type of input being captured",
					},
					"content": map[string]interface{}{
						"type": "string",
						"description": "Text content (for text modality, or pre-transcribed text)",
					},
					"file_path": map[string]interface{}{
						"type": "string",
						"description": "Path to media file (for audio/image/video modality)",
					},
					"about_person": map[string]interface{}{
						"type": "string",
						"description": "Who is this memory about? (person name, optional — the system will also try to detect this)",
					},
				},
				"required": []string{"modality"},
			},
		},
		{
			Name:        "recall",
			Description: "Retrieve context about a person for a specific situation (1:1, PDI, feedback, team review). Returns memories, tasks, feedbacks, assessments, patterns, and signals from the knowledge graph.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"person_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the person to get context for",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"1:1", "pdi", "feedback", "team_review", "progression", "general"},
						"description": "The situation this context will be used for",
					},
					"time_range": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"last_30d", "last_90d", "last_year", "all"},
						"description": "How far back to look (default: last_90d)",
					},
				},
				"required": []string{"person_name"},
			},
		},
		{
			Name:        "get_team_context",
			Description: "Get an overview of all people reporting to a leader (including indirect reports). Returns each person with their pending tasks, recent feedbacks, and risk signals.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"leader_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the leader whose team to review",
					},
				},
				"required": []string{"leader_name"},
			},
		},
		{
			Name:        "search_memories",
			Description: "Search memories by keyword (FTS5 full-text search) or semantic meaning (vector search). Returns ranked results with snippets.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query — matches on content, about_person, and type",
					},
					"semantic": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, uses vector similarity search instead of keyword search",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Max results (default: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
	}
	return tools
}

// ============================================================
// Tool handlers
// ============================================================

// HandleIngest processes an ingest tool call.
// All content types — observations, feedback, 1:1 transcripts, voice notes —
// enter through this single tool. The LLM detects the content type and
// extracts entities accordingly. Feedback items are structured based on
// the configured feedback format (stop/start/continue, freeform, etc.).
func (s *Server) HandleIngest(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	modality, _ := args["modality"].(string)
	content, _ := args["content"].(string)
	filePath, _ := args["file_path"].(string)
	aboutPersonHint, _ := args["about_person"].(string)

	// Step 1: Normalize input to text based on modality
	var normalizedText string
	switch modality {
	case "text":
		normalizedText = content
	case "audio":
		// TODO: call transcription provider
		normalizedText = fmt.Sprintf("[transcription of %s — TODO: implement transcription provider]", filePath)
	case "image":
		// TODO: call OCR + VLM providers
		normalizedText = fmt.Sprintf("[OCR + description of %s — TODO: implement OCR/VLM providers]", filePath)
	case "video":
		// TODO: extract audio track + transcribe, extract key frames + describe
		normalizedText = fmt.Sprintf("[transcription + frame analysis of %s — TODO]", filePath)
	}

	// Step 2: Extract entities using LLM provider
	// Pass the feedback categories from config so the LLM knows how to
	// structure feedback items if it detects feedback content
	extractionCfg := providers.ExtractionConfig{
		FeedbackCategories: s.cfg.FeedbackCategoryIDs(),
		FeedbackEnabled:    s.cfg.Skills.Feedback,
	}
	extraction, err := s.llm.ExtractEntities(normalizedText, extractionCfg)
	if err != nil {
		extraction = &providers.EntityExtraction{
			MemoryType:  "observation",
			Summary:     normalizedText,
			AboutPerson: aboutPersonHint,
		}
	}

	// Use hint if LLM didn't detect about_person
	if extraction.AboutPerson == "" && aboutPersonHint != "" {
		extraction.AboutPerson = aboutPersonHint
	}

	// Step 3: Generate embedding
	embedding, _ := s.embedding.Embed(normalizedText)

	memID := generateID("mem")
	memType := extraction.MemoryType
	if memType == "" {
		memType = "observation"
	}

	// Steps 4-9 write the memory, its embedding and its graph in a single
	// transaction: either everything is stored or nothing is. Extraction and
	// embedding (above) stay outside so slow provider calls don't hold the
	// write lock.
	feedbackItemsCount := 0
	err = s.store.InTx(func(tx *sql.Tx, st *sqlite.Store) error {
		g := s.graph.WithTx(tx)

		// Step 4: Store memory in SQLite
		if err := st.InsertMemory(
			memID, memType, normalizedText, modality, "mcp", "", extraction.AboutPerson, 1.0,
		); err != nil {
			return fmt.Errorf("store memory: %w", err)
		}

		// Step 5: Store embedding vector
		if embedding != nil {
			if err := st.InsertVector(memID, embedding); err != nil {
				return fmt.Errorf("store vector: %w", err)
			}
		}

		// Step 6: Add persons to graph
		for _, p := range extraction.Persons {
			if _, err := ensurePerson(st, g, p.Name, p.Role); err != nil {
				return err
			}
		}

		// Step 7: Add memory node and edges to graph
		if err := g.AddNode("Memory", memID, map[string]interface{}{
			"type": memType, "content": normalizedText,
			"modality": modality, "created_at": time.Now().Format(time.RFC3339),
		}); err != nil {
			return fmt.Errorf("add memory node: %w", err)
		}

		aboutPersonID := ""
		if extraction.AboutPerson != "" {
			personID, err := ensurePerson(st, g, extraction.AboutPerson, "")
			if err != nil {
				return err
			}
			if err := g.AddEdge(memID, personID, "ABOUT", nil); err != nil {
				return err
			}
			aboutPersonID = personID
		}

		// Step 8: Add tasks to graph
		for _, t := range extraction.Tasks {
			taskID := generateID("task")
			if err := st.InsertTask(taskID, t.Description, t.Owner, "pending"); err != nil {
				return fmt.Errorf("store task: %w", err)
			}
			if err := g.AddNode("Task", taskID, map[string]interface{}{
				"description": t.Description, "owner": t.Owner, "status": "pending",
			}); err != nil {
				return fmt.Errorf("add task node: %w", err)
			}
			if err := g.AddEdge(taskID, memID, "DERIVED_FROM", nil); err != nil {
				return err
			}
		}

		// Step 9: If this is feedback, create feedback nodes and items
		// The feedback format is driven by config — not hardcoded in the tool
		if len(extraction.FeedbackItems) > 0 {
			fbID := generateID("fb")
			if err := g.AddNode("Feedback", fbID, map[string]interface{}{
				"type":          "detected",
				"format":        s.cfg.Feedback.Format,
				"created_at":    time.Now().Format(time.RFC3339),
				"source_memory": memID,
			}); err != nil {
				return fmt.Errorf("add feedback node: %w", err)
			}

			// Connect memory -> feedback
			if err := g.AddEdge(memID, fbID, "FORMALIZED_IN", nil); err != nil {
				return err
			}

			// Connect feedback -> about_person
			if aboutPersonID != "" {
				if err := g.AddEdge(fbID, aboutPersonID, "ABOUT", nil); err != nil {
					return err
				}
			}

			// Create feedback items based on configured categories
			for _, item := range extraction.FeedbackItems {
				itemID := generateID("fi")
				if err := g.AddNode("FeedbackItem", itemID, map[string]interface{}{
					"category": item.Category,
					"content":  item.Content,
				}); err != nil {
					return fmt.Errorf("add feedback item node: %w", err)
				}
				if err := g.AddEdge(fbID, itemID, "CONTAINS", nil); err != nil {
					return err
				}
				feedbackItemsCount++
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	// Build result
	result := map[string]interface{}{
		"memory_id":      memID,
		"memory_type":    memType,
		"about_person":   extraction.AboutPerson,
		"summary":        extraction.Summary,
		"persons_found":  len(extraction.Persons),
		"topics_found":   len(extraction.Topics),
		"tasks_found":    len(extraction.Tasks),
		"feedback_items": feedbackItemsCount,
		"status":         "stored",
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// HandleRecall processes a recall tool call
func (s *Server) HandleRecall(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	personName, _ := args["person_name"].(string)
	contextType, _ := args["context"].(string)
	timeRange, _ := args["time_range"].(string)

	if timeRange == "" {
		timeRange = "last_90d"
	}

	// Find the person
	personID, err := s.store.GetPersonByName(personName)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf("Person '%s' not found in the knowledge base. Use 'ingest' to add memories about this person first.", personName),
			}},
		}, nil
	}

	// Get full context from graph
	personCtx, err := s.graph.GetPersonContext(personID)
	if err != nil {
		return nil, fmt.Errorf("get person context: %w", err)
	}

	// Get pending tasks
	tasks, _ := s.store.GetPendingTasks(personID)

	// Search for related memories via FTS5
	ftsResults, _ := s.store.SearchFTS(personName, 10)

	// Assemble the briefing
	briefing := s.retriever.AssembleBriefing(personName, contextType, personCtx, tasks, ftsResults)

	briefingJSON, _ := json.MarshalIndent(briefing, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(briefingJSON)}},
	}, nil
}

// HandleGetTeamContext processes a get_team_context tool call
func (s *Server) HandleGetTeamContext(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	leaderName, _ := args["leader_name"].(string)

	leaderID, err := s.store.GetPersonByName(leaderName)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf("Leader '%s' not found in the knowledge base.", leaderName),
			}},
		}, nil
	}

	// Get team hierarchy from graph
	team, err := s.graph.GetTeamHierarchy(leaderID)
	if err != nil {
		return nil, fmt.Errorf("get team hierarchy: %w", err)
	}

	result := map[string]interface{}{
		"leader":    leaderName,
		"team_size": len(team),
		"members":   team,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// HandleSearchMemories processes a search_memories tool call
func (s *Server) HandleSearchMemories(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	query, _ := args["query"].(string)
	semantic, _ := args["semantic"].(bool)
	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	var results []map[string]interface{}
	var err error

	if semantic {
		queryVec, embErr := s.embedding.Embed(query)
		if embErr != nil {
			return nil, fmt.Errorf("embed query: %w", embErr)
		}
		results, err = s.store.SearchVector(queryVec, limit)
	} else {
		results, err = s.store.SearchFTS(query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	resultJSON, _ := json.MarshalIndent(map[string]interface{}{
		"query":   query,
		"mode":    map[bool]string{true: "semantic", false: "keyword"}[semantic],
		"count":   len(results),
		"results": results,
	}, "", "  ")

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// Close shuts down the server and persists data
func (s *Server) Close() error {
	if err := s.graph.Close(); err != nil {
		return err
	}
	return s.store.Close()
}

// generateID creates a unique ID with a prefix
func generateID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// ensurePerson returns the ID of the person with the given name,
// creating them in SQLite and in the graph if they don't exist yet
func ensurePerson(st *sqlite.Store, g graph.GraphEngine, name, role string) (string, error) {
	personID, err := st.GetPersonByName(name)
	if err == nil {
		return personID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("find person %q: %w", name, err)
	}
	personID = generateID("person")
	if err := st.UpsertPerson(personID, name, role, "", "", 0); err != nil {
		return "", fmt.Errorf("upsert person: %w", err)
	}
	if err := g.AddNode("Person", personID, map[string]interface{}{
		"name": name, "role": role,
	}); err != nil {
		return "", fmt.Errorf("add person node: %w", err)
	}
	return personID, nil
}

// Adapter returns the configured output adapter
func (s *Server) Adapter() providers.OutputAdapter {
	return s.adapter
}
