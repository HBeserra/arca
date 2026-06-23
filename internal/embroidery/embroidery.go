// Package embroidery defines a neutral, library-independent domain model for an
// embroidery design. It is the contract the Reader must produce — today the native
// Go reader in internal/ingest/goreader — so that everything downstream (render,
// metadata, classification) is written once against this type.
//
// Canonical conventions the Reader MUST respect:
//   - Coordinates are in millimetres (float64). Embroidery formats store tenths of a
//     millimetre, so the reader divides by 10.
//   - The Y axis points DOWN (screen / image convention). This matches the raster
//     produced by the renderer, so no axis flip happens anywhere in Go. Each format
//     decoder negates Y at the point it reads a delta (mirroring the reference), so
//     a vertically-mirrored sample is fixed there.
package embroidery

// Command is a single stitch operation. The integer values are the canonical
// StitchVault set; the native reader maps each embroidery format's command set onto
// these (see internal/ingest/goreader).
type Command uint8

const (
	Stitch      Command = iota // normal stitch to (X,Y) — the thread is down
	Jump                       // move to (X,Y) without stitching (thread up)
	ColorChange                // change thread/needle; advances the palette index
	Trim                       // cut the thread (thread up)
	Stop                       // machine stop (e.g. for appliqué placement)
	End                        // end of the design
	SequinEject                // eject a sequin at (X,Y)
)

// Point is a single coordinate plus the command that produced it. Folding the
// command into the point (rather than a parallel slice) keeps the stitch stream a
// single ordered list, which is how every embroidery format stores it.
type Point struct {
	X, Y float64 // millimetres, Y-down (see package doc)
	Cmd  Command
}

// Thread is one entry of the design's colour palette. Threads is optional in the
// domain model: PES files carry colours, DST (Tajima) files do not — so a reader
// may legitimately produce an empty palette.
type Thread struct {
	R, G, B     uint8
	Description string // human label, e.g. "Madeira Rayon 1147"
	Catalog     string // catalogue number; "" if unknown
	Brand       string // thread brand; "" if unknown
}

// Design is the neutral domain model. Derived facts (size, counts, render blocks)
// are computed here in pure Go — never asked of the LLM and never part of the
// Reader interface.
type Design struct {
	Stitches []Point
	Threads  []Thread          // may be empty — DST carries no colours
	Format   string            // normalized lowercase extension, e.g. ".pes"
	Extras   map[string]string // format-specific metadata not yet modelled (hoop, label…)
}

// Bounds returns the axis-aligned bounding box of the needle path, considering
// only Stitch and Jump points (the commands that carry meaningful coordinates).
// The second return reports whether any such point existed.
func (d *Design) Bounds() (min, max Point, ok bool) {
	for _, p := range d.Stitches {
		if p.Cmd != Stitch && p.Cmd != Jump {
			continue
		}
		if !ok {
			min, max, ok = p, p, true
			continue
		}
		if p.X < min.X {
			min.X = p.X
		}
		if p.Y < min.Y {
			min.Y = p.Y
		}
		if p.X > max.X {
			max.X = p.X
		}
		if p.Y > max.Y {
			max.Y = p.Y
		}
	}
	return min, max, ok
}

// SizeMM returns the design's width and height in millimetres (0,0 if empty).
func (d *Design) SizeMM() (w, h float64) {
	min, max, ok := d.Bounds()
	if !ok {
		return 0, 0
	}
	return max.X - min.X, max.Y - min.Y
}

// StitchCount returns the number of actual stitches (Stitch commands only),
// matching what machines and pattern sites report.
func (d *Design) StitchCount() int {
	n := 0
	for _, p := range d.Stitches {
		if p.Cmd == Stitch {
			n++
		}
	}
	return n
}

// ColorChanges returns the number of ColorChange commands in the stream.
func (d *Design) ColorChanges() int {
	n := 0
	for _, p := range d.Stitches {
		if p.Cmd == ColorChange {
			n++
		}
	}
	return n
}

// ColorCount returns the number of colour blocks: one implicit first block plus
// one per ColorChange. Returns 0 for an empty design.
func (d *Design) ColorCount() int {
	if d.StitchCount() == 0 {
		return 0
	}
	return d.ColorChanges() + 1
}

// Palette returns the real thread palette read from the file. It may be empty
// (DST and other colourless formats). It is intentionally NOT synthesized here so
// that stored metadata stays honest about what the file actually contained; the
// renderer supplies fallback colours for display (see Block.Thread).
func (d *Design) Palette() []Thread {
	return d.Threads
}

// Block is one colour section of the design: a single thread plus the polylines
// stitched with it. A polyline is a contiguous run of Stitch points; it is broken
// whenever the thread lifts (Jump or Trim). This is what the renderer consumes.
type Block struct {
	Thread    Thread    // real thread when known, otherwise a default display colour
	ThreadSet bool      // true when Thread came from the file palette
	Polylines [][]Point // each inner slice is a connected run of stitches
}

// Blocks walks the stitch stream and groups it into colour blocks for rendering.
// The palette index advances on every ColorChange; within a block, Jump and Trim
// break the current polyline. When the file carries no colour for a block, a
// default display colour is assigned (cycling through defaultPalette) so the
// thumbnail is always visible.
func (d *Design) Blocks() []Block {
	var blocks []Block
	threadIdx := 0

	// current accumulating state
	var cur Block
	var line []Point
	started := false

	flushLine := func() {
		if len(line) > 1 { // a single point draws nothing
			cur.Polylines = append(cur.Polylines, line)
		}
		line = nil
	}
	flushBlock := func() {
		flushLine()
		if started {
			blocks = append(blocks, cur)
		}
	}
	beginBlock := func(idx int) {
		cur = Block{Thread: d.threadAt(idx)}
		cur.ThreadSet = idx < len(d.Threads)
		line = nil
		started = true
	}

	beginBlock(threadIdx)

	for _, p := range d.Stitches {
		switch p.Cmd {
		case Stitch:
			line = append(line, p)
		case Jump, Trim:
			flushLine()
		case ColorChange:
			flushBlock()
			threadIdx++
			beginBlock(threadIdx)
		case Stop:
			flushLine()
		case End:
			flushBlock()
			started = false
		}
	}
	flushBlock()

	return blocks
}

// threadAt returns the file thread at idx, or a default display colour cycled from
// defaultPalette when the palette is missing or too short.
func (d *Design) threadAt(idx int) Thread {
	if idx >= 0 && idx < len(d.Threads) {
		return d.Threads[idx]
	}
	return defaultPalette[((idx%len(defaultPalette))+len(defaultPalette))%len(defaultPalette)]
}

// defaultPalette is a small set of visually distinct colours used to render blocks
// for formats (DST, etc.) that carry no thread information. Deriving real colours
// for such formats is a separate, deferred concern (see project spec §10).
var defaultPalette = []Thread{
	{R: 0x1f, G: 0x77, B: 0xb4, Description: "default-1"},
	{R: 0xff, G: 0x7f, B: 0x0e, Description: "default-2"},
	{R: 0x2c, G: 0xa0, B: 0x2c, Description: "default-3"},
	{R: 0xd6, G: 0x27, B: 0x28, Description: "default-4"},
	{R: 0x94, G: 0x67, B: 0xbd, Description: "default-5"},
	{R: 0x8c, G: 0x56, B: 0x4b, Description: "default-6"},
	{R: 0xe3, G: 0x77, B: 0xc2, Description: "default-7"},
	{R: 0x7f, G: 0x7f, B: 0x7f, Description: "default-8"},
}
