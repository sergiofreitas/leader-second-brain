package mcpserver

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/adapters"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/chunk"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/indexer"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers/embedding"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/retrieve"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/graph"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// Server holds all dependencies for the MCP server
type Server struct {
	cfg       *config.Config
	store     *sqlite.Store
	graph     graph.GraphEngine
	embedding providers.EmbeddingProvider // nil: semantic search disabled
	indexer   *indexer.Indexer            // embeds chunks in the background (nil without embedding)
	llm       providers.LLMProvider
	retriever *retrieve.HybridRetriever
	adapter   providers.OutputAdapter

	stopIndexer context.CancelFunc
	indexerDone chan struct{}
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

	// Memories stored before chunking existed get their chunks now, so they
	// are indexed like new ones
	if err := backfillChunks(store); err != nil {
		store.Close()
		return nil, fmt.Errorf("backfill chunks: %w", err)
	}

	// Entity extraction is done by the MCP host; the LLM stub is only a
	// fallback when the host sends no extraction
	llm := &providers.StubLLM{}

	s := &Server{
		cfg:       cfg,
		store:     store,
		graph:     graphEngine,
		llm:       llm,
		retriever: retrieve.NewHybridRetriever(store, graphEngine, nil),
		adapter:   adapters.NewAdapter(cfg.Feedback.TargetSystem),
	}

	// Semantic search only when an embedding provider is configured: by
	// default nothing leaves the machine through the server
	embedder, err := embedding.New(cfg.Providers.Embedding)
	if err != nil {
		store.Close()
		return nil, err
	}
	if embedder == nil {
		log.Printf("Semantic search: disabled (no embedding provider configured)")
		return s, nil
	}
	if err := s.EnableSemanticSearch(embedder); err != nil {
		store.Close()
		return nil, fmt.Errorf("enable semantic search: %w", err)
	}
	return s, nil
}

// EnableSemanticSearch uses embedder for semantic search and starts the
// background indexer that embeds every chunk without a vector for its model
func (s *Server) EnableSemanticSearch(embedder providers.EmbeddingProvider) error {
	if s.indexer != nil {
		return fmt.Errorf("semantic search is already enabled")
	}
	ix, err := indexer.New(s.store, embedder, 32)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.embedding, s.indexer = embedder, ix
	s.retriever = retrieve.NewHybridRetriever(s.store, s.graph, embedder)
	s.stopIndexer, s.indexerDone = cancel, make(chan struct{})
	go func() {
		defer close(s.indexerDone)
		ix.Run(ctx)
	}()
	log.Printf("Semantic search: enabled with %s", ix.Model())
	return nil
}

// backfillChunks splits the memories that have no chunks yet
func backfillChunks(store *sqlite.Store) error {
	memories, err := store.MemoriesWithoutChunks()
	if err != nil || len(memories) == 0 {
		return err
	}
	return store.InTx(func(_ *sql.Tx, st *sqlite.Store) error {
		for _, m := range memories {
			if err := st.InsertChunks(m.ID, chunk.Split(m.Content, nil)); err != nil {
				return fmt.Errorf("memory %s: %w", m.ID, err)
			}
		}
		return nil
	})
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
	start, err := rangeStart(timeRange, time.Now())
	if err != nil {
		return nil, err
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

	// Memories and feedbacks of the time range, by when they happened.
	// Pending tasks (Task -[TARGETS]-> Person, whoever has to do them) are
	// listed whatever their age: they are still open.
	personCtx.Memories = since(personCtx.Memories, start)
	personCtx.Feedbacks = since(personCtx.Feedbacks, start)
	tasks := personCtx.Tasks

	// Search for related memories via FTS5
	ftsResults, _ := s.store.SearchFTS(personName, nil, 10)
	ftsResults = since(ftsResults, start)

	// Assemble the briefing
	briefing := s.retriever.AssembleBriefing(personName, contextType, personCtx, tasks, ftsResults)
	briefing["time_range"] = timeRange

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

// Close shuts down the server and persists data
func (s *Server) Close() error {
	// Stop the indexer before the database goes away; a batch in flight
	// is simply retried on the next start
	if s.stopIndexer != nil {
		s.stopIndexer()
		<-s.indexerDone
	}
	if err := s.graph.Close(); err != nil {
		return err
	}
	return s.store.Close()
}

// now is the clock behind generateID; tests replace it to freeze time
var now = time.Now

// generateID creates a unique ID with a prefix. The timestamp keeps IDs
// roughly in creation order; the random suffix keeps them unique when the
// clock doesn't advance between calls (on Windows it ticks every ~100ns or
// coarser, so the people of one ingest used to get the same ID).
func generateID(prefix string) string {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return fmt.Sprintf("%s_%d_%s", prefix, now().UnixNano(), hex.EncodeToString(suffix[:]))
}

// Adapter returns the configured output adapter
func (s *Server) Adapter() providers.OutputAdapter {
	return s.adapter
}
