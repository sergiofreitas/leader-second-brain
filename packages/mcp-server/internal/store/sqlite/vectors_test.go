package sqlite

import (
	"fmt"
	"math/rand"
	"testing"
)

// addMemory stores a memory with the given chunks and returns the chunk ids
func addMemory(t *testing.T, s *Store, id string, chunks ...string) []int64 {
	t.Helper()
	if err := s.InsertMemory(id, "observation", "content "+id, "text", "test", "", "Ana", 1); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	if err := s.InsertChunks(id, chunks); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}
	rows, err := s.DB().Query(`SELECT id FROM memory_chunks WHERE memory_id = ? ORDER BY seq`, id)
	if err != nil {
		t.Fatalf("chunk ids: %v", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var cid int64
		rows.Scan(&cid)
		ids = append(ids, cid)
	}
	return ids
}

func memoryIDs(results []map[string]interface{}) string {
	var ids []string
	for _, r := range results {
		ids = append(ids, r["memory_id"].(string))
	}
	return fmt.Sprint(ids)
}

func TestVectorSearch(t *testing.T) {
	dbPath := t.TempDir() + "/vectors.db"
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.UseEmbeddingModel("m1"); err != nil {
		t.Fatalf("use model: %v", err)
	}

	// m_long has two chunks: one close to the query, one far from it
	long := addMemory(t, s, "m_long", "pauta da 1:1", "microgestão do time")
	middle := addMemory(t, s, "m_middle", "feedback construtivo")
	far := addMemory(t, s, "m_far", "férias")
	zero := addMemory(t, s, "m_zero", "vazio")
	ids := []int64{long[0], long[1], middle[0], far[0], zero[0]}
	vectors := [][]float32{{0, 1, 0}, {1, 0.1, 0}, {1, 1, 0}, {-1, 0, 0}, {0, 0, 0}}
	if err := s.SaveEmbeddings("m1", ids, vectors); err != nil {
		t.Fatalf("save embeddings: %v", err)
	}

	check := func(s *Store, label string) {
		t.Helper()
		results, err := s.SearchVector([]float32{2, 0, 0}, 10)
		if err != nil {
			t.Fatalf("%s: search: %v", label, err)
		}
		// One result per memory, ranked by its best chunk; the zero vector
		// was never stored
		if got := memoryIDs(results); got != "[m_long m_middle m_far]" {
			t.Fatalf("%s: results = %s", label, got)
		}
		if results[0]["excerpt"] != "microgestão do time" || results[0]["about_person"] != "Ana" {
			t.Errorf("%s: best result = %v, want the matching chunk as excerpt", label, results[0])
		}
		if sim := results[0]["similarity"].(float64); sim < 0.99 || sim > 1.0001 {
			t.Errorf("%s: similarity = %v, want ~0.995", label, sim)
		}
	}
	check(s, "fresh")
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 1); memoryIDs(results) != "[m_long]" {
		t.Errorf("limit 1 = %s", memoryIDs(results))
	}
	if results, _ := s.SearchVector([]float32{0, 0, 0}, 5); len(results) != 0 {
		t.Errorf("zero query = %v, want no results", results)
	}

	// Vectors persist and are reloaded for the same model
	s.Close()
	if s, err = New(dbPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 5); len(results) != 0 {
		t.Errorf("search before UseEmbeddingModel = %v, want nothing loaded", results)
	}
	if err := s.UseEmbeddingModel("m1"); err != nil {
		t.Fatalf("use model: %v", err)
	}
	check(s, "reopened")

	// Another model drops m1's vectors: every chunk is pending again
	if err := s.UseEmbeddingModel("m2"); err != nil {
		t.Fatalf("switch model: %v", err)
	}
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 5); len(results) != 0 {
		t.Errorf("search after switching model = %s, want nothing", memoryIDs(results))
	}
	var left int
	s.DB().QueryRow(`SELECT count(*) FROM chunk_embeddings`).Scan(&left)
	if left != 0 {
		t.Errorf("%d embeddings of the old model left", left)
	}
}

// BenchmarkVectorSearch measures a search over a knowledge base of the size
// expected after a few years of use by a head (~75k chunks, 384 dims).
func BenchmarkVectorSearch(b *testing.B) {
	const n, dim = 75000, 384
	rng := rand.New(rand.NewSource(1))
	ix := newVectorIndex()
	for i := 0; i < n; i++ {
		vec := make([]float32, dim)
		for j := range vec {
			vec[j] = rng.Float32()*2 - 1
		}
		ix.put(fmt.Sprintf("%d", i), normalize(vec))
	}
	query := make([]float32, dim)
	for j := range query {
		query[j] = rng.Float32()*2 - 1
	}
	query = normalize(query)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.search(query, 40)
	}
}
