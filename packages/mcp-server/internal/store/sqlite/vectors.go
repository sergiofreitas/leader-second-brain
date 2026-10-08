package sqlite

import (
	"container/heap"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
)

// Vector search in pure Go.
//
// Memories are split into chunks (memory_chunks). Each chunk's embedding is
// stored L2-normalized as a little-endian float32 BLOB in chunk_embeddings,
// keyed by chunk and model, and the current model's vectors are mirrored in an
// in-memory index. Search is an exact (brute-force) cosine similarity scan: at
// the volume of a single leader's knowledge base (tens of thousands of
// chunks) this answers in milliseconds, needs no SQLite extension and keeps
// the build CGO-free.

// vectorIndex is the in-memory mirror of the current model's chunk_embeddings,
// keyed by chunk id
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

func (ix *vectorIndex) reset() {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.ids, ix.vecs, ix.pos = nil, nil, map[string]int{}
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
