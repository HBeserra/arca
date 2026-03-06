package enginebus

import (
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/google/uuid"
)

// Session represents a user session, which can have multiple documents and associated metadata.
type Session struct {
	ID          uuid.UUID
	ChatHistory []model.D

	// Configurable parameters for embedding
	BatchSize     int
	BatchsOverlap int
}
