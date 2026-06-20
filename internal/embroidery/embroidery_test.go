package embroidery

import "testing"

// squareThenTriangle builds a two-colour design: a closed square stitched in
// thread 0, a ColorChange, then a triangle stitched in thread 1, with a Jump in
// the middle of the triangle to exercise polyline splitting.
func squareThenTriangle() *Design {
	return &Design{
		Format:  ".pes",
		Threads: []Thread{{R: 255}, {G: 255}},
		Stitches: []Point{
			{X: 0, Y: 0, Cmd: Stitch},
			{X: 10, Y: 0, Cmd: Stitch},
			{X: 10, Y: 10, Cmd: Stitch},
			{X: 0, Y: 10, Cmd: Stitch},
			{X: 0, Y: 0, Cmd: Stitch},
			{X: 0, Y: 0, Cmd: ColorChange},
			{X: 20, Y: 0, Cmd: Stitch},
			{X: 30, Y: 0, Cmd: Stitch},
			{X: 25, Y: 8, Cmd: Stitch},
			{X: 40, Y: 0, Cmd: Jump}, // lifts the thread: splits the polyline
			{X: 50, Y: 0, Cmd: Stitch},
			{X: 55, Y: 8, Cmd: Stitch},
			{X: 0, Y: 0, Cmd: End},
		},
	}
}

func TestDerivedMetadata(t *testing.T) {
	d := squareThenTriangle()

	if got := d.StitchCount(); got != 10 {
		t.Errorf("StitchCount = %d, want 10", got)
	}
	if got := d.ColorChanges(); got != 1 {
		t.Errorf("ColorChanges = %d, want 1", got)
	}
	if got := d.ColorCount(); got != 2 {
		t.Errorf("ColorCount = %d, want 2", got)
	}

	w, h := d.SizeMM()
	if w != 55 || h != 10 {
		t.Errorf("SizeMM = (%g, %g), want (55, 10)", w, h)
	}
}

func TestBlocks(t *testing.T) {
	d := squareThenTriangle()
	blocks := d.Blocks()

	if len(blocks) != 2 {
		t.Fatalf("Blocks len = %d, want 2", len(blocks))
	}

	// Block 0: the square — one polyline of 5 points, thread 0 (red).
	if len(blocks[0].Polylines) != 1 {
		t.Errorf("block0 polylines = %d, want 1", len(blocks[0].Polylines))
	}
	if !blocks[0].ThreadSet || blocks[0].Thread.R != 255 {
		t.Errorf("block0 thread = %+v (set=%v), want red from palette", blocks[0].Thread, blocks[0].ThreadSet)
	}

	// Block 1: the triangle — Jump splits it into two polylines, thread 1 (green).
	if len(blocks[1].Polylines) != 2 {
		t.Errorf("block1 polylines = %d, want 2 (Jump splits)", len(blocks[1].Polylines))
	}
	if !blocks[1].ThreadSet || blocks[1].Thread.G != 255 {
		t.Errorf("block1 thread = %+v (set=%v), want green from palette", blocks[1].Thread, blocks[1].ThreadSet)
	}
}

func TestEmptyPaletteFallsBackToDefaults(t *testing.T) {
	// DST-like: stitches but no threads.
	d := &Design{
		Format: ".dst",
		Stitches: []Point{
			{X: 0, Y: 0, Cmd: Stitch},
			{X: 10, Y: 10, Cmd: Stitch},
			{X: 0, Y: 0, Cmd: ColorChange},
			{X: 5, Y: 5, Cmd: Stitch},
			{X: 8, Y: 2, Cmd: Stitch},
		},
	}

	if len(d.Palette()) != 0 {
		t.Errorf("Palette = %d threads, want 0 (DST carries no colours)", len(d.Palette()))
	}

	blocks := d.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("Blocks len = %d, want 2", len(blocks))
	}
	for i, b := range blocks {
		if b.ThreadSet {
			t.Errorf("block%d ThreadSet = true, want false (no file palette)", i)
		}
		// A default display colour must still be assigned so the thumbnail renders.
		if b.Thread == (Thread{}) {
			t.Errorf("block%d has zero thread, want a default display colour", i)
		}
	}
	// Distinct default colours per block.
	if blocks[0].Thread == blocks[1].Thread {
		t.Errorf("default colours not distinct: both %+v", blocks[0].Thread)
	}
}
