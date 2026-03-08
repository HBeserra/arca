package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"changeme/internal/business/enginebus"

	"github.com/gen2brain/beeep"
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
	ParentID    string `json:"parent_id"` // empty string = root-level
	Type        string `json:"type"`      // "file" or "folder"
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
	eng     *enginebus.Engine
	mu      sync.RWMutex
	appIcon []byte
}

// NewIndexService returns a new IndexService.
func NewIndexService(eng *enginebus.Engine, appIcon []byte) *IndexService {
	return &IndexService{eng: eng, appIcon: appIcon}
}

func (s *IndexService) Eject() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.eng.Eject(ctx)
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

// RenameSession updates the name of a session.
func (s *IndexService) RenameSession(id string, name string) error {
	ctx := context.Background()

	sid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("index: invalid session id: %w", err)
	}

	err = s.eng.RenameSession(ctx, sid, name)
	if err != nil {
		return fmt.Errorf("index: rename session: %w", err)
	}
	return nil
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
// Directories are represented as a single folder document in the sidebar;
// their files are indexed as children. Plain files are indexed at the root level.
func (s *IndexService) IndexPaths(sessionID string, paths []string) error {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("index: invalid session id: %w", err)
	}

	go func() {
		ctx := context.Background()

		for _, p := range paths {
			if ctx.Err() != nil {
				return
			}
			info, err := os.Stat(p)
			if err != nil {
				application.Get().Event.Emit("index:error", IndexErrorEvent{
					SessionID: sessionID,
					DocName:   filepath.Base(p),
					Error:     err.Error(),
				})
				continue
			}
			if info.IsDir() {
				s.indexFolder(ctx, sid, sessionID, p)
			} else {
				s.indexFile(ctx, sid, sessionID, p, nil)
			}

		}
		application.Get().Event.Emit("index:complete", IndexCompleteEvent{SessionID: sessionID})
		_ = beeep.Notify("Arca — Indexing complete", fmt.Sprintf("All documents have been indexed into session %s.", sessionID), s.appIcon)

	}()

	return nil
}

// indexFolder creates a folder document tree mirroring the directory structure,
// then indexes each supported file under its immediate parent folder.
func (s *IndexService) indexFolder(ctx context.Context, sid uuid.UUID, sessionID, folderPath string) {
	// folderIDs maps an absolute directory path → its document ID so that
	// sub-folders and files can reference the correct parent.
	folderIDs := map[string]uuid.UUID{}

	// Create the root folder first.
	root, err := s.eng.CreateFolder(ctx, sid, filepath.Base(folderPath), folderPath, nil)
	if err != nil {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID,
			DocName:   filepath.Base(folderPath),
			Error:     err.Error(),
		})
		return
	}
	folderIDs[folderPath] = root.ID

	filepath.Walk(folderPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil || path == folderPath {
			return nil
		}

		parentID := folderIDs[filepath.Dir(path)]

		if fi.IsDir() {
			sub, err := s.eng.CreateFolder(ctx, sid, fi.Name(), path, &parentID)
			if err != nil {
				application.Get().Event.Emit("index:error", IndexErrorEvent{
					SessionID: sessionID,
					DocName:   fi.Name(),
					Error:     err.Error(),
				})
				return nil
			}
			folderIDs[path] = sub.ID
		} else if supportedExts[strings.ToLower(filepath.Ext(path))] {
			s.indexFile(ctx, sid, sessionID, path, &parentID)
		}
		return nil
	})
}

// indexFile reads, validates, embeds, and stores a single file document.
func (s *IndexService) indexFile(ctx context.Context, sid uuid.UUID, sessionID, path string, parentID *uuid.UUID) {
	name := filepath.Base(path)
	docID := uuid.New()

	emit := func(stage string, pct int) {
		application.Get().Event.Emit("index:progress", IndexProgressEvent{
			SessionID: sessionID,
			DocID:     docID.String(),
			DocName:   name,
			Stage:     stage,
			Pct:       pct,
		})
	}

	emit("reading", 5)

	raw, err := os.ReadFile(path)
	if err != nil {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID,
			DocName:   name,
			Error:     fmt.Sprintf("read: %v", err),
		})
		return
	}

	text := string(raw)
	if !utf8.ValidString(text) {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID,
			DocName:   name,
			Error:     "file is not valid UTF-8 text",
		})
		return
	}

	emit("embedding", 20)

	input := enginebus.AddDocumentInput{
		ID:          docID,
		SessionID:   sid,
		ParentID:    parentID,
		Name:        name,
		Path:        path,
		Text:        text,
		ContentType: strings.ToLower(filepath.Ext(path)),
	}

	if err := s.eng.AddDocumentText(ctx, input); err != nil {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID,
			DocName:   name,
			Error:     err.Error(),
		})
		return
	}

	emit("done", 100)
}

// ─── helpers ───────────────────────────────────────────────────────────────

func sessionToInfo(sess enginebus.Session, docs []enginebus.Document) SessionInfo {
	docInfos := make([]DocumentInfo, 0, len(docs))
	for _, d := range docs {
		parentID := ""
		if d.ParentID != nil {
			parentID = d.ParentID.String()
		}
		docType := d.Type
		if docType == "" {
			docType = "file"
		}
		docInfos = append(docInfos, DocumentInfo{
			ID:          d.ID.String(),
			ParentID:    parentID,
			Type:        docType,
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
