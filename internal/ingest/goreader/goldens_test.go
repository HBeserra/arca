package goreader

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The wire shape emitted by testdata/gen.py (and historically by reader.py): the
// oracle the native reader is checked against.
type wireThread struct {
	R           int    `json:"r"`
	G           int    `json:"g"`
	B           int    `json:"b"`
	Description string `json:"description"`
	Brand       string `json:"brand"`
	Catalog     string `json:"catalog"`
}

type wireDesign struct {
	Format   string       `json:"format"`
	Stitches [][3]float64 `json:"stitches"`
	Threads  []wireThread `json:"threads"`
}

func (w wireDesign) count(cmd int) int {
	n := 0
	for _, s := range w.Stitches {
		if int(s[2]) == cmd {
			n++
		}
	}
	return n
}

// bounds mirrors embroidery.Bounds: only Stitch(0) and Jump(1) points count.
func (w wireDesign) bounds() (minX, minY, maxX, maxY float64, ok bool) {
	for _, s := range w.Stitches {
		if c := int(s[2]); c != 0 && c != 1 {
			continue
		}
		if !ok {
			minX, maxX, minY, maxY, ok = s[0], s[0], s[1], s[1], true
			continue
		}
		minX, maxX = math.Min(minX, s[0]), math.Max(maxX, s[0])
		minY, maxY = math.Min(minY, s[1]), math.Max(maxY, s[1])
	}
	return
}

// TestGoldenFixtures is the differential test pinning the native reader to
// pyembroidery. For every format pyembroidery can write, reading the same bytes must
// reproduce pyembroidery's own read on the metrics StitchVault relies on: stitch
// count, colour-change count, palette (RGB + names), and bounding box. The exact
// placement of TRIMs and zero-length JUMP clipping introduced by pyembroidery's
// post-read passes is intentionally NOT compared — it leaves all of those metrics
// unchanged. Regenerate fixtures with testdata/gen.py (dev venv).
func TestGoldenFixtures(t *testing.T) {
	goldens, err := filepath.Glob("testdata/canonical.*.golden.json")
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no golden fixtures (run testdata/gen.py): %v", err)
	}
	r := New()
	for _, gp := range goldens {
		fixture := strings.TrimSuffix(gp, ".golden.json")
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			raw, err := os.ReadFile(gp)
			if err != nil {
				t.Fatal(err)
			}
			var want wireDesign
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("golden parse: %v", err)
			}

			d, err := r.Read(context.Background(), fixture)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}

			if got, w := d.StitchCount(), want.count(0); got != w {
				t.Errorf("StitchCount = %d, want %d", got, w)
			}
			if got, w := d.ColorChanges(), want.count(2); got != w {
				t.Errorf("ColorChanges = %d, want %d", got, w)
			}

			if got, w := len(d.Threads), len(want.Threads); got != w {
				t.Fatalf("palette size = %d, want %d", got, w)
			}
			for i, wt := range want.Threads {
				gt := d.Threads[i]
				if int(gt.R) != wt.R || int(gt.G) != wt.G || int(gt.B) != wt.B {
					t.Errorf("thread[%d] rgb = (%d,%d,%d), want (%d,%d,%d)",
						i, gt.R, gt.G, gt.B, wt.R, wt.G, wt.B)
				}
				if gt.Description != wt.Description || gt.Brand != wt.Brand || gt.Catalog != wt.Catalog {
					t.Errorf("thread[%d] meta = (%q,%q,%q), want (%q,%q,%q)",
						i, gt.Description, gt.Brand, gt.Catalog, wt.Description, wt.Brand, wt.Catalog)
				}
			}

			wMinX, wMinY, wMaxX, wMaxY, wok := want.bounds()
			mn, mx, ok := d.Bounds()
			if ok != wok {
				t.Fatalf("bounds present = %v, want %v", ok, wok)
			}
			const eps = 0.05
			if math.Abs(mn.X-wMinX) > eps || math.Abs(mn.Y-wMinY) > eps ||
				math.Abs(mx.X-wMaxX) > eps || math.Abs(mx.Y-wMaxY) > eps {
				t.Errorf("bounds = [(%.2f,%.2f)..(%.2f,%.2f)], want [(%.2f,%.2f)..(%.2f,%.2f)]",
					mn.X, mn.Y, mx.X, mx.Y, wMinX, wMinY, wMaxX, wMaxY)
			}
		})
	}
}
