package ml_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"stitchvault/internal/ml"
)

// TestEmbedLive loads the embedding model via Kronk v1.28.0 and embeds a string.
// It proves the dependency bump works end-to-end (no llama.cpp ABI mismatch) and
// produces a usable vector. Skipped in -short; needs the model (first run downloads
// the llama.cpp libs + the GGUF).
func TestEmbedLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live model load in -short mode")
	}

	eng := ml.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer eng.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	vec, err := eng.Embed(ctx, "a red rose embroidery design with green leaves")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != ml.EmbedDim {
		t.Fatalf("embedding dim = %d, want %d", len(vec), ml.EmbedDim)
	}

	nonzero := false
	for _, v := range vec {
		if v != 0 {
			nonzero = true
			break
		}
	}
	if !nonzero {
		t.Error("embedding is all zeros")
	}
}
