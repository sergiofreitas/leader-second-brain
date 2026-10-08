package mcpserver

import (
	"fmt"
	"strings"
	"time"
)

// dateLayout is how dates a memory is about are stored (occurred_at)
const dateLayout = "2006-01-02"

// TimeRanges are the accepted values of the recall "time_range" argument
var TimeRanges = []string{"last_30d", "last_90d", "last_year", "all"}

// parseOccurredAt validates when what a memory is about happened: a date
// (YYYY-MM-DD) or an RFC 3339 timestamp, of which only the date is kept.
// "" means it happened when it is stored. A date after today is refused:
// it is probably a typo, and a memory is about something that happened.
func parseOccurredAt(s string, today time.Time) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	date, err := time.Parse(dateLayout, s)
	if err != nil {
		t, rfcErr := time.Parse(time.RFC3339, s)
		if rfcErr != nil {
			return "", fmt.Errorf("invalid occurred_at %q: use YYYY-MM-DD (e.g. 2026-09-02)", s)
		}
		date = t
	}
	day := date.Format(dateLayout)
	if day > today.Format(dateLayout) {
		return "", fmt.Errorf("occurred_at %s is in the future: pass the day it happened", day)
	}
	return day, nil
}

// whenOf returns when what a node or search hit records happened: its
// occurred_at, else its created_at. ok is false when neither parses.
func whenOf(m map[string]interface{}) (t time.Time, ok bool) {
	for _, key := range []string{"occurred_at", "created_at"} {
		s, _ := m[key].(string)
		if s == "" {
			continue
		}
		// graph props are RFC 3339, the memories table uses SQLite's format
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", dateLayout} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// rangeStart returns the earliest moment a time range covers, or the zero
// time for "all"
func rangeStart(timeRange string, now time.Time) (time.Time, error) {
	switch timeRange {
	case "last_30d":
		return now.AddDate(0, 0, -30), nil
	case "last_90d":
		return now.AddDate(0, 0, -90), nil
	case "last_year":
		return now.AddDate(-1, 0, 0), nil
	case "all":
		return time.Time{}, nil
	}
	return time.Time{}, fmt.Errorf("invalid time_range %q (valid: %s)", timeRange, strings.Join(TimeRanges, ", "))
}

// since keeps the items that happened at or after start (all of them for
// the zero time). Items without a date are kept: better shown than lost.
func since(items []map[string]interface{}, start time.Time) []map[string]interface{} {
	if start.IsZero() {
		return items
	}
	kept := items[:0:0]
	for _, item := range items {
		if t, ok := whenOf(item); !ok || !t.Before(start) {
			kept = append(kept, item)
		}
	}
	return kept
}
