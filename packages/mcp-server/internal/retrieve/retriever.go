package retrieve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/graph"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// HybridRetriever combines graph traversal, FTS5 keyword search,
// and vector semantic search to assemble context for recall queries.
type HybridRetriever struct {
	store     *sqlite.Store
	graph     graph.GraphEngine
	embedding providers.EmbeddingProvider
}

// NewHybridRetriever creates a new hybrid retriever
func NewHybridRetriever(store *sqlite.Store, graph graph.GraphEngine, embedding providers.EmbeddingProvider) *HybridRetriever {
	return &HybridRetriever{
		store:     store,
		graph:     graph,
		embedding: embedding,
	}
}

// AssembleBriefing combines graph context, FTS5 results, and pending tasks
// into a structured briefing for the leader.
func (r *HybridRetriever) AssembleBriefing(
	personName string,
	contextType string,
	personCtx *graph.PersonContext,
	pendingTasks []map[string]interface{},
	ftsResults []map[string]interface{},
) map[string]interface{} {

	briefing := map[string]interface{}{
		"person":  personName,
		"context": contextType,
	}

	// Person info from graph
	if personCtx != nil && personCtx.NodeProps != nil {
		briefing["info"] = personCtx.NodeProps
	}

	// Hierarchy
	if personCtx != nil {
		if len(personCtx.Managers) > 0 {
			managers := make([]string, len(personCtx.Managers))
			for i, m := range personCtx.Managers {
				if name, ok := m["name"].(string); ok {
					managers[i] = name
				}
			}
			briefing["managers"] = managers
		}
		if len(personCtx.Reports) > 0 {
			reports := make([]string, len(personCtx.Reports))
			for i, rep := range personCtx.Reports {
				if name, ok := rep["name"].(string); ok {
					reports[i] = name
				}
			}
			briefing["reports"] = reports
		}
	}

	// Memories from graph
	if personCtx != nil && len(personCtx.Memories) > 0 {
		memories := make([]map[string]interface{}, 0, len(personCtx.Memories))
		for _, m := range personCtx.Memories {
			entry := map[string]interface{}{}
			if t, ok := m["type"].(string); ok {
				entry["type"] = t
			}
			if s, ok := m["summary"].(string); ok {
				entry["summary"] = s
			}
			if c, ok := m["content"].(string); ok {
				// Truncate long content for the briefing (by runes, so accented
				// characters aren't cut in half)
				if runes := []rune(c); len(runes) > 200 {
					entry["content"] = string(runes[:200]) + "..."
				} else {
					entry["content"] = c
				}
			}
			if ca, ok := m["created_at"].(string); ok {
				entry["created_at"] = ca
			}
			memories = append(memories, entry)
		}
		briefing["memories"] = memories

		// Recurring topics across this person's memories
		if topics := r.topicCounts(personCtx.Memories); len(topics) > 0 {
			briefing["topics"] = topics
		}
	}

	// Pending tasks
	if len(pendingTasks) > 0 {
		tasks := make([]map[string]interface{}, 0, len(pendingTasks))
		for _, t := range pendingTasks {
			task := map[string]interface{}{}
			if d, ok := t["description"].(string); ok {
				task["description"] = d
			}
			if s, ok := t["status"].(string); ok {
				task["status"] = s
			}
			if o, ok := t["owner"].(string); ok {
				task["owner"] = o
			}
			if ca, ok := t["created_at"].(string); ok {
				task["created_at"] = ca
			}
			tasks = append(tasks, task)
		}
		briefing["pending_tasks"] = tasks
	}

	// Feedbacks from graph
	if personCtx != nil && len(personCtx.Feedbacks) > 0 {
		feedbacks := make([]map[string]interface{}, 0, len(personCtx.Feedbacks))
		for _, f := range personCtx.Feedbacks {
			entry := map[string]interface{}{}
			if t, ok := f["type"].(string); ok {
				entry["type"] = t
			}
			if fmt_s, ok := f["format"].(string); ok {
				entry["format"] = fmt_s
			}
			if ca, ok := f["created_at"].(string); ok {
				entry["created_at"] = ca
			}
			if from, ok := f["from"].(string); ok {
				entry["from"] = from
			}
			if id, ok := f["id"].(string); ok {
				if items := r.feedbackItems(id); len(items) > 0 {
					entry["items"] = items
				}
			}
			feedbacks = append(feedbacks, entry)
		}
		briefing["feedbacks"] = feedbacks
	}

	// Assessments from graph
	if personCtx != nil && len(personCtx.Assessments) > 0 {
		assessments := make([]map[string]interface{}, 0, len(personCtx.Assessments))
		for _, a := range personCtx.Assessments {
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
			assessments = append(assessments, entry)
		}
		briefing["assessments"] = assessments
	}

	// FTS search results (keyword-matched memories)
	if len(ftsResults) > 0 {
		searchHits := make([]map[string]interface{}, 0, len(ftsResults))
		for _, r := range ftsResults {
			hit := map[string]interface{}{}
			if s, ok := r["snippet"].(string); ok {
				hit["snippet"] = s
			}
			if t, ok := r["type"].(string); ok {
				hit["type"] = t
			}
			if ca, ok := r["created_at"].(string); ok {
				hit["created_at"] = ca
			}
			searchHits = append(searchHits, hit)
		}
		briefing["keyword_matches"] = searchHits
	}

	// Context-specific recommendations
	briefing["recommendation"] = r.buildRecommendation(contextType, personCtx, pendingTasks)

	return briefing
}

