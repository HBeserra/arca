package indexdb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"changeme/internal/business/enginebus"

	"github.com/google/uuid"
)

// Store implements enginebus.Store using DuckDB with the VSS extension.
type Store struct {
	log        *slog.Logger
	db         *sql.DB
	dimensions int
}

// New creates a new Store, applies options, initialises the schema, and returns
// an error if initialisation fails.
func New(log *slog.Logger, db *sql.DB, opts ...Option) (*Store, error) {
	s := &Store{
		log:        log,
		db:         db,
		dimensions: 768,
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.init(); err != nil {
		return nil, fmt.Errorf("indexdb init: %w", err)
	}
	return s, nil
}

// validateOrResetChunks checks whether the existing chunks table (if any) was
// created with the same embedding dimension as s.dimensions. If the dimensions
// differ the chunks table (and its HNSW index) are dropped so that init()
// recreates them with the correct schema.
func (s *Store) validateOrResetChunks() error {
	var tableCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM duckdb_tables() WHERE table_name = 'chunks'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("check chunks table existence: %w", err)
	}
	if tableCount == 0 {
		return nil // nothing to validate
	}

	var colType string
	if err := s.db.QueryRow(
		`SELECT data_type FROM duckdb_columns()
		 WHERE table_name = 'chunks' AND column_name = 'embedding'`,
	).Scan(&colType); err != nil {
		return fmt.Errorf("read embedding column type: %w", err)
	}

	existingDim, err := parseDimFromType(colType)
	if err != nil {
		s.log.Warn("could not parse embedding dimension from schema, recreating chunks table",
			"column_type", colType, "err", err)
	}

	if err != nil || existingDim != s.dimensions {
		s.log.Warn("embedding dimension mismatch — dropping and recreating chunks table",
			"existing_dim", existingDim, "configured_dim", s.dimensions)
		if _, err := s.db.Exec(`DROP TABLE IF EXISTS chunks`); err != nil {
			return fmt.Errorf("drop chunks table: %w", err)
		}
	}
	return nil
}

// parseDimFromType extracts the array length from a DuckDB column type string
// such as "FLOAT[768]" or "float[768]".
var reDim = regexp.MustCompile(`(?i)float\[(\d+)\]`)

func parseDimFromType(t string) (int, error) {
	m := reDim.FindStringSubmatch(t)
	if m == nil {
		return 0, fmt.Errorf("unexpected type format: %q", t)
	}
	return strconv.Atoi(m[1])
}

func (s *Store) init() error {
	if err := s.validateOrResetChunks(); err != nil {
		return err
	}

	stmts := []string{
		`INSTALL vss; LOAD vss;`,
		`SET hnsw_enable_experimental_persistence = true;`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id            TEXT      PRIMARY KEY,
			name          VARCHAR   NOT NULL DEFAULT '',
			created_at    TIMESTAMP NOT NULL DEFAULT current_timestamp,
			chat_history  JSON      NOT NULL DEFAULT '[]',
			batch_size    INTEGER   NOT NULL DEFAULT 4096,
			batch_overlap INTEGER   NOT NULL DEFAULT 512
		);`,
		`CREATE TABLE IF NOT EXISTS documents (
			id           TEXT    PRIMARY KEY,
			session_id   TEXT    NOT NULL,
			name         VARCHAR NOT NULL,
			path         VARCHAR NOT NULL,
			content_type VARCHAR NOT NULL,
			status       VARCHAR NOT NULL DEFAULT 'waiting'
		);`,
		`CREATE SEQUENCE IF NOT EXISTS chunk_id_seq START 1;`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS chunks (
			id          INTEGER PRIMARY KEY DEFAULT nextval('chunk_id_seq'),
			document_id TEXT    NOT NULL,
			session_id  TEXT    NOT NULL,
			text        VARCHAR NOT NULL,
			embedding   FLOAT[%d]
		);`, s.dimensions),
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}

	// Create HNSW index only if it doesn't already exist.
	var idxCount int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM duckdb_indexes()
		WHERE index_name = 'idx_chunk_embedding'
	`).Scan(&idxCount)
	if err != nil {
		return fmt.Errorf("checking hnsw index: %w", err)
	}

	if idxCount == 0 {
		idxSQL := `
			CREATE INDEX idx_chunk_embedding ON chunks
			USING HNSW (embedding)
			WITH (metric = 'cosine');
		`
		if _, err := s.db.Exec(idxSQL); err != nil {
			return fmt.Errorf("creating hnsw index: %w", err)
		}
	}

	return nil
}

// ─── Session methods ───────────────────────────────────────────────────────

func (s *Store) CreateSession(ctx context.Context, session enginebus.Session) error {
	m, err := toDBSession(session)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, name, created_at, chat_history, batch_size, batch_overlap)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.Name, m.CreatedAt, m.ChatHistory, m.BatchSize, m.BatchOverlap,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, sessionID uuid.UUID) (enginebus.Session, error) {
	var m dbSession
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at, chat_history, batch_size, batch_overlap
		 FROM sessions WHERE id = $1`,
		sessionID.String(),
	).Scan(&m.ID, &m.Name, &m.CreatedAt, &m.ChatHistory, &m.BatchSize, &m.BatchOverlap)
	if err != nil {
		return enginebus.Session{}, fmt.Errorf("get session: %w", err)
	}

	return toSession(m)
}

