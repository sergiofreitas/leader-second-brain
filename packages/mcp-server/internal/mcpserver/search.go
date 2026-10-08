package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SearchModes are the accepted values of the search_memories "mode" argument
var SearchModes = []string{"hybrid", "keyword", "semantic"}

// rrfK dampens the weight of top ranks in Reciprocal Rank Fusion; 60 is the
// value from the original paper and the usual default
const rrfK = 60

// HandleSearchMemories processes a search_memories tool call.
//
// keyword: FTS5 over the query's words plus the host's extra terms
// (synonyms, inflections, prefixes), ranked by BM25.
// semantic: the query embedded and compared with every passage.
// hybrid (default with an embedding provider): both, fused with Reciprocal
// Rank Fusion — each memory scores the sum of 1/(60 + rank) over the lists it
// appears in, so agreeing signals rise and neither scale dominates.
func (s *Server) HandleSearchMemories(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	query, _ := args["query"].(string)
	terms, _ := args["terms"].([]string)
	mode, _ := args["mode"].(string)
	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	query = strings.TrimSpace(query)
	if query == "" && len(terms) == 0 {
		return nil, fmt.Errorf("query is required")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if semantic, _ := args["semantic"].(bool); mode == "" && semantic {
		mode = "semantic" // the old boolean argument
	}
	if mode == "" {
		mode = "keyword"
		if s.embedding != nil {
			mode = "hybrid"
		}
	}
	if !contains(SearchModes, mode) {
		return nil, fmt.Errorf("invalid mode %q (valid: %s)", mode, strings.Join(SearchModes, ", "))
	}

	out := map[string]interface{}{"query": query, "mode": mode}
	if s.semanticProblem != "" {
		out["semantic_unavailable"] = s.semanticProblem
	}
	if len(terms) > 0 {
		out["terms"] = terms
	}
	// Each list fetches more than limit, so fusion has candidates to rank
	fetch := limit * 3

	var keyword, semantic []map[string]interface{}
	var err error
	if mode == "keyword" || mode == "hybrid" {
		if keyword, err = s.store.SearchFTS(query, terms, fetch); err != nil {
			return nil, fmt.Errorf("keyword search: %w", err)
		}
	}
	if mode == "semantic" || mode == "hybrid" {
		semantic, err = s.semanticSearch(query, fetch)
		switch {
		case err != nil && mode == "semantic":
			return nil, err
		case err != nil:
			// Hybrid degrades to keyword search: a gateway outage shouldn't
			// make search unusable
			out["semantic_error"] = err.Error()
		}
		if status, err := s.indexStatus(); err == nil && status != nil {
			out["index_status"] = status
		}
	}

	results := fuse(keyword, semantic, limit)
	out["count"] = len(results)
	out["results"] = results
	resultJSON, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// SemanticSearchEnabled reports whether an embedding provider is configured
func (s *Server) SemanticSearchEnabled() bool { return s.embedding != nil }

// SemanticSearchProblem says why a configured embedding provider couldn't
// start, or "" when none is configured or it started
func (s *Server) SemanticSearchProblem() string { return s.semanticProblem }

// semanticSearch embeds the query and returns the most similar memories
func (s *Server) semanticSearch(query string, limit int) ([]map[string]interface{}, error) {
	if s.embedding == nil && s.semanticProblem != "" {
		return nil, fmt.Errorf("semantic search is disabled: %s; use keyword search instead", s.semanticProblem)
	}
	if s.embedding == nil {
		return nil, fmt.Errorf("semantic search is disabled: no embedding provider is configured (see the embedding section of docs/configuration.md); use keyword search instead")
	}
	if query == "" {
		return nil, fmt.Errorf("semantic search needs a query")
	}
	queryVec, err := s.embedding.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	results, err := s.store.SearchVector(queryVec, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	return results, nil
}

// indexStatus returns the indexing progress when passages are pending or
// failing (recent memories may not be searchable by meaning yet), else nil
func (s *Server) indexStatus() (interface{}, error) {
	if s.indexer == nil {
		return nil, nil
	}
	status, err := s.indexer.Status()
	if err != nil || (status.Pending == 0 && status.Failed == 0 && status.LastError == "") {
		return nil, err
	}
	return status, nil
}

// fuse merges keyword and semantic results (each best first) into one list
// of memories, ranked by Reciprocal Rank Fusion. A memory found by only one
// search keeps that search's order; one found by both rises.
func fuse(keyword, semantic []map[string]interface{}, limit int) []map[string]interface{} {
	type hit struct {
		result    map[string]interface{}
		score     float64
		matchedBy []string
	}
	hits := map[string]*hit{}
	var order []string // first appearance, for stable ties

	add := func(list []map[string]interface{}, source string) {
		for rank, r := range list {
			id, _ := r["memory_id"].(string)
			h, ok := hits[id]
			if !ok {
				h = &hit{result: map[string]interface{}{
					"memory_id":    id,
					"type":         r["type"],
					"about_person": r["about_person"],
					"created_at":   r["created_at"],
				}}
				if occurredAt, ok := r["occurred_at"]; ok {
					h.result["occurred_at"] = occurredAt
				}
				hits[id] = h
				order = append(order, id)
			}
			h.score += 1.0 / float64(rrfK+rank+1)
			h.matchedBy = append(h.matchedBy, source)
			// The semantic passage is the better excerpt (a whole passage,
			// not a few words around a keyword); keep the snippet as well
			if excerpt, ok := r["excerpt"]; ok {
				h.result["excerpt"] = excerpt
				h.result["similarity"] = r["similarity"]
			}
			if snippet, ok := r["snippet"]; ok {
				h.result["snippet"] = snippet
			}
		}
	}
	add(keyword, "keyword")
	add(semantic, "semantic")

	sort.SliceStable(order, func(i, j int) bool { return hits[order[i]].score > hits[order[j]].score })
	if len(order) > limit {
		order = order[:limit]
	}
	results := make([]map[string]interface{}, len(order))
	for i, id := range order {
		h := hits[id]
		h.result["matched_by"] = h.matchedBy
		h.result["score"] = h.score
		results[i] = h.result
	}
	return results
}
