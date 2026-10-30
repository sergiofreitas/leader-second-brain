package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration loaded from config.yaml
type Config struct {
	Profile   string          `yaml:"profile"`
	Storage   StorageConfig   `yaml:"storage"`
	Providers ProvidersConfig `yaml:"providers"`
	Transport TransportConfig `yaml:"transport"`
	Graph     GraphConfig     `yaml:"graph"`
	Feedback  FeedbackConfig  `yaml:"feedback"`
	Skills    SkillsConfig    `yaml:"skills"`

	// The organization's method (its playbook): how each rite is run, the
	// assessment rubric and when to alert. Skills read it with get_playbook.
	Rites      map[string]Rite  `yaml:"rites"`
	Assessment AssessmentConfig `yaml:"assessment"`
	Alerts     AlertsConfig     `yaml:"alerts"`

	// PlaybookFile overlays the method with a file kept outside the
	// profile, for content an organization doesn't publish (default:
	// ~/.second-brain/playbook.yaml, if it exists)
	PlaybookFile string `yaml:"playbook_file"`
	// PlaybookSource is the playbook file that was loaded, if any, and
	// PlaybookProblem why it couldn't be
	PlaybookSource  string `yaml:"-"`
	PlaybookProblem string `yaml:"-"`
}

// StorageConfig defines where data lives locally
type StorageConfig struct {
	SQLite struct {
		Path string `yaml:"path"`
	} `yaml:"sqlite"`
	Media struct {
		Path          string `yaml:"path"`
		MaxSizeMB     int    `yaml:"max_size_mb"`
		RetentionDays int    `yaml:"retention_days"`
	} `yaml:"media"`
}

// ProvidersConfig defines which AI providers to use
type ProvidersConfig struct {
	Transcription string          `yaml:"transcription"`
	OCR           string          `yaml:"ocr"`
	VLM           string          `yaml:"vlm"`
	Embedding     EmbeddingConfig `yaml:"embedding"`
	LLM           string          `yaml:"llm"`

	// Provider-specific options
	OpenAI    map[string]interface{} `yaml:"openai,omitempty"`
	Anthropic map[string]interface{} `yaml:"anthropic,omitempty"`
	Local     map[string]interface{} `yaml:"local,omitempty"`
}

