package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/mcpserver"
)

func main() {
	configPath := flag.String("config", "", "Path to config.yaml (default: ~/.second-brain/config.yaml)")
	flag.Parse()

	// Resolve config path
	path := *configPath
	if path == "" {
		path = config.DefaultConfigPath()
	}

	// Load config
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Config not found at %s. Copy a profile from configs/ to get started.", path)
		}
		log.Fatalf("load config: %v", err)
	}

	log.Printf("Second Brain — profile: %s, graph: %s, transport: %s",
		cfg.Profile, cfg.Graph.Engine, cfg.Transport.Type)
	log.Printf("Storage: %s", cfg.Storage.SQLite.Path)

	// Initialize the Second Brain server (graph + sqlite + providers)
	sb, err := mcpserver.New(cfg)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}
	defer sb.Close()

	// Create the MCP server
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "second-brain",
			Version: "0.1.0",
		},
		nil,
	)

	// ============================================================
	// Register tools
	// ============================================================

	// --- ingest ---
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "ingest",
			Description: "Capture a memory from text, audio, image, or video. The system transcribes/OCR's/describes the input, extracts entities (persons, topics, tasks), detects content type (observation, feedback, 1:1, etc.), and stores everything in the local knowledge graph + vector index. Feedback items are structured according to the configured feedback format.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in ingestArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"modality":     in.Modality,
				"content":       in.Content,
				"file_path":     in.FilePath,
				"about_person":  in.AboutPerson,
			}
			result, err := sb.HandleIngest(ctx, argsMap)
			if err != nil {
				return nil, nil, err
			}
			return toCallToolResult(result), nil, nil
		},
	)

	// --- recall ---
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "recall",
			Description: "Retrieve context about a person for a specific situation (1:1, PDI, feedback, team review). Returns memories, tasks, feedbacks, assessments, patterns, and signals from the knowledge graph.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in recallArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"person_name": in.PersonName,
				"context":     in.Context,
				"time_range":  in.TimeRange,
			}
			result, err := sb.HandleRecall(ctx, argsMap)
			if err != nil {
				return nil, nil, err
			}
			return toCallToolResult(result), nil, nil
		},
	)

	// --- get_team_context ---
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "get_team_context",
			Description: "Get an overview of all people reporting to a leader (including indirect reports). Returns each person with their pending tasks, recent feedbacks, and risk signals.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in teamArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"leader_name": in.LeaderName,
			}
			result, err := sb.HandleGetTeamContext(ctx, argsMap)
			if err != nil {
				return nil, nil, err
			}
			return toCallToolResult(result), nil, nil
		},
	)

	// --- search_memories ---
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "search_memories",
			Description: "Search memories by keyword (FTS5 full-text search) or semantic meaning (vector search). Returns ranked results with snippets.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"query":    in.Query,
				"semantic": in.Semantic,
				"limit":    in.Limit,
			}
			result, err := sb.HandleSearchMemories(ctx, argsMap)
			if err != nil {
				return nil, nil, err
			}
			return toCallToolResult(result), nil, nil
		},
	)

	// Run the server over stdio
	log.Printf("Second Brain MCP server ready — %d tools registered", len(sb.GetToolDefinitions()))

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// ============================================================
// Tool argument structs — the Go MCP SDK infers JSON schema
// from these struct tags (json + jsonschema)
// ============================================================

type ingestArgs struct {
	Modality    string `json:"modality" jsonschema:"the type of input: text, audio, image, or video. Required."`
	Content     string `json:"content" jsonschema:"text content (for text modality, or pre-transcribed text)"`
	FilePath    string `json:"file_path" jsonschema:"path to media file (for audio/image/video modality)"`
	AboutPerson string `json:"about_person" jsonschema:"who is this memory about? (person name, optional)"`
}

type recallArgs struct {
	PersonName string `json:"person_name" jsonschema:"name of the person to get context for. Required."`
	Context    string `json:"context" jsonschema:"the situation: 1:1, pdi, feedback, team_review, progression, or general"`
	TimeRange  string `json:"time_range" jsonschema:"how far back to look: last_30d, last_90d, last_year, or all (default: last_90d)"`
}

type teamArgs struct {
	LeaderName string `json:"leader_name" jsonschema:"name of the leader whose team to review. Required."`
}

type searchArgs struct {
	Query    string `json:"query" jsonschema:"search query — matches on content, about_person, and type. Required."`
	Semantic bool   `json:"semantic" jsonschema:"if true, uses vector similarity search instead of keyword search"`
	Limit    int    `json:"limit" jsonschema:"max results (default: 10)"`
}

// toCallToolResult converts our internal ToolResult to the MCP SDK's CallToolResult
func toCallToolResult(r *mcpserver.ToolResult) *mcp.CallToolResult {
	content := make([]mcp.Content, len(r.Content))
	for i, c := range r.Content {
		content[i] = &mcp.TextContent{Text: c.Text}
	}
	return &mcp.CallToolResult{Content: content}
}
