package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// ============================================================
// MarkdownAdapter — writes feedback/1:1 as Markdown (default, no API)
// ============================================================

type MarkdownAdapter struct{}

func NewMarkdownAdapter() *MarkdownAdapter { return &MarkdownAdapter{} }

func (a *MarkdownAdapter) SystemName() string { return "markdown" }

func (a *MarkdownAdapter) PushFeedback(f *providers.FeedbackOutput) (*providers.PushResult, error) {
	md := formatFeedbackMarkdown(f)
	fmt.Print(md)
	return &providers.PushResult{Success: true, Message: "Printed as Markdown"}, nil
}

func (a *MarkdownAdapter) PushOneOnOne(s *providers.OneOnOneOutput) (*providers.PushResult, error) {
	md := formatOneOnOneMarkdown(s)
	fmt.Print(md)
	return &providers.PushResult{Success: true, Message: "Printed as Markdown"}, nil
}

func formatFeedbackMarkdown(f *providers.FeedbackOutput) string {
	md := fmt.Sprintf("# Feedback — %s\n**De:** %s  |  **Data:** %s  |  **Formato:** %s\n\n",
		f.AboutPerson, f.FromPerson, f.Date, f.Format)
	for _, item := range f.Items {
		md += fmt.Sprintf("- **%s:** %s\n", item.Category, item.Content)
	}
	return md
}

func formatOneOnOneMarkdown(s *providers.OneOnOneOutput) string {
	md := fmt.Sprintf("# 1:1 — %s\n**Data:** %s  |  **Área:** %s\n\n## Resumo\n%s\n\n",
		s.PersonName, s.Date, s.Area, s.Summary)
	if len(s.Topics) > 0 {
		md += "## Tópicos Discutidos\n"
		for _, t := range s.Topics {
			md += fmt.Sprintf("- %s\n", t)
		}
	}
	if len(s.LeaderTasks) > 0 {
		md += "\n### Tarefas do Líder\n"
		for _, t := range s.LeaderTasks {
			md += fmt.Sprintf("- [ ] %s\n", t)
		}
	}
	if len(s.LedTasks) > 0 {
		md += "\n### Tarefas do Liderado\n"
		for _, t := range s.LedTasks {
			md += fmt.Sprintf("- [ ] %s\n", t)
		}
	}
	if s.Notes != "" {
		md += fmt.Sprintf("\n## Anotações\n%s\n", s.Notes)
	}
	return md
}

// ============================================================
// JSONAdapter — outputs structured JSON (for programmatic consumption)
// ============================================================

type JSONAdapter struct{}

func NewJSONAdapter() *JSONAdapter { return &JSONAdapter{} }

func (a *JSONAdapter) SystemName() string { return "json" }

func (a *JSONAdapter) PushFeedback(f *providers.FeedbackOutput) (*providers.PushResult, error) {
	b, _ := json.MarshalIndent(f, "", "  ")
	fmt.Println(string(b))
	return &providers.PushResult{Success: true, Message: "Printed as JSON"}, nil
}

func (a *JSONAdapter) PushOneOnOne(s *providers.OneOnOneOutput) (*providers.PushResult, error) {
	b, _ := json.MarshalIndent(s, "", "  ")
	fmt.Println(string(b))
	return &providers.PushResult{Success: true, Message: "Printed as JSON"}, nil
}

// ============================================================
// QultureAdapter — pushes to Qulture API (Saipos HR system)
// ============================================================

type QultureAdapter struct {
	APIURL string
	APIKey string
	Client *http.Client
}

// NewQultureAdapter creates a Qulture adapter.
// API URL and key come from environment variables — never hardcoded.
func NewQultureAdapter() *QultureAdapter {
	return &QultureAdapter{
		APIURL: os.Getenv("QULTURE_API_URL"),
		APIKey: os.Getenv("QULTURE_API_KEY"),
		Client: &http.Client{},
	}
}

func (a *QultureAdapter) SystemName() string { return "qulture" }

func (a *QultureAdapter) PushFeedback(f *providers.FeedbackOutput) (*providers.PushResult, error) {
	if a.APIURL == "" || a.APIKey == "" {
		return nil, fmt.Errorf("QULTURE_API_URL and QULTURE_API_KEY environment variables must be set")
	}

	body, _ := json.Marshal(f)
	req, _ := http.NewRequest("POST", a.APIURL+"/api/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qulture API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return &providers.PushResult{
			Success: false,
			Message: fmt.Sprintf("Qulture API returned status %d", resp.StatusCode),
		}, nil
	}

	var result struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	return &providers.PushResult{
		Success:    true,
		ExternalID: result.ID,
		Message:    "Pushed to Qulture",
	}, nil
}

func (a *QultureAdapter) PushOneOnOne(s *providers.OneOnOneOutput) (*providers.PushResult, error) {
	if a.APIURL == "" || a.APIKey == "" {
		return nil, fmt.Errorf("QULTURE_API_URL and QULTURE_API_KEY environment variables must be set")
	}

	body, _ := json.Marshal(s)
	req, _ := http.NewRequest("POST", a.APIURL+"/api/one-on-one", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qulture API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return &providers.PushResult{
			Success: false,
			Message: fmt.Sprintf("Qulture API returned status %d", resp.StatusCode),
		}, nil
	}

	var result struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	return &providers.PushResult{
		Success:    true,
		ExternalID: result.ID,
		Message:    "Pushed to Qulture",
	}, nil
}

// ============================================================
// Factory — selects adapter based on config string
// ============================================================

func NewAdapter(systemName string) providers.OutputAdapter {
	switch systemName {
	case "qulture":
		return NewQultureAdapter()
	case "lattice":
		// TODO: implement Lattice adapter (same pattern as Qulture)
		return NewMarkdownAdapter() // fallback
	case "json":
		return NewJSONAdapter()
	case "markdown":
		fallthrough
	default:
		return NewMarkdownAdapter()
	}
}
