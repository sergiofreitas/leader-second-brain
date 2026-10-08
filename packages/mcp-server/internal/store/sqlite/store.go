package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages SQLite for memories, metadata, FTS5, and vector embeddings.
// CGO-free via modernc.org/sqlite.
type Store struct {
	db *sql.DB
}

// New creates a new SQLite store at the given path
func New(dbPath string) (*Store, error) {
	// Use SQLite with WAL mode for better concurrent reads
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return s, nil
}

func (s *Store) initSchema() error {
	schema := []string{
		// Memories table — the core content store
		`CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			modality TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'manual',
			raw_file_ref TEXT,
			about_person TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			confidence REAL NOT NULL DEFAULT 1.0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_about ON memories(about_person)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at DESC)`,

		// Persons table
		`CREATE TABLE IF NOT EXISTS persons (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			role TEXT,
			area TEXT,
			track TEXT,
			job_level INTEGER,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_persons_name ON persons(name)`,

		// Tasks table
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			description TEXT NOT NULL,
			owner TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			deadline TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_owner ON tasks(owner)`,

		// Topics table
		`CREATE TABLE IF NOT EXISTS topics (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			category TEXT
		)`,

		// Memory-topics junction
		`CREATE TABLE IF NOT EXISTS memory_topics (
			memory_id TEXT NOT NULL,
			topic_id TEXT NOT NULL,
			PRIMARY KEY (memory_id, topic_id),
			FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE,
			FOREIGN KEY (topic_id) REFERENCES topics(id) ON DELETE CASCADE
		)`,

		// Memory-participants junction (who was present)
		`CREATE TABLE IF NOT EXISTS memory_participants (
			memory_id TEXT NOT NULL,
			person_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'participant',
			PRIMARY KEY (memory_id, person_id),
			FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE,
			FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE CASCADE
		)`,

		// FTS5 virtual table for full-text search on memory content
		`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			content,
			about_person,
			type,
			content='memories',
			content_rowid='rowid'
		)`,

		// Triggers to keep FTS5 in sync with memories table
		`CREATE TRIGGER IF NOT EXISTS memories_fts_insert AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content, about_person, type)
			VALUES (new.rowid, new.content, new.about_person, new.type);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_fts_delete AFTER DELETE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, about_person, type)
			VALUES ('delete', old.rowid, old.content, old.about_person, old.type);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_fts_update AFTER UPDATE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, about_person, type)
			VALUES ('delete', old.rowid, old.content, old.about_person, old.type);
			INSERT INTO memories_fts(rowid, content, about_person, type)
			VALUES (new.rowid, new.content, new.about_person, new.type);
		END`,
	}

	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec [%s]: %w", stmt[:60], err)
		}
	}

	// Try to load sqlite-vec extension (optional — graceful fallback)
	if err := s.loadVecExtension(); err != nil {
		// Non-fatal: vector search won't work, but FTS5 and structured queries will
		fmt.Printf("warning: sqlite-vec not loaded: %v\n", err)
	}

	return nil
}

func (s *Store) loadVecExtension() error {
	// Try loading sqlite-vec extension
	// The exact loading mechanism depends on the Go SQLite driver used
	// With modernc.org/sqlite, we may need to use a pure-Go vec implementation
	// For now, we create the vector table schema and handle loading at query time
	_, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS memory_vectors USING vec0(
		embedding float[384]
	)`)
	if err != nil {
		return err
	}
	return nil
}

// ============================================================
// Memory CRUD
// ============================================================

