package config

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, yaml string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func TestEmbeddingConfig(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		cfg := load(t, "profile: test\n")
		if cfg.Providers.Embedding.Provider != "" {
			t.Errorf("provider = %q, want empty (semantic search off)", cfg.Providers.Embedding.Provider)
		}
	})

	t.Run("short form", func(t *testing.T) {
		cfg := load(t, "providers:\n  embedding: none\n")
		if cfg.Providers.Embedding.Provider != "none" {
			t.Errorf("provider = %q, want none", cfg.Providers.Embedding.Provider)
		}
	})

	t.Run("block with the key from the environment", func(t *testing.T) {
		t.Setenv("TEST_EMBEDDING_KEY", "sk-from-env")
		cfg := load(t, `
providers:
  embedding:
    provider: openai-compatible
    base_url: https://litellm.example.com/v1
    model: text-embedding-3-small
    api_key: ${TEST_EMBEDDING_KEY}
    dimensions: 512
    query_prefix: "query: "
    headers:
      X-Team: lideranca
    batch_size: 16
    timeout_seconds: 30
`)
		e := cfg.Providers.Embedding
		if e.Provider != "openai-compatible" || e.BaseURL != "https://litellm.example.com/v1" ||
			e.Model != "text-embedding-3-small" || e.APIKey != "sk-from-env" || e.Dimensions != 512 ||
			e.QueryPrefix != "query: " || e.Headers["X-Team"] != "lideranca" || e.BatchSize != 16 || e.TimeoutSeconds != 30 {
			t.Errorf("embedding config = %+v", e)
		}
	})

	t.Run("legacy value is kept for the factory to reject", func(t *testing.T) {
		cfg := load(t, "providers:\n  embedding: \"local:sentence_transformers\"\n")
		if cfg.Providers.Embedding.Provider != "local:sentence_transformers" {
			t.Errorf("provider = %q", cfg.Providers.Embedding.Provider)
		}
	})
}
