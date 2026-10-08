package sqlite

import (
	"container/heap"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
)

// Vector search in pure Go.
//
// Embeddings are stored L2-normalized as little-endian float32 BLOBs in
// memory_embeddings and mirrored in an in-memory index loaded at startup.
// Search is an exact (brute-force) cosine similarity scan: at the volume of a
// single leader's knowledge base (tens of thousands of chunks) this answers in
// milliseconds, needs no SQLite extension and keeps the build CGO-free.

// vectorIndex is the in-memory mirror of memory_embeddings
type vectorIndex struct {
	mu   sync.RWMutex
	ids  []string
	vecs [][]float32
	pos  map[string]int
}

func newVectorIndex() *vectorIndex {
	return &vectorIndex{pos: map[string]int{}}
}

func (ix *vectorIndex) put(id string, vec []float32) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if i, ok := ix.pos[id]; ok {
		ix.vecs[i] = vec
		return
	}
	ix.pos[id] = len(ix.ids)
	ix.ids = append(ix.ids, id)
	ix.vecs = append(ix.vecs, vec)
}

func (ix *vectorIndex) remove(id string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	i, ok := ix.pos[id]
	if !ok {
		return
	}
	last := len(ix.ids) - 1
	ix.ids[i], ix.vecs[i] = ix.ids[last], ix.vecs[last]
	ix.pos[ix.ids[i]] = i
	ix.ids, ix.vecs = ix.ids[:last], ix.vecs[:last]
	delete(ix.pos, id)
}

func (ix *vectorIndex) len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.ids)
}

// vectorHit is a single search match
type vectorHit struct {
	id    string
	score float32
}

// hitHeap is a min-heap on score, used to keep the top-k matches
type hitHeap []vectorHit

func (h hitHeap) Len() int            { return len(h) }
func (h hitHeap) Less(i, j int) bool  { return h[i].score < h[j].score }
func (h hitHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *hitHeap) Push(x interface{}) { *h = append(*h, x.(vectorHit)) }
func (h *hitHeap) Pop() interface{} {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// search returns the k vectors most similar to query (already normalized),
// best first. Vectors with a different dimension (e.g. from a previous
// embedding model) are skipped.
func (ix *vectorIndex) search(query []float32, k int) []vectorHit {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	h := make(hitHeap, 0, k+1)
	for i, vec := range ix.vecs {
		if len(vec) != len(query) {
			continue
		}
		score := dot(query, vec)
		if len(h) < k {
			heap.Push(&h, vectorHit{ix.ids[i], score})
		} else if score > h[0].score {
			h[0] = vectorHit{ix.ids[i], score}
			heap.Fix(&h, 0)
		}
	}
	hits := make([]vectorHit, len(h))
	for i := len(h) - 1; i >= 0; i-- {
		hits[i] = heap.Pop(&h).(vectorHit)
	}
	return hits
}

func dot(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// normalize returns a unit-length copy of vec, or nil for a zero vector
// (which has no direction and can't be compared by cosine similarity)
func normalize(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return nil
	}
	norm := float32(math.Sqrt(sum))
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = v / norm
	}
	return out
}

func encodeVector(vec []float32) []byte {
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(v))
	}
	return buf
}

func decodeVector(buf []byte) ([]float32, error) {
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("invalid vector blob length %d", len(buf))
	}
	vec := make([]float32, len(buf)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[4*i:]))
	}
	return vec, nil
}

// loadVectors fills the in-memory index from memory_embeddings
func (s *Store) loadVectors() error {
	rows, err := s.db.Query(`SELECT memory_id, vector FROM memory_embeddings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return err
		}
		vec, err := decodeVector(blob)
		if err != nil {
			return fmt.Errorf("memory %s: %w", id, err)
		}
		s.vectors.put(id, vec)
	}
	return rows.Err()
}

// InsertVector stores the embedding of a memory, replacing any previous one.
// Zero vectors are ignored, since they can't be ranked by similarity.
func (s *Store) InsertVector(memoryID string, embedding []float32) error {
	vec := normalize(embedding)
	if vec == nil {
		return nil
	}
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO memory_embeddings (memory_id, dim, vector) VALUES (?, ?, ?)`,
		memoryID, len(vec), encodeVector(vec),
	); err != nil {
		return err
	}
	s.vectors.put(memoryID, vec)
	return nil
}

// DeleteVector removes the embedding of a memory
func (s *Store) DeleteVector(memoryID string) error {
	if _, err := s.db.Exec(`DELETE FROM memory_embeddings WHERE memory_id = ?`, memoryID); err != nil {
		return err
	}
	s.vectors.remove(memoryID)
	return nil
}

// SearchVector returns the memories most similar to queryVec, best first.
// "similarity" is the cosine similarity and "distance" is 1 - similarity.
func (s *Store) SearchVector(queryVec []float32, limit int) ([]map[string]interface{}, error) {
	query := normalize(queryVec)
	if query == nil || limit <= 0 {
		return nil, nil
	}
	hits := s.vectors.search(query, limit)
	if len(hits) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(hits))
	args := make([]interface{}, len(hits))
	for i, h := range hits {
		placeholders[i] = "?"
		args[i] = h.id
	}
	rows, err := s.db.Query(
		`SELECT id, content, type, COALESCE(about_person, ''), created_at
		 FROM memories WHERE id IN (`+strings.Join(placeholders, ", ")+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := make(map[string]map[string]interface{}, len(hits))
	for rows.Next() {
		var id, content, memType, aboutPerson, createdAt string
		if err := rows.Scan(&id, &content, &memType, &aboutPerson, &createdAt); err != nil {
			return nil, err
		}
		memories[id] = map[string]interface{}{
			"memory_id": id, "content": content, "type": memType,
			"about_person": aboutPerson, "created_at": createdAt,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]map[string]interface{}, 0, len(hits))
	for _, h := range hits {
		m, ok := memories[h.id]
		if !ok {
			continue // memory deleted after the index was loaded
		}
		m["similarity"] = float64(h.score)
		m["distance"] = 1 - float64(h.score)
		results = append(results, m)
	}
	return results, nil
}
