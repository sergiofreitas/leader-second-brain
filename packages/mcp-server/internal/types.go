package internal

import "time"

// ============================================================
// Core domain types — shared across all layers
// ============================================================

// Person represents anyone in the hierarchy
type Person struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"` // head | leader | report
	Area      string    `json:"area"`
	Track     string    `json:"track"` // technical | leadership
	JobLevel  int       `json:"job_level"`
	CreatedAt time.Time `json:"created_at"`
}

// Memory is the atomic unit of captured information
type Memory struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`         // conversation | voice_note | observation | image_note | video_note | document
	Content    string    `json:"content"`      // normalized text (transcribed / OCR'd / typed)
	Modality   string    `json:"modality"`     // text | audio | image | video
	Source     string    `json:"source"`       // app | voice_note | screenshot | meeting | manual
	RawFileRef string    `json:"raw_file_ref"` // path to original media (cold storage)
	CreatedAt  time.Time `json:"created_at"`
	Confidence float64   `json:"confidence"`
}

// Task is a derived action
type Task struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Owner       string     `json:"owner"`  // person ID
	Status      string     `json:"status"` // pending | in_progress | done
	CreatedAt   time.Time  `json:"created_at"`
	Deadline    *time.Time `json:"deadline,omitempty"`
}

// Topic is a reusable tag for memories
type Topic struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"` // technical | behavioral | interpersonal | operational
}

// ============================================================
// Graph types — node and edge abstractions
// ============================================================

// NodeLabel identifies a node type in the graph
type NodeLabel string

const (
	NodePerson     NodeLabel = "Person"
	NodeMemory     NodeLabel = "Memory"
	NodeTask       NodeLabel = "Task"
	NodeTopic      NodeLabel = "Topic"
	NodeAssessment NodeLabel = "Assessment"
	NodeFeedback   NodeLabel = "Feedback"
	NodeSkill      NodeLabel = "Skill"
	NodeGoal       NodeLabel = "Goal"
)

// EdgeLabel identifies a relationship type
type EdgeLabel string

const (
	EdgeReportsTo      EdgeLabel = "REPORTS_TO"
	EdgeParticipatedIn EdgeLabel = "PARTICIPATED_IN"
	EdgeAbout          EdgeLabel = "ABOUT"
	EdgeMentions       EdgeLabel = "MENTIONS"
	EdgeHasTopic       EdgeLabel = "HAS_TOPIC"
	EdgeDerivedFrom    EdgeLabel = "DERIVED_FROM"
	EdgeTargets        EdgeLabel = "TARGETS"
	EdgeMentors        EdgeLabel = "MENTORS"
	EdgeHasStrength    EdgeLabel = "HAS_STRENGTH"
	EdgeNeedsDev       EdgeLabel = "NEEDS_DEVELOPMENT"
	EdgeDemonstrated   EdgeLabel = "DEMONSTRATED"
	EdgeAssessed       EdgeLabel = "ASSESSED"
	EdgeGave           EdgeLabel = "GAVE"
	EdgeContains       EdgeLabel = "CONTAINS"
)

// GraphNode is a generic node in the graph
type GraphNode struct {
	ID    string                 `json:"id"`
	Label NodeLabel              `json:"label"`
	Props map[string]interface{} `json:"props"`
}

// GraphEdge is a generic edge in the graph
type GraphEdge struct {
	From  string                 `json:"from"`
	To    string                 `json:"to"`
	Label EdgeLabel              `json:"label"`
	Props map[string]interface{} `json:"props,omitempty"`
}

// ============================================================
// Ingestion types — what comes in from capture
// ============================================================

// IngestInput is what the MCP tool receives
type IngestInput struct {
	Modality string `json:"modality"`  // text | audio | image | video
	Content  string `json:"content"`   // text content (for text modality)
	FilePath string `json:"file_path"` // path to media file (for audio/image/video)
	RawText  string `json:"raw_text"`  // pre-existing text (e.g., pasted transcript)
}

// IngestResult is what the pipeline returns after processing
type IngestResult struct {
	Memory        *Memory     `json:"memory"`
	Persons       []Person    `json:"persons"`       // extracted persons
	Topics        []Topic     `json:"topics"`        // extracted topics
	Tasks         []Task      `json:"tasks"`         // derived tasks
	Relationships []GraphEdge `json:"relationships"` // edges to create
	AboutPerson   string      `json:"about_person"`  // who is the subject
	Summary       string      `json:"summary"`
}

// ============================================================
// Retrieval types — what recall returns
// ============================================================

// RecallQuery is a request for context
type RecallQuery struct {
	PersonName   string   `json:"person_name,omitempty"`
	Context      string   `json:"context,omitempty"`       // "1:1" | "pdi" | "feedback" | "team_review"
	TimeRange    string   `json:"time_range,omitempty"`    // "last_30d" | "last_90d" | "last_year" | "all"
	IncludeTypes []string `json:"include_types,omitempty"` // filter by memory types
}

// RecallResult is the assembled context
type RecallResult struct {
	Person      *Person             `json:"person"`
	Hierarchy   []Person            `json:"hierarchy"` // reports and managers
	Memories    []MemoryWithMeta    `json:"memories"`
	Tasks       []Task              `json:"tasks"`
	Feedbacks   []FeedbackSummary   `json:"feedbacks,omitempty"`
	Assessments []AssessmentSummary `json:"assessments,omitempty"`
	Patterns    []PatternSummary    `json:"patterns,omitempty"`
	Signals     []SignalSummary     `json:"signals,omitempty"`
}

type MemoryWithMeta struct {
	Memory       Memory   `json:"memory"`
	Topics       []Topic  `json:"topics"`
	Participants []Person `json:"participants"`
	Score        float64  `json:"score"` // relevance score from hybrid retrieval
}

type FeedbackSummary struct {
	ID          string   `json:"id"`
	FromPerson  string   `json:"from_person"`
	AboutPerson string   `json:"about_person"`
	Date        string   `json:"date"`
	Format      string   `json:"format"`
	Items       []string `json:"items"`
}

type AssessmentSummary struct {
	ID          string   `json:"id"`
	Date        string   `json:"date"`
	Track       string   `json:"track"`
	Level       int      `json:"level"`
	TargetLevel int      `json:"target_level"`
	Gaps        []string `json:"gaps"`
}

type PatternSummary struct {
	Description string  `json:"description"`
	Trend       string  `json:"trend"`
	Confidence  float64 `json:"confidence"`
}

type SignalSummary struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Date     string `json:"date"`
}
