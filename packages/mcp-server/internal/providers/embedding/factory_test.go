package embedding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/configs"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
)

// TestShippedProfiles loads every profile embedded in the binary and builds
// its embedding provider, so profiles can't drift from what the code accepts
func TestShippedProfiles(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("SAIPOS_LITELLM_URL", "https://litellm.example.com/v1")
	t.Setenv("SECOND_BRAIN_EMBEDDING_KEY", "sk-test")

	want := map[string]string{
		"default":  "",
		"personal": "bge-m3",
		"startup":  "text-embedding-3-small",
		"saipos":   "text-embedding-3-small",
	}
	if got := configs.Profiles(); len(got) != len(want) {
		t.Errorf("profiles = %v, want %d", got, len(want))
	}
	for name, model := range want {
		data, err := configs.Profile(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		path := filepath.Join(t.TempDir(), name+".yaml")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		p, err := New(cfg.Providers.Embedding)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := ""
		if p != nil {
			got = p.Model()
		}
		if got != model {
			t.Errorf("%s: embedding model = %q, want %q", name, got, model)
		}
	}
}

func TestFactory(t *testing.T) {
	for _, name := range []string{"", "none", " None "} {
		if p, err := New(config.EmbeddingConfig{Provider: name}); p != nil || err != nil {
			t.Errorf("provider %q = %v, %v; want semantic search off", name, p, err)
		}
	}

	p, err := New(config.EmbeddingConfig{Provider: "openai", APIKey: "sk-x"})
	if err != nil {
		t.Fatalf("openai: %v", err)
	}
	if o := p.(*OpenAI); o.cfg.BaseURL != "https://api.openai.com/v1" || o.Model() != "text-embedding-3-small" {
		t.Errorf("openai defaults = %s, %s", o.cfg.BaseURL, o.Model())
	}

	p, err = New(config.EmbeddingConfig{Provider: "ollama", Model: "nomic-embed-text"})
	if err != nil {
		t.Fatalf("ollama: %v", err)
	}
	if o := p.(*OpenAI); o.cfg.BaseURL != "http://localhost:11434/v1" || o.cfg.APIKey != "" {
		t.Errorf("ollama defaults = %+v", o.cfg)
	}

	p, err = New(config.EmbeddingConfig{
		Provider: "openai-compatible", BaseURL: "https://litellm.example.com/v1", Model: "embeddings",
		APIKey: "sk-gw", Dimensions: 256, TimeoutSeconds: 5, BatchSize: 8,
	})
	if err != nil {
		t.Fatalf("openai-compatible: %v", err)
	}
	if o := p.(*OpenAI); o.Model() != "embeddings@256" || o.cfg.Timeout.Seconds() != 5 || o.cfg.BatchSize != 8 {
		t.Errorf("openai-compatible = %+v", o.cfg)
	}

	errs := []struct {
		cfg  config.EmbeddingConfig
		want string
	}{
		{config.EmbeddingConfig{Provider: "openai"}, "needs api_key"},
		{config.EmbeddingConfig{Provider: "openai-compatible", Model: "m"}, "needs base_url"},
		{config.EmbeddingConfig{Provider: "ollama"}, "model is required"},
		{config.EmbeddingConfig{Provider: "local:sentence_transformers"}, "supported: none, openai, ollama, openai-compatible"},
	}
	for _, c := range errs {
		if _, err := New(c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("New(%+v) = %v, want %q", c.cfg, err, c.want)
		}
	}
}
