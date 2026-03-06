package enginebus

import (
	"changeme/internal/business/types/status"

	"github.com/google/uuid"
)

type Document struct {
	ID          string
	Name        string
	Path        string
	ContentType string
	Status      status.Status

	// Similarity float64
	// Text       string
	// Embedding  []float64
}

type Fragment struct {
	SessionID   uuid.UUID
	DocumentID  uuid.UUID
	Path        string
	ContentType string
	Similarity  float64
	Text        string
	Embedding   Vector
}
