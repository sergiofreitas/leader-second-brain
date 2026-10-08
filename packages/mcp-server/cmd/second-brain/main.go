package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/mcpserver"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// version is set at build time: -ldflags "-X main.version=v0.2.0"
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			os.Exit(runInit(os.Args[2:]))
		case "version", "--version", "-version":
			fmt.Println("second-brain", version)
			return
		case "help", "--help", "-help", "-h":
			fmt.Print(usage)
			return
		}
	}
	serve(os.Args[1:])
}

const usage = `second-brain — memory for leadership (MCP server)

Usage:
  second-brain [--config PATH]   run the MCP server over stdio (what MCP hosts call)
  second-brain init [options]    write a config file from a built-in profile
  second-brain version           print the version

Run "second-brain init --help" for the init options.
`

// serve runs the MCP server over stdio
func serve(args []string) {
	flags := flag.NewFlagSet("second-brain", flag.ExitOnError)
	configPath := flags.String("config", "", "path to config.yaml (default: ~/.second-brain/config.yaml)")
	flags.Parse(args)

	// Without a config file the server runs with the defaults (local
	// storage, keyword search), so it works right after install
	path := *configPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	cfg, err := config.Load(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && *configPath == "":
		cfg = config.Default()
		log.Printf("No config at %s: using the defaults (run `second-brain init` to create one)", path)
	case err != nil:
		log.Fatalf("load config: %v", err)
	}

	log.Printf("Second Brain %s — profile: %s, graph: %s, transport: %s",
		version, cfg.Profile, cfg.Graph.Engine, cfg.Transport.Type)
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
			Version: version,
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
				"segments":     in.Segments,
				"occurred_at":  in.OccurredAt,
				"new_persons":  in.NewPersons,
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

	// --- rename_person ---
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "rename_person",
			Description: "Rename a known person, e.g. to their full name or to fix a typo. Updates the name everywhere it is stored: the person, the memories about them, the tasks they own and the feedbacks they gave. Use it instead of ingesting the new name, which would create a second person. It refuses a name that already belongs to someone else.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in renameArgs) (*mcp.CallToolResult, any, error) {
			result, err := sb.HandleRenamePerson(ctx, map[string]interface{}{
				"name":     in.Name,
				"new_name": in.NewName,
			})
			if err != nil {
				return nil, nil, err
			}
			return toCallToolResult(result), nil, nil
		},
	)

	// --- complete_task ---
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "complete_task",
			Description: "Mark a task as done, when the user says it was done (e.g. \"já fiz a avaliação do Evandro\"). Task ids are in the pending_tasks of recall and get_team_context. Done tasks leave the pending list and appear under completed_tasks in recall.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in completeTaskArgs) (*mcp.CallToolResult, any, error) {
			result, err := sb.HandleCompleteTask(ctx, map[string]interface{}{"task_id": in.TaskID})
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
			Description: "Review a leader's team (direct and indirect reports). For each person: pending tasks (with ids), last feedback with its items, last assessment, last memory, recurring topics and signals (no_feedback, no_recent_memory, stale_task). For the team: shared_topics — topics several people share, with who, which point to collective actions — and leader_pending_tasks, what the leader has to do. The leader can be named by part of the name.",
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
			Description: searchDescription(sb.SemanticSearchEnabled()),
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"query": in.Query,
				"terms": in.Terms,
				"mode":  in.Mode,
				"limit": float64(in.Limit),
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
	Modality    string   `json:"modality" jsonschema:"where the content came from: text, audio, image or video"`
	Content     string   `json:"content" jsonschema:"the text to remember, in the user's own language: the note itself, or your transcription/description of the audio, image or video"`
	FilePath    string   `json:"file_path,omitempty" jsonschema:"path of the original media file, kept as a reference"`
	AboutPerson string   `json:"about_person,omitempty" jsonschema:"the person this memory is mainly about"`
	MemoryType  string   `json:"memory_type,omitempty" jsonschema:"observation, feedback, one_on_one, assessment or voice_note (default: observation)"`
	Summary     string   `json:"summary,omitempty" jsonschema:"one-sentence summary of the memory"`
	Segments    []string `json:"segments,omitempty" jsonschema:"for long content (transcripts of meetings, long voice notes): the content split into consecutive passages by subject, a few paragraphs each, together covering all of it"`
	OccurredAt  string   `json:"occurred_at,omitempty" jsonschema:"the day it happened, YYYY-MM-DD, when it isn't today (a 1:1, an assessment or a feedback recorded later); briefings order and filter by it"`

	Persons       []personArg       `json:"persons,omitempty" jsonschema:"everyone mentioned, with their role when it is stated"`
	Topics        []string          `json:"topics,omitempty" jsonschema:"short themes, reused across memories so patterns emerge (e.g. microgestão, delegação, autonomia)"`
	Tasks         []taskArg         `json:"tasks,omitempty" jsonschema:"follow-ups or commitments that came out of this memory"`
	Relationships []relationshipArg `json:"relationships,omitempty" jsonschema:"reporting lines and other person-to-person relationships stated in the content"`
	FeedbackItems []feedbackItemArg `json:"feedback_items,omitempty" jsonschema:"feedback about someone, split into the configured categories"`
	FeedbackFrom  string            `json:"feedback_from,omitempty" jsonschema:"who gave the feedback, when it was relayed by someone else"`
	NewPersons    []string          `json:"new_persons,omitempty" jsonschema:"names that are new people although ingest said they may be someone already stored, after the user confirmed they are someone else"`
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
		"For long content (a meeting transcript, a long voice note), also pass segments: the same text split by subject into consecutive passages of a few paragraphs, so search can find each subject.",
		"Then extract what the content states, without inventing:",
		"- about_person: who the memory is mainly about.",
		"- persons: everyone mentioned (with role when stated). Call list_people first and reuse the stored names for people already known. A new name that looks like a stored one (\"Osmar\" when \"Osmar de Morais Junior\" is stored) is refused: ask the user whether it's the same person (then use the stored name) or someone else (then pass it in new_persons).",
		"- topics: a few short themes, reusing the same words across memories (they are counted to spot patterns).",
		"- tasks: follow-ups, with owner (who does it) and about_person (who it concerns).",
		"- relationships: reporting lines (REPORTS_TO) and mentoring (MENTORS) stated in the content.",
		"- memory_type feedback with feedback_items when someone's behavior is assessed, and feedback_from when the feedback was relayed by someone else.",
		"- occurred_at: the day it happened (YYYY-MM-DD) when the user says it wasn't today, e.g. \"a avaliação foi em 02/09\". Don't put the date only in content.",
		feedback,
		"If the call is rejected, the error says which field to fix.",
	}, "\n")
}

