package sqlite

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ============================================================
// Chunks and their embeddings
//
// The embedding work queue is derived from the data: a chunk is pending for
// a model when it has no embedding for that model. Failed attempts are kept
// in embedding_failures to back off and eventually give up.
// ============================================================

// Chunk is a passage of a memory, the unit that gets embedded
type Chunk struct {
	ID      int64
	Content string
}

// MemoryText is a memory's id and content
type MemoryText struct {
	ID      string
	Content string
}

// MaxEmbeddingAttempts is how many times a chunk is tried before giving up
const MaxEmbeddingAttempts = 8

func chunkKey(id int64) string { return strconv.FormatInt(id, 10) }

// InsertChunks stores the chunks of a memory, in order
func (s *Store) InsertChunks(memoryID string, chunks []string) error {
	for i, c := range chunks {
		if _, err := s.q.Exec(
			`INSERT INTO memory_chunks (memory_id, seq, content) VALUES (?, ?, ?)`,
			memoryID, i, c,
		); err != nil {
			return fmt.Errorf("insert chunk %d: %w", i, err)
		}
	}
	return nil
}

// MemoriesWithoutChunks returns memories stored before chunking existed
func (s *Store) MemoriesWithoutChunks() ([]MemoryText, error) {
	rows, err := s.q.Query(
		`SELECT m.id, m.content FROM memories m
		 WHERE NOT EXISTS (SELECT 1 FROM memory_chunks c WHERE c.memory_id = m.id)
		 ORDER BY m.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryText
	for rows.Next() {
		var m MemoryText
		if err := rows.Scan(&m.ID, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PendingChunks returns up to limit chunks with no embedding for model,
// skipping those waiting for a retry or that failed too many times
func (s *Store) PendingChunks(model string, now time.Time, limit int) ([]Chunk, error) {
	rows, err := s.q.Query(
		`SELECT c.id, c.content
		 FROM memory_chunks c
		 LEFT JOIN chunk_embeddings e ON e.chunk_id = c.id AND e.model = ?
		 LEFT JOIN embedding_failures f ON f.chunk_id = c.id AND f.model = ?
		 WHERE e.chunk_id IS NULL
		   AND (f.chunk_id IS NULL OR (f.attempts < ? AND f.next_attempt_at <= ?))
		 ORDER BY c.id
		 LIMIT ?`,
		model, model, MaxEmbeddingAttempts, now.Unix(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.Content); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// NextRetryAt returns when the earliest failed chunk can be retried for
// model, or false if there is none
func (s *Store) NextRetryAt(model string) (time.Time, bool, error) {
	var next sql.NullInt64
	err := s.q.QueryRow(
		`SELECT MIN(f.next_attempt_at) FROM embedding_failures f
		 WHERE f.model = ? AND f.attempts < ?
		   AND NOT EXISTS (SELECT 1 FROM chunk_embeddings e WHERE e.chunk_id = f.chunk_id AND e.model = f.model)`,
		model, MaxEmbeddingAttempts,
	).Scan(&next)
	if err != nil || !next.Valid {
		return time.Time{}, false, err
	}
	return time.Unix(next.Int64, 0), true, nil
}

// SaveEmbeddings stores the embeddings of chunks for model in one
// transaction; the vectors reach the search index after the commit.
// Zero vectors are skipped (they can't be ranked by similarity).
func (s *Store) SaveEmbeddings(model string, chunkIDs []int64, vectors [][]float32) error {
	if len(chunkIDs) != len(vectors) {
		return fmt.Errorf("save embeddings: %d chunks but %d vectors", len(chunkIDs), len(vectors))
	}
	return s.InTx(func(_ *sql.Tx, st *Store) error {
		for i, id := range chunkIDs {
			vec := normalize(vectors[i])
			if vec == nil {
				continue
			}
			// The chunk may have been deleted with its memory meanwhile
			res, err := st.q.Exec(
				`INSERT OR REPLACE INTO chunk_embeddings (chunk_id, model, dim, vector)
				 SELECT id, ?, ?, ? FROM memory_chunks WHERE id = ?`,
				model, len(vec), encodeVector(vec), id,
			)
			if err != nil {
				return fmt.Errorf("save embedding of chunk %d: %w", id, err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if _, err := st.q.Exec(`DELETE FROM embedding_failures WHERE chunk_id = ? AND model = ?`, id, model); err != nil {
				return err
			}
			st.updateIndex(chunkKey(id), vec)
		}
		return nil
	})
}

// RecordEmbeddingFailure counts a failed attempt for each chunk and schedules
// its retry with exponential backoff (1 min, 2 min, 4 min... capped at 6 h)
func (s *Store) RecordEmbeddingFailure(model string, chunkIDs []int64, cause error, now time.Time) error {
	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return s.InTx(func(_ *sql.Tx, st *Store) error {
		for _, id := range chunkIDs {
			var attempts int
			err := st.q.QueryRow(
				`SELECT attempts FROM embedding_failures WHERE chunk_id = ? AND model = ?`, id, model,
			).Scan(&attempts)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			attempts++
			backoff := time.Minute << uint(attempts-1)
			if backoff > 6*time.Hour || backoff <= 0 {
				backoff = 6 * time.Hour
			}
			if _, err := st.q.Exec(
				`INSERT OR REPLACE INTO embedding_failures (chunk_id, model, attempts, next_attempt_at, last_error)
				 SELECT id, ?, ?, ?, ? FROM memory_chunks WHERE id = ?`,
				model, attempts, now.Add(backoff).Unix(), msg, id,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// UseEmbeddingModel makes model the one served by vector search: embeddings
// of other models are deleted (their chunks become pending again for model)
// and the index is loaded with model's vectors.
func (s *Store) UseEmbeddingModel(model string) error {
	if s.inTx() {
		return fmt.Errorf("use embedding model: store is bound to a transaction")
	}
	for _, stmt := range []string{
		`DELETE FROM chunk_embeddings WHERE model != ?`,
		`DELETE FROM embedding_failures WHERE model != ?`,
	} {
		if _, err := s.q.Exec(stmt, model); err != nil {
			return fmt.Errorf("use embedding model: %w", err)
		}
	}
	s.vectors.reset()
	rows, err := s.q.Query(`SELECT chunk_id, vector FROM chunk_embeddings WHERE model = ?`, model)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return err
		}
		vec, err := decodeVector(blob)
		if err != nil {
			return fmt.Errorf("chunk %d: %w", id, err)
		}
		s.vectors.put(chunkKey(id), vec)
	}
	return rows.Err()
}

// EmbeddingStatus counts the chunks embedded with model, still pending,
// and given up after too many failures
type EmbeddingStatus struct {
	Chunks   int `json:"chunks"`
	Embedded int `json:"embedded"`
	Pending  int `json:"pending"`
	Failed   int `json:"failed"`
}

// GetEmbeddingStatus returns the indexing progress for model
func (s *Store) GetEmbeddingStatus(model string) (EmbeddingStatus, error) {
	var st EmbeddingStatus
	err := s.q.QueryRow(
		`SELECT
			(SELECT count(*) FROM memory_chunks),
			(SELECT count(*) FROM chunk_embeddings WHERE model = ?),
			(SELECT count(*) FROM embedding_failures f WHERE f.model = ? AND f.attempts >= ?
			   AND NOT EXISTS (SELECT 1 FROM chunk_embeddings e WHERE e.chunk_id = f.chunk_id AND e.model = f.model))`,
		model, model, MaxEmbeddingAttempts,
	).Scan(&st.Chunks, &st.Embedded, &st.Failed)
	st.Pending = st.Chunks - st.Embedded - st.Failed
	return st, err
}

// SearchVector returns the memories with the chunks most similar to
// queryVec, best first, one result per memory with its best matching chunk
// as "excerpt". "similarity" is the cosine similarity and "distance" is
// 1 - similarity.
func (s *Store) SearchVector(queryVec []float32, limit int) ([]map[string]interface{}, error) {
	query := normalize(queryVec)
	if query == nil || limit <= 0 {
		return nil, nil
	}
	// A memory can have many matching chunks: fetch extra hits, keep the
	// best chunk of each memory
	hits := s.vectors.search(query, limit*4)
	if len(hits) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(hits))
	args := make([]interface{}, len(hits))
	for i, h := range hits {
		placeholders[i] = "?"
		args[i] = h.id
	}
	rows, err := s.q.Query(
		`SELECT c.id, c.content, m.id, m.type, COALESCE(m.about_person, ''), m.created_at
		 FROM memory_chunks c JOIN memories m ON m.id = c.memory_id
		 WHERE c.id IN (`+strings.Join(placeholders, ", ")+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byChunk := make(map[string]map[string]interface{}, len(hits))
	for rows.Next() {
		var chunkID int64
		var excerpt, memID, memType, aboutPerson, createdAt string
		if err := rows.Scan(&chunkID, &excerpt, &memID, &memType, &aboutPerson, &createdAt); err != nil {
			return nil, err
		}
		byChunk[chunkKey(chunkID)] = map[string]interface{}{
			"memory_id": memID, "excerpt": excerpt, "type": memType,
			"about_person": aboutPerson, "created_at": createdAt,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]map[string]interface{}, 0, limit)
	seen := map[string]bool{}
	for _, h := range hits {
		m, ok := byChunk[h.id]
		if !ok || seen[m["memory_id"].(string)] {
			continue // chunk deleted after the index was loaded, or memory already listed
		}
		seen[m["memory_id"].(string)] = true
		m["similarity"] = float64(h.score)
		m["distance"] = 1 - float64(h.score)
		results = append(results, m)
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

// updateIndex puts vec in the in-memory index (or removes the entry when vec
// is nil). Inside a transaction the change is deferred until commit.
func (s *Store) updateIndex(key string, vec []float32) {
	if s.inTx() {
		*s.pendingVectors = append(*s.pendingVectors, pendingVector{key, vec})
		return
	}
	if vec == nil {
		s.vectors.remove(key)
	} else {
		s.vectors.put(key, vec)
	}
}
