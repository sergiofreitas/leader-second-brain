package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Store manages SQLite for memories, metadata, FTS5, and vector embeddings.
// CGO-free via modernc.org/sqlite.
type Store struct {
	db      *sql.DB
	q       DBTX // db, or the transaction of a store returned by InTx
	vectors *vectorIndex
	// pendingVectors holds the embeddings written inside a transaction; they
	// reach the in-memory index only after the transaction commits
	pendingVectors *[]pendingVector
}

// DBTX is the query interface shared by *sql.DB and *sql.Tx
type DBTX interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// pendingVector is an index change deferred until commit (nil vec = removal)
type pendingVector struct {
	id  string
	vec []float32
}

// New creates a new SQLite store at the given path
func New(dbPath string) (*Store, error) {
	// WAL for concurrent reads; busy_timeout so concurrent writers wait for
	// the lock instead of failing with SQLITE_BUSY; _txlock=immediate takes
	// the write lock at BEGIN, so a transaction that reads before writing
	// can't fail with SQLITE_BUSY when it upgrades to a writer
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db, q: db, vectors: newVectorIndex()}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	// The vector index is loaded by UseEmbeddingModel, once the embedding
	// model is known
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

		// Replaced by chunk_embeddings. No release ever stored rows in it: the
		// only embedding provider was a stub whose zero vectors were skipped.
		`DROP TABLE IF EXISTS memory_embeddings`,

		// Memory chunks — the passages that get embedded (see chunks.go)
		`CREATE TABLE IF NOT EXISTS memory_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			memory_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			content TEXT NOT NULL,
			UNIQUE (memory_id, seq),
			FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,

		// Chunk embeddings — L2-normalized float32 vectors per model, searched in Go
		`CREATE TABLE IF NOT EXISTS chunk_embeddings (
			chunk_id INTEGER NOT NULL,
			model TEXT NOT NULL,
			dim INTEGER NOT NULL,
			vector BLOB NOT NULL,
			PRIMARY KEY (chunk_id, model),
			FOREIGN KEY (chunk_id) REFERENCES memory_chunks(id) ON DELETE CASCADE
		)`,

		// Failed embedding attempts, for backoff and giving up
		`CREATE TABLE IF NOT EXISTS embedding_failures (
			chunk_id INTEGER NOT NULL,
			model TEXT NOT NULL,
			attempts INTEGER NOT NULL,
			next_attempt_at INTEGER NOT NULL,
			last_error TEXT,
			PRIMARY KEY (chunk_id, model),
			FOREIGN KEY (chunk_id) REFERENCES memory_chunks(id) ON DELETE CASCADE
		)`,

		// Key-value metadata (used by SetMeta/GetMeta)
		`CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}

	for _, stmt := range schema {
		if _, err := s.q.Exec(stmt); err != nil {
			return fmt.Errorf("exec [%s]: %w", stmt[:60], err)
		}
	}

	return nil
}

// ============================================================
// Memory CRUD
// ============================================================

func (s *Store) InsertMemory(id, memType, content, modality, source, rawFileRef, aboutPerson string, confidence float64) error {
	_, err := s.q.Exec(
		`INSERT INTO memories (id, type, content, modality, source, raw_file_ref, about_person, confidence)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, memType, content, modality, source, rawFileRef, aboutPerson, confidence,
	)
	return err
}

func (s *Store) GetMemory(id string) (map[string]interface{}, error) {
	var m map[string]interface{}
	row := s.q.QueryRow(
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
	_, err := s.q.Exec(
		`INSERT OR REPLACE INTO persons (id, name, role, area, track, job_level)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, role, area, track, jobLevel,
	)
	return err
}

// GetPersonByName returns the ID of the person with the given name, ignoring
// case, accents and extra spaces. Returns sql.ErrNoRows if there is none.
func (s *Store) GetPersonByName(name string) (string, error) {
	id, _, err := s.FindPerson(name)
	return id, err
}

// FindPerson returns the ID and stored name of the person matching name.
// An exact match wins; otherwise names are compared ignoring case, accents
// and extra spaces ("sergio" finds "Sérgio"). Returns sql.ErrNoRows if none.
func (s *Store) FindPerson(name string) (id, storedName string, err error) {
	err = s.q.QueryRow(`SELECT id, name FROM persons WHERE name = ?`, name).Scan(&id, &storedName)
	if err != sql.ErrNoRows {
		return id, storedName, err
	}
	// A leader's base has tens of people, so a scan is cheap
	people, err := s.ListPersons()
	if err != nil {
		return "", "", err
	}
	key := FoldName(name)
	for _, p := range people {
		if FoldName(p.Name) == key {
			return p.ID, p.Name, nil
		}
	}
	return "", "", sql.ErrNoRows
}

// Person is a row of the persons table
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

// ListPersons returns every person, ordered by name
func (s *Store) ListPersons() ([]Person, error) {
	rows, err := s.q.Query(`SELECT id, name, COALESCE(role, '') FROM persons ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var people []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Name, &p.Role); err != nil {
			return nil, err
		}
		people = append(people, p)
	}
	return people, rows.Err()
}

// SetPersonRole updates a person's role
func (s *Store) SetPersonRole(id, role string) error {
	_, err := s.q.Exec(`UPDATE persons SET role = ? WHERE id = ?`, role, id)
	return err
}

// accentFolder maps accented Latin letters (as used in Portuguese and
// Spanish names) to their base letter
var accentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// FoldName normalizes a name for comparison: lowercase, no accents,
// single spaces
func FoldName(name string) string {
	return accentFolder.Replace(strings.Join(strings.Fields(strings.ToLower(name)), " "))
}

// ============================================================
// Task CRUD
// ============================================================

func (s *Store) InsertTask(id, description, owner, status string) error {
	_, err := s.q.Exec(
		`INSERT INTO tasks (id, description, owner, status) VALUES (?, ?, ?, ?)`,
		id, description, owner, status,
	)
	return err
}

func (s *Store) GetPendingTasks(personID string) ([]map[string]interface{}, error) {
	rows, err := s.q.Query(
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
// Metadata
// ============================================================

func (s *Store) SetMeta(key, value string) error {
	_, err := s.q.Exec(
		`INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)`,
		key, value,
	)
	return err
}

func (s *Store) GetMeta(key string) (string, error) {
	var value string
	err := s.q.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (s *Store) Close() error {
	if s.inTx() {
		return fmt.Errorf("close: store is bound to a transaction")
	}
	return s.db.Close()
}

// ============================================================
// Transactions
// ============================================================

func (s *Store) inTx() bool { return s.pendingVectors != nil }

// InTx runs fn in a single transaction. fn receives the transaction (to bind
// other components, like the graph engine, to it) and a copy of the store
// whose queries run inside it. The transaction commits if fn returns nil and
// rolls back otherwise; embeddings written by fn reach the vector index only
// after the commit.
func (s *Store) InTx(fn func(tx *sql.Tx, st *Store) error) (err error) {
	if s.inTx() {
		return fmt.Errorf("nested transactions are not supported")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	pending := []pendingVector{}
	txStore := &Store{db: s.db, q: tx, vectors: s.vectors, pendingVectors: &pending}
	if err := fn(tx, txStore); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	for _, p := range pending {
		s.updateIndex(p.id, p.vec)
	}
	return nil
}

// DB returns the underlying *sql.DB for shared access (e.g., by the graph engine).
func (s *Store) DB() *sql.DB {
	return s.db
}
