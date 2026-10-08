package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration loaded from config.yaml
type Config struct {
	Profile    string         `yaml:"profile"`
	Storage    StorageConfig  `yaml:"storage"`
	Providers  ProvidersConfig `yaml:"providers"`
	Transport  TransportConfig `yaml:"transport"`
	Graph      GraphConfig    `yaml:"graph"`
	Feedback   FeedbackConfig `yaml:"feedback"`
	Skills     SkillsConfig   `yaml:"skills"`
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
	Transcription string `yaml:"transcription"`
	OCR           string `yaml:"ocr"`
	VLM           string `yaml:"vlm"`
	Embedding     string `yaml:"embedding"`
	LLM           string `yaml:"llm"`

	// Provider-specific options
	OpenAI    map[string]interface{} `yaml:"openai,omitempty"`
	Anthropic map[string]interface{} `yaml:"anthropic,omitempty"`
	Toqan     map[string]interface{} `yaml:"toqan,omitempty"`
	Local     map[string]interface{} `yaml:"local,omitempty"`
}

// TransportConfig defines how the MCP server communicates
type TransportConfig struct {
	Type string `yaml:"type"` // stdio | http
	Port int   `yaml:"port,omitempty"`
}

// GraphConfig defines which graph engine to use
type GraphConfig struct {
	Engine string `yaml:"engine"` // sqlite (default: graph tables + recursive CTEs in the same SQLite file)
}

// FeedbackConfig defines the feedback format for this organization
type FeedbackConfig struct {
	Format string `yaml:"format"` // stop_start_continue | freeform | sandwich
	Categories []FeedbackCategory `yaml:"categories"`
	ItemsPerCategory int `yaml:"items_per_category"`
	TargetSystem string `yaml:"target_system"` // qulture | lattice | workday | markdown | json
}

// FeedbackCategory defines a category within a feedback format
type FeedbackCategory struct {
	ID    string `yaml:"id"`
	Label string `yaml:"label"`
	Color string `yaml:"color"`
}

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

	// Expand environment variables in the YAML
	expanded := os.Expand(string(data), func(key string) string {
		return os.Getenv(key)
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults
	cfg.applyDefaults()

	return &cfg, nil
}

func (c *Config) applyDefaults() {
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
