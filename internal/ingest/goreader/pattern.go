package goreader

import (
	"context"

	"stitchvault/internal/embroidery"
)

// builder accumulates a stitch stream exactly the way pyembroidery's EmbPattern
// does: positions are tracked in the file's native units (1/10 mm for every format
// here) and each operation is relative to the previous absolute position unless an
// *_abs variant is used. The semantic method names (stitch/move/color_change/…)
// match EmbPattern so the ported readers read one-to-one against the reference.
//
// toDesign divides by 10 to reach the millimetres the embroidery.Design contract
// requires — the single unit conversion, identical to reader.py's `x / 10.0`.
type builder struct {
	pts     []rawPoint
	px, py  float64 // previous absolute position, native units
	threads []*embroidery.Thread
	extras  map[string]string

	ctx context.Context
	n   int // operation counter for periodic cancellation checks
}

type rawPoint struct {
	x, y float64
	cmd  embroidery.Command
}

func newBuilder(ctx context.Context) *builder {
	return &builder{ctx: ctx, extras: map[string]string{}}
}

// canceled reports whether the context was cancelled, checked cheaply (only every
// 16384 operations) so a batch import can abort mid-file without slowing the hot
// decode loop. Readers test it at the top of their stitch loop.
func (b *builder) canceled() bool {
	b.n++
	if b.n&0x3FFF != 0 {
		return false
	}
	return b.ctx != nil && b.ctx.Err() != nil
}

func (b *builder) addAbs(cmd embroidery.Command, x, y float64) {
	b.pts = append(b.pts, rawPoint{x: x, y: y, cmd: cmd})
	b.px, b.py = x, y
}

func (b *builder) addRel(cmd embroidery.Command, dx, dy float64) {
	b.addAbs(cmd, b.px+dx, b.py+dy)
}

// Stitch-stream operations (EmbPattern equivalents).

func (b *builder) stitch(dx, dy float64)      { b.addRel(embroidery.Stitch, dx, dy) }
func (b *builder) move(dx, dy float64)        { b.addRel(embroidery.Jump, dx, dy) }
func (b *builder) moveAbs(x, y float64)       { b.addAbs(embroidery.Jump, x, y) }
func (b *builder) trim()                      { b.addRel(embroidery.Trim, 0, 0) }
func (b *builder) stop(dx, dy float64)        { b.addRel(embroidery.Stop, dx, dy) }
func (b *builder) end()                       { b.addRel(embroidery.End, 0, 0) }
func (b *builder) colorChange(dx, dy float64) { b.addRel(embroidery.ColorChange, dx, dy) }
func (b *builder) sequinEject(dx, dy float64) { b.addRel(embroidery.SequinEject, dx, dy) }

// needleChange maps to a colour change: pyembroidery encodes it as NEEDLE_SET, and
// reader.py mapped NEEDLE_SET → ColorChange. Used by U01.
func (b *builder) needleChange() { b.addRel(embroidery.ColorChange, 0, 0) }

// sequinMode / fast / slow are mode/speed markers (SEQUIN_MODE, FAST, SLOW) that
// carry no command in StitchVault's 7-command model — reader.py mapped them to a
// no-op stitch at the current position. We drop them entirely (cleaner; they never
// appear in a real catalogue's common-format files and are not exercised by the
// fixtures). Kept as named methods so the ported readers stay literal.
func (b *builder) sequinMode(dx, dy float64) {}
func (b *builder) fast()                     {}
func (b *builder) slow()                     {}

func (b *builder) addThread(t embroidery.Thread) { b.threads = append(b.threads, &t) }
func (b *builder) addNilThread()                 { b.threads = append(b.threads, nil) }

func (b *builder) meta(k, v string) {
	if v != "" {
		b.extras[k] = v
	}
}

// interpolateDuplicateColorAsStop ports EmbPattern.interpolate_duplicate_color_as_stop,
// which PEC and PES apply after reading. When a colour block uses the same thread as
// the block before it, the redundant ColorChange becomes a Stop and the duplicate
// palette entry is removed — so colour-count and palette size match the machine's
// view (and the old pyembroidery pipeline). A no-op on designs whose blocks all use
// distinct threads (the common case).
func (b *builder) interpolateDuplicateColorAsStop() {
	threadIdx := 0
	initColor := true
	lastChange := -1
	for pos := range b.pts {
		switch b.pts[pos].cmd {
		case embroidery.Stitch:
			if initColor {
				if lastChange >= 0 && threadIdx != 0 {
					if threadIdx >= len(b.threads) {
						return // threadlist[threadIdx] would IndexError → abort
					}
					if threadEqual(b.threads[threadIdx-1], b.threads[threadIdx]) {
						// duplicate of the previous colour: drop it, turn the
						// triggering colour change into a stop.
						b.threads = append(b.threads[:threadIdx], b.threads[threadIdx+1:]...)
						b.pts[lastChange].cmd = embroidery.Stop
					} else {
						threadIdx++
					}
				} else {
					threadIdx++
				}
				initColor = false
			}
		case embroidery.ColorChange:
			initColor = true
			lastChange = pos
		}
	}
}

// threadEqual compares two palette entries the way EmbThread.__eq__ does (colour +
// all descriptive fields). nil slots are never equal (JEF only, not used here).
func threadEqual(a, b *embroidery.Thread) bool {
	if a == nil || b == nil {
		return false
	}
	return a.R == b.R && a.G == b.G && a.B == b.B &&
		a.Description == b.Description && a.Catalog == b.Catalog && a.Brand == b.Brand
}

// toDesign converts the accumulated native-unit stream into the millimetre,
// Y-down embroidery.Design. nil thread slots (JEF colour-0 placeholders) are
// dropped from the palette.
func (b *builder) toDesign(format string) *embroidery.Design {
	d := &embroidery.Design{
		Format:   format,
		Extras:   b.extras,
		Stitches: make([]embroidery.Point, len(b.pts)),
		Threads:  make([]embroidery.Thread, 0, len(b.threads)),
	}
	for i, p := range b.pts {
		d.Stitches[i] = embroidery.Point{X: p.x / 10.0, Y: p.y / 10.0, Cmd: p.cmd}
	}
	for _, t := range b.threads {
		if t == nil {
			continue
		}
		d.Threads = append(d.Threads, *t)
	}
	if len(d.Extras) == 0 {
		d.Extras = nil
	}
	return d
}
