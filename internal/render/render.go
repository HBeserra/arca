// Package render rasterizes an embroidery.Design into a PNG thumbnail entirely in
// Go. Rendering is deliberately never delegated to Python: the only thing a future
// native parser must replace is the parse step, not the drawing.
package render

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"

	"github.com/fogleman/gg"

	"stitchvault/internal/embroidery"
)

// Defaults for the renderer.
const (
	DefaultSize = 512 // thumbnail edge in pixels (square)
	padding     = 12  // px of empty margin around the design
)

// background is a soft off-white "paper" so light threads remain visible.
var background = color.RGBA{R: 0xFA, G: 0xFA, B: 0xF8, A: 0xFF}

// Renderer turns a Design into a thumbnail file. Kept as an interface so a future
// GPU or alternative renderer can be swapped in, and so it is easy to fake in tests.
type Renderer interface {
	// Thumbnail renders d into a size×size PNG at outPath, creating parent dirs.
	Thumbnail(d *embroidery.Design, outPath string, size int) error
}

// New returns the default polyline renderer.
func New() Renderer { return &renderer{} }

type renderer struct{}

func (r *renderer) Thumbnail(d *embroidery.Design, outPath string, size int) error {
	if size <= 0 {
		size = DefaultSize
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("render: mkdir: %w", err)
	}

	dc := gg.NewContext(size, size)
	dc.SetColor(background)
	dc.Clear()

	min, max, ok := d.Bounds()
	if ok {
		drawDesign(dc, d, min, max, size)
	}

	if err := dc.SavePNG(outPath); err != nil {
		return fmt.Errorf("render: save %s: %w", outPath, err)
	}
	return nil
}

// drawDesign maps design millimetres into the canvas (centred, aspect-preserving)
// and strokes one polyline per run, coloured by its block's thread. The design's
// Y axis is already down (image convention), so there is no axis flip here.
func drawDesign(dc *gg.Context, d *embroidery.Design, min, max embroidery.Point, size int) {
	w := max.X - min.X
	h := max.Y - min.Y
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}

	avail := float64(size - 2*padding)
	scale := avail / w
	if s := avail / h; s < scale {
		scale = s
	}

	// Centre the scaled design on the square canvas.
	originX := (float64(size) - w*scale) / 2
	originY := (float64(size) - h*scale) / 2

	project := func(p embroidery.Point) (float64, float64) {
		return originX + (p.X-min.X)*scale, originY + (p.Y-min.Y)*scale
	}

	lineWidth := float64(size) / 400
	if lineWidth < 1 {
		lineWidth = 1
	}
	dc.SetLineWidth(lineWidth)
	dc.SetLineCapRound()

	for _, block := range d.Blocks() {
		t := block.Thread
		dc.SetRGB(float64(t.R)/255, float64(t.G)/255, float64(t.B)/255)

		for _, line := range block.Polylines {
			if len(line) < 2 {
				continue
			}
			x0, y0 := project(line[0])
			dc.MoveTo(x0, y0)
			for _, p := range line[1:] {
				x, y := project(p)
				dc.LineTo(x, y)
			}
			dc.Stroke()
		}
	}
}
