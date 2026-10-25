package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlaybook(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "playbook.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultMethod(t *testing.T) {
	cfg := Default()
	for _, rite := range []string{"one_on_one", "weekly", "skip_level", "feedback"} {
		if _, ok := cfg.Rites[rite]; !ok {
			t.Errorf("default rites lack %s", rite)
		}
	}
	a := cfg.Assessment
	if a.ScaleMin != 1 || a.ScaleMax != 5 || a.DivergenceThreshold != 2 || a.Visibility != "leader_only" || len(a.Criteria) != 0 {
		t.Errorf("default assessment = %+v", a)
	}
	if cfg.Alerts.QuietDays != 60 || cfg.Alerts.StaleTaskDays != 30 {
		t.Errorf("default alerts = %+v", cfg.Alerts)
	}
}

// TestLoadPlaybook overlays a profile with a playbook file: what the file
// sets replaces the profile's, the rest is kept
func TestLoadPlaybook(t *testing.T) {
	cfg := load(t, `
feedback:
  categories:
    - { id: stop, label: Parar }
    - { id: continue, label: Continuar }
    - { id: start, label: Começar }
  target_system: qulture
rites:
  one_on_one: { name: "1:1", cadence_days: 60 }
  weekly: { name: Semanal, cadence_days: 7 }
assessment:
  name: Score de Cultura
`)
	if _, ok := cfg.Rites["skip_level"]; ok {
		t.Errorf("a profile with rites got the default ones too: %v", riteKeys(cfg))
	}
	cfg.PlaybookFile = writePlaybook(t, `
rites:
  one_on_one:
    name: "1:1"
    cadence_days: 60
    agenda:
      - { block: Rapport, minutes: "5" }
feedback:
  checks: [Zero surpresa]
assessment:
  criteria:
    - { name: Ter coragem, group: Protagonismo }
    - { name: Antecipar os problemas }
alerts:
  quiet_days: 28
`)
	if err := cfg.LoadPlaybook(); err != nil {
		t.Fatal(err)
	}
	if cfg.PlaybookSource != cfg.PlaybookFile {
		t.Errorf("PlaybookSource = %q", cfg.PlaybookSource)
	}
	if got := cfg.Rites["one_on_one"]; len(got.Agenda) != 1 || got.CadenceDays != 60 {
		t.Errorf("one_on_one = %+v, want the file's", got)
	}
	if got := cfg.Rites["weekly"]; got.Name != "Semanal" {
		t.Errorf("weekly = %+v, want the profile's (the file doesn't name it)", got)
	}
	if ids := cfg.FeedbackCategoryIDs(); strings.Join(ids, ",") != "stop,continue,start" || cfg.Feedback.TargetSystem != "qulture" {
		t.Errorf("feedback = %+v, want the profile's categories in their order", cfg.Feedback)
	}
	if len(cfg.Feedback.Checks) != 1 {
		t.Errorf("checks = %v", cfg.Feedback.Checks)
	}
	a := cfg.Assessment
	if a.Name != "Score de Cultura" || len(a.Criteria) != 2 || a.Criteria[0].ID != "ter-coragem" || a.Criteria[1].ID != "antecipar-os-problemas" {
		t.Errorf("assessment = %+v", a)
	}
	if cfg.Alerts.QuietDays != 28 || cfg.Alerts.StaleTaskDays != 30 {
		t.Errorf("alerts = %+v", cfg.Alerts)
	}
}

func TestLoadPlaybookErrors(t *testing.T) {
	t.Run("a configured file must exist", func(t *testing.T) {
		cfg := Default()
		cfg.PlaybookFile = filepath.Join(t.TempDir(), "missing.yaml")
		if err := cfg.LoadPlaybook(); err == nil {
			t.Error("want an error")
		}
	})
	for name, content := range map[string]string{
		"a typo":                "assesment:\n  criteria: []\n",
		"a repeated criterion":  "assessment:\n  criteria:\n    - { name: Ter coragem }\n    - { name: ter  CORAGEM }\n",
		"an inverted scale":     "assessment:\n  scale_min: 5\n  scale_max: 1\n",
		"an unknown visibility": "assessment:\n  visibility: everyone\n",
	} {
		t.Run(name+" leaves the config as it was", func(t *testing.T) {
			cfg := Default()
			cfg.PlaybookFile = writePlaybook(t, content)
			if err := cfg.LoadPlaybook(); err == nil {
				t.Fatal("want an error")
			}
			if cfg.Assessment.ScaleMax != 5 || cfg.Assessment.Visibility != "leader_only" || len(cfg.Assessment.Criteria) != 0 {
				t.Errorf("assessment changed: %+v", cfg.Assessment)
			}
		})
	}
}

func TestCriterionID(t *testing.T) {
	for in, want := range map[string]string{
		"Ter coragem":                        "ter-coragem",
		"  Obsessão  pelo Cliente ":          "obsessao-pelo-cliente",
		"Trabalho atende o job level":        "trabalho-atende-o-job-level",
		"Estar consciente que velocidade e ": "estar-consciente-que-velocidade-e",
	} {
		if got := CriterionID(in); got != want {
			t.Errorf("CriterionID(%q) = %q, want %q", in, got, want)
		}
	}
}

func riteKeys(cfg *Config) []string {
	var keys []string
	for k := range cfg.Rites {
		keys = append(keys, k)
	}
	return keys
}
