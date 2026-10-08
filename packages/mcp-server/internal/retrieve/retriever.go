package retrieve

import (
	"fmt"
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
			if c, ok := m["content"].(string); ok {
				// Truncate long content for the briefing
				if len(c) > 200 {
					entry["content"] = c[:200] + "..."
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
