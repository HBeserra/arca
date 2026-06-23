package goreader

import (
	"context"
	"testing"
)

// TestReadPES checks the colourful Brother format on a real sample: units, counts,
// bounding box and palette. Mirrors the assertions the old pyreader test made, now
// satisfied by the native reader.
func TestReadPES(t *testing.T) {
	d, err := New().Read(context.Background(), "testdata/sample.pes")
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
	// The fixture is a 10mm square plus a triangle out to x=30, y:0..8.
	w, h := d.SizeMM()
	if w < 28 || w > 32 {
		t.Errorf("width = %.1f mm, want ~30", w)
	}
	if h < 8 || h > 12 {
		t.Errorf("height = %.1f mm, want ~10", h)
	}
}

// TestReadDST checks the colourless Tajima format: stitches present, empty palette,
// but Blocks() still yields renderable blocks (with default colours).
func TestReadDST(t *testing.T) {
	d, err := New().Read(context.Background(), "testdata/sample.dst")
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
	if _, err := New().Read(context.Background(), "testdata/does-not-exist.pes"); err == nil {
		t.Fatal("Read of missing file: want error, got nil")
	}
}

// TestReadCanceledContext verifies a cancelled context aborts before any work.
func TestReadCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Read(ctx, "testdata/sample.dst"); err == nil {
		t.Fatal("Read with cancelled context: want error, got nil")
	}
}