// EmbeddingConfig selects the embedding provider used for semantic search.
// Without one, semantic search is off and nothing is sent anywhere.
//
//	embedding: none                 # short form
//	embedding:
//	  provider: openai-compatible   # none | openai | ollama | openai-compatible
//	  base_url: https://litellm.example.com/v1
//	  model: text-embedding-3-small
//	  api_key: ${SECOND_BRAIN_EMBEDDING_KEY}
type EmbeddingConfig struct {
	Provider       string            `yaml:"provider"`
	BaseURL        string            `yaml:"base_url"`
	Model          string            `yaml:"model"`
	APIKey         string            `yaml:"api_key"`
	Dimensions     int               `yaml:"dimensions"`
	QueryPrefix    string            `yaml:"query_prefix"`
	DocumentPrefix string            `yaml:"document_prefix"`
	Headers        map[string]string `yaml:"headers"`
	BatchSize      int               `yaml:"batch_size"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
}

// UnmarshalYAML accepts the short form (`embedding: none`) as well as a block
func (e *EmbeddingConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		return value.Decode(&e.Provider)
	}
	type plain EmbeddingConfig // no UnmarshalYAML: avoids recursion
	return value.Decode((*plain)(e))
}

// TransportConfig defines how the MCP server communicates
type TransportConfig struct {
	Type string `yaml:"type"` // stdio | http
	Port int    `yaml:"port,omitempty"`
}

// GraphConfig defines which graph engine to use
type GraphConfig struct {
	Engine string `yaml:"engine"` // sqlite (default: graph tables + recursive CTEs in the same SQLite file)
}

// FeedbackConfig defines the feedback format for this organization
type FeedbackConfig struct {
	Format           string             `yaml:"format" json:"format"`                         // stop_start_continue | freeform | sandwich
	Categories       []FeedbackCategory `yaml:"categories" json:"categories"`                 // in the order they are given
	ItemsPerCategory int                `yaml:"items_per_category" json:"items_per_category"` // at most
	TargetSystem     string             `yaml:"target_system" json:"target_system"`           // qulture | lattice | workday | markdown | json
	// ItemTemplate is how each item is phrased
	ItemTemplate string `yaml:"item_template" json:"item_template,omitempty"`
	// Checks are answered before preparing a feedback
	Checks []string `yaml:"checks" json:"checks,omitempty"`
}

// FeedbackCategory defines a category within a feedback format
type FeedbackCategory struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
	Color string `yaml:"color" json:"-"`
}

// Rite is a recurring leadership conversation (1:1, weekly, skip-level,
// feedback): what it is for, how often, how it is prepared and run, and how
// it is recorded. Rites are keyed by the memory type that records them.
type Rite struct {
	Name            string        `yaml:"name" json:"name"`
	Purpose         string        `yaml:"purpose" json:"purpose,omitempty"`
	CadenceDays     int           `yaml:"cadence_days" json:"cadence_days,omitempty"`
	DurationMinutes int           `yaml:"duration_minutes" json:"duration_minutes,omitempty"`
	Preparation     []string      `yaml:"preparation" json:"preparation,omitempty"`
	Agenda          []AgendaBlock `yaml:"agenda" json:"agenda,omitempty"`
	MaxAgreements   int           `yaml:"max_agreements" json:"max_agreements,omitempty"`
	Record          string        `yaml:"record" json:"record,omitempty"` // how and where it is recorded
	// RecordTemplate is the text the record follows, filled in section by
	// section, when the organization has one
	RecordTemplate string   `yaml:"record_template" json:"record_template,omitempty"`
	Guidelines     []string `yaml:"guidelines" json:"guidelines,omitempty"`
}

// AgendaBlock is a part of a rite's agenda
type AgendaBlock struct {
	Block    string `yaml:"block" json:"block"`
	Minutes  string `yaml:"minutes" json:"minutes,omitempty"` // e.g. "5" or "15-20"
	Guidance string `yaml:"guidance" json:"guidance,omitempty"`
}

// AssessmentConfig is how people are scored: the scale, the criteria, when
// the leader's and the person's own scores diverge, and who may see them
type AssessmentConfig struct {
	Name                string      `yaml:"name" json:"name,omitempty"`
	ScaleMin            int         `yaml:"scale_min" json:"scale_min"`
	ScaleMax            int         `yaml:"scale_max" json:"scale_max"`
	DivergenceThreshold int         `yaml:"divergence_threshold" json:"divergence_threshold"`
	Visibility          string      `yaml:"visibility" json:"visibility"` // leader_only | shared
	Guidelines          []string    `yaml:"guidelines" json:"guidelines,omitempty"`
	Criteria            []Criterion `yaml:"criteria" json:"criteria,omitempty"` // none: any criterion is accepted
}

// Criterion is one thing an assessment scores
type Criterion struct {
	ID          string `yaml:"id" json:"id"` // default: derived from the name (CriterionID)
	Name        string `yaml:"name" json:"name"`
	Group       string `yaml:"group" json:"group,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
	Question    string `yaml:"question" json:"question,omitempty"` // a question for reflecting on it
	Example     string `yaml:"example" json:"example,omitempty"`
}

// AlertsConfig sets when the team view flags someone
type AlertsConfig struct {
	QuietDays     int `yaml:"quiet_days" json:"quiet_days"`           // nothing recorded about someone for this long
	StaleTaskDays int `yaml:"stale_task_days" json:"stale_task_days"` // a task pending for this long
}

// Visibilities are the accepted values of AssessmentConfig.Visibility
var Visibilities = []string{"leader_only", "shared"}

// SkillsConfig defines which skills are enabled
type SkillsConfig struct {
	SecondBrain bool `yaml:"second_brain"`
	OneOnOne    bool `yaml:"one_on_one"`
	PDIGapMap   bool `yaml:"pdi_gap_map"`
	Feedback    bool `yaml:"feedback"`
}

// Load reads the config from the given path, expanding ~ and env vars
func Load(path string) (*Config, error) {
	path = expandPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return Parse(data)
}

