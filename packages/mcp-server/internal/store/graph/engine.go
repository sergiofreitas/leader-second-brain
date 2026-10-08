package graph

import "database/sql"

// GraphEngine is the interface for graph operations.
// Currently implemented by SQLiteEngine (graph tables in the store's SQLite file).
type GraphEngine interface {
	// WithTx returns a copy of the engine whose operations run inside tx,
	// so graph writes can commit or roll back together with the store's
	WithTx(tx *sql.Tx) GraphEngine

	// Node operations
	AddNode(label string, id string, props map[string]interface{}) error
	GetNode(label string, id string) (map[string]interface{}, error)
	UpdateNode(label string, id string, props map[string]interface{}) error
	DeleteNode(label string, id string) error

	// Edge operations
	AddEdge(from string, to string, label string, props map[string]interface{}) error
	GetEdges(from string, label string) ([]EdgeResult, error)
	GetEdgesTo(to string, label string) ([]EdgeResult, error)

	// Traversal
	Traverse(startID string, edgeLabels []string, maxHops int) ([]TraversalResult, error)

	// Pattern matching
	QueryByPattern(pattern PatternQuery) ([]QueryResult, error)

	// Context retrieval
	GetPersonContext(personID string) (*PersonContext, error)
	GetTeamHierarchy(leaderID string) ([]HierarchyNode, error)

	// Lifecycle
	Close() error
}

// EdgeResult represents a matched edge
type EdgeResult struct {
	From  string                 `json:"from"`
	To    string                 `json:"to"`
	Label string                 `json:"label"`
	Props map[string]interface{} `json:"props,omitempty"`
}

// TraversalResult represents a node found during traversal
type TraversalResult struct {
	NodeID string                 `json:"node_id"`
	Label  string                 `json:"label"`
	Depth  int                    `json:"depth"`
	Props  map[string]interface{} `json:"props"`
	Path   []string               `json:"path"`
}

// PatternQuery defines a graph pattern to match
type PatternQuery struct {
	StartLabel  string                 `json:"start_label"`
	StartID     string                 `json:"start_id,omitempty"`
	EdgeLabels  []string               `json:"edge_labels"`
	TargetLabel string                 `json:"target_label"`
	WhereProps  map[string]interface{} `json:"where_props,omitempty"`
}

// QueryResult is a single match from a pattern query
type QueryResult struct {
	NodeID string                 `json:"node_id"`
	Label  string                 `json:"label"`
	Props  map[string]interface{} `json:"props"`
}

// PersonContext is the full context for a person in the graph
type PersonContext struct {
	NodeProps   map[string]interface{}   `json:"node_props"`
	Managers    []map[string]interface{} `json:"managers"`
	Reports     []map[string]interface{} `json:"reports"`
	Memories    []map[string]interface{} `json:"memories"`
	Tasks       []map[string]interface{} `json:"tasks"`
	Feedbacks   []map[string]interface{} `json:"feedbacks"`
	Assessments []map[string]interface{} `json:"assessments"`
	Skills      []map[string]interface{} `json:"skills"`
	Patterns    []map[string]interface{} `json:"patterns"`
	Signals     []map[string]interface{} `json:"signals"`
}

// HierarchyNode is a person in the hierarchy tree
type HierarchyNode struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Role  string                 `json:"role"`
	Depth int                    `json:"depth"`
	Props map[string]interface{} `json:"props,omitempty"`
}
