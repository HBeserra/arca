package ml_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stitchvault/internal/embroidery"
	"stitchvault/internal/ml"
	"stitchvault/internal/render"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestEmbedLive loads the embedding model via Kronk v1.28.0 and embeds a string.
// It proves the dependency bump works end-to-end (no llama.cpp ABI mismatch) and
// produces a usable vector. Skipped in -short; first run downloads libs + GGUF.
func TestEmbedLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live model load in -short mode")
	}

	eng := ml.New(discardLog())
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

// squareTriangle renders a recognisable two-shape design to PNG bytes.
func squareTriangle(t *testing.T) []byte {
	t.Helper()
	d := &embroidery.Design{
		Format:  ".pes",
		Threads: []embroidery.Thread{{R: 220}, {G: 160}},
		Stitches: []embroidery.Point{
			{X: 0, Y: 0, Cmd: embroidery.Stitch},
			{X: 40, Y: 0, Cmd: embroidery.Stitch},
			{X: 40, Y: 40, Cmd: embroidery.Stitch},
			{X: 0, Y: 40, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.ColorChange},
			{X: 60, Y: 0, Cmd: embroidery.Stitch},
			{X: 100, Y: 0, Cmd: embroidery.Stitch},
			{X: 80, Y: 36, Cmd: embroidery.Stitch},
			{X: 60, Y: 0, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.End},
		},
	}
	out := filepath.Join(t.TempDir(), "shape.png")
	if err := render.New().Thumbnail(d, out, 512); err != nil {
		t.Fatalf("render: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read png: %v", err)
	}
	return b
}

// TestClassifyLive loads the vision model and classifies a rendered design,
// verifying the full image -> structured-JSON pipeline (model.ImageMessage +
// json_schema grammar). Skipped in -short; first run downloads the ~3GB VLM.
func TestClassifyLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live vision model load in -short mode")
	}

	png := squareTriangle(t)

	eng := ml.New(discardLog())
	defer eng.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	c, err := eng.Classify(ctx, png, "rose flower")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if c.Caption == "" {
		t.Error("classification caption is empty")
	}
	if len(c.Tags) == 0 {
		t.Error("classification produced no tags")
	}
	// Surface the result so we can eyeball quality.
	t.Logf("caption=%q\n elements=%v\n style=%q theme=%q mood=%q\n tags=%v",
		c.Caption, c.Elements, c.Style, c.Theme, c.Mood, c.Tags)
}
