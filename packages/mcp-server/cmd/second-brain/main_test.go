package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.yaml")
	existing := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(existing, []byte("profile: personal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		configPath  string
		defaultPath string
		profile     string
		want        string // profile of the loaded config; "" expects an error
	}{
		{"no config, no profile: defaults", "", missing, "", "default"},
		{"no config: the profile", "", missing, "saipos", "saipos"},
		{"a config file wins over the profile", "", existing, "saipos", "personal"},
		{"--config wins over the profile", existing, missing, "saipos", "personal"},
		{"a missing --config is an error", missing, existing, "saipos", ""},
		{"an unknown profile is an error", "", missing, "nope", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(tt.configPath, tt.defaultPath, tt.profile)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("want an error, got profile %q", cfg.Profile)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Profile != tt.want {
				t.Errorf("profile = %q, want %q", cfg.Profile, tt.want)
			}
		})
	}
}
