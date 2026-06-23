package goreader

import "stitchvault/internal/embroidery"

// Thread-palette resolution for the Brother PEC/PES colour model. The colour tables
// themselves (brotherThreads, janomeJefThreads, janomeSewThreads) are generated
// verbatim from pyembroidery's charts in threads_gen.go.
//
// PEC stores one byte per colour block; that byte indexes the Brother chart. PES
// may instead carry its own thread list (v5+), in which case map_pec_colors selects
// between three strategies — all ported here from PecReader.map_pec_colors.

// mapPECColors adds the block palette to b. chart is the PES-supplied thread list
// (nil/empty for a bare PEC, where colours come straight from the Brother chart).
func mapPECColors(colorbytes []byte, b *builder, chart []embroidery.Thread) {
	switch {
	case len(chart) == 0:
		// Bare PEC: each colour byte indexes the Brother chart.
		processPECColors(colorbytes, b)
	case len(chart) >= len(colorbytes):
		// 1:1 — the PES thread list is authoritative; use it whole.
		for _, t := range chart {
			b.addThread(t)
		}
	default:
		// Tabled mode: map distinct chart-index → next PES thread.
		processPECTable(colorbytes, b, chart)
	}
}

func processPECColors(colorbytes []byte, b *builder) {
	maxv := len(brotherThreads)
	for _, by := range colorbytes {
		b.addThread(brotherThreads[int(by)%maxv])
	}
}

func processPECTable(colorbytes []byte, b *builder, chart []embroidery.Thread) {
	maxv := len(brotherThreads)
	seen := map[int]embroidery.Thread{}
	chartPos := 0
	for _, by := range colorbytes {
		ci := int(by) % maxv
		t, ok := seen[ci]
		if !ok {
			if chartPos < len(chart) {
				t = chart[chartPos]
				chartPos++
			} else {
				t = brotherThreads[ci]
			}
			seen[ci] = t
		}
		b.addThread(t)
	}
}

// janomeThreadAt returns the chart entry at index (mod len), the index resolution
// JEF and SEW both use.
func janomeThreadAt(chart []embroidery.Thread, index int) embroidery.Thread {
	n := len(chart)
	return chart[((index%n)+n)%n]
}
