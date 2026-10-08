package embedding

import (
	"fmt"
	"strings"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// Providers lists the accepted values of providers.embedding.provider
var Providers = []string{"none", "openai", "ollama", "openai-compatible"}

// New builds the embedding provider configured in cfg, or returns nil when
// semantic search is off ("none" or no provider).
func New(cfg config.EmbeddingConfig) (providers.EmbeddingProvider, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Provider))
	oc := OpenAIConfig{
		BaseURL:        cfg.BaseURL,
		Model:          cfg.Model,
		APIKey:         strings.TrimSpace(cfg.APIKey),
		Dimensions:     cfg.Dimensions,
		QueryPrefix:    cfg.QueryPrefix,
		DocumentPrefix: cfg.DocumentPrefix,
		Headers:        cfg.Headers,
		BatchSize:      cfg.BatchSize,
		Timeout:        time.Duration(cfg.TimeoutSeconds) * time.Second,
	}

	switch name {
	case "", "none":
		return nil, nil
	case "openai":
		if oc.BaseURL == "" {
			oc.BaseURL = "https://api.openai.com/v1"
		}
		if oc.Model == "" {
			oc.Model = "text-embedding-3-small"
		}
		if oc.APIKey == "" {
			return nil, fmt.Errorf("embedding provider openai needs api_key (e.g. api_key: ${OPENAI_API_KEY}; is the variable set?)")
		}
	case "ollama":
		if oc.BaseURL == "" {
			oc.BaseURL = "http://localhost:11434/v1"
		}
	case "openai-compatible":
		if oc.BaseURL == "" {
			return nil, fmt.Errorf("embedding provider openai-compatible needs base_url (e.g. https://your-gateway/v1)")
		}
	default:
		return nil, fmt.Errorf("unknown embedding provider %q (supported: %s)", cfg.Provider, strings.Join(Providers, ", "))
	}

	p, err := NewOpenAI(oc)
	if err != nil {
		return nil, fmt.Errorf("embedding provider %s: %w", name, err)
	}
	return p, nil
}
