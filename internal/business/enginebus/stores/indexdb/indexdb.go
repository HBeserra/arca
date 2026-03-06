package indexdb

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

type Store struct {
	log slog.Logger
	db  *sql.DB

	dimentions int
}

func New(log slog.Logger, db *sql.DB, opts ...Option) *Store {
	s := &Store{
		log:        log,
		db:         db,
		dimentions: 768,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.init()

	return s
}

func (s *Store) init() error {
	// Install and load VSS extension for vector similarity search.
	sql := `
		INSTALL vss; LOAD vss;
	`

	_, err := s.db.Exec(sql)
	if err != nil {
		return fmt.Errorf("error loading VSS extension: %w", err)
	}

	checkSQL := `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_name = 'items';
	`

	var tableExists int
	err = s.db.QueryRow(checkSQL).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("error checking if table exists: %w", err)
	}

	if tableExists > 0 {
		var rowCount int
		err = s.db.QueryRow("SELECT COUNT(*) FROM items").Scan(&rowCount)
		if err != nil {
			return fmt.Errorf("error checking row count: %w", err)
		}

		slog.Info("Table 'items' already exists", "rowCount", rowCount)
		return nil
	}

	_, err = s.db.Exec("SET hnsw_enable_experimental_persistence = true;")
	if err != nil {
		return fmt.Errorf("error setting HNSW persistence: %w", err)
	}

	sql = `
		CREATE TABLE items (
			id        INTEGER   PRIMARY KEY,
			text      VARCHAR,
			embedding FLOAT[%d]
		);
	`

	sql = fmt.Sprintf(sql, s.dimentions)

	if _, err = s.db.Exec(sql); err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	sql = `
		CREATE INDEX idx_embedding ON items 
		USING HNSW (embedding) 
		WITH (metric = 'cosine');
	`

	if _, err = s.db.Exec(sql); err != nil {
		return fmt.Errorf("error creating HNSW index: %w", err)
	}

	return nil
}

func (s *Store) Insert(chunk string, vec []float32) error {
	vecStr := strings.ReplaceAll(fmt.Sprintf("%v", vec), " ", ",")
	sql := fmt.Sprintf("INSERT INTO items (text, embedding) VALUES('%s', %v);", chunk, vecStr)

	if _, err := s.db.Exec(sql); err != nil {
		return fmt.Errorf("insert chunk: %s %w", sql, err)
	}

	return nil
}
