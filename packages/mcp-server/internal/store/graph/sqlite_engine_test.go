package graph

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestEngine(t *testing.T) *SQLiteEngine {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/graph.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	g, err := NewSQLiteEngine(db)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return g
}

func must(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// TestSQLiteEngine exercises every GraphEngine method
func TestSQLiteEngine(t *testing.T) {
	g := newTestEngine(t)

	must(t, "add head", g.AddNode("Person", "head", map[string]interface{}{"name": "Head", "role": "head"}))
	must(t, "add lead", g.AddNode("Person", "lead", map[string]interface{}{"name": "Sérgio", "role": "manager"}))
	must(t, "add person", g.AddNode("Person", "p1", map[string]interface{}{"name": "Ana", "role": ""}))
	must(t, "re-add person", g.AddNode("Person", "p1", map[string]interface{}{"name": "Ana Silva"}))
	must(t, "update person", g.UpdateNode("Person", "p1", map[string]interface{}{"role": "dev"}))
	must(t, "add memory", g.AddNode("Memory", "m1", map[string]interface{}{"type": "observation", "created_at": "2026-10-01"}))
	must(t, "add memory 2", g.AddNode("Memory", "m2", map[string]interface{}{"type": "observation", "created_at": "2026-10-07"}))
	must(t, "add task", g.AddNode("Task", "t1", map[string]interface{}{"status": "pending", "created_at": "2026-10-07"}))
	must(t, "add done task", g.AddNode("Task", "t2", map[string]interface{}{"status": "done", "created_at": "2026-10-07"}))
	must(t, "edge ABOUT", g.AddEdge("m1", "p1", "ABOUT", nil))
	must(t, "edge ABOUT again", g.AddEdge("m1", "p1", "ABOUT", nil))
	must(t, "edge ABOUT 2", g.AddEdge("m2", "p1", "ABOUT", nil))
	must(t, "edge TARGETS", g.AddEdge("t1", "p1", "TARGETS", map[string]interface{}{"weight": 1}))
	must(t, "edge TARGETS done", g.AddEdge("t2", "p1", "TARGETS", nil))
	must(t, "edge p1 REPORTS_TO lead", g.AddEdge("p1", "lead", "REPORTS_TO", nil))
	must(t, "edge lead REPORTS_TO head", g.AddEdge("lead", "head", "REPORTS_TO", nil))

	node, err := g.GetNode("Person", "p1")
	must(t, "get node", err)
	if node["id"] != "p1" || node["name"] != "Ana Silva" || node["role"] != "dev" {
		t.Errorf("node props = %v, want id p1, name 'Ana Silva' and role 'dev'", node)
	}
	if _, err := g.GetNode("Memory", "p1"); err == nil {
		t.Error("GetNode with the wrong label found a node")
	}

	edges, err := g.GetEdges("m1", "ABOUT")
	must(t, "get edges", err)
	if len(edges) != 1 || edges[0].To != "p1" || edges[0].Label != "ABOUT" {
		t.Errorf("edges from m1 = %+v, want a single ABOUT edge to p1", edges)
	}
	edges, err = g.GetEdgesTo("p1", "TARGETS")
	must(t, "get edges to", err)
	if len(edges) != 2 || edges[0].From != "t1" || edges[0].Props["weight"] != float64(1) {
		t.Errorf("edges to p1 = %+v, want TARGETS from t1 (weight 1) and t2", edges)
	}

	pc, err := g.GetPersonContext("p1")
	must(t, "person context", err)
	if len(pc.Managers) != 1 || pc.Managers[0]["name"] != "Sérgio" {
		t.Errorf("managers = %v, want Sérgio", pc.Managers)
	}
	if len(pc.Memories) != 2 || pc.Memories[0]["id"] != "m2" {
		t.Errorf("memories = %v, want m2 then m1 (newest first)", pc.Memories)
	}
	if len(pc.Tasks) != 1 || pc.Tasks[0]["id"] != "t1" {
		t.Errorf("tasks = %v, want only the pending t1", pc.Tasks)
	}

	traversed, err := g.Traverse("m1", []string{"ABOUT", "REPORTS_TO"}, 3)
	must(t, "traverse", err)
	got := fmt.Sprint(traversed)
	want := fmt.Sprint([]TraversalResult{
		{NodeID: "p1", Label: "Person", Depth: 1, Props: node, Path: []string{"m1", "p1"}},
	})
	if len(traversed) != 3 || fmt.Sprint(traversed[:1]) != want ||
		traversed[1].NodeID != "lead" || traversed[1].Depth != 2 ||
		traversed[2].NodeID != "head" || fmt.Sprint(traversed[2].Path) != "[m1 p1 lead head]" {
		t.Errorf("traverse = %s, want p1 (1), lead (2), head (3)", got)
	}
	if traversed, _ := g.Traverse("m1", []string{"ABOUT"}, 3); len(traversed) != 1 {
		t.Errorf("traverse filtered by ABOUT = %+v, want only p1", traversed)
	}

	team, err := g.GetTeamHierarchy("head")
	must(t, "team hierarchy", err)
	if len(team) != 2 || team[0].ID != "lead" || team[0].Depth != 1 || team[0].Name != "Sérgio" ||
		team[1].ID != "p1" || team[1].Depth != 2 || team[1].Role != "dev" {
		t.Errorf("team = %+v, want lead (1) and p1 (2)", team)
	}

	matches, err := g.QueryByPattern(PatternQuery{
		EdgeLabels:  []string{"ABOUT", "REPORTS_TO"},
		StartID:     "m1",
		TargetLabel: "Person",
	})
	must(t, "query by pattern", err)
	if len(matches) != 1 || matches[0].NodeID != "lead" || matches[0].Label != "Person" {
		t.Errorf("pattern matches = %+v, want lead", matches)
	}

	if err := g.AddEdge("m1", "missing", "ABOUT", nil); err == nil {
		t.Error("AddEdge to a missing node succeeded")
	}

	must(t, "delete task", g.DeleteNode("Task", "t1"))
	if _, err := g.GetNode("Task", "t1"); err == nil {
		t.Error("task still exists after DeleteNode")
	}
	if edges, _ := g.GetEdgesTo("p1", "TARGETS"); len(edges) != 1 {
		t.Errorf("edges to p1 after delete = %+v, want only t2's", edges)
	}
}

// TestSQLiteEngineCycles checks that traversals terminate on cyclic graphs
func TestSQLiteEngineCycles(t *testing.T) {
	g := newTestEngine(t)
	for _, id := range []string{"a", "b", "c"} {
		must(t, "add "+id, g.AddNode("Person", id, map[string]interface{}{"name": id}))
	}
	must(t, "a->b", g.AddEdge("a", "b", "REPORTS_TO", nil))
	must(t, "b->c", g.AddEdge("b", "c", "REPORTS_TO", nil))
	must(t, "c->a", g.AddEdge("c", "a", "REPORTS_TO", nil))

	traversed, err := g.Traverse("a", nil, 10)
	must(t, "traverse", err)
	if len(traversed) != 2 || traversed[0].NodeID != "b" || traversed[1].NodeID != "c" {
		t.Errorf("traverse = %+v, want b (1) and c (2)", traversed)
	}

	team, err := g.GetTeamHierarchy("a")
	must(t, "team hierarchy", err)
	if len(team) != 2 || team[0].ID != "c" || team[1].ID != "b" {
		t.Errorf("team = %+v, want c (1) and b (2)", team)
	}
}
