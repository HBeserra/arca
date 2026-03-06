package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"changeme/internal/engine"
	"changeme/internal/grouper"
	"changeme/internal/indexer"
	"changeme/internal/session"
	"changeme/internal/store"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// IndexProgressEvent is emitted as each document is ingested.
type IndexProgressEvent struct {
	SessionID string `json:"sessionID"`
	DocID     string `json:"docID"`
	DocName   string `json:"docName"`
	Stage     string `json:"stage"`
	Pct       int    `json:"pct"`
}

// IndexCompleteEvent is emitted when all documents in a batch finish.
type IndexCompleteEvent struct {
	SessionID string `json:"sessionID"`
}

// IndexErrorEvent is emitted when a document fails to index.
type IndexErrorEvent struct {
	SessionID string `json:"sessionID"`
	DocName   string `json:"docName"`
	Error     string `json:"error"`
}

// IndexService is the Wails-facing service for session and ingestion management.
type IndexService struct {
	eng      *engine.Engine
	mu       sync.RWMutex
	sessions map[string]*session.Session
	stores   map[string]*store.Store
}

// NewIndexService returns a new IndexService.
func NewIndexService(eng *engine.Engine) *IndexService {
	return &IndexService{
		eng:      eng,
		sessions: make(map[string]*session.Session),
		stores:   make(map[string]*store.Store),
	}
}

// LoadAll restores persisted sessions into memory. Call once at startup.
func (s *IndexService) LoadAll() error {
	sessions, err := session.ListSessions()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sess := range sessions {
		s.sessions[sess.ID] = sess

		st, err := store.Load(sess.ID)
		if err != nil {
			st = store.New()
		}

		s.stores[sess.ID] = st
	}

	return nil
}

// CreateSession creates and persists a new named session.
func (s *IndexService) CreateSession(name string) (*session.Session, error) {
	sess := &session.Session{
		ID:        uuid.New().String(),
		Name:      name,
		CreatedAt: time.Now(),
	}

	if err := sess.Save(); err != nil {
		return nil, fmt.Errorf("index: create session: %w", err)
	}

	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.stores[sess.ID] = store.New()
	s.mu.Unlock()

	return sess, nil
}

// ListSessions returns all in-memory sessions.
func (s *IndexService) ListSessions() []*session.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*session.Session, 0, len(s.sessions))

	for _, sess := range s.sessions {
		out = append(out, sess)
	}

	return out
}

// GetSession returns a single session by ID.
func (s *IndexService) GetSession(id string) (*session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("index: session %s not found", id)
	}

	return sess, nil
}

// DeleteSession removes a session from memory and disk.
func (s *IndexService) DeleteSession(id string) error {
	s.mu.Lock()
	delete(s.sessions, id)
	delete(s.stores, id)
	s.mu.Unlock()

	return session.Delete(id)
}

// PickFiles opens a native multi-file dialog and returns selected paths.
func (s *IndexService) PickFiles() ([]string, error) {
	paths, err := application.Get().Dialog.OpenFile().
		SetTitle("Select Files to Index").
		PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}

	return paths, nil
}

// PickFolder opens a native folder dialog and returns the selected path.
func (s *IndexService) PickFolder() (string, error) {
	path, err := application.Get().Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetTitle("Select Folder to Index").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}

	return path, nil
}

// PickDisk opens a native dialog for selecting a mount point (treated as a folder).
func (s *IndexService) PickDisk() (string, error) {
	return s.PickFolder()
}

// IndexPaths triggers ingestion of the given paths into the session (async).
func (s *IndexService) IndexPaths(sessionID string, paths []string) error {
	s.mu.RLock()
	sess, ok := s.sessions[sessionID]
	st := s.stores[sessionID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("index: session %s not found", sessionID)
	}

	go func() {
		ctx := context.Background()
		app := application.Get()

		onProgress := func(docID, docName, stage string, pct int) {
			if stage == "done" || pct == 100 {
				return
			}

			if len(stage) > 5 && stage[:5] == "error" {
				app.Event.Emit("index:error", IndexErrorEvent{
					SessionID: sessionID,
					DocName:   docName,
					Error:     stage,
				})

				return
			}

			app.Event.Emit("index:progress", IndexProgressEvent{
				SessionID: sessionID,
				DocID:     docID,
				DocName:   docName,
				Stage:     stage,
				Pct:       pct,
			})
		}

		if err := indexer.IndexPaths(ctx, s.eng, sess, st, paths, onProgress); err != nil {
			app.Event.Emit("index:error", IndexErrorEvent{
				SessionID: sessionID,
				DocName:   "batch",
				Error:     err.Error(),
			})

			return
		}

		// Save store.
		if err := st.Save(sessionID); err != nil {
			app.Event.Emit("index:error", IndexErrorEvent{
				SessionID: sessionID,
				DocName:   "store",
				Error:     err.Error(),
			})

			return
		}

		// Regroup.
		s.mu.RLock()
		currentSess := s.sessions[sessionID]
		s.mu.RUnlock()

		currentSess.Groups = grouper.GroupDocuments(currentSess.Documents)
		_ = currentSess.Save()

		app.Event.Emit("index:complete", IndexCompleteEvent{SessionID: sessionID})
	}()

	return nil
}
