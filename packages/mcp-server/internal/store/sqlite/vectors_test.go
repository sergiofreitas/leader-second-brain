package sqlite

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestVectorSearch(t *testing.T) {
	dbPath := t.TempDir() + "/vectors.db"
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	memories := map[string][]float32{
		"m_close":  {1, 0.1, 0},
		"m_middle": {1, 1, 0},
		"m_far":    {-1, 0, 0},
		"m_zero":   {0, 0, 0},
		"m_old":    {1, 0}, // different dimension (previous embedding model)
	}
	for id, vec := range memories {
		if err := s.InsertMemory(id, "observation", "content "+id, "text", "test", "", "Ana", 1); err != nil {
			t.Fatalf("insert memory: %v", err)
		}
		if err := s.InsertVector(id, vec); err != nil {
			t.Fatalf("insert vector %s: %v", id, err)
		}
	}

	check := func(s *Store, label string) {
		t.Helper()
		results, err := s.SearchVector([]float32{2, 0, 0}, 10)
		if err != nil {
			t.Fatalf("%s: search: %v", label, err)
		}
		var got []string
		for _, r := range results {
			got = append(got, r["memory_id"].(string))
		}
		want := []string{"m_close", "m_middle", "m_far"}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: results = %v, want %v", label, got, want)
		}
		if sim := results[0]["similarity"].(float64); sim < 0.99 || sim > 1.0001 {
			t.Errorf("%s: similarity of closest = %v, want ~0.995", label, sim)
		}
		if results[0]["content"] != "content m_close" || results[0]["about_person"] != "Ana" {
			t.Errorf("%s: memory fields = %v", label, results[0])
		}
	}
	check(s, "fresh")

	// Limit keeps only the best matches
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 1); len(results) != 1 || results[0]["memory_id"] != "m_close" {
		t.Errorf("limit 1 = %v, want only m_close", results)
	}
	// A zero query vector has no direction to compare
	if results, _ := s.SearchVector([]float32{0, 0, 0}, 5); len(results) != 0 {
		t.Errorf("zero query = %v, want no results", results)
	}

	// Replacing a vector moves the memory in the ranking
	if err := s.InsertVector("m_far", []float32{1, 0, 0}); err != nil {
		t.Fatalf("replace vector: %v", err)
	}
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 1); results[0]["memory_id"] != "m_far" {
		t.Errorf("after replace, best = %v, want m_far", results[0]["memory_id"])
	}
	if err := s.InsertVector("m_far", []float32{-1, 0, 0}); err != nil {
		t.Fatalf("restore vector: %v", err)
	}

	// Vectors persist and are reloaded when the store is reopened
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s, err = New(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	check(s, "reopened")

	// Deleted vectors leave the index
	if err := s.DeleteVector("m_close"); err != nil {
		t.Fatalf("delete vector: %v", err)
	}
	if results, _ := s.SearchVector([]float32{1, 0, 0}, 1); results[0]["memory_id"] != "m_middle" {
		t.Errorf("after delete, best = %v, want m_middle", results[0]["memory_id"])
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
		ix.put(fmt.Sprintf("m%d", i), normalize(vec))
	}
	query := make([]float32, dim)
	for j := range query {
		query[j] = rng.Float32()*2 - 1
	}
	query = normalize(query)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.search(query, 10)
	}
}
