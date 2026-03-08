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

// IndexQueuedEvent is emitted once all documents have been registered (before embedding starts).
type IndexQueuedEvent struct {
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
	eng     enginebus.ExtEngine
	mu      sync.RWMutex
	appIcon []byte
}

// NewIndexService returns a new IndexService.
func NewIndexService(eng enginebus.ExtEngine, appIcon []byte) *IndexService {
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

// pendingFile holds a registered document waiting to be embedded.
type pendingFile struct {
	doc  enginebus.Document
	text string
}

// IndexPaths registers all documents immediately (status: waiting), emits
// index:queued so the UI can show them, then embeds each file in a background
// goroutine.
func (s *IndexService) IndexPaths(sessionID string, paths []string) error {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("index: invalid session id: %w", err)
	}

	ctx := context.Background()

	// Phase 1 — register all files synchronously so they appear in the UI.
	var pending []pendingFile
	for _, p := range paths {
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
			if s.ignoreFolder(p) {
				continue
			}
			files := s.registerFolder(ctx, sid, sessionID, p)
			pending = append(pending, files...)
		} else {
			if s.ignoreFile(p) {
				continue
			}
			if pf, ok := s.registerFile(ctx, sid, sessionID, p, nil); ok {
				pending = append(pending, pf)
			}
		}
	}

	// Notify frontend: all documents are now visible as "waiting".
	application.Get().Event.Emit("index:queued", IndexQueuedEvent{SessionID: sessionID})

	// Phase 2 — embed each file in the background.
	go func() {
		for _, pf := range pending {
			s.embedFile(ctx, sessionID, pf)
		}
		application.Get().Event.Emit("index:complete", IndexCompleteEvent{SessionID: sessionID})
		_ = beeep.Notify("Arca — Indexing complete", fmt.Sprintf("All documents have been indexed into session %s.", sessionID), s.appIcon)
	}()

	return nil
}

// registerFolder creates all folder records and registers every supported file
// under the folder tree with status.Waiting. Returns the pending file list.
func (s *IndexService) registerFolder(ctx context.Context, sid uuid.UUID, sessionID, folderPath string) []pendingFile {
	folderIDs := map[string]uuid.UUID{}

	root, err := s.eng.CreateFolder(ctx, sid, filepath.Base(folderPath), folderPath, nil)
	if err != nil {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID,
			DocName:   filepath.Base(folderPath),
			Error:     err.Error(),
		})
		return nil
	}
	folderIDs[folderPath] = root.ID

	var pending []pendingFile

	filepath.Walk(folderPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil || path == folderPath {
			return nil
		}
		parentID := folderIDs[filepath.Dir(path)]

		if fi.IsDir() {
			sub, err := s.eng.CreateFolder(ctx, sid, fi.Name(), path, &parentID)
			if err != nil {
				application.Get().Event.Emit("index:error", IndexErrorEvent{
					SessionID: sessionID, DocName: fi.Name(), Error: err.Error(),
				})
				return nil
			}
			folderIDs[path] = sub.ID
		} else if supportedExts[strings.ToLower(filepath.Ext(path))] {
			if pf, ok := s.registerFile(ctx, sid, sessionID, path, &parentID); ok {
				pending = append(pending, pf)
			}
		}
		return nil
	})

	return pending
}

// registerFile reads and validates the file, creates the document record with
// status.Waiting, and returns the pending work item.
func (s *IndexService) registerFile(ctx context.Context, sid uuid.UUID, sessionID, path string, parentID *uuid.UUID) (pendingFile, bool) {
	name := filepath.Base(path)

	raw, err := os.ReadFile(path)
	if err != nil {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID, DocName: name, Error: fmt.Sprintf("read: %v", err),
		})
		return pendingFile{}, false
	}

	text := string(raw)
	if !utf8.ValidString(text) {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID, DocName: name, Error: "file is not valid UTF-8 text",
		})
		return pendingFile{}, false
	}

	doc, err := s.eng.RegisterDocument(ctx, enginebus.AddDocumentInput{
		SessionID:   sid,
		ParentID:    parentID,
		Name:        name,
		Path:        path,
		ContentType: strings.ToLower(filepath.Ext(path)),
	})
	if err != nil {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID, DocName: name, Error: err.Error(),
		})
		return pendingFile{}, false
	}

	return pendingFile{doc: doc, text: text}, true
}

// embedFile runs the embedding pipeline for a single registered file and
// emits progress events.
func (s *IndexService) embedFile(ctx context.Context, sessionID string, pf pendingFile) {
	name := pf.doc.Name
	docID := pf.doc.ID.String()

	emit := func(stage string, pct int) {
		application.Get().Event.Emit("index:progress", IndexProgressEvent{
			SessionID: sessionID,
			DocID:     docID,
			DocName:   name,
			Stage:     stage,
			Pct:       pct,
		})
	}

	emit("embedding", 20)

	if err := s.eng.EmbedDocument(ctx, pf.doc, pf.text); err != nil {
		application.Get().Event.Emit("index:error", IndexErrorEvent{
			SessionID: sessionID, DocName: name, Error: err.Error(),
		})
		return
	}

	emit("done", 100)
}

func (s *IndexService) ignoreFolder(path string) bool {
	base := filepath.Base(path)
	// Ignore common VCS and OS folders.
	ignored := []string{".git", ".svn", ".hg", "node_modules", "__pycache__", "venv", "env", "tmp", "temp"}
	for _, ig := range ignored {
		if base == ig {
			return true
		}
	}
	return false
}

func (s *IndexService) ignoreFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	// Ignore unsupported file types.
	if !supportedExts[ext] {
		return true
	}
	// Ignore hidden files.
	if strings.HasPrefix(filepath.Base(path), ".") {
		return true
	}
	return false
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