func (s *Store) InsertMemory(id, memType, content, modality, source, rawFileRef, aboutPerson string, confidence float64) error {
	_, err := s.db.Exec(
		`INSERT INTO memories (id, type, content, modality, source, raw_file_ref, about_person, confidence)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, memType, content, modality, source, rawFileRef, aboutPerson, confidence,
	)
	return err
}

func (s *Store) GetMemory(id string) (map[string]interface{}, error) {
	var m map[string]interface{}
	row := s.db.QueryRow(
		`SELECT id, type, content, modality, source, raw_file_ref, about_person, created_at, confidence
		 FROM memories WHERE id = ?`, id,
	)
	var memID, memType, content, modality, source, createdAt string
	var rawFileRef, aboutPerson sql.NullString
	var confidence float64
	if err := row.Scan(&memID, &memType, &content, &modality, &source, &rawFileRef, &aboutPerson, &createdAt, &confidence); err != nil {
		return nil, err
	}
	m = map[string]interface{}{
		"id": memID, "type": memType, "content": content, "modality": modality,
		"source": source, "raw_file_ref": rawFileRef.String, "about_person": aboutPerson.String,
		"created_at": createdAt, "confidence": confidence,
	}
	return m, nil
}

// ============================================================
// Person CRUD
// ============================================================

func (s *Store) UpsertPerson(id, name, role, area, track string, jobLevel int) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO persons (id, name, role, area, track, job_level)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, role, area, track, jobLevel,
	)
	return err
}

func (s *Store) GetPersonByName(name string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM persons WHERE name = ?`, name).Scan(&id)
	return id, err
}

// ============================================================
// Task CRUD
// ============================================================

func (s *Store) InsertTask(id, description, owner, status string) error {
	_, err := s.db.Exec(
		`INSERT INTO tasks (id, description, owner, status) VALUES (?, ?, ?, ?)`,
		id, description, owner, status,
	)
	return err
}

func (s *Store) GetPendingTasks(personID string) ([]map[string]interface{}, error) {
	rows, err := s.db.Query(
		`SELECT id, description, owner, status, created_at FROM tasks
		 WHERE owner = ? AND status = 'pending' ORDER BY created_at DESC`,
		personID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []map[string]interface{}
	for rows.Next() {
		var id, desc, owner, status, createdAt string
		if err := rows.Scan(&id, &desc, &owner, &status, &createdAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, map[string]interface{}{
			"id": id, "description": desc, "owner": owner,
			"status": status, "created_at": createdAt,
		})
	}
	return tasks, nil
}

// ============================================================
// FTS5 Search
// ============================================================

func (s *Store) SearchFTS(query string, limit int) ([]map[string]interface{}, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.type, m.content, m.about_person, m.created_at,
			snippet(memories_fts, 0, '<mark>', '</mark>', '...', 32) as snippet,
			bm25(memories_fts) as rank
		 FROM memories_fts
		 JOIN memories m ON m.rowid = memories_fts.rowid
		 WHERE memories_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, memType, content, aboutPerson, createdAt, snippet string
		var rank float64
		if err := rows.Scan(&id, &memType, &content, &aboutPerson, &createdAt, &snippet, &rank); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"id": id, "type": memType, "content": content,
			"about_person": aboutPerson, "created_at": createdAt,
			"snippet": snippet, "rank": rank,
		})
	}
	return results, nil
}

// ============================================================
// Vector Search (sqlite-vec)
// ============================================================

func (s *Store) InsertVector(memoryID string, embedding []float32) error {
	vecJSON, _ := json.Marshal(embedding)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO memory_vectors (rowid, embedding) VALUES (?, ?)`,
		memoryID, string(vecJSON),
	)
	return err
}

func (s *Store) SearchVector(queryVec []float32, limit int) ([]map[string]interface{}, error) {
	vecJSON, _ := json.Marshal(queryVec)
	rows, err := s.db.Query(
		`SELECT v.rowid, v.distance, m.content, m.type, m.about_person, m.created_at
		 FROM memory_vectors v
		 JOIN memories m ON m.id = v.rowid
		 WHERE v.embedding MATCH ?
		 ORDER BY v.distance
		 LIMIT ?`,
		string(vecJSON), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var rowid, distance, content, memType, aboutPerson, createdAt string
		if err := rows.Scan(&rowid, &distance, &content, &memType, &aboutPerson, &createdAt); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"memory_id": rowid, "distance": distance, "content": content,
			"type": memType, "about_person": aboutPerson, "created_at": createdAt,
		})
	}
	return results, nil
}

// ============================================================
// Metadata
// ============================================================

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)`,
		key, value,
	)
	return err
}

func (s *Store) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (s *Store) Close() error {
	return s.db.Close()
}


// DB returns the underlying *sql.DB for shared access (e.g., by Graphlite).
func (s *Store) DB() *sql.DB {
	return s.db
}
