package enginebus

import (
	"stitchvault/internal/business/types/status"

	"github.com/google/uuid"
)

type Document struct {
	ID          uuid.UUID
	SessionID   uuid.UUID
	ParentID    *uuid.UUID // nil = root-level document
	Type        string     // "file" or "folder"
	Name        string
	Path        string
	ContentType string
	Status      status.Status
}

type Fragment struct {
	SessionID   uuid.UUID
	DocumentID  uuid.UUID
	Path        string
	ContentType string
	Similarity  float64
	Text        string
	Embedding   Vector
	FileName    string
	StartLine   int
}
