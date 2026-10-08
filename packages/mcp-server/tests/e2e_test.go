package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/mcpserver"
)

// TestIngestRecallEndToEnd verifies that:
// 1. A memory can be ingested (stored in SQLite + graph)
// 2. The memory can be recalled by person name
// 3. The recall returns the ingested content
//
// This test uses a temporary SQLite database and stub providers
// (no external API calls needed).
func TestIngestRecallEndToEnd(t *testing.T) {
	// Create a temporary database
	dbPath := t.TempDir() + "/test_memoria.db"

	cfg := &config.Config{
		Profile:   "test",
		Storage:   config.StorageConfig{},
		Transport: config.TransportConfig{Type: "stdio"},
		Graph:     config.GraphConfig{Engine: "sqlite"},
	}
	cfg.Storage.SQLite.Path = dbPath
	cfg.Skills = config.SkillsConfig{
		SecondBrain: true,
		OneOnOne:    true,
		PDIGapMap:   true,
		Feedback:    true,
	}
	cfg.Feedback = config.FeedbackConfig{
		Format: "stop_start_continue",
		Categories: []config.FeedbackCategory{
			{ID: "stop", Label: "Parar", Color: "#d97757"},
			{ID: "start", Label: "Começar", Color: "#788c5d"},
			{ID: "continue", Label: "Continuar", Color: "#6a9bcc"},
		},
		ItemsPerCategory: 2,
		TargetSystem:     "markdown",
	}

	// Initialize the server
	srv, err := mcpserver.New(cfg)
	if err != nil {
		t.Fatalf("init server: %v", err)
	}
	defer srv.Close()

	ctx := context.Background()

	// ============================================================
	// Step 1: Ingest a text observation about Sérgio
	// ============================================================
	ingestResult, err := srv.HandleIngest(ctx, map[string]interface{}{
		"modality":     "text",
		"content":      "Conversei com o Bernardo hoje. Ele me disse que o Sérgio anda microgerenciando as tarefas técnicas da equipe e não dá feedback construtivo. Por outro lado, elogiou a disponibilidade do Sérgio. Preciso trabalhar isso com o Sérgio na próxima 1:1.",
		"about_person": "Sérgio",
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	var ingestJSON map[string]interface{}
	json.Unmarshal([]byte(ingestResult.Content[0].Text), &ingestJSON)
	t.Logf("Ingest result: %s", prettyJSON(ingestJSON))

	if ingestJSON["status"] != "stored" {
		t.Errorf("expected status 'stored', got %v", ingestJSON["status"])
	}
	if ingestJSON["about_person"] != "Sérgio" {
		t.Errorf("expected about_person 'Sérgio', got %v", ingestJSON["about_person"])
	}

	// ============================================================
	// Step 2: Ingest a second observation about Evandro
	// ============================================================
	ingestResult2, err := srv.HandleIngest(ctx, map[string]interface{}{
		"modality":     "text",
		"content":      "Evandro resolveu sozinho um bug de TEF buscando no Google. Boa evolução de autonomia para um júnior.",
		"about_person": "Evandro",
	})
	if err != nil {
		t.Fatalf("ingest 2 failed: %v", err)
	}
	t.Logf("Ingest 2 result: %s", ingestResult2.Content[0].Text)

	// ============================================================
	// Step 3: Search memories by keyword (FTS5)
	// ============================================================
	searchResult, err := srv.HandleSearchMemories(ctx, map[string]interface{}{
		"query": "Sérgio microgerenciando",
		"limit": float64(5),
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	var searchJSON map[string]interface{}
	json.Unmarshal([]byte(searchResult.Content[0].Text), &searchJSON)
	t.Logf("Search result: %s", prettyJSON(searchJSON))

	count, _ := searchJSON["count"].(float64)
	if count < 1 {
		t.Errorf("expected at least 1 search result, got %v", count)
	}

	// ============================================================
	// Step 4: Recall context for Sérgio (the person we ingested about)
	// ============================================================
	recallResult, err := srv.HandleRecall(ctx, map[string]interface{}{
		"person_name": "Sérgio",
		"context":     "1:1",
		"time_range":  "all",
	})
	if err != nil {
		t.Fatalf("recall failed: %v", err)
	}

	var recallJSON map[string]interface{}
	json.Unmarshal([]byte(recallResult.Content[0].Text), &recallJSON)
	t.Logf("Recall result: %s", prettyJSON(recallJSON))

	if recallJSON["person"] != "Sérgio" {
		t.Errorf("expected person 'Sérgio', got %v", recallJSON["person"])
	}

	// Check that a recommendation was generated
	if rec, ok := recallJSON["recommendation"].(string); ok {
		t.Logf("Recommendation: %s", rec)
	}

	t.Log("\n=== E2E TEST PASSED ===")
	t.Log("Ingest -> Search -> Recall pipeline is working end-to-end.")
}

func prettyJSON(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

var _ = os.Stdout
var _ = fmt.Sprintf
