package sqlite

import (
	"errors"
	"testing"
	"time"
)

func TestEmbeddingQueue(t *testing.T) {
	s, err := New(t.TempDir() + "/queue.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.UseEmbeddingModel("m1"); err != nil {
		t.Fatalf("use model: %v", err)
	}
	now := time.Unix(1_800_000_000, 0)

	a := addMemory(t, s, "m_a", "a1", "a2")
	b := addMemory(t, s, "m_b", "b1")

	pending, err := s.PendingChunks("m1", now, 10)
	if err != nil || len(pending) != 3 || pending[0].Content != "a1" || pending[2].Content != "b1" {
		t.Fatalf("pending = %+v (err %v), want a1, a2, b1", pending, err)
	}
	if pending, _ := s.PendingChunks("m1", now, 2); len(pending) != 2 {
		t.Errorf("pending with limit 2 = %d chunks", len(pending))
	}

	// Embedded chunks leave the queue
	if err := s.SaveEmbeddings("m1", a, [][]float32{{1, 0}, {0, 1}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if pending, _ := s.PendingChunks("m1", now, 10); len(pending) != 1 || pending[0].ID != b[0] {
		t.Errorf("pending after saving a = %+v, want only b1", pending)
	}

	// A failure postpones the chunk with exponential backoff
	cause := errors.New("rate limited")
	if err := s.RecordEmbeddingFailure("m1", b, cause, now); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	if pending, _ := s.PendingChunks("m1", now, 10); len(pending) != 0 {
		t.Errorf("failed chunk is pending before its retry time: %+v", pending)
	}
	next, ok, err := s.NextRetryAt("m1")
	if err != nil || !ok || !next.Equal(now.Add(time.Minute)) {
		t.Errorf("next retry = %v, %v (err %v), want now+1m", next, ok, err)
	}
	if pending, _ := s.PendingChunks("m1", now.Add(time.Minute), 10); len(pending) != 1 {
		t.Errorf("failed chunk not pending at its retry time")
	}
	s.RecordEmbeddingFailure("m1", b, cause, now)
	if next, _, _ := s.NextRetryAt("m1"); !next.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("second retry = %v, want now+2m", next)
	}

	// After MaxEmbeddingAttempts the chunk is given up
	for i := 2; i < MaxEmbeddingAttempts; i++ {
		s.RecordEmbeddingFailure("m1", b, cause, now)
	}
	if pending, _ := s.PendingChunks("m1", now.Add(24*time.Hour), 10); len(pending) != 0 {
		t.Errorf("chunk still pending after %d failures", MaxEmbeddingAttempts)
	}
	if _, ok, _ := s.NextRetryAt("m1"); ok {
		t.Error("a given-up chunk still has a retry scheduled")
	}
	status, err := s.GetEmbeddingStatus("m1")
	if err != nil || status != (EmbeddingStatus{Chunks: 3, Embedded: 2, Pending: 0, Failed: 1, LastError: "rate limited"}) {
		t.Errorf("status = %+v (err %v)", status, err)
	}

	// A new model starts over: failures of the old one don't count
	if err := s.UseEmbeddingModel("m2"); err != nil {
		t.Fatalf("switch model: %v", err)
	}
	if pending, _ := s.PendingChunks("m2", now, 10); len(pending) != 3 {
		t.Errorf("pending for the new model = %d chunks, want 3", len(pending))
	}
}

func TestMemoriesWithoutChunks(t *testing.T) {
	s, err := New(t.TempDir() + "/backfill.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	addMemory(t, s, "m_chunked", "c")
	if err := s.InsertMemory("m_old", "observation", "stored before chunking", "text", "test", "", "", 1); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := s.MemoriesWithoutChunks()
	if err != nil || len(got) != 1 || got[0].ID != "m_old" || got[0].Content != "stored before chunking" {
		t.Errorf("memories without chunks = %+v (err %v), want m_old", got, err)
	}
}
