package render

import (
	"image"
	_ "image/png" // register PNG decoder for image.Decode
	"os"
	"path/filepath"
	"testing"

	"stitchvault/internal/embroidery"
)

func synthDesign() *embroidery.Design {
	return &embroidery.Design{
		Format:  ".pes",
		Threads: []embroidery.Thread{{R: 220}, {G: 180}},
		Stitches: []embroidery.Point{
			{X: 0, Y: 0, Cmd: embroidery.Stitch},
			{X: 20, Y: 0, Cmd: embroidery.Stitch},
			{X: 20, Y: 20, Cmd: embroidery.Stitch},
			{X: 0, Y: 20, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.ColorChange},
			{X: 30, Y: 0, Cmd: embroidery.Stitch},
			{X: 50, Y: 0, Cmd: embroidery.Stitch},
			{X: 40, Y: 18, Cmd: embroidery.Stitch},
			{X: 30, Y: 0, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.End},
		},
	}
}

func TestThumbnailProducesNonBlankPNG(t *testing.T) {
	out := filepath.Join(t.TempDir(), "thumb.png")

	const size = 256
	if err := New().Thumbnail(synthDesign(), out, size); err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open png: %v", err)
	}
	defer f.Close()

	img, format, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want png", format)
	}

	b := img.Bounds()
	if b.Dx() != size || b.Dy() != size {
		t.Errorf("dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), size, size)
	}

	// Non-blank: at least some pixels must differ markedly from the paper bg.
	nonBg := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			// RGBA returns 16-bit; background is ~ (250,250,248)<<8.
			if abs16(r, 250) > 6000 || abs16(g, 250) > 6000 || abs16(bl, 248) > 6000 {
				nonBg++
			}
		}
	}
	if nonBg == 0 {
		t.Error("rendered thumbnail is blank (no stitches drawn)")
	}
}

// TestThumbnailEmptyDesign ensures a degenerate design still yields a valid PNG.
func TestThumbnailEmptyDesign(t *testing.T) {
	out := filepath.Join(t.TempDir(), "empty.png")
	if err := New().Thumbnail(&embroidery.Design{Format: ".dst"}, out, 128); err != nil {
		t.Fatalf("Thumbnail(empty): %v", err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Fatalf("empty thumbnail not written: err=%v", err)
	}
}

// abs16 returns |a-b8<<8| comparing a 16-bit channel against an 8-bit reference.
func abs16(a uint32, ref8 uint32) uint32 {
	want := ref8 << 8
	if a > want {
		return a - want
	}
	return want - a
}
