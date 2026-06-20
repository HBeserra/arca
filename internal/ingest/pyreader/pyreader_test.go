package pyreader_test

import (
	"context"
	"testing"

	"stitchvault/internal/ingest/pyreader"
)

// newReaderOrSkip builds a pyreader, skipping the test when Python/pyembroidery
// is not available so CI without the dependency stays green.
func newReaderOrSkip(t *testing.T) *pyreader.Reader {
	t.Helper()
	r, err := pyreader.New()
	if err != nil {
		t.Skipf("pyreader unavailable: %v", err)
	}
	if err := r.Check(context.Background()); err != nil {
		t.Skipf("pyembroidery not installed (install it to run): %v", err)
	}
	return r
}

// TestReadPES checks the colourful format: units, counts, bounding box, palette.
func TestReadPES(t *testing.T) {
	r := newReaderOrSkip(t)

	d, err := r.Read(context.Background(), "testdata/sample.pes")
	if err != nil {
		t.Fatalf("Read sample.pes: %v", err)
	}

	if d.Format != ".pes" {
		t.Errorf("Format = %q, want .pes", d.Format)
	}
	if got := d.StitchCount(); got < 8 {
		t.Errorf("StitchCount = %d, want >= 8", got)
	}
	if got := d.ColorChanges(); got < 1 {
		t.Errorf("ColorChanges = %d, want >= 1", got)
	}
	if got := len(d.Palette()); got != 2 {
		t.Errorf("Palette = %d threads, want 2", got)
	}

	// The fixture is a 10mm square (x:0..10) plus a triangle out to x=30, y:0..8.
	w, h := d.SizeMM()
	if w < 28 || w > 32 {
		t.Errorf("width = %.1f mm, want ~30", w)
	}
	if h < 8 || h > 12 {
		t.Errorf("height = %.1f mm, want ~10", h)
	}
}

// TestReadDST checks the colourless format: stitches present, empty palette, but
// the renderer-facing Blocks() still yields blocks (with default colours).
func TestReadDST(t *testing.T) {
	r := newReaderOrSkip(t)

	d, err := r.Read(context.Background(), "testdata/sample.dst")
	if err != nil {
		t.Fatalf("Read sample.dst: %v", err)
	}

	if d.Format != ".dst" {
		t.Errorf("Format = %q, want .dst", d.Format)
	}
	if got := d.StitchCount(); got < 8 {
		t.Errorf("StitchCount = %d, want >= 8", got)
	}
	if got := len(d.Palette()); got != 0 {
		t.Errorf("Palette = %d threads, want 0 (DST carries no colour)", got)
	}
	if got := len(d.Blocks()); got < 1 {
		t.Errorf("Blocks = %d, want >= 1", got)
	}
}

// TestReadMissingFile surfaces a clear error (and does not hang) for a bad path.
func TestReadMissingFile(t *testing.T) {
	r := newReaderOrSkip(t)

	if _, err := r.Read(context.Background(), "testdata/does-not-exist.pes"); err == nil {
		t.Fatal("Read of missing file: want error, got nil")
	}
}
