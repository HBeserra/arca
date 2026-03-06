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

// SessionOption is a functional option for configuring a Session.
type SessionOption func(*Session)

// WithBatchSize sets the batch size for document chunking.
func WithBatchSize(size int) SessionOption {
	return func(s *Session) { s.BatchSize = size }
}

// WithBatchOverlap sets the overlap between document chunks.
func WithBatchOverlap(overlap int) SessionOption {
	return func(s *Session) { s.BatchsOverlap = overlap }
}
