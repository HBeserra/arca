package indexdb_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"changeme/internal/business/enginebus"
	"changeme/internal/business/enginebus/stores/indexdb"
	"changeme/internal/business/types/status"

	"github.com/google/uuid"
	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *indexdb.Store {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	log := slog.Default()
	store, err := indexdb.New(log, db, indexdb.WithDimensions(4))
	require.NoError(t, err)
	return store
}

func TestSession_CreateGetUpdateDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	session := enginebus.Session{
		ID:            uuid.New(),
		ChatHistory:   nil,
		BatchSize:     4096,
		BatchsOverlap: 512,
	}

	// Create
	require.NoError(t, store.CreateSession(ctx, session))

	// Get
	got, err := store.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, got.ID)
	assert.Equal(t, session.BatchSize, got.BatchSize)
	assert.Equal(t, session.BatchsOverlap, got.BatchsOverlap)

	// Update
	session.BatchSize = 2048
	require.NoError(t, store.UpdateSession(ctx, session))

	got, err = store.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 2048, got.BatchSize)

	// Delete
	require.NoError(t, store.DeleteSession(ctx, session.ID))
	_, err = store.GetSession(ctx, session.ID)
	assert.Error(t, err)
}

func TestDocument_CreateListUpdate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	session := enginebus.Session{
		ID:            uuid.New(),
		BatchSize:     4096,
		BatchsOverlap: 512,
	}
	require.NoError(t, store.CreateSession(ctx, session))

	doc := enginebus.Document{
		ID:          uuid.New(),
		SessionID:   session.ID,
		Name:        "test.txt",
		Path:        "/tmp/test.txt",
		ContentType: "text/plain",
		Status:      status.Waiting,
	}

	require.NoError(t, store.CreateDocument(ctx, doc))

	docs, err := store.ListDocuments(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, doc.ID, docs[0].ID)
	assert.Equal(t, doc.Name, docs[0].Name)

	doc.Status = status.Completed
	require.NoError(t, store.UpdateDocument(ctx, doc))

	docs, err = store.ListDocuments(ctx, session.ID)
	require.NoError(t, err)
	assert.True(t, docs[0].Status.Equal(status.Completed))
}

func TestAddChunkAndSearchDocuments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	session := enginebus.Session{
		ID:            uuid.New(),
		BatchSize:     4096,
		BatchsOverlap: 512,
	}
	require.NoError(t, store.CreateSession(ctx, session))

	doc := enginebus.Document{
		ID:          uuid.New(),
		SessionID:   session.ID,
		Name:        "embed.txt",
		Path:        "/tmp/embed.txt",
		ContentType: "text/plain",
		Status:      status.Processing,
	}
	require.NoError(t, store.CreateDocument(ctx, doc))

	vec := enginebus.Vector{0.1, 0.2, 0.3, 0.4}
	require.NoError(t, store.AddDocumentChunk(ctx, doc.ID, "hello world", vec))

	queryVec := []float32{0.1, 0.2, 0.3, 0.4}
	fragments, err := store.SearchDocuments(ctx, session.ID, queryVec)
	require.NoError(t, err)
	require.Len(t, fragments, 1)
	assert.Equal(t, "hello world", fragments[0].Text)
	assert.Equal(t, doc.ID, fragments[0].DocumentID)
	assert.Equal(t, session.ID, fragments[0].SessionID)
	assert.InDelta(t, 1.0, fragments[0].Similarity, 0.001)
}
