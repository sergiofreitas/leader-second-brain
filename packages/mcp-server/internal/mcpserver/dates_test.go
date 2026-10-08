package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

func TestParseOccurredAt(t *testing.T) {
	today := time.Date(2026, 10, 8, 15, 0, 0, 0, time.Local)
	for _, tc := range []struct{ in, want, err string }{
		{"", "", ""},
		{"2026-09-02", "2026-09-02", ""},
		{" 2026-09-02 ", "2026-09-02", ""},
		{"2026-09-02T10:30:00-03:00", "2026-09-02", ""},
		{"2026-10-08", "2026-10-08", ""},
		{"02/09/2026", "", "use YYYY-MM-DD"},
		{"2026-10-09", "", "in the future"},
	} {
		got, err := parseOccurredAt(tc.in, today)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("parseOccurredAt(%q) error = %v, want %q", tc.in, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("parseOccurredAt(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// TestOccurredAt stores memories recorded later than they happened and checks
// recall orders and filters them by when they happened, not when stored
func TestOccurredAt(t *testing.T) {
	srv := newTestServer(t)
	day := func(daysAgo int) string { return time.Now().AddDate(0, 0, -daysAgo).Format(dateLayout) }

	stored := call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Score de cultura da Ana: notas parciais.", "about_person": "Ana",
		"occurred_at": day(10),
		"extraction":  &providers.EntityExtraction{MemoryType: "assessment", Summary: "Score de 10 dias atrás"},
	})
	if stored["occurred_at"] != day(10) {
		t.Errorf("ingest result occurred_at = %v, want %s", stored["occurred_at"], day(10))
	}
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Feedback antigo da Ana.", "about_person": "Ana",
		"occurred_at": day(120),
		"extraction": &providers.EntityExtraction{
			MemoryType: "feedback", Summary: "Feedback de 120 dias atrás",
			FeedbackItems: []providers.ExtractedFeedbackItem{{Category: "start", Content: "Delegar mais"}},
		},
	})
	call(t, "ingest", srv.HandleIngest, map[string]interface{}{
		"modality": "text", "content": "Ana conduziu a daily hoje.", "about_person": "Ana",
		"extraction": &providers.EntityExtraction{Summary: "Daily de hoje"},
	})

	summaries := func(brief map[string]interface{}) string {
		memories, _ := brief["memories"].([]interface{})
		var s []string
		for _, m := range memories {
			s = append(s, m.(map[string]interface{})["summary"].(string))
		}
		return strings.Join(s, " | ")
	}
	recall := func(timeRange string) map[string]interface{} {
		args := map[string]interface{}{"person_name": "Ana", "context": "1:1"}
		if timeRange != "" {
			args["time_range"] = timeRange
		}
		return call(t, "recall", srv.HandleRecall, args)
	}

	// Default last_90d: the feedback stored today but given 120 days ago is out
	brief := recall("")
	if got := summaries(brief); got != "Daily de hoje | Score de 10 dias atrás" {
		t.Errorf("recall (last_90d) memories = %q", got)
	}
	if brief["time_range"] != "last_90d" || brief["feedbacks"] != nil {
		t.Errorf("recall (last_90d): time_range = %v, feedbacks = %s", brief["time_range"], toJSON(brief["feedbacks"]))
	}
	if got := summaries(recall("all")); got != "Daily de hoje | Score de 10 dias atrás | Feedback de 120 dias atrás" {
		t.Errorf("recall (all) memories = %q", got)
	}
	if got := summaries(recall("last_30d")); got != "Daily de hoje | Score de 10 dias atrás" {
		t.Errorf("recall (last_30d) memories = %q", got)
	}

	// The date is shown where the memory and the feedback appear
	all := recall("all")
	memories := all["memories"].([]interface{})
	if memories[1].(map[string]interface{})["occurred_at"] != day(10) {
		t.Errorf("memory occurred_at = %s", toJSON(memories[1]))
	}
	if _, ok := memories[0].(map[string]interface{})["occurred_at"]; ok {
		t.Errorf("memory without a date has occurred_at: %s", toJSON(memories[0]))
	}
	feedbacks := all["feedbacks"].([]interface{})
	if feedbacks[0].(map[string]interface{})["occurred_at"] != day(120) {
		t.Errorf("feedback occurred_at = %s", toJSON(feedbacks[0]))
	}
	found := call(t, "search_memories", srv.HandleSearchMemories, map[string]interface{}{"query": "score", "mode": "keyword"})
	if results := found["results"].([]interface{}); len(results) != 1 || results[0].(map[string]interface{})["occurred_at"] != day(10) {
		t.Errorf("search results = %s, want the score with occurred_at %s", toJSON(found["results"]), day(10))
	}

	if _, err := srv.HandleRecall(context.Background(), map[string]interface{}{"person_name": "Ana", "time_range": "last_week"}); err == nil {
		t.Error("recall with an invalid time_range succeeded")
	}
	if _, err := srv.HandleIngest(context.Background(), map[string]interface{}{
		"modality": "text", "content": "x", "about_person": "Ana", "occurred_at": "02/09/2026",
	}); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Errorf("ingest with a Brazilian date: err = %v, want the format", err)
	}
}
