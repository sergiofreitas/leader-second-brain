package sqlite

import (
	"database/sql"
	"testing"
)

// TestOldDatabaseGetsNewColumns opens a database created before occurred_at
// and completed_at existed: it gets them, and its data is kept
func TestOldDatabaseGetsNewColumns(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := New(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.InsertMemory("m1", "observation", "antes", "text", "test", "", "Ana", 1); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	if err := s.InsertTask("t1", "Falar com a Ana", "", "pending"); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	// Back to the v0.2 schema
	for _, stmt := range []string{
		`ALTER TABLE memories DROP COLUMN occurred_at`,
		`ALTER TABLE tasks DROP COLUMN completed_at`,
	} {
		if _, err := s.DB().Exec(stmt); err != nil {
			t.Fatalf("old schema: %v", err)
		}
	}
	s.Close()

	for i := 0; i < 2; i++ { // opening twice must not add the columns twice
		s, err := New(path)
		if err != nil {
			t.Fatalf("open old database (%d): %v", i+1, err)
		}
		if err := s.SetMemoryOccurredAt("m1", "2026-09-02"); err != nil {
			t.Errorf("set occurred_at: %v", err)
		}
		var occurred, completed sql.NullString
		s.DB().QueryRow(`SELECT occurred_at FROM memories WHERE id = 'm1'`).Scan(&occurred)
		if err := s.DB().QueryRow(`SELECT completed_at FROM tasks WHERE id = 't1'`).Scan(&completed); err != nil {
			t.Errorf("tasks.completed_at: %v", err)
		}
		if occurred.String != "2026-09-02" {
			t.Errorf("occurred_at = %q", occurred.String)
		}
		if results, err := s.SearchFTS("antes", nil, 10); err != nil || len(results) != 1 {
			t.Errorf("old memory in FTS = %v, %v", results, err)
		}
		s.Close()
	}
}
