package goreader

import "stitchvault/internal/embroidery"

// Pfaff/Husqvarna VP3 — a big-endian, string-laden header followed by one section
// per colour, each carrying its thread RGB directly plus a relative stitch stream
// with a 0x80 long-move escape. Port of pyembroidery Vp3Reader.py. Coordinates are
// absolute block origins (hundredths) plus signed-byte deltas, exactly as in the
// reference; the builder's final /10 keeps parity with pyembroidery's output.

func readVP3(c *cursor, b *builder) error {
	c.read(6)        // magic "%vsm%\0"
	skipVP3String(c) // "Produced by …"
	c.skip(7)
	skipVP3String(c) // comments / note
	c.skip(32)
	cx, _ := c.u32be()
	centerX := float64(signed32(cx)) / 100
	cy, _ := c.u32be()
	centerY := -(float64(signed32(cy)) / 100)
	c.skip(27)
	skipVP3String(c)
	c.skip(24)
	skipVP3String(c)
	countColors, _ := c.u16be()
	for i := range countColors {
		if b.canceled() {
			return nil
		}
		vp3ReadColorblock(c, b, centerX, centerY)
		if i+1 < countColors { // no colour change after the final block
			b.colorChange(0, 0)
		}
	}
	b.end()
	return nil
}

func vp3ReadColorblock(c *cursor, b *builder, centerX, centerY float64) {
	c.read(3) // \x00\x05\x00
	dist, _ := c.u32be()
	blockEnd := dist + c.tell()

	sx, _ := c.u32be()
	startX := float64(signed32(sx)) / 100
	sy, _ := c.u32be()
	startY := -(float64(signed32(sy)) / 100)
	absX := startX + centerX
	absY := startY + centerY
	if absX != 0 && absY != 0 {
		b.moveAbs(absX, absY)
	}

	b.addThread(vp3ReadThread(c))
	c.skip(15)
	c.read(3) // \x0A\xF6\x00

	stitchBytes := readSignedBytes(c, blockEnd-c.tell())
	i := 0
	for i < len(stitchBytes)-1 {
		x := stitchBytes[i]
		y := stitchBytes[i+1]
		i += 2
		if (x & 0xFF) != 0x80 {
			b.stitch(float64(x), float64(y))
			continue
		}
		switch y {
		case 0x01: // long stitch: two big-endian signed16 deltas
			x = signed16be(stitchBytes[i], stitchBytes[i+1])
			i += 2
			y = signed16be(stitchBytes[i], stitchBytes[i+1])
			i += 2
			b.stitch(float64(x), float64(y))
			i += 2 // trailing 0x80 0x02 terminator, skipped regardless
		case 0x02: // only follows 0x80 0x01; no effect
		case 0x03:
			b.trim()
		}
	}
}

func vp3ReadThread(c *cursor) embroidery.Thread {
	var color int
	colors, _ := c.u8()
	c.u8() // transition
	for range colors {
		color, _ = c.u24be()
		c.u8()    // parts
		c.u16be() // colour length
	}
	c.u8() // thread type
	c.u8() // weight
	catalog := readVP3String8(c)
	desc := readVP3String8(c)
	brand := readVP3String8(c)
	return embroidery.Thread{
		R:           uint8(color >> 16),
		G:           uint8(color >> 8),
		B:           uint8(color),
		Catalog:     catalog,
		Description: desc,
		Brand:       brand,
	}
}

func skipVP3String(c *cursor) {
	n, _ := c.u16be()
	c.skip(n)
}

func readVP3String8(c *cursor) string {
	n, _ := c.u16be()
	return c.string8(n)
}

// readSignedBytes reads n bytes as signed8 values (ReadHelper.read_signed).
func readSignedBytes(c *cursor, n int) []int {
	raw := c.read(n)
	out := make([]int, len(raw))
	for i, by := range raw {
		out[i] = signed8(int(by))
	}
	return out
}
