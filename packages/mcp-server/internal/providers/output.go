package providers

// OutputAdapter pushes structured content to an external HR system.
// Implementations: QultureAdapter, LatticeAdapter, MarkdownAdapter, JSONAdapter.
// Selected via config: feedback.target_system
type OutputAdapter interface {
	// PushFeedback sends structured feedback to the external system
	PushFeedback(feedback *FeedbackOutput) (*PushResult, error)

	// PushOneOnOne sends a 1:1 synthesis to the external system
	PushOneOnOne(summary *OneOnOneOutput) (*PushResult, error)

	// SystemName returns the adapter's identifier (e.g., "qulture", "lattice")
	SystemName() string
}

// FeedbackOutput is the structured feedback ready for external systems
type FeedbackOutput struct {
	AboutPerson string            `json:"about_person"`
	FromPerson  string            `json:"from_person"`
	Format      string            `json:"format"`        // stop_start_continue | freeform
	Items       []FeedbackItemOut `json:"items"`
	Date        string            `json:"date"`
	Source      string            `json:"source"`        // second-brain
}

// FeedbackItemOut is a single feedback item formatted for output
type FeedbackItemOut struct {
	Category string `json:"category"`  // stop | start | continue | general
	Content  string `json:"content"`
}

// OneOnOneOutput is a 1:1 synthesis ready for external systems
type OneOnOneOutput struct {
	PersonName string   `json:"person_name"`
	Date       string   `json:"date"`
	Area       string   `json:"area"`
	Summary    string   `json:"summary"`
	Topics     []string `json:"topics"`
	LeaderTasks []string `json:"leader_tasks"`
	LedTasks    []string `json:"led_tasks"`
	Notes      string   `json:"notes"`
}

// PushResult is what the adapter returns after pushing
type PushResult struct {
	Success  bool   `json:"success"`
	ExternalID string `json:"external_id,omitempty"` // ID in the external system
	Message  string `json:"message"`
}
