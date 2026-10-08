package sqlite

import (
	"database/sql"
	"errors"
	"testing"
)

// TestDeleteMemory checks a deleted memory leaves nothing behind: passages,
// embeddings, vectors in the index, FTS entries — and that a rolled back
// delete changes nothing
func TestDeleteMemory(t *testing.T) {
	s, err := New(t.TempDir() + "/delete.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.UseEmbeddingModel("m1"); err != nil {
		t.Fatalf("use model: %v", err)
	}
	gone := addMemory(t, s, "m_gone", "primeiro trecho", "segundo trecho")
	kept := addMemory(t, s, "m_kept", "outro assunto")
	if err := s.SaveEmbeddings("m1", append(gone, kept...), [][]float32{{1, 0, 0}, {0.9, 0.1, 0}, {0, 1, 0}}); err != nil {
		t.Fatalf("save embeddings: %v", err)
	}
	if s.vectors.len() != 3 {
		t.Fatalf("indexed vectors = %d, want 3", s.vectors.len())
	}

	// Rolled back: everything is still there
	boom := errors.New("boom")
	err = s.InTx(func(_ *sql.Tx, st *Store) error {
		if found, err := st.DeleteMemory("m_gone"); err != nil || !found {
			t.Fatalf("delete in tx = %v, %v", found, err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v", err)
	}
	if _, err := s.GetMemory("m_gone"); err != nil || s.vectors.len() != 3 {
		t.Errorf("after rollback: memory err %v, %d vectors; want it kept with 3 vectors", err, s.vectors.len())
	}

	err = s.InTx(func(_ *sql.Tx, st *Store) error {
		found, err := st.DeleteMemory("m_gone")
		if !found {
			t.Error("DeleteMemory reported the memory missing")
		}
		return err
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetMemory("m_gone"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("memory after delete: err = %v", err)
	}
	count := func(query string) (n int) {
		s.DB().QueryRow(query).Scan(&n)
		return n
	}
	if n := count(`SELECT count(*) FROM memory_chunks WHERE memory_id = 'm_gone'`); n != 0 {
		t.Errorf("chunks left: %d", n)
	}
	if n := count(`SELECT count(*) FROM chunk_embeddings`); n != 1 {
		t.Errorf("embeddings = %d, want only the kept memory's", n)
	}
	if s.vectors.len() != 1 {
		t.Errorf("indexed vectors = %d, want 1", s.vectors.len())
	}
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 5); memoryIDs(results) != "[m_kept]" {
		t.Errorf("vector search = %s, want only m_kept", memoryIDs(results))
	}
	if results, _ := s.SearchFTS("content", nil, 5); len(results) != 1 || results[0]["memory_id"] != "m_kept" {
		t.Errorf("FTS = %v, want only m_kept", results)
	}
	if found, err := s.DeleteMemory("m_gone"); found || err != nil {
		t.Errorf("deleting again = %v, %v; want not found", found, err)
	}
}
