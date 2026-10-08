package graph

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LackOfMorals/graphlite/v2"
)

// GraphliteEngine implements GraphEngine using Graphlite (openCypher on SQLite).
// CGO-free — Graphlite uses modernc.org/sqlite internally.
// All graph data lives in the same SQLite file as memories, FTS5, and vectors.
type GraphliteEngine struct {
	db *graphlite.DB
}

// NewGraphliteEngine creates a new graph engine backed by Graphlite.
// dbPath is the path to the SQLite database file (shared with the memory store).
func NewGraphliteEngine(dbPath string) (*GraphliteEngine, error) {
	db, err := graphlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open graphlite: %w", err)
	}
	return &GraphliteEngine{db: db}, nil
}

// AddNode creates or replaces a node with the given label and properties
func (g *GraphliteEngine) AddNode(label string, id string, props map[string]interface{}) error {
	// Build Cypher CREATE with MERGE to be idempotent
	propsMap := buildPropsMap(props)
	query := fmt.Sprintf(
		`MERGE (n:%s {id: $id}) SET n += $props`,
		label,
	)
	params := map[string]interface{}{
		"id":    id,
		"props": propsMap,
	}
	// Ensure 'id' is in props too
	propsMap["id"] = id
	return g.db.RunQuery(query, params)
}

// GetNode retrieves a node by label and id
func (g *GraphliteEngine) GetNode(label string, id string) (map[string]interface{}, error) {
	query := fmt.Sprintf(
		`MATCH (n:%s {id: $id}) RETURN n`,
		label,
	)
	result, err := g.db.RunQuery(query, map[string]interface{}{"id": id})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("node not found: %s/%s", label, id)
	}
	// Extract the node properties from the first row
	row := result[0]
	if node, ok := row["n"].(map[string]interface{}); ok {
		return node, nil
	}
	return row, nil
}

// UpdateNode merges properties into an existing node
func (g *GraphliteEngine) UpdateNode(label string, id string, props map[string]interface{}) error {
	propsMap := buildPropsMap(props)
	query := fmt.Sprintf(
		`MATCH (n:%s {id: $id}) SET n += $props`,
		label,
	)
	return g.db.RunQuery(query, map[string]interface{}{
		"id":    id,
		"props": propsMap,
	})
}

// DeleteNode removes a node and all its edges
func (g *GraphliteEngine) DeleteNode(label string, id string) error {
	query := fmt.Sprintf(
		`MATCH (n:%s {id: $id}) DETACH DELETE n`,
		label,
	)
	return g.db.RunQuery(query, map[string]interface{}{"id": id})
}

// AddEdge creates or replaces an edge between two nodes
func (g *GraphliteEngine) AddEdge(from string, to string, label string, props map[string]interface{}) error {
	propsMap := buildPropsMap(props)
	// MERGE on the relationship to be idempotent
	query := fmt.Sprintf(
		`MATCH (from {id: $from_id}), (to {id: $to_id})
		 MERGE (from)-[r:%s]->(to)
		 SET r += $props`,
		label,
	)
	return g.db.RunQuery(query, map[string]interface{}{
		"from_id": from,
		"to_id":   to,
		"props":   propsMap,
	})
}

