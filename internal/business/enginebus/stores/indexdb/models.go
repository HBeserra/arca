package indexdb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"changeme/internal/business/enginebus"
	"changeme/internal/business/types/status"

	"github.com/google/uuid"
)

// ─── DB model structs ──────────────────────────────────────────────────────
// These mirror the database table rows exactly, using primitive types that
// the sql.Rows scanner can handle directly.

type dbSession struct {
	ID           string
	Name         string
	CreatedAt    time.Time
	ChatHistory  any // DuckDB returns JSON columns as already-decoded interface{}
	BatchSize    int
	BatchOverlap int
}

type dbDocument struct {
	ID          string
	SessionID   string
	ParentID    sql.NullString
	Type        string
	Name        string
	Path        string
	ContentType string
	Status      string
}

type dbFragment struct {
	SessionID   string
	DocumentID  string
	Path        string
	ContentType string
	Text        string
	Embedding   any // DuckDB returns FLOAT[] as []interface{}{float32,...}
	Similarity  float64
}

// ─── Business → DB model ───────────────────────────────────────────────────

func toDBSession(s enginebus.Session) (dbSession, error) {
	history := s.ChatHistory
	if history == nil {
		history = []enginebus.ChatMessage{}
	}
	b, err := json.Marshal(history)
	if err != nil {
		return dbSession{}, fmt.Errorf("marshal chat_history: %w", err)
	}
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return dbSession{
		ID:           s.ID.String(),
		Name:         s.Name,
		CreatedAt:    createdAt,
		ChatHistory:  string(b),
		BatchSize:    s.BatchSize,
		BatchOverlap: s.BatchsOverlap,
	}, nil
}

func toDBDocument(d enginebus.Document) dbDocument {
	var parentID sql.NullString
	if d.ParentID != nil {
		parentID = sql.NullString{String: d.ParentID.String(), Valid: true}
	}
	docType := d.Type
	if docType == "" {
		docType = "file"
	}
	return dbDocument{
		ID:          d.ID.String(),
		SessionID:   d.SessionID.String(),
		ParentID:    parentID,
		Type:        docType,
		Name:        d.Name,
		Path:        d.Path,
		ContentType: d.ContentType,
		Status:      d.Status.String(),
	}
}

// ─── DB model → Business ───────────────────────────────────────────────────

func toSession(d dbSession) (enginebus.Session, error) {
	id, err := uuid.Parse(d.ID)
	if err != nil {
		return enginebus.Session{}, fmt.Errorf("parse session id: %w", err)
	}

	// DuckDB returns JSON columns as already-decoded Go values;
	// re-encode so we can unmarshal into the typed slice.
	b, err := json.Marshal(d.ChatHistory)
	if err != nil {
		return enginebus.Session{}, fmt.Errorf("re-marshal chat_history: %w", err)
	}
	var history []enginebus.ChatMessage
	if err := json.Unmarshal(b, &history); err != nil {
		return enginebus.Session{}, fmt.Errorf("unmarshal chat_history: %w", err)
	}

	return enginebus.Session{
		ID:            id,
		Name:          d.Name,
		CreatedAt:     d.CreatedAt,
		ChatHistory:   history,
		BatchSize:     d.BatchSize,
		BatchsOverlap: d.BatchOverlap,
	}, nil
}

func toDocument(d dbDocument) (enginebus.Document, error) {
	id, err := uuid.Parse(d.ID)
	if err != nil {
		return enginebus.Document{}, fmt.Errorf("parse document id: %w", err)
	}
	sid, err := uuid.Parse(d.SessionID)
	if err != nil {
		return enginebus.Document{}, fmt.Errorf("parse session id: %w", err)
	}
	st, err := status.Parse(d.Status)
	if err != nil {
		return enginebus.Document{}, fmt.Errorf("parse status: %w", err)
	}
	var parentID *uuid.UUID
	if d.ParentID.Valid && d.ParentID.String != "" {
		pid, err := uuid.Parse(d.ParentID.String)
		if err != nil {
			return enginebus.Document{}, fmt.Errorf("parse parent id: %w", err)
		}
		parentID = &pid
	}
	docType := d.Type
	if docType == "" {
		docType = "file"
	}
	return enginebus.Document{
		ID:          id,
		SessionID:   sid,
		ParentID:    parentID,
		Type:        docType,
		Name:        d.Name,
		Path:        d.Path,
		ContentType: d.ContentType,
		Status:      st,
	}, nil
}

func toFragment(d dbFragment) (enginebus.Fragment, error) {
	sid, err := uuid.Parse(d.SessionID)
	if err != nil {
		return enginebus.Fragment{}, fmt.Errorf("parse session id: %w", err)
	}
	did, err := uuid.Parse(d.DocumentID)
	if err != nil {
		return enginebus.Fragment{}, fmt.Errorf("parse document id: %w", err)
	}

	// DuckDB returns FLOAT[] as []interface{}{float32,...}
	var embedding enginebus.Vector
	if raw, ok := d.Embedding.([]interface{}); ok {
		embedding = make(enginebus.Vector, len(raw))
		for i, v := range raw {
			switch f := v.(type) {
			case float32:
				embedding[i] = f
			case float64:
				embedding[i] = float32(f)
			}
		}
	}

	return enginebus.Fragment{
		SessionID:   sid,
		DocumentID:  did,
		Path:        d.Path,
		ContentType: d.ContentType,
		Text:        d.Text,
		Embedding:   embedding,
		Similarity:  d.Similarity,
	}, nil
}