type recallArgs struct {
	PersonName string `json:"person_name" jsonschema:"name of the person to get context for"`
	Context    string `json:"context,omitempty" jsonschema:"the situation: 1:1, pdi, feedback, team_review, progression, or general"`
	TimeRange  string `json:"time_range,omitempty" jsonschema:"how far back to look, by when things happened: last_30d, last_90d, last_year, or all (default: last_90d); pending tasks are always listed"`
}

type teamArgs struct {
	LeaderName string `json:"leader_name" jsonschema:"name of the leader whose team to review"`
}

type renameArgs struct {
	Name    string `json:"name" jsonschema:"the person's current name, as stored (see list_people)"`
	NewName string `json:"new_name" jsonschema:"the new name, e.g. the full name"`
}

type completeTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"the task's id, from pending_tasks in recall or get_team_context"`
}

type searchArgs struct {
	Query string   `json:"query" jsonschema:"what to look for, in the user's words"`
	Terms []string `json:"terms,omitempty" jsonschema:"extra keywords in the language of the memories: synonyms, other inflections, prefixes ending in * (e.g. microger*, deleg*), related expressions"`
	Mode  string   `json:"mode,omitempty" jsonschema:"hybrid (keyword + meaning), keyword or semantic; default: hybrid when semantic search is enabled, otherwise keyword"`
	Limit int      `json:"limit,omitempty" jsonschema:"max results (default: 10)"`
}

// searchDescription tells the host how to search, depending on whether
// semantic search is enabled
func searchDescription(semantic bool) string {
	lines := []string{
		"Search the leader's memories. Returns each matching memory once, best first, with an excerpt of the matching part (for long transcripts, the relevant passage) and how it was found.",
		"Always pass terms: the keyword search matches exact words and doesn't know that \"microgestão\", \"microgerenciando\" and \"não delega\" are related, so add 5 to 15 synonyms, other inflections, prefixes ending in * (microger*, deleg*) and related expressions, in the language of the memories.",
	}
	if semantic {
		lines = append(lines, "Semantic search is enabled: the default mode, hybrid, also matches by meaning. It always returns the nearest memories, so results found only by meaning (matched_by: semantic) with a low similarity may be unrelated — judge them before using them. If the result has index_status, recent memories may not be searchable by meaning yet; if it has semantic_error, only keywords were used.")
	} else {
		lines = append(lines, "Semantic search is not configured on this server, so search is by keyword only: the terms are what make it find related memories.")
	}
	return strings.Join(lines, "\n")
}

// toCallToolResult converts our internal ToolResult to the MCP SDK's CallToolResult
func toCallToolResult(r *mcpserver.ToolResult) *mcp.CallToolResult {
	content := make([]mcp.Content, len(r.Content))
	for i, c := range r.Content {
		content[i] = &mcp.TextContent{Text: c.Text}
	}
	return &mcp.CallToolResult{Content: content}
}
