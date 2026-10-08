package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/mcpserver"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
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
			Description: ingestDescription(cfg),
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in ingestArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"modality":     in.Modality,
				"content":      in.Content,
				"file_path":    in.FilePath,
				"about_person": in.AboutPerson,
				"extraction":   in.extraction(),
			}
			result, err := sb.HandleIngest(ctx, argsMap)
			if err != nil {
				return nil, nil, err
			}
			return toCallToolResult(result), nil, nil
		},
	)

	// --- list_people ---
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "list_people",
			Description: "List everyone in the knowledge base (name and role). Call it before ingest to reuse the exact names already stored, so the same person isn't recorded under two names.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
			result, err := sb.HandleListPeople(ctx, nil)
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
				"limit":    float64(in.Limit),
			}
			result, err := sb.HandleSearchMemories(ctx, argsMap)
			if err != nil {
				return nil, nil, err
			}
			return toCallToolResult(result), nil, nil
		},
	)

	// Run the server over stdio
	log.Printf("Second Brain MCP server ready")

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// ============================================================
// Tool argument structs — the Go MCP SDK infers JSON schema
// from these struct tags (json + jsonschema)
// ============================================================

// Fields without omitempty are required in the inferred schema.

type ingestArgs struct {
	Modality    string `json:"modality" jsonschema:"where the content came from: text, audio, image or video"`
	Content     string `json:"content" jsonschema:"the text to remember, in the user's own language: the note itself, or your transcription/description of the audio, image or video"`
	FilePath    string `json:"file_path,omitempty" jsonschema:"path of the original media file, kept as a reference"`
	AboutPerson string `json:"about_person,omitempty" jsonschema:"the person this memory is mainly about"`
	MemoryType  string `json:"memory_type,omitempty" jsonschema:"observation, feedback, one_on_one, assessment or voice_note (default: observation)"`
	Summary     string `json:"summary,omitempty" jsonschema:"one-sentence summary of the memory"`

	Persons       []personArg       `json:"persons,omitempty" jsonschema:"everyone mentioned, with their role when it is stated"`
	Topics        []string          `json:"topics,omitempty" jsonschema:"short themes, reused across memories so patterns emerge (e.g. microgestão, delegação, autonomia)"`
	Tasks         []taskArg         `json:"tasks,omitempty" jsonschema:"follow-ups or commitments that came out of this memory"`
	Relationships []relationshipArg `json:"relationships,omitempty" jsonschema:"reporting lines and other person-to-person relationships stated in the content"`
	FeedbackItems []feedbackItemArg `json:"feedback_items,omitempty" jsonschema:"feedback about someone, split into the configured categories"`
	FeedbackFrom  string            `json:"feedback_from,omitempty" jsonschema:"who gave the feedback, when it was relayed by someone else"`
}

type personArg struct {
	Name string `json:"name" jsonschema:"the person's name, as stored if they are already known (see list_people)"`
	Role string `json:"role,omitempty" jsonschema:"their role, if stated (e.g. Tech Lead, dev júnior)"`
}

type taskArg struct {
	Description string `json:"description" jsonschema:"what has to be done"`
	Owner       string `json:"owner,omitempty" jsonschema:"who has to do it"`
	AboutPerson string `json:"about_person,omitempty" jsonschema:"who it concerns (default: the memory's about_person)"`
}

type relationshipArg struct {
	From string `json:"from" jsonschema:"person name"`
	To   string `json:"to" jsonschema:"person name"`
	Type string `json:"type" jsonschema:"REPORTS_TO (from reports to to), MENTORS (from mentors to) or WORKS_WITH"`
}

type feedbackItemArg struct {
	Category    string `json:"category" jsonschema:"one of the configured feedback categories"`
	Content     string `json:"content" jsonschema:"the feedback point, written so it can be shared with the person"`
	AboutPerson string `json:"about_person,omitempty" jsonschema:"who the feedback is about (default: the memory's about_person)"`
}

