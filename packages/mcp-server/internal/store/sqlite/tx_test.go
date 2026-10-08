package sqlite

import (
	"database/sql"
	"errors"
	"testing"
)

func TestInTx(t *testing.T) {
	s, err := New(t.TempDir() + "/tx.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Rollback: nothing written inside the transaction survives, and the
	// vector index never sees the embedding
	boom := errors.New("boom")
	err = s.InTx(func(tx *sql.Tx, st *Store) error {
		if err := st.InsertMemory("m_rb", "observation", "rolled back", "text", "test", "", "Ana", 1); err != nil {
			return err
		}
		if err := st.InsertVector("m_rb", []float32{1, 0, 0}); err != nil {
			return err
		}
		if err := st.UpsertPerson("p_rb", "Ana", "", "", "", 0); err != nil {
			return err
		}
		if s.vectors.len() != 0 {
			t.Error("vector reached the index before commit")
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx error = %v, want boom", err)
	}
	if _, err := s.GetMemory("m_rb"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("memory after rollback: err = %v, want ErrNoRows", err)
	}
	if _, err := s.GetPersonByName("Ana"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("person after rollback: err = %v, want ErrNoRows", err)
	}
	if s.vectors.len() != 0 {
		t.Errorf("vector index has %d entries after rollback, want 0", s.vectors.len())
	}

	// Commit: everything is visible, including to vector search
	err = s.InTx(func(tx *sql.Tx, st *Store) error {
		if err := st.InsertMemory("m_ok", "observation", "committed", "text", "test", "", "Ana", 1); err != nil {
			return err
		}
		return st.InsertVector("m_ok", []float32{1, 0, 0})
	})
	if err != nil {
		t.Fatalf("InTx commit: %v", err)
	}
	if _, err := s.GetMemory("m_ok"); err != nil {
		t.Errorf("memory after commit: %v", err)
	}
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 1); len(results) != 1 || results[0]["memory_id"] != "m_ok" {
		t.Errorf("vector search after commit = %v, want m_ok", results)
	}

	// A rolled back deletion keeps the vector in the index
	_ = s.InTx(func(tx *sql.Tx, st *Store) error {
		if err := st.DeleteVector("m_ok"); err != nil {
			return err
		}
		return boom
	})
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 1); len(results) != 1 {
		t.Errorf("vector search after rolled back delete = %v, want m_ok", results)
	}

	// Nested transactions and closing from inside a transaction are refused
	_ = s.InTx(func(tx *sql.Tx, st *Store) error {
		if err := st.InTx(func(*sql.Tx, *Store) error { return nil }); err == nil {
			t.Error("nested InTx succeeded")
		}
		if err := st.Close(); err == nil {
			t.Error("Close inside a transaction succeeded")
		}
		return nil
	})
}
