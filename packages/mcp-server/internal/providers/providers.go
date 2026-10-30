package providers

// ============================================================
// Provider interfaces — pluggable AI capabilities
// Each can be backed by local (Whisper, Tesseract, Ollama,
// sentence-transformers) or cloud (OpenAI, Anthropic, Google, Toqan)
// depending on configuration.
// ============================================================

// TranscriptionProvider converts audio to text
type TranscriptionProvider interface {
	Transcribe(audioPath string) (*TranscriptionResult, error)
}

type TranscriptionResult struct {
	Text     string
	Language string
	Duration float64 // seconds
	Segments []TranscriptionSegment
}

type TranscriptionSegment struct {
	Speaker string
	Start   float64
	End     float64
	Text    string
}

// OCRProvider extracts text from images
type OCRProvider interface {
	Extract(imagePath string) (*OCRResult, error)
}

type OCRResult struct {
	Text       string
	Language   string
	Confidence float64
}

// VLMProvider describes images visually (captioning, scene understanding)
type VLMProvider interface {
	Describe(imagePath string, prompt string) (string, error)
}

// EmbeddingProvider generates vector embeddings for semantic search
type EmbeddingProvider interface {
	// Embed embeds a search query
	Embed(text string) ([]float32, error)
	// EmbedBatch embeds documents (memory chunks). Some models embed
	// queries and documents differently (e.g. e5's "query: "/"passage: ").
	EmbedBatch(texts []string) ([][]float32, error)
	Dimensions() int
	// Model identifies the model (and provider) that produced a vector.
	// Vectors from different models aren't comparable: changing it makes
	// every chunk be embedded again.
	Model() string
}

// LLMProvider does entity extraction, summarization, classification
// This is the LLM of PROCESSING, not the LLM of CONVERSATION (which is the MCP host's)
type LLMProvider interface {
	Complete(prompt string, systemPrompt string) (string, error)
	ExtractEntities(text string, config ExtractionConfig) (*EntityExtraction, error)
}

// ExtractionConfig passes domain configuration to the LLM so it knows
// what to extract and how to structure it — without hardcoding formats
// in the tool interface.
type ExtractionConfig struct {
	FeedbackCategories []string // e.g., ["stop", "start", "continue"] or ["general"]
	FeedbackEnabled    bool
}

// EntityExtraction is the structured output from the LLM after processing raw input.
// The LLM detects what kind of content this is (observation, feedback, 1:1, etc.)
// and extracts entities accordingly.
type EntityExtraction struct {
	MemoryType    string                  `json:"memory_type"` // observation | feedback | one_on_one | assessment | voice_note
	Summary       string                  `json:"summary"`
	AboutPerson   string                  `json:"about_person"`
	Persons       []ExtractedPerson       `json:"persons"`
	Topics        []string                `json:"topics"`
	Tasks         []ExtractedTask         `json:"tasks"`
	Relationships []ExtractedRel          `json:"relationships"`
	FeedbackItems []ExtractedFeedbackItem `json:"feedback_items,omitempty"`
	// FeedbackFrom is who gave the feedback, when it was relayed by someone
	// else (e.g. a report talking about their manager)
	FeedbackFrom string `json:"feedback_from,omitempty"`
	// Assessment is a structured score of the about_person, by criterion
	Assessment *ExtractedAssessment `json:"assessment,omitempty"`
}

// MemoryTypes are the accepted values of EntityExtraction.MemoryType
var MemoryTypes = []string{"observation", "feedback", "one_on_one", "weekly", "skip_level", "assessment", "voice_note"}

// PersonRelationshipTypes are the accepted person-to-person relationship types
var PersonRelationshipTypes = []string{"REPORTS_TO", "MENTORS", "WORKS_WITH"}

type ExtractedPerson struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

type ExtractedTask struct {
	Description string `json:"description"`
	Owner       string `json:"owner"`                  // who has to do it (optional)
	AboutPerson string `json:"about_person,omitempty"` // who it concerns (defaults to the memory's about_person)
	DueDate     string `json:"due_date,omitempty"`     // YYYY-MM-DD (optional)
}

type ExtractedRel struct {
	From string `json:"from"` // person name
	To   string `json:"to"`   // person name
	Type string `json:"type"` // one of PersonRelationshipTypes
}

// ExtractedFeedbackItem is a single feedback item detected by the LLM.
// Categories come from the configured feedback format — not hardcoded.
type ExtractedFeedbackItem struct {
	Category    string `json:"category"` // matches config FeedbackCategoryIDs
	Content     string `json:"content"`
	AboutPerson string `json:"about_person,omitempty"`
	// The parts of the item, when it was built from them (all optional)
	Situation    string `json:"situation,omitempty"`
	Behavior     string `json:"behavior,omitempty"`
	Impact       string `json:"impact,omitempty"`
	ChangeSignal string `json:"change_signal,omitempty"` // how both will know it changed
}

// ExtractedAssessment is a score by criterion: the leader's and the
// person's own, with the evidence behind it
type ExtractedAssessment struct {
	Name   string           `json:"name,omitempty"`  // default: the configured assessment
	Cycle  string           `json:"cycle,omitempty"` // e.g. "2026-10" or "2026 H2"
	Scores []ExtractedScore `json:"scores"`
}

type ExtractedScore struct {
	Criterion string `json:"criterion"`        // a configured criterion's id or name
	Leader    *int   `json:"leader,omitempty"` // the leader's score
	Self      *int   `json:"self,omitempty"`   // the person's own score
	Evidence  string `json:"evidence,omitempty"`
}