// extraction converts the host's extracted entities to the provider type,
// or returns nil when the host sent none (the server then uses its own LLM)
func (a ingestArgs) extraction() *providers.EntityExtraction {
	if a.MemoryType == "" && a.Summary == "" && a.FeedbackFrom == "" && len(a.Persons) == 0 &&
		len(a.Topics) == 0 && len(a.Tasks) == 0 && len(a.Relationships) == 0 && len(a.FeedbackItems) == 0 {
		return nil
	}
	ex := &providers.EntityExtraction{
		MemoryType:   a.MemoryType,
		Summary:      a.Summary,
		AboutPerson:  a.AboutPerson,
		Topics:       a.Topics,
		FeedbackFrom: a.FeedbackFrom,
	}
	for _, p := range a.Persons {
		ex.Persons = append(ex.Persons, providers.ExtractedPerson{Name: p.Name, Role: p.Role})
	}
	for _, t := range a.Tasks {
		ex.Tasks = append(ex.Tasks, providers.ExtractedTask{Description: t.Description, Owner: t.Owner, AboutPerson: t.AboutPerson})
	}
	for _, r := range a.Relationships {
		ex.Relationships = append(ex.Relationships, providers.ExtractedRel{From: r.From, To: r.To, Type: r.Type})
	}
	for _, f := range a.FeedbackItems {
		ex.FeedbackItems = append(ex.FeedbackItems, providers.ExtractedFeedbackItem{Category: f.Category, Content: f.Content, AboutPerson: f.AboutPerson})
	}
	return ex
}

// ingestDescription tells the host how to extract entities, including the
// feedback categories configured for this organization
func ingestDescription(cfg *config.Config) string {
	categories := make([]string, len(cfg.Feedback.Categories))
	for i, c := range cfg.Feedback.Categories {
		categories[i] = fmt.Sprintf("%s (%s)", c.ID, c.Label)
	}
	feedback := "Feedback categories: none configured."
	if len(categories) > 0 {
		feedback = "Feedback categories (format " + cfg.Feedback.Format + "): " + strings.Join(categories, ", ") + "."
	}
	return strings.Join([]string{
		"Store a memory in the leader's second brain: an observation, a conversation, a 1:1, a feedback, a voice note.",
		"You do the understanding; the server only stores. Pass the text in content, in the user's own language and with its details.",
		"For audio, image or video, transcribe or describe it yourself and pass that text in content (the server doesn't process media).",
		"Then extract what the content states, without inventing:",
		"- about_person: who the memory is mainly about.",
		"- persons: everyone mentioned (with role when stated). Call list_people first and reuse the stored names for people already known.",
		"- topics: a few short themes, reusing the same words across memories (they are counted to spot patterns).",
		"- tasks: follow-ups, with owner (who does it) and about_person (who it concerns).",
		"- relationships: reporting lines (REPORTS_TO) and mentoring (MENTORS) stated in the content.",
		"- memory_type feedback with feedback_items when someone's behavior is assessed, and feedback_from when the feedback was relayed by someone else.",
		feedback,
		"If the call is rejected, the error says which field to fix.",
	}, "\n")
}

type recallArgs struct {
	PersonName string `json:"person_name" jsonschema:"name of the person to get context for"`
	Context    string `json:"context,omitempty" jsonschema:"the situation: 1:1, pdi, feedback, team_review, progression, or general"`
	TimeRange  string `json:"time_range,omitempty" jsonschema:"how far back to look: last_30d, last_90d, last_year, or all (default: last_90d)"`
}

type teamArgs struct {
	LeaderName string `json:"leader_name" jsonschema:"name of the leader whose team to review"`
}

type searchArgs struct {
	Query    string `json:"query" jsonschema:"search query — matches on content, about_person, and type"`
	Semantic bool   `json:"semantic,omitempty" jsonschema:"if true, uses vector similarity search instead of keyword search"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max results (default: 10)"`
}

// toCallToolResult converts our internal ToolResult to the MCP SDK's CallToolResult
func toCallToolResult(r *mcpserver.ToolResult) *mcp.CallToolResult {
	content := make([]mcp.Content, len(r.Content))
	for i, c := range r.Content {
		content[i] = &mcp.TextContent{Text: c.Text}
	}
	return &mcp.CallToolResult{Content: content}
}
