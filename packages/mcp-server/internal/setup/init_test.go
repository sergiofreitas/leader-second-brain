package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
)

func TestInitDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	res, err := Init(InitOptions{Path: path})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if len(res.EnvVars) != 0 {
		t.Errorf("default profile reads env vars %v (only comments mention them)", res.EnvVars)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Providers.Embedding.Provider != "none" {
		t.Errorf("default embedding = %q, want none", cfg.Providers.Embedding.Provider)
	}
	if info, _ := os.Stat(path); info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("config permissions = %v, want private", info.Mode().Perm())
	}

	// An existing config is kept unless --force
	if _, err := Init(InitOptions{Path: path, Profile: "saipos"}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("init over an existing config = %v, want a refusal mentioning --force", err)
	}
	if _, err := Init(InitOptions{Path: path, Profile: "saipos", Force: true}); err != nil {
		t.Errorf("init --force: %v", err)
	}
}

func TestInitSaiposWithOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	os.Unsetenv("OPENAI_API_KEY")
	path := filepath.Join(t.TempDir(), "config.yaml")
	res, err := Init(InitOptions{Profile: "saipos", Embedding: "openai", Path: path})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if set, ok := res.EnvVars["OPENAI_API_KEY"]; !ok || set {
		t.Errorf("env vars = %v, want OPENAI_API_KEY reported as not set", res.EnvVars)
	}
	if _, ok := res.EnvVars["SAIPOS_LITELLM_URL"]; ok {
		t.Errorf("env vars = %v: the gateway's variables should be gone", res.EnvVars)
	}

	t.Setenv("OPENAI_API_KEY", "sk-test")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e := cfg.Providers.Embedding
	if e.Provider != "openai" || e.Model != "text-embedding-3-small" || e.APIKey != "sk-test" {
		t.Errorf("embedding = %+v, want openai with the key from the environment", e)
	}
	// The rest of the saipos profile is kept
	if cfg.Profile != "saipos" || cfg.Feedback.Format != "stop_start_continue" || cfg.Feedback.TargetSystem != "qulture" {
		t.Errorf("profile settings lost: profile %q, feedback %+v", cfg.Profile, cfg.Feedback)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "LiteLLM") || !strings.Contains(string(data), "# The MCP host") ||
		!strings.Contains(string(data), "# Semantic search with OpenAI embeddings: memory text is sent to OpenAI") {
		t.Errorf("comments: the gateway's should be replaced by OpenAI's, the others kept:\n%s", data)
	}
}

func TestInitErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(InitOptions{Profile: "acme", Path: filepath.Join(dir, "a.yaml")}); err == nil ||
		!strings.Contains(err.Error(), "available: default, personal, saipos, startup") {
		t.Errorf("unknown profile: %v", err)
	}
	if _, err := Init(InitOptions{Embedding: "voyage", Path: filepath.Join(dir, "b.yaml")}); err == nil ||
		!strings.Contains(err.Error(), "available: none, openai, ollama") {
		t.Errorf("unknown embedding: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.yaml")); err == nil {
		t.Error("a failed init wrote a file")
	}
}
