package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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

// TestRotatePNG checks the quadrant rotations swap dimensions correctly and move a
// corner marker to the expected place. A 4×2 image with a red pixel at the
// top-left (0,0) is rotated clockwise; the marker must land at the rotated corner.
func TestRotatePNG(t *testing.T) {
	const w, h = 4, 2
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	src.Set(0, 0, color.RGBA{R: 255, A: 255}) // marker: top-left, pure red

	var srcBuf bytes.Buffer
	if err := png.Encode(&srcBuf, src); err != nil {
		t.Fatalf("encode src: %v", err)
	}

	isRed := func(c color.Color) bool {
		r, g, b, _ := c.RGBA()
		return r>>8 > 200 && g>>8 < 60 && b>>8 < 60
	}

	cases := []struct {
		deg   int
		wantW int
		wantH int
		markX int
		markY int
	}{
		{0, w, h, 0, 0},           // unchanged: top-left
		{90, h, w, h - 1, 0},      // top-left -> top-right
		{180, w, h, w - 1, h - 1}, // -> bottom-right
		{270, h, w, 0, w - 1},     // -> bottom-left
	}
	for _, tc := range cases {
		out, err := RotatePNG(srcBuf.Bytes(), tc.deg)
		if err != nil {
			t.Fatalf("RotatePNG(%d): %v", tc.deg, err)
		}
		img, _, err := image.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("decode rotated %d: %v", tc.deg, err)
		}
		b := img.Bounds()
		if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
			t.Errorf("deg %d: dims = %dx%d, want %dx%d", tc.deg, b.Dx(), b.Dy(), tc.wantW, tc.wantH)
		}
		if !isRed(img.At(b.Min.X+tc.markX, b.Min.Y+tc.markY)) {
			t.Errorf("deg %d: marker not at (%d,%d)", tc.deg, tc.markX, tc.markY)
		}
	}
}

// TestZoomCapKeepsTinyDesignSmall verifies the zoom cap: a tiny design must not be
// blown up to fill the canvas (the reported "3mm point becomes a tangle" bug),
// while a normal-sized design still fills it.
func TestZoomCapKeepsTinyDesignSmall(t *testing.T) {
	square := func(mm float64) *embroidery.Design {
		return &embroidery.Design{
			Format:  ".pes",
			Threads: []embroidery.Thread{{R: 220}},
			Stitches: []embroidery.Point{
				{X: 0, Y: 0, Cmd: embroidery.Stitch},
				{X: mm, Y: 0, Cmd: embroidery.Stitch},
				{X: mm, Y: mm, Cmd: embroidery.Stitch},
				{X: 0, Y: mm, Cmd: embroidery.Stitch},
				{X: 0, Y: 0, Cmd: embroidery.End},
			},
		}
	}

	const size = 256
	span := func(d *embroidery.Design) int {
		out := filepath.Join(t.TempDir(), "z.png")
		if err := New().Thumbnail(d, out, size); err != nil {
			t.Fatalf("Thumbnail: %v", err)
		}
		f, err := os.Open(out)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		b := img.Bounds()
		minX, maxX := b.Max.X, b.Min.X
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				if abs16(r, 250) > 6000 || abs16(g, 250) > 6000 || abs16(bl, 248) > 6000 {
					if x < minX {
						minX = x
					}
					if x > maxX {
						maxX = x
					}
				}
			}
		}
		if maxX < minX {
			return 0
		}
		return maxX - minX
	}

	tiny := span(square(5))   // 5mm — well under minFillMM, must stay small
	large := span(square(80)) // 80mm — must fill the canvas
	if tiny >= size/2 {
		t.Errorf("tiny design span = %dpx in a %dpx canvas — zoom not capped", tiny, size)
	}
	if large <= size/2 {
		t.Errorf("large design span = %dpx — should fill the canvas", large)
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
