package goreader

import "stitchvault/internal/embroidery"

// Singer XXX — a small header (colour count at 0x27) then a stitch stream beginning
// at 0x100, with 0x7D/0x7E long jumps and 0x7F-prefixed controls; the RGB colour
// table follows the stitch data. Port of pyembroidery XxxReader.py. Colours are
// stored directly (no chart lookup).

func readXXX(c *cursor, b *builder) error {
	c.skip(0x27)
	numColors, _ := c.u16le()
	c.seek(0x100)

loop:
	for {
		if b.canceled() {
			break
		}
		b1, ok := c.u8()
		if !ok {
			break
		}
		if b1 == 0x7D || b1 == 0x7E { // long jump (0x7E unconfirmed, per pyembroidery)
			x, _ := c.u16le()
			y, _ := c.u16le()
			b.move(float64(signed16(x)), float64(-signed16(y)))
			continue
		}
		b2, ok := c.u8()
		if !ok {
			break
		}
		if b1 != 0x7F {
			b.stitch(float64(signed8(b1)), float64(-signed8(b2)))
			continue
		}
		b3, _ := c.u8()
		b4, _ := c.u8()
		switch {
		case b2 == 0x01: // unstitched move
			b.move(float64(signed8(b3)), float64(-signed8(b4)))
		case b2 == 0x03: // trim (+ optional move)
			b.trim()
			x := signed8(b3)
			y := -signed8(b4)
			if x != 0 || y != 0 {
				b.move(float64(x), float64(y))
			}
		case b2 == 0x08 || (b2 >= 0x0A && b2 <= 0x17): // colour change
			b.colorChange(0, 0)
		case b2 == 0x7F, b2 == 0x18: // end
			break loop
		}
	}
	b.end()

	c.skip(2)
	for range numColors {
		color, ok := c.u32be()
		if !ok {
			break
		}
		b.addThread(embroidery.Thread{
			R: uint8(color >> 16),
			G: uint8(color >> 8),
			B: uint8(color),
		})
	}
	return nil
}
