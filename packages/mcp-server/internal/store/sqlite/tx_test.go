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
	if err := s.UseEmbeddingModel("m1"); err != nil {
		t.Fatalf("use model: %v", err)
	}

	// Rollback: nothing written inside the transaction survives, and index
	// changes made inside it never reach the vector index
	boom := errors.New("boom")
	err = s.InTx(func(tx *sql.Tx, st *Store) error {
		if err := st.InsertMemory("m_rb", "observation", "rolled back", "text", "test", "", "Ana", 1); err != nil {
			return err
		}
		if err := st.InsertChunks("m_rb", []string{"rolled back"}); err != nil {
			return err
		}
		if err := st.UpsertPerson("p_rb", "Ana", "", "", "", 0); err != nil {
			return err
		}
		st.updateIndex("1", []float32{1, 0, 0})
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
	var chunks int
	s.DB().QueryRow(`SELECT count(*) FROM memory_chunks`).Scan(&chunks)
	if chunks != 0 || s.vectors.len() != 0 {
		t.Errorf("after rollback: %d chunks, %d indexed vectors, want 0", chunks, s.vectors.len())
	}

	// Commit: memory, chunks and embeddings are visible, including to search
	ids := addMemory(t, s, "m_ok", "committed")
	if err := s.SaveEmbeddings("m1", ids, [][]float32{{1, 0, 0}}); err != nil {
		t.Fatalf("save embeddings: %v", err)
	}
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 1); memoryIDs(results) != "[m_ok]" {
		t.Errorf("vector search after commit = %s, want m_ok", memoryIDs(results))
	}

	// Nested transactions, closing or switching model from inside a
	// transaction are refused
	_ = s.InTx(func(tx *sql.Tx, st *Store) error {
		if err := st.InTx(func(*sql.Tx, *Store) error { return nil }); err == nil {
			t.Error("nested InTx succeeded")
		}
		if err := st.Close(); err == nil {
			t.Error("Close inside a transaction succeeded")
		}
		if err := st.UseEmbeddingModel("m2"); err == nil {
			t.Error("UseEmbeddingModel inside a transaction succeeded")
		}
		return nil
	})
}
