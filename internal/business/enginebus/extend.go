package enginebus

import (
	"context"

	"github.com/google/uuid"
)

type (
	ExtEngine interface {
		Status(ctx context.Context) (*Status, error)
		Load(ctx context.Context) error
		Eject(ctx context.Context) error
		LoadedModels() bool
		Close(ctx context.Context) error
		CreateSession(ctx context.Context, name string, opts ...SessionOption) (Session, error)
		ListSessions(ctx context.Context) ([]Session, error)
		RenameSession(context.Context, uuid.UUID, string) error
		DeleteSession(ctx context.Context, id uuid.UUID) error
		GetSession(ctx context.Context, sessionID uuid.UUID) (Session, error)
		CreateFolder(ctx context.Context, sessionID uuid.UUID, name, path string, parentID *uuid.UUID) (Document, error)
		RegisterDocument(ctx context.Context, input AddDocumentInput) (Document, error)
		EmbedDocument(ctx context.Context, doc Document, text string) error
		AddDocumentText(ctx context.Context, input AddDocumentInput) error
		AddDocumentTextStream(ctx context.Context, doc AddDocumentStreamInput) error
		SearchDocs(ctx context.Context, sessionID uuid.UUID, query string, topK int, similarityThreshold float32) ([]Fragment, error)
		Chat(ctx context.Context, q Question) (Answer, error)
		ChatStream(ctx context.Context, q Question) (<-chan ChatEvent, error)
		ClearChatHistory(ctx context.Context, sessionID uuid.UUID) error
		Rerank(ctx context.Context, query string, docs []string) ([]RankedDoc, error)
		Summarize(ctx context.Context, text string) (string, error)
		ListDocuments(ctx context.Context, sessionID uuid.UUID) ([]Document, error)
	}
)