// GetEdges returns all edges of the given label starting from 'from'
func (g *GraphliteEngine) GetEdges(from string, label string) ([]EdgeResult, error) {
	query := fmt.Sprintf(
		`MATCH (from {id: $from_id})-[r:%s]->(to)
		 RETURN from.id AS from_id, to.id AS to_id, type(r) AS label, r AS props`,
		label,
	)
	results, err := g.db.RunQuery(query, map[string]interface{}{"from_id": from})
	if err != nil {
		return nil, err
	}
	var edges []EdgeResult
	for _, row := range results {
		edge := EdgeResult{
			From:  getString(row, "from_id"),
			To:    getString(row, "to_id"),
			Label: getString(row, "label"),
		}
		if props, ok := row["props"].(map[string]interface{}); ok {
			edge.Props = props
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// GetEdgesTo returns all edges of the given label pointing to 'to'
func (g *GraphliteEngine) GetEdgesTo(to string, label string) ([]EdgeResult, error) {
	query := fmt.Sprintf(
		`MATCH (from)-[r:%s]->(to {id: $to_id})
		 RETURN from.id AS from_id, to.id AS to_id, type(r) AS label, r AS props`,
		label,
	)
	results, err := g.db.RunQuery(query, map[string]interface{}{"to_id": to})
	if err != nil {
		return nil, err
	}
	var edges []EdgeResult
	for _, row := range results {
		edge := EdgeResult{
			From:  getString(row, "from_id"),
			To:    getString(row, "to_id"),
			Label: getString(row, "label"),
		}
		if props, ok := row["props"].(map[string]interface{}); ok {
			edge.Props = props
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// Traverse walks the graph from a starting node, following edges of the given labels,
// up to maxHops deep. Uses Cypher variable-length paths.
func (g *GraphliteEngine) Traverse(startID string, edgeLabels []string, maxHops int) ([]TraversalResult, error) {
	// Build the relationship pattern: -[:LABEL1|LABEL2*1..maxHops]->
	labelPattern := buildLabelPattern(edgeLabels, maxHops)

	query := fmt.Sprintf(
		`MATCH (start {id: $start_id})%s(target)
		 RETURN target.id AS node_id, labels(target) AS labels, target AS props, 
		        length(p) AS depth, [n IN nodes(p) | n.id] AS path
		 ORDER BY depth, node_id`,
		labelPattern,
	)

	// For variable-length path, we need to assign the path
	query = strings.Replace(query, "%s(target)", fmt.Sprintf("%s(target) ", labelPattern), 1)

	results, err := g.db.RunQuery(query, map[string]interface{}{"start_id": startID})
	if err != nil {
		return nil, fmt.Errorf("traverse: %w", err)
	}

	var traversed []TraversalResult
	for _, row := range results {
		r := TraversalResult{
			NodeID: getString(row, "node_id"),
			Depth:  getInt(row, "depth"),
			Props:  getMap(row, "props"),
		}
		if labels, ok := row["labels"].([]interface{}); ok && len(labels) > 0 {
			if l, ok := labels[0].(string); ok {
				r.Label = l
			}
		}
		if path, ok := row["path"].([]interface{}); ok {
			for _, p := range path {
				r.Path = append(r.Path, fmt.Sprintf("%v", p))
			}
		}
		traversed = append(traversed, r)
	}
	return traversed, nil
}

// QueryByPattern finds nodes connected through a specific relationship pattern.
// For Graphlite, we translate the pattern into a Cypher query.
func (g *GraphliteEngine) QueryByPattern(pattern PatternQuery) ([]QueryResult, error) {
	if len(pattern.EdgeLabels) != 2 {
		return nil, fmt.Errorf("currently only 2-hop patterns are supported")
	}

	whereClause := ""
	params := map[string]interface{}{}

	if pattern.StartID != "" {
		whereClause = " WHERE s.id = $start_id"
		params["start_id"] = pattern.StartID
	}

	if pattern.TargetLabel != "" {
		if whereClause != "" {
			whereClause += " AND t:" + pattern.TargetLabel
		} else {
			whereClause = " WHERE t:" + pattern.TargetLabel
		}
	}

	query := fmt.Sprintf(
		`MATCH (s)-[:%s]->(i)-[:%s]->(t)%s
		 RETURN DISTINCT t.id AS node_id, labels(t) AS labels, t AS props`,
		pattern.EdgeLabels[0], pattern.EdgeLabels[1], whereClause,
	)

	results, err := g.db.RunQuery(query, params)
	if err != nil {
		return nil, err
	}

	var qr []QueryResult
	for _, row := range results {
		r := QueryResult{
			NodeID: getString(row, "node_id"),
			Props:  getMap(row, "props"),
		}
		if labels, ok := row["labels"].([]interface{}); ok && len(labels) > 0 {
			if l, ok := labels[0].(string); ok {
				r.Label = l
			}
		}
		qr = append(qr, r)
	}
	return qr, nil
}

// GetPersonContext returns the full context for a person in the graph
func (g *GraphliteEngine) GetPersonContext(personID string) (*PersonContext, error) {
	ctx := &PersonContext{}

	// Get the person node
	props, err := g.GetNode("Person", personID)
	if err != nil {
		return nil, err
	}
	ctx.NodeProps = props

	// Managers — who this person reports to
	managerResults, _ := g.db.RunQuery(
		`MATCH (p:Person {id: $id})-[:REPORTS_TO]->(m:Person) RETURN m`,
		map[string]interface{}{"id": personID},
	)
	for _, row := range managerResults {
		if m, ok := row["m"].(map[string]interface{}); ok {
			ctx.Managers = append(ctx.Managers, m)
		}
	}

	// Reports — who reports to this person
	reportResults, _ := g.db.RunQuery(
		`MATCH (r:Person)-[:REPORTS_TO]->(p:Person {id: $id}) RETURN r`,
		map[string]interface{}{"id": personID},
	)
	for _, row := range reportResults {
		if r, ok := row["r"].(map[string]interface{}); ok {
			ctx.Reports = append(ctx.Reports, r)
		}
	}

	// Memories about this person
	memResults, _ := g.db.RunQuery(
		`MATCH (m:Memory)-[:ABOUT]->(p:Person {id: $id})
		 RETURN m ORDER BY m.created_at DESC`,
		map[string]interface{}{"id": personID},
	)
	for _, row := range memResults {
		if m, ok := row["m"].(map[string]interface{}); ok {
			ctx.Memories = append(ctx.Memories, m)
		}
	}

	// Pending tasks targeting this person
	taskResults, _ := g.db.RunQuery(
		`MATCH (t:Task)-[:TARGETS]->(p:Person {id: $id})
		 WHERE t.status = 'pending'
		 RETURN t ORDER BY t.created_at DESC`,
		map[string]interface{}{"id": personID},
	)
	for _, row := range taskResults {
		if t, ok := row["t"].(map[string]interface{}); ok {
			ctx.Tasks = append(ctx.Tasks, t)
		}
	}

	// Feedbacks about this person
	fbResults, _ := g.db.RunQuery(
		`MATCH (f:Feedback)-[:ABOUT]->(p:Person {id: $id})
		 RETURN f ORDER BY f.created_at DESC`,
		map[string]interface{}{"id": personID},
	)
	for _, row := range fbResults {
		if f, ok := row["f"].(map[string]interface{}); ok {
			ctx.Feedbacks = append(ctx.Feedbacks, f)
		}
	}

	// Assessments of this person
	assessResults, _ := g.db.RunQuery(
		`MATCH (a:Assessment)-[:ASSESSED]->(p:Person {id: $id})
		 RETURN a ORDER BY a.date DESC`,
		map[string]interface{}{"id": personID},
	)
	for _, row := range assessResults {
		if a, ok := row["a"].(map[string]interface{}); ok {
			ctx.Assessments = append(ctx.Assessments, a)
		}
	}

	// Skills — strengths and development needs
	skillResults, _ := g.db.RunQuery(
		`MATCH (p:Person {id: $id})-[:HAS_STRENGTH]->(s:Skill)
		 RETURN s`,
		map[string]interface{}{"id": personID},
	)
	for _, row := range skillResults {
		if s, ok := row["s"].(map[string]interface{}); ok {
			ctx.Skills = append(ctx.Skills, s)
		}
	}

	return ctx, nil
}

// GetTeamHierarchy returns all people reporting to a leader (recursive)
func (g *GraphliteEngine) GetTeamHierarchy(leaderID string) ([]HierarchyNode, error) {
	// Use variable-length path for recursive hierarchy traversal
	results, err := g.db.RunQuery(
		`MATCH (leader:Person {id: $id})-[:REPORTS_TO*1..5]->(report:Person)
		 RETURN report.id AS id, report.name AS name, report.role AS role,
		        report AS props,
		        length(p) AS depth
		 ORDER BY depth`,
		map[string]interface{}{"id": leaderID},
	)
	if err != nil {
		// Fallback: try simpler query without path variable
		results, err = g.db.RunQuery(
			`MATCH (leader:Person {id: $id})-[:REPORTS_TO*1..5]->(report:Person)
			 RETURN report.id AS id, report.name AS name, report.role AS role,
			        report AS props
			 ORDER BY report.name`,
			map[string]interface{}{"id": leaderID},
		)
		if err != nil {
			return nil, fmt.Errorf("team hierarchy: %w", err)
		}
	}

	var nodes []HierarchyNode
	for _, row := range results {
		node := HierarchyNode{
			ID:   getString(row, "id"),
			Name: getString(row, "name"),
			Role: getString(row, "role"),
			Props: getMap(row, "props"),
		}
		if d, ok := row["depth"]; ok {
			node.Depth = getInt(row, "depth")
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// Close persists and releases resources
func (g *GraphliteEngine) Close() error {
	return g.db.Close()
}

// ============================================================
// Helpers
// ============================================================

func buildPropsMap(props map[string]interface{}) map[string]interface{} {
	if props == nil {
		return map[string]interface{}{}
	}
	// Deep copy to avoid mutation
	out := make(map[string]interface{}, len(props))
	for k, v := range props {
		out[k] = v
	}
	return out
}

func buildLabelPattern(labels []string, maxHops int) string {
	if len(labels) == 0 {
		return fmt.Sprintf("-[*1..%d]->", maxHops)
	}
	quoted := make([]string, len(labels))
	for i, l := range labels {
		quoted[i] = l
	}
	return fmt.Sprintf("-[:%s*1..%d]->", strings.Join(quoted, "|"), maxHops)
}

func getString(row map[string]interface{}, key string) string {
	if v, ok := row[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func getInt(row map[string]interface{}, key string) int {
	if v, ok := row[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

func getMap(row map[string]interface{}, key string) map[string]interface{} {
	if v, ok := row[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
		// Try to marshal/unmarshal if it's a different type
		if b, err := json.Marshal(v); err == nil {
			var m map[string]interface{}
			if json.Unmarshal(b, &m) == nil {
				return m
			}
		}
	}
	return nil
}