// buildRecommendation generates a short textual recommendation based on the context type
func (r *HybridRetriever) buildRecommendation(
	contextType string,
	personCtx *graph.PersonContext,
	pendingTasks []map[string]interface{},
) string {
	var parts []string

	switch contextType {
	case "1:1":
		parts = append(parts, "Revise o histórico de conversas antes da 1:1.")
		if len(pendingTasks) > 0 {
			parts = append(parts, fmt.Sprintf("Você tem %d tarefa(s) pendente(s) com esta pessoa.", len(pendingTasks)))
		}
		if personCtx != nil && len(personCtx.Feedbacks) > 0 {
			parts = append(parts, "Existem feedbacks registrados — verifique se foram trabalhados.")
		}
	case "pdi":
		parts = append(parts, "Revise a última avaliação e os gaps identificados.")
		if personCtx != nil && len(personCtx.Assessments) > 0 {
			parts = append(parts, fmt.Sprintf("Última avaliação: %d registro(s) disponível(is).", len(personCtx.Assessments)))
		}
	case "feedback":
		parts = append(parts, "Revise feedbacks anteriores para evitar repetição.")
		if personCtx != nil && len(personCtx.Feedbacks) > 0 {
			parts = append(parts, fmt.Sprintf("%d feedback(s) já registrado(s).", len(personCtx.Feedbacks)))
		}
	case "team_review":
		parts = append(parts, "Verifique pendências e sinais de risco em toda a equipe.")
	case "progression":
		parts = append(parts, "Compare avaliações históricas para identificar evolução.")
		if personCtx != nil && len(personCtx.Assessments) > 0 {
			parts = append(parts, fmt.Sprintf("%d avaliação(ões) disponível(is) para comparação.", len(personCtx.Assessments)))
		}
	default:
		if len(pendingTasks) > 0 {
			parts = append(parts, fmt.Sprintf("%d tarefa(s) pendente(s).", len(pendingTasks)))
		}
	}

	if len(parts) == 0 {
		return "Nenhuma recomendação específica para este contexto."
	}
	return strings.Join(parts, " ")
}

// feedbackItems returns the items (category and content) of a Feedback node
func (r *HybridRetriever) feedbackItems(feedbackID string) []map[string]interface{} {
	edges, err := r.graph.GetEdges(feedbackID, "CONTAINS")
	if err != nil {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(edges))
	for _, e := range edges {
		item, err := r.graph.GetNode("FeedbackItem", e.To)
		if err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"category": item["category"],
			"content":  item["content"],
		})
	}
	return items
}

// topicCounts counts how many of the given memories discuss each topic,
// most frequent first
func (r *HybridRetriever) topicCounts(memories []map[string]interface{}) []map[string]interface{} {
	counts := map[string]int{}
	names := map[string]string{}
	for _, m := range memories {
		memID, _ := m["id"].(string)
		edges, err := r.graph.GetEdges(memID, "DISCUSSES")
		if err != nil {
			continue
		}
		for _, e := range edges {
			counts[e.To]++
			if _, ok := names[e.To]; !ok {
				if topic, err := r.graph.GetNode("Topic", e.To); err == nil {
					names[e.To], _ = topic["name"].(string)
				}
			}
		}
	}
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if counts[ids[i]] != counts[ids[j]] {
			return counts[ids[i]] > counts[ids[j]]
		}
		return names[ids[i]] < names[ids[j]]
	})
	topics := make([]map[string]interface{}, len(ids))
	for i, id := range ids {
		topics[i] = map[string]interface{}{"topic": names[id], "mentions": counts[id]}
	}
	return topics
}

// SemanticSearch performs vector similarity search and returns ranked results
func (r *HybridRetriever) SemanticSearch(query string, limit int) ([]map[string]interface{}, error) {
	if r.embedding == nil {
		return nil, fmt.Errorf("embedding provider not configured")
	}
	queryVec, err := r.embedding.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return r.store.SearchVector(queryVec, limit)
}
