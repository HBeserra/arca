package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Document represents an ingested file.
type Document struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Ext        string    `json:"ext"`
	Size       int64     `json:"size"`
	Status     string    `json:"status"` // "indexed" | "processing" | "error"
	ChunkCount int       `json:"chunk_count"`
	Summary    string    `json:"summary"`
	Centroid   []float32 `json:"centroid"`
	IndexedAt  time.Time `json:"indexed_at"`
}

// DocumentGroup clusters related documents.
type DocumentGroup struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	DocIDs   []string  `json:"doc_ids"`
	Centroid []float32 `json:"centroid"`
}

// Session represents a named knowledge base.
type Session struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	CreatedAt time.Time       `json:"created_at"`
	Documents []Document      `json:"documents"`
	Groups    []DocumentGroup `json:"groups"`
}

func sessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".arca", "sessions"), nil
}

func sessionDir(id string) (string, error) {
	base, err := sessionsDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(base, id), nil
}

// Load reads a session from disk.
func Load(id string) (*Session, error) {
	dir, err := sessionDir(id)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("session: load %s: %w", id, err)
	}

	var s Session

	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session: unmarshal %s: %w", id, err)
	}

	return &s, nil
}

// Save persists the session to disk.
func (s *Session) Save() error {
	dir, err := sessionDir(s.ID)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("session: mkdir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0644); err != nil {
		return fmt.Errorf("session: write: %w", err)
	}

	return nil
}

// ListSessions returns all persisted sessions.
func ListSessions() ([]*Session, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("session: list: %w", err)
	}

	var sessions []*Session

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		s, err := Load(e.Name())
		if err != nil {
			continue
		}

		sessions = append(sessions, s)
	}

	return sessions, nil
}

// Delete removes a session directory from disk.
func Delete(id string) error {
	dir, err := sessionDir(id)
	if err != nil {
		return err
	}

	return os.RemoveAll(dir)
}

// UpsertDocument adds or replaces a document in the session by ID.
func (s *Session) UpsertDocument(doc Document) {
	for i, d := range s.Documents {
		if d.ID == doc.ID {
			s.Documents[i] = doc
			return
		}
	}

	s.Documents = append(s.Documents, doc)
}
