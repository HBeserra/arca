package goreader

import (
	"fmt"
	"strings"

	"stitchvault/internal/embroidery"
)

// Brother PEC — the colour + stitch block also embedded inside PES. Port of
// pyembroidery PecReader.py. Colours are byte indices into the Brother thread chart
// (brotherThreads); stitches use a 7-bit short / 12-bit long delta encoding with
// jump and trim flag bits.

const (
	pecJumpCode = 0x10
	pecTrimCode = 0x20
	pecFlagLong = 0x80
)

// readPECFile reads a standalone .pec file (header "#PEC0001").
func readPECFile(c *cursor, b *builder) error {
	magic := c.string8(8)
	if !strings.HasPrefix(magic, "#PEC") {
		return fmt.Errorf("not a valid PEC file (bad header — likely encrypted or corrupt)")
	}
	readPEC(c, b, nil)
	b.interpolateDuplicateColorAsStop()
	return nil
}

// readPEC parses the PEC block at the cursor's current position. chart is the
// PES-supplied thread list (nil for a bare PEC). It mirrors PecReader.read_pec but
// skips the trailing embedded thumbnail graphics (unused by StitchVault).
func readPEC(c *cursor, b *builder, chart []embroidery.Thread) {
	c.skip(3) // "LA:"
	if label := strings.TrimSpace(c.string8(16)); label != "" {
		b.meta("Name", label)
	}
	c.skip(0xF) // spaces then 0xFF 0x00
	c.u8()      // pec_graphic_byte_stride (thumbnail; unused)
	c.u8()      // pec_graphic_icon_height (thumbnail; unused)
	c.skip(0xC)
	colorChanges, ok := c.u8()
	if !ok {
		return
	}
	countColors := colorChanges + 1 // PEC stores cc-1; 0xFF means 0
	colorbytes := c.read(countColors)
	mapPECColors(colorbytes, b, chart)
	c.skip(0x1D0 - colorChanges)
	c.u24le() // stitch block length (already 5 into the block; value unused)
	c.skip(0x0B)
	readPECStitches(c, b)
}

func readPECStitches(c *cursor, b *builder) {
	for {
		if b.canceled() {
			return
		}
		val1, _ := c.u8()
		val2, ok2 := c.u8()
		if !ok2 || (val1 == 0xFF && val2 == 0x00) {
			break
		}
		if val1 == 0xFE && val2 == 0xB0 {
			c.skip(1)
			b.colorChange(0, 0)
			continue
		}
		jump, trim := false, false

		var x int
		if val1&pecFlagLong != 0 {
			if val1&pecTrimCode != 0 {
				trim = true
			}
			if val1&pecJumpCode != 0 {
				jump = true
			}
			x = signed12((val1 << 8) | val2)
			var ok bool
			if val2, ok = c.u8(); !ok {
				break
			}
		} else {
			x = signed7(val1)
		}

		var y int
		if val2&pecFlagLong != 0 {
			if val2&pecTrimCode != 0 {
				trim = true
			}
			if val2&pecJumpCode != 0 {
				jump = true
			}
			val3, ok := c.u8()
			if !ok {
				break
			}
			y = signed12((val2 << 8) | val3)
		} else {
			y = signed7(val2)
		}

		switch {
		case jump:
			b.move(float64(x), float64(y))
		case trim:
			b.trim()
			b.move(float64(x), float64(y))
		default:
			b.stitch(float64(x), float64(y))
		}
	}
	b.end()
}
