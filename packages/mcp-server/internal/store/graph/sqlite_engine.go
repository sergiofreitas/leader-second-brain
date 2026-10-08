package graph

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SQLiteEngine implements GraphEngine with two plain tables (graph_nodes,
// graph_edges) in the same SQLite database as the memory store, using
// recursive CTEs for traversals. Node and edge properties are JSON objects.
//
// It shares the store's *sql.DB, so there is a single connection pool and a
// single write lock for the whole knowledge base.
type SQLiteEngine struct {
	q dbtx // the database, or a transaction (see WithTx)
}

// dbtx is the query interface shared by *sql.DB and *sql.Tx
type dbtx interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// WithTx returns a copy of the engine whose operations run inside tx
func (g *SQLiteEngine) WithTx(tx *sql.Tx) GraphEngine {
	return &SQLiteEngine{q: tx}
}

// NewSQLiteEngine creates the graph tables (if needed) on db.
// The engine doesn't own db: Close doesn't close it.
func NewSQLiteEngine(db *sql.DB) (*SQLiteEngine, error) {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS graph_nodes (
			id TEXT PRIMARY KEY,
			label TEXT NOT NULL,
			props TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_graph_nodes_label ON graph_nodes(label)`,
		`CREATE TABLE IF NOT EXISTS graph_edges (
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			label TEXT NOT NULL,
			props TEXT NOT NULL DEFAULT '{}',
			PRIMARY KEY (from_id, label, to_id),
			FOREIGN KEY (from_id) REFERENCES graph_nodes(id) ON DELETE CASCADE,
			FOREIGN KEY (to_id) REFERENCES graph_nodes(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_graph_edges_to ON graph_edges(to_id, label)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("init graph schema: %w", err)
		}
	}
	return &SQLiteEngine{q: db}, nil
}

// AddNode creates a node, or merges props into it if it already exists
func (g *SQLiteEngine) AddNode(label string, id string, props map[string]interface{}) error {
	propsJSON, err := encodeProps(props)
	if err != nil {
		return err
	}
	_, err = g.q.Exec(
		`INSERT INTO graph_nodes (id, label, props) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET label = excluded.label, props = json_patch(props, excluded.props)`,
		id, label, propsJSON,
	)
	return err
}

// GetNode retrieves a node's properties (including its id) by label and id
func (g *SQLiteEngine) GetNode(label string, id string) (map[string]interface{}, error) {
	var propsJSON string
	err := g.q.QueryRow(
		`SELECT props FROM graph_nodes WHERE id = ? AND label = ?`, id, label,
	).Scan(&propsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("node not found: %s/%s", label, id)
	}
	if err != nil {
		return nil, err
	}
	return decodeNodeProps(id, propsJSON)
}

// UpdateNode merges props into an existing node
func (g *SQLiteEngine) UpdateNode(label string, id string, props map[string]interface{}) error {
	propsJSON, err := encodeProps(props)
	if err != nil {
		return err
	}
	_, err = g.q.Exec(
		`UPDATE graph_nodes SET props = json_patch(props, ?) WHERE id = ? AND label = ?`,
		propsJSON, id, label,
	)
	return err
}

// DeleteNode removes a node; its edges go with it (ON DELETE CASCADE)
func (g *SQLiteEngine) DeleteNode(label string, id string) error {
	_, err := g.q.Exec(`DELETE FROM graph_nodes WHERE id = ? AND label = ?`, id, label)
	return err
}

// AddEdge creates an edge, or merges props into it if it already exists.
// Both nodes must exist.
func (g *SQLiteEngine) AddEdge(from string, to string, label string, props map[string]interface{}) error {
	propsJSON, err := encodeProps(props)
	if err != nil {
		return err
	}
	_, err = g.q.Exec(
		`INSERT INTO graph_edges (from_id, to_id, label, props) VALUES (?, ?, ?, ?)
		 ON CONFLICT(from_id, label, to_id) DO UPDATE SET props = json_patch(props, excluded.props)`,
		from, to, label, propsJSON,
	)
	if err != nil {
		return fmt.Errorf("add edge %s -[%s]-> %s: %w", from, label, to, err)
	}
	return nil
}

// GetEdges returns all edges of the given label starting from 'from', in the
// order they were added (IDs made in the same clock tick don't sort by time)
func (g *SQLiteEngine) GetEdges(from string, label string) ([]EdgeResult, error) {
	return g.queryEdges(
		`SELECT from_id, to_id, label, props FROM graph_edges WHERE from_id = ? AND label = ? ORDER BY rowid`,
		from, label,
	)
}

// GetEdgesTo returns all edges of the given label pointing to 'to', in the
// order they were added
func (g *SQLiteEngine) GetEdgesTo(to string, label string) ([]EdgeResult, error) {
	return g.queryEdges(
		`SELECT from_id, to_id, label, props FROM graph_edges WHERE to_id = ? AND label = ? ORDER BY rowid`,
		to, label,
	)
}

func (g *SQLiteEngine) queryEdges(query string, args ...interface{}) ([]EdgeResult, error) {
	rows, err := g.q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var edges []EdgeResult
	for rows.Next() {
		var e EdgeResult
		var propsJSON string
		if err := rows.Scan(&e.From, &e.To, &e.Label, &propsJSON); err != nil {
			return nil, err
		}
		if e.Props, err = decodeProps(propsJSON); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// Traverse walks outgoing edges of the given labels (any label if empty) from
// startID, up to maxHops deep. Each node is reported once, at its shallowest
// depth, with the path (node ids, start included) that reached it.
func (g *SQLiteEngine) Traverse(startID string, edgeLabels []string, maxHops int) ([]TraversalResult, error) {
	labelFilter, args := inClause("e.label", edgeLabels)
	// The path is kept as ",id1,id2," so cycles can be cut with a LIKE check.
	// SQLite returns the bare "path" column from the row holding MIN(depth).
	query := `
		WITH RECURSIVE walk(id, depth, path) AS (
			SELECT ?, 0, ',' || ? || ','
			UNION ALL
			SELECT e.to_id, w.depth + 1, w.path || e.to_id || ','
			FROM walk w
			JOIN graph_edges e ON e.from_id = w.id
			WHERE w.depth < ?` + labelFilter + `
			  AND w.path NOT LIKE '%,' || e.to_id || ',%'
		)
		SELECT n.id, n.label, n.props, MIN(w.depth) AS depth, w.path
		FROM walk w
		JOIN graph_nodes n ON n.id = w.id
		WHERE w.depth > 0
		GROUP BY n.id
		ORDER BY depth, n.id`
	rows, err := g.q.Query(query, append([]interface{}{startID, startID, maxHops}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("traverse: %w", err)
	}
	defer rows.Close()

	var traversed []TraversalResult
	for rows.Next() {
		var r TraversalResult
		var propsJSON, path string
		if err := rows.Scan(&r.NodeID, &r.Label, &propsJSON, &r.Depth, &path); err != nil {
			return nil, err
		}
		if r.Props, err = decodeNodeProps(r.NodeID, propsJSON); err != nil {
			return nil, err
		}
		r.Path = strings.Split(strings.Trim(path, ","), ",")
		traversed = append(traversed, r)
	}
	return traversed, rows.Err()
}

// QueryByPattern finds the distinct nodes t matching (s)-[l0]->(i)-[l1]->(t),
// optionally restricted to a start node id and a target label.
func (g *SQLiteEngine) QueryByPattern(pattern PatternQuery) ([]QueryResult, error) {
	if len(pattern.EdgeLabels) != 2 {
		return nil, fmt.Errorf("currently only 2-hop patterns are supported")
	}
	query := `
		SELECT DISTINCT t.id, t.label, t.props
		FROM graph_edges e1
		JOIN graph_edges e2 ON e2.from_id = e1.to_id AND e2.label = ?
		JOIN graph_nodes t ON t.id = e2.to_id
		WHERE e1.label = ?`
	args := []interface{}{pattern.EdgeLabels[1], pattern.EdgeLabels[0]}
	if pattern.StartID != "" {
		query += ` AND e1.from_id = ?`
		args = append(args, pattern.StartID)
	}
	if pattern.TargetLabel != "" {
		query += ` AND t.label = ?`
		args = append(args, pattern.TargetLabel)
	}
	query += ` ORDER BY t.id`

	rows, err := g.q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []QueryResult
	for rows.Next() {
		var r QueryResult
		var propsJSON string
		if err := rows.Scan(&r.NodeID, &r.Label, &propsJSON); err != nil {
			return nil, err
		}
		if r.Props, err = decodeNodeProps(r.NodeID, propsJSON); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// neighbors returns the props of the nodes with nodeLabel linked to id by an
// edge of edgeLabel. outgoing selects (id)-[edge]->(n); otherwise (n)-[edge]->(id).
// where and orderBy are SQL fragments on the neighbor's props (alias n).
func (g *SQLiteEngine) neighbors(id, edgeLabel, nodeLabel string, outgoing bool, where, orderBy string) ([]map[string]interface{}, error) {
	join, match := "n.id = e.to_id", "e.from_id = ?"
	if !outgoing {
		join, match = "n.id = e.from_id", "e.to_id = ?"
	}
	query := `SELECT n.id, n.props FROM graph_edges e JOIN graph_nodes n ON ` + join +
		` WHERE ` + match + ` AND e.label = ? AND n.label = ?`
	if where != "" {
		query += ` AND ` + where
	}
	if orderBy == "" {
		orderBy = "e.rowid"
	}
	query += ` ORDER BY ` + orderBy

	rows, err := g.q.Query(query, id, edgeLabel, nodeLabel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []map[string]interface{}
	for rows.Next() {
		var nodeID, propsJSON string
		if err := rows.Scan(&nodeID, &propsJSON); err != nil {
			return nil, err
		}
		props, err := decodeNodeProps(nodeID, propsJSON)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, props)
	}
	return nodes, rows.Err()
}

// GetPersonContext returns the full context for a person in the graph
func (g *SQLiteEngine) GetPersonContext(personID string) (*PersonContext, error) {
	props, err := g.GetNode("Person", personID)
	if err != nil {
		return nil, err
	}
	ctx := &PersonContext{NodeProps: props}

	// By when it happened: occurred_at (a date) when the memory was stored
	// later, else when it was stored
	newestFirst := "COALESCE(json_extract(n.props, '$.occurred_at'), json_extract(n.props, '$.created_at')) DESC"
	lookups := []struct {
		dest      *[]map[string]interface{}
		edge      string
		nodeLabel string
		outgoing  bool
		where     string
		orderBy   string
	}{
		{&ctx.Managers, "REPORTS_TO", "Person", true, "", ""},
		{&ctx.Reports, "REPORTS_TO", "Person", false, "", ""},
		{&ctx.Memories, "ABOUT", "Memory", false, "", newestFirst},
		{&ctx.Tasks, "TARGETS", "Task", false, "json_extract(n.props, '$.status') = 'pending'", newestFirst},
		{&ctx.Feedbacks, "ABOUT", "Feedback", false, "", newestFirst},
		{&ctx.Assessments, "ASSESSED", "Assessment", false, "", "json_extract(n.props, '$.date') DESC"},
		{&ctx.Skills, "HAS_STRENGTH", "Skill", true, "", ""},
	}
	for _, l := range lookups {
		nodes, err := g.neighbors(personID, l.edge, l.nodeLabel, l.outgoing, l.where, l.orderBy)
		if err != nil {
			return nil, fmt.Errorf("person context (%s %s): %w", l.edge, l.nodeLabel, err)
		}
		*l.dest = nodes
	}
	return ctx, nil
}

// GetTeamHierarchy returns everyone reporting to a leader, directly or
// indirectly (up to 5 levels), with their depth below the leader.
func (g *SQLiteEngine) GetTeamHierarchy(leaderID string) ([]HierarchyNode, error) {
	rows, err := g.q.Query(`
		WITH RECURSIVE team(id, depth) AS (
			SELECT ?, 0
			UNION
			SELECT e.from_id, t.depth + 1
			FROM team t
			JOIN graph_edges e ON e.to_id = t.id AND e.label = 'REPORTS_TO'
			WHERE t.depth < 5
		)
		SELECT n.id, n.props, MIN(t.depth) AS depth
		FROM team t
		JOIN graph_nodes n ON n.id = t.id AND n.label = 'Person'
		WHERE t.id != ?
		GROUP BY n.id
		ORDER BY depth, json_extract(n.props, '$.name')`,
		leaderID, leaderID,
	)
	if err != nil {
		return nil, fmt.Errorf("team hierarchy: %w", err)
	}
	defer rows.Close()

	var nodes []HierarchyNode
	for rows.Next() {
		var node HierarchyNode
		var propsJSON string
		if err := rows.Scan(&node.ID, &propsJSON, &node.Depth); err != nil {
			return nil, err
		}
		if node.Props, err = decodeNodeProps(node.ID, propsJSON); err != nil {
			return nil, err
		}
		node.Name, _ = node.Props["name"].(string)
		node.Role, _ = node.Props["role"].(string)
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// Close is a no-op: the database belongs to the store that opened it
func (g *SQLiteEngine) Close() error {
	return nil
}

// ============================================================
// Helpers
// ============================================================

// encodeProps serializes props as a JSON object; "id" is dropped because the
// node id lives in its own column
func encodeProps(props map[string]interface{}) (string, error) {
	clean := make(map[string]interface{}, len(props))
	for k, v := range props {
		if k != "id" {
			clean[k] = v
		}
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return "", fmt.Errorf("encode props: %w", err)
	}
	return string(b), nil
}

func decodeProps(propsJSON string) (map[string]interface{}, error) {
	props := map[string]interface{}{}
	if err := json.Unmarshal([]byte(propsJSON), &props); err != nil {
		return nil, fmt.Errorf("decode props: %w", err)
	}
	return props, nil
}

// decodeNodeProps decodes a node's props and adds its id, so callers see the
// same shape a property graph would return
func decodeNodeProps(id, propsJSON string) (map[string]interface{}, error) {
	props, err := decodeProps(propsJSON)
	if err != nil {
		return nil, err
	}
	props["id"] = id
	return props, nil
}

// inClause returns " AND column IN (?, ...)" and its args, or "" for no values
func inClause(column string, values []string) (string, []interface{}) {
	if len(values) == 0 {
		return "", nil
	}
	placeholders := make([]string, len(values))
	args := make([]interface{}, len(values))
	for i, v := range values {
		placeholders[i] = "?"
		args[i] = v
	}
	return " AND " + column + " IN (" + strings.Join(placeholders, ", ") + ")", args
}