func (s *Store) UpdateSession(ctx context.Context, session enginebus.Session) error {
	m, err := toDBSession(session)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE sessions
		 SET chat_history = $2, batch_size = $3, batch_overlap = $4
		 WHERE id = $1`,
		m.ID, m.ChatHistory, m.BatchSize, m.BatchOverlap,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE id = $1`,
		sessionID.String(),
	)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) ListSessions(ctx context.Context) ([]enginebus.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, created_at, chat_history, batch_size, batch_overlap FROM sessions`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []enginebus.Session
	for rows.Next() {
		var m dbSession
		if err := rows.Scan(&m.ID, &m.Name, &m.CreatedAt, &m.ChatHistory, &m.BatchSize, &m.BatchOverlap); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess, err := toSession(m)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions rows: %w", err)
	}

	return sessions, nil
}

// ─── Document methods ──────────────────────────────────────────────────────

func (s *Store) CreateDocument(ctx context.Context, doc enginebus.Document) error {
	m := toDBDocument(doc)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO documents (id, session_id, name, path, content_type, status)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.SessionID, m.Name, m.Path, m.ContentType, m.Status,
	)
	if err != nil {
		return fmt.Errorf("create document: %w", err)
	}
	return nil
}

func (s *Store) UpdateDocument(ctx context.Context, doc enginebus.Document) error {
	m := toDBDocument(doc)
	_, err := s.db.ExecContext(ctx,
		`UPDATE documents
		 SET name = $2, path = $3, content_type = $4, status = $5
		 WHERE id = $1`,
		m.ID, m.Name, m.Path, m.ContentType, m.Status,
	)
	if err != nil {
		return fmt.Errorf("update document: %w", err)
	}
	return nil
}

func (s *Store) ListDocuments(ctx context.Context, sessionID uuid.UUID) ([]enginebus.Document, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, name, path, content_type, status
		 FROM documents WHERE session_id = $1`,
		sessionID.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	var docs []enginebus.Document
	for rows.Next() {
		var m dbDocument
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Name, &m.Path, &m.ContentType, &m.Status); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		doc, err := toDocument(m)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list documents rows: %w", err)
	}

	return docs, nil
}

// ─── Chunk methods ─────────────────────────────────────────────────────────

func (s *Store) AddDocumentChunk(ctx context.Context, documentID uuid.UUID, chunk string, vec enginebus.Vector) error {
	var sessionIDStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT session_id FROM documents WHERE id = $1`,
		documentID.String(),
	).Scan(&sessionIDStr)
	if err != nil {
		return fmt.Errorf("resolve session for document: %w", err)
	}

	vecLiteral := floatSliceToLiteral(vec)
	insertSQL := fmt.Sprintf(
		`INSERT INTO chunks (document_id, session_id, text, embedding)
		 VALUES  ($1, $2, $3, %s)`,
		vecLiteral,
	)
	if _, err := s.db.ExecContext(ctx, insertSQL, documentID.String(), sessionIDStr, chunk); err != nil {
		return fmt.Errorf("add document chunk: %w", err)
	}

	return nil
}

func (s *Store) SearchDocuments(ctx context.Context, sessionID uuid.UUID, queryVec []float32) ([]enginebus.Fragment, error) {
	vecLiteral := floatSliceToLiteral(queryVec)
	querySQL := fmt.Sprintf(`
		SELECT c.session_id, c.document_id, d.path, d.content_type,
		       c.text, c.embedding,
		       array_cosine_similarity(c.embedding, %s::FLOAT[%d]) AS similarity
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		WHERE c.session_id = $1
		ORDER BY similarity DESC
		LIMIT 10
	`, vecLiteral, s.dimensions)

	rows, err := s.db.QueryContext(ctx, querySQL, sessionID.String())
	if err != nil {
		return nil, fmt.Errorf("search documents: %w", err)
	}
	defer rows.Close()

	var fragments []enginebus.Fragment
	for rows.Next() {
		var m dbFragment
		if err := rows.Scan(
			&m.SessionID, &m.DocumentID, &m.Path, &m.ContentType,
			&m.Text, &m.Embedding, &m.Similarity,
		); err != nil {
			return nil, fmt.Errorf("scan fragment: %w", err)
		}
		f, err := toFragment(m)
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search documents rows: %w", err)
	}

	return fragments, nil
}

// ─── helpers ───────────────────────────────────────────────────────────────

func floatSliceToLiteral[T ~float32 | ~float64](vec []T) string {
	sb := strings.Builder{}
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%v", v))
	}
	sb.WriteByte(']')
	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
