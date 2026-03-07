package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"changeme/internal/business/enginebus"

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

// SessionInfo is the JSON-serialisable view of a session.
type SessionInfo struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	CreatedAt string         `json:"created_at"`
	Documents []DocumentInfo `json:"documents"`
}

// DocumentInfo is the JSON-serialisable view of a document.
type DocumentInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Status      string `json:"status"`
}

// supportedExts lists file extensions that can be text-extracted.
var supportedExts = map[string]bool{
	".txt": true, ".md": true, ".mdx": true,
	".go": true, ".py": true, ".js": true, ".ts": true,
	".jsx": true, ".tsx": true, ".json": true,
	".yaml": true, ".yml": true, ".toml": true,
	".csv": true, ".xml": true, ".html": true,
	".sh": true, ".sql": true, ".rs": true,
	".java": true, ".c": true, ".cpp": true, ".h": true,
}

// IndexService is the Wails-facing service for session and ingestion management.
type IndexService struct {
	eng *enginebus.Engine
	mu  sync.RWMutex
}

// NewIndexService returns a new IndexService.
func NewIndexService(eng *enginebus.Engine) *IndexService {
	return &IndexService{eng: eng}
}

// CreateSession creates and persists a new named session.
func (s *IndexService) CreateSession(name string) (*SessionInfo, error) {
	ctx := context.Background()

	sess, err := s.eng.CreateSession(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("index: create session: %w", err)
	}

	info := sessionToInfo(sess, nil)
	return &info, nil
}

// ListSessions returns all persisted sessions.
func (s *IndexService) ListSessions() ([]SessionInfo, error) {
	ctx := context.Background()

	sessions, err := s.eng.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("index: list sessions: %w", err)
	}

	out := make([]SessionInfo, 0, len(sessions))
	for _, sess := range sessions {
		docs, _ := s.eng.ListDocuments(ctx, sess.ID)
		out = append(out, sessionToInfo(sess, docs))
	}

	return out, nil
}

// GetSession returns a single session by ID with its documents.
func (s *IndexService) GetSession(id string) (*SessionInfo, error) {
	ctx := context.Background()

	sid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("index: invalid session id: %w", err)
	}

	sess, err := s.eng.GetSession(ctx, sid)
	if err != nil {
		return nil, fmt.Errorf("index: session %s not found: %w", id, err)
	}

	docs, _ := s.eng.ListDocuments(ctx, sid)
	info := sessionToInfo(sess, docs)
	return &info, nil
}

// DeleteSession removes a session.
func (s *IndexService) DeleteSession(id string) error {
	ctx := context.Background()

	sid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("index: invalid session id: %w", err)
	}

	return s.eng.DeleteSession(ctx, sid)
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
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("index: invalid session id: %w", err)
	}

	go func() {
		ctx := context.Background()
		app := application.Get()

		files, err := collectFiles(paths)
		if err != nil {
			app.Event.Emit("index:error", IndexErrorEvent{
				SessionID: sessionID,
				DocName:   "batch",
				Error:     err.Error(),
			})
			return
		}

		for _, path := range files {
			if err := ctx.Err(); err != nil {
				return
			}

			name := filepath.Base(path)

			app.Event.Emit("index:progress", IndexProgressEvent{
				SessionID: sessionID,
				DocName:   name,
				Stage:     "reading",
				Pct:       5,
			})

			raw, err := os.ReadFile(path)
			if err != nil {
				app.Event.Emit("index:error", IndexErrorEvent{
					SessionID: sessionID,
					DocName:   name,
					Error:     fmt.Sprintf("read: %v", err),
				})
				continue
			}

			text := string(raw)
			if !utf8.ValidString(text) {
				app.Event.Emit("index:error", IndexErrorEvent{
					SessionID: sessionID,
					DocName:   name,
					Error:     "file is not valid UTF-8 text",
				})
				continue
			}

			app.Event.Emit("index:progress", IndexProgressEvent{
				SessionID: sessionID,
				DocName:   name,
				Stage:     "embedding",
				Pct:       20,
			})

			ext := strings.ToLower(filepath.Ext(path))
			input := enginebus.AddDocumentInput{
				SessionID:   sid,
				Name:        name,
				Path:        path,
				Text:        text,
				ContentType: ext,
			}

			if err := s.eng.AddDocumentText(ctx, input); err != nil {
				app.Event.Emit("index:error", IndexErrorEvent{
					SessionID: sessionID,
					DocName:   name,
					Error:     err.Error(),
				})
				continue
			}

			app.Event.Emit("index:progress", IndexProgressEvent{
				SessionID: sessionID,
				DocName:   name,
				Stage:     "done",
				Pct:       100,
			})
		}

		app.Event.Emit("index:complete", IndexCompleteEvent{SessionID: sessionID})
	}()

	return nil
}

// ─── helpers ───────────────────────────────────────────────────────────────

func sessionToInfo(sess enginebus.Session, docs []enginebus.Document) SessionInfo {
	docInfos := make([]DocumentInfo, 0, len(docs))
	for _, d := range docs {
		docInfos = append(docInfos, DocumentInfo{
			ID:          d.ID.String(),
			Name:        d.Name,
			Path:        d.Path,
			ContentType: d.ContentType,
			Status:      d.Status.String(),
		})
	}

	return SessionInfo{
		ID:        sess.ID.String(),
		Name:      sess.Name,
		CreatedAt: sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Documents: docInfos,
	}
}

func collectFiles(paths []string) ([]string, error) {
	var files []string

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}

		if info.IsDir() {
			err := filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !fi.IsDir() && supportedExts[strings.ToLower(filepath.Ext(path))] {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else if supportedExts[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
	}

	return files, nil
}