// Parse reads a config from YAML (a file or an embedded profile), expanding
// env vars and applying the defaults
func Parse(data []byte) (*Config, error) {
	// Expand environment variables in the YAML
	expanded := os.Expand(string(data), func(key string) string {
		return os.Getenv(key)
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults
	cfg.ApplyDefaults()
	if err := cfg.validateMethod(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	return &cfg, nil
}

// Default returns the configuration used when there is no config file:
// local storage under ~/.second-brain, keyword search only
func Default() *Config {
	cfg := &Config{Profile: "default"}
	cfg.ApplyDefaults()
	return cfg
}

func (c *Config) ApplyDefaults() {
	if c.Storage.SQLite.Path == "" {
		c.Storage.SQLite.Path = "~/.second-brain/memoria.db"
	}
	c.Storage.SQLite.Path = expandPath(c.Storage.SQLite.Path)

	if c.Storage.Media.Path == "" {
		c.Storage.Media.Path = "~/.second-brain/media/"
	}
	c.Storage.Media.Path = expandPath(c.Storage.Media.Path)

	if c.Storage.Media.MaxSizeMB == 0 {
		c.Storage.Media.MaxSizeMB = 5000
	}
	if c.Storage.Media.RetentionDays == 0 {
		c.Storage.Media.RetentionDays = 365
	}

	if c.Transport.Type == "" {
		c.Transport.Type = "stdio"
	}

	if c.Graph.Engine == "" {
		c.Graph.Engine = "sqlite"
	}

	// Feedback defaults: stop/start/continue
	if c.Feedback.Format == "" {
		c.Feedback.Format = "stop_start_continue"
	}
	if len(c.Feedback.Categories) == 0 {
		c.Feedback.Categories = []FeedbackCategory{
			{ID: "stop", Label: "Parar de fazer", Color: "#d97757"},
			{ID: "start", Label: "Começar a fazer", Color: "#788c5d"},
			{ID: "continue", Label: "Continuar fazendo", Color: "#6a9bcc"},
		}
	}
	if c.Feedback.ItemsPerCategory == 0 {
		c.Feedback.ItemsPerCategory = 2
	}
	if c.Feedback.TargetSystem == "" {
		c.Feedback.TargetSystem = "markdown"
	}
	if c.Feedback.ItemTemplate == "" {
		c.Feedback.ItemTemplate = "In [situation], I noticed [behavior]. The impact was [impact]. We'll know it changed when [signal]."
	}

	// The method: generic rites, a 1-5 scale, scores only the leader sees
	if c.Rites == nil {
		c.Rites = defaultRites()
	}
	if c.Assessment.ScaleMin == 0 && c.Assessment.ScaleMax == 0 {
		c.Assessment.ScaleMin, c.Assessment.ScaleMax = 1, 5
	}
	if c.Assessment.DivergenceThreshold == 0 {
		c.Assessment.DivergenceThreshold = 2
	}
	if c.Assessment.Visibility == "" {
		c.Assessment.Visibility = "leader_only"
	}
	for i := range c.Assessment.Criteria {
		if cr := &c.Assessment.Criteria[i]; cr.ID == "" {
			cr.ID = CriterionID(cr.Name)
		}
	}
	if c.Alerts.QuietDays == 0 {
		c.Alerts.QuietDays = 60
	}
	if c.Alerts.StaleTaskDays == 0 {
		c.Alerts.StaleTaskDays = 30
	}

	// Enable all skills by default
	if !c.Skills.SecondBrain && !c.Skills.OneOnOne && !c.Skills.PDIGapMap && !c.Skills.Feedback {
		c.Skills = SkillsConfig{
			SecondBrain: true,
			OneOnOne:    true,
			PDIGapMap:   true,
			Feedback:    true,
		}
	}
}

// defaultRites is the method of a profile that defines none: rites every
// leader has, without any organization's specifics
func defaultRites() map[string]Rite {
	return map[string]Rite{
		"one_on_one": {
			Name:            "1:1",
			Purpose:         "The led's meeting: their development, what blocks them, what they need from the leader.",
			CadenceDays:     14,
			DurationMinutes: 30,
			Preparation:     []string{"Ask the led to bring their own agenda."},
			Agenda: []AgendaBlock{
				{Block: "Check-in", Minutes: "5", Guidance: "Open with the agreements of the last 1:1."},
				{Block: "The led's agenda", Minutes: "15", Guidance: "The led talks first."},
				{Block: "Development and feedback", Minutes: "5"},
				{Block: "Agreements", Minutes: "5", Guidance: "Each with an owner and a due date."},
			},
			MaxAgreements: 3,
		},
		"weekly": {
			Name:        "Weekly sync",
			Purpose:     "Priorities, blockers and alignment for the week.",
			CadenceDays: 7,
		},
		"skip_level": {
			Name:        "Skip-level",
			Purpose:     "A conversation with someone two levels below: how the team and their leader are doing.",
			CadenceDays: 90,
			Guidelines:  []string{"What is said is about the team, not an evaluation of their leader: don't attribute it to the person."},
		},
		"feedback": {
			Name:    "Feedback",
			Purpose: "Behavior-based feedback, in the configured categories.",
			Agenda: []AgendaBlock{
				{Block: "Opening", Guidance: "Say how the conversation will go."},
				{Block: "The items, by category"},
				{Block: "The person's response and next steps"},
			},
		},
	}
}

// CriterionID derives a criterion id from its name: lowercase, without
// accents, words joined by hyphens ("Ter coragem" -> "ter-coragem")
func CriterionID(name string) string {
	return strings.Join(strings.Fields(accents.Replace(strings.ToLower(name))), "-")
}

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ç", "c", "ñ", "n",
)

// LoadPlaybook overlays the method with the playbook file: what the file
// sets replaces what the profile set (a rite it names replaces that rite, a
// list replaces the list), the rest is kept. The default file is optional;
// a playbook_file that doesn't exist is an error.
func (c *Config) LoadPlaybook() error {
	path := c.PlaybookFile
	if path == "" {
		path = DefaultPlaybookPath()
	}
	path = expandPath(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && c.PlaybookFile == "" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read playbook: %w", err)
	}

	// Decoded into a copy, so a broken file leaves the config as it was
	next := *c
	next.Rites = make(map[string]Rite, len(c.Rites))
	for id, r := range c.Rites {
		next.Rites[id] = r
	}
	overlay := struct {
		Feedback   *FeedbackConfig   `yaml:"feedback"`
		Rites      map[string]Rite   `yaml:"rites"`
		Assessment *AssessmentConfig `yaml:"assessment"`
		Alerts     *AlertsConfig     `yaml:"alerts"`
	}{&next.Feedback, next.Rites, &next.Assessment, &next.Alerts}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // a typo is an error, not a setting silently ignored
	if err := dec.Decode(&overlay); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse playbook %s: %w", path, err)
	}
	if overlay.Rites != nil {
		next.Rites = overlay.Rites
	}
	next.ApplyDefaults()
	if err := next.validateMethod(); err != nil {
		return fmt.Errorf("playbook %s: %w", path, err)
	}
	next.PlaybookSource = path
	*c = next
	return nil
}

// validateMethod checks what ingest relies on: a usable scale, a known
// visibility and criteria that can be told apart
func (c *Config) validateMethod() error {
	a := c.Assessment
	if a.ScaleMin >= a.ScaleMax {
		return fmt.Errorf("assessment: scale_min (%d) must be below scale_max (%d)", a.ScaleMin, a.ScaleMax)
	}
	valid := false
	for _, v := range Visibilities {
		valid = valid || v == a.Visibility
	}
	if !valid {
		return fmt.Errorf("assessment: invalid visibility %q (valid: %s)", a.Visibility, strings.Join(Visibilities, ", "))
	}
	seen := map[string]bool{}
	for _, cr := range a.Criteria {
		if strings.TrimSpace(cr.Name) == "" {
			return errors.New("assessment: a criterion has no name")
		}
		if seen[cr.ID] {
			return fmt.Errorf("assessment: criterion id %q is repeated", cr.ID)
		}
		seen[cr.ID] = true
	}
	return nil
}

// DefaultPlaybookPath returns the default playbook file location
func DefaultPlaybookPath() string {
	return expandPath("~/.second-brain/playbook.yaml")
}

// FeedbackCategoryIDs returns the list of category IDs for the configured format
func (c *Config) FeedbackCategoryIDs() []string {
	ids := make([]string, len(c.Feedback.Categories))
	for i, cat := range c.Feedback.Categories {
		ids[i] = cat.ID
	}
	return ids
}

// expandPath expands ~ to the user's home directory
func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		usr, err := user.Current()
		if err == nil {
			return filepath.Join(usr.HomeDir, path[1:])
		}
	}
	return path
}

// DefaultConfigPath returns the default config file location
func DefaultConfigPath() string {
	return expandPath("~/.second-brain/config.yaml")
}

// EnsureDir creates the directory for the config if it doesn't exist
func EnsureDir(path string) error {
	dir := filepath.Dir(expandPath(path))
	return os.MkdirAll(dir, 0755)
}
