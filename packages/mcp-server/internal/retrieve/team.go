package retrieve

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/graph"
)

// Thresholds of the team view's signals
const (
	quietDays     = 60 // nothing recorded about someone for this long
	staleTaskDays = 30 // a task concerning someone pending for this long
)

// Member is a person of a leader's team, with their context
type Member struct {
	Node    graph.HierarchyNode
	Context *graph.PersonContext
}

// AssembleTeam builds the team view: for each member their pending tasks,
// last feedback, last assessment, recurring topics and signals; for the
// team, the topics several members share (what collective actions address)
// and the tasks the leader has pending.
func (r *HybridRetriever) AssembleTeam(leaderName string, leader *graph.PersonContext, members []Member, now time.Time) map[string]interface{} {
	entries := make([]map[string]interface{}, 0, len(members))
	type share struct {
		name   string
		people []string
	}
	shared := map[string]*share{}

	for _, m := range members {
		ctx := m.Context
		entry := map[string]interface{}{
			"name":     m.Node.Name,
			"role":     m.Node.Role,
			"depth":    m.Node.Depth,
			"memories": len(ctx.Memories),
		}
		if len(ctx.Managers) > 0 {
			entry["manager"] = ctx.Managers[0]["name"]
		}
		if len(ctx.Tasks) > 0 {
			entry["pending_tasks"] = TaskEntries(ctx.Tasks)
		}
		if len(ctx.Feedbacks) > 0 {
			entry["last_feedback"] = r.feedbackEntry(ctx.Feedbacks[0])
		}
		if a := assessmentEntries(ctx); len(a) > 0 {
			entry["last_assessment"] = a[0]
		}
		if len(ctx.Memories) > 0 {
			last := map[string]interface{}{}
			copyDates(last, ctx.Memories[0])
			entry["last_memory"] = last
		}
		topics := r.TopicCounts(ctx.Memories)
		if len(topics) > 0 {
			entry["topics"] = topics
		}
		for _, t := range topics {
			name := t["topic"].(string)
			if shared[name] == nil {
				shared[name] = &share{name: name}
			}
			shared[name].people = append(shared[name].people, m.Node.Name)
		}
		if signals := memberSignals(ctx, now); len(signals) > 0 {
			entry["signals"] = signals
		}
		entries = append(entries, entry)
	}

	// Topics of two people or more, the most shared first
	var common []*share
	for _, s := range shared {
		if len(s.people) >= 2 {
			common = append(common, s)
		}
	}
	sort.Slice(common, func(i, j int) bool {
		if len(common[i].people) != len(common[j].people) {
			return len(common[i].people) > len(common[j].people)
		}
		return strings.ToLower(common[i].name) < strings.ToLower(common[j].name)
	})
	sharedTopics := make([]map[string]interface{}, len(common))
	for i, s := range common {
		sharedTopics[i] = map[string]interface{}{"topic": s.name, "people": s.people, "count": len(s.people)}
	}

	team := map[string]interface{}{
		"leader":        leaderName,
		"team_size":     len(members),
		"members":       entries,
		"shared_topics": sharedTopics,
	}
	if leader != nil && len(leader.OwnedTasks) > 0 {
		team["leader_pending_tasks"] = TaskEntries(leader.OwnedTasks)
	}
	return team
}

// memberSignals flags what may need the leader's attention: no feedback
// recorded, nothing recorded for a while, tasks pending for long
func memberSignals(ctx *graph.PersonContext, now time.Time) []map[string]interface{} {
	var signals []map[string]interface{}
	add := func(signal, detail string) {
		signals = append(signals, map[string]interface{}{"signal": signal, "detail": detail})
	}
	if len(ctx.Feedbacks) == 0 {
		add("no_feedback", "no feedback recorded")
	}
	if len(ctx.Memories) == 0 {
		add("no_recent_memory", "nothing recorded about them")
	} else if last, ok := WhenOf(ctx.Memories[0]); ok && now.Sub(last) > quietDays*24*time.Hour {
		add("no_recent_memory", fmt.Sprintf("nothing recorded in %d days (last: %s)", int(now.Sub(last).Hours()/24), last.Format("2006-01-02")))
	}
	for _, t := range ctx.Tasks {
		if created, ok := WhenOf(t); ok && now.Sub(created) > staleTaskDays*24*time.Hour {
			add("stale_task", fmt.Sprintf("%q pending for %d days", t["description"], int(now.Sub(created).Hours()/24)))
		}
	}
	return signals
}

// feedbackEntry lists a Feedback node with its items, who gave it and when
func (r *HybridRetriever) feedbackEntry(f map[string]interface{}) map[string]interface{} {
	entry := map[string]interface{}{}
	copyDates(entry, f)
	if from, ok := f["from"].(string); ok {
		entry["from"] = from
	}
	if id, ok := f["id"].(string); ok {
		if items := r.feedbackItems(id); len(items) > 0 {
			entry["items"] = items
		}
	}
	return entry
}

// assessmentEntries lists a person's assessments, newest first: Assessment
// nodes when there are, else the memories of type assessment (what ingest
// stores, e.g. a culture score)
func assessmentEntries(ctx *graph.PersonContext) []map[string]interface{} {
	var entries []map[string]interface{}
	for _, a := range ctx.Assessments {
		entry := map[string]interface{}{}
		if d, ok := a["date"].(string); ok {
			entry["date"] = d
		}
		if l, ok := a["current_level"]; ok {
			entry["level"] = l
		}
		if tl, ok := a["target_level"]; ok {
			entry["target_level"] = tl
		}
		entries = append(entries, entry)
	}
	if len(entries) > 0 {
		return entries
	}
	for _, m := range ctx.Memories {
		if m["type"] != "assessment" {
			continue
		}
		entry := map[string]interface{}{}
		if s, ok := m["summary"].(string); ok {
			entry["summary"] = s
		}
		copyDates(entry, m)
		entries = append(entries, entry)
	}
	return entries
}

// WhenOf returns when what a node or search hit records happened: when a
// task was completed, when a memory occurred, else when it was created.
func WhenOf(m map[string]interface{}) (time.Time, bool) {
	for _, key := range []string{"completed_at", "occurred_at", "created_at"} {
		s, _ := m[key].(string)
		if s == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}
