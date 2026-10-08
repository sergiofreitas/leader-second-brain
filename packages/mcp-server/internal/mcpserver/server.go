package mcpserver

import (
	"context"
	"encoding/json"
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
// MCP tool results
// ============================================================

type ToolResult struct {
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ============================================================
// Tool handlers
// ============================================================

// HandleRecall processes a recall tool call
func (s *Server) HandleRecall(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	personName, _ := args["person_name"].(string)
	contextType, _ := args["context"].(string)
	timeRange, _ := args["time_range"].(string)

	if timeRange == "" {
		timeRange = "last_90d"
	}

	// Find the person (ignoring case and accents) and use the stored name
	personID, storedName, err := s.store.FindPerson(personName)
	if err == nil {
		personName = storedName
	}
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

	// Pending tasks concerning this person (Task -[TARGETS]-> Person),
	// whoever has to do them
	tasks := personCtx.Tasks

	// Search for related memories via FTS5
	ftsResults, _ := s.store.SearchFTS(personName, 10)

	// Assemble the briefing
	briefing := s.retriever.AssembleBriefing(personName, contextType, personCtx, tasks, ftsResults)

	briefingJSON, _ := json.MarshalIndent(briefing, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(briefingJSON)}},
	}, nil
}

// HandleListPeople processes a list_people tool call: everyone in the
// knowledge base, so the host can reuse their exact names when ingesting
func (s *Server) HandleListPeople(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	people, err := s.store.ListPersons()
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	type entry struct {
		Name string `json:"name"`
		Role string `json:"role,omitempty"`
	}
	entries := make([]entry, len(people))
	for i, p := range people {
		entries[i] = entry{p.Name, p.Role}
	}
	resultJSON, _ := json.MarshalIndent(map[string]interface{}{
		"count":  len(entries),
		"people": entries,
	}, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
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
	if l, ok := args["limit"].(float64); ok && l > 0 {
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

// Adapter returns the configured output adapter
func (s *Server) Adapter() providers.OutputAdapter {
	return s.adapter
}
