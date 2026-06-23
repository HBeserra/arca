package goreader

// Janome SEW — a colour count + Janome thread-chart indices, then the stitch stream
// at a fixed offset (0x1D78). Port of pyembroidery SewReader.py. The stitch stream
// is JEF-like: any odd control byte is a colour change; 0x02/0x04 are jumps; 0x10 is
// a long stitch.

func readSEW(c *cursor, b *builder) error {
	colors, _ := c.u16le()
	for range colors {
		index, _ := c.u16le()
		b.addThread(janomeThreadAt(janomeSewThreads, index))
	}
	c.seek(0x1D78)
	readSEWStitches(c, b)
	return nil
}

func readSEWStitches(c *cursor, b *builder) {
	for {
		if b.canceled() {
			return
		}
		rec := c.read(2)
		if len(rec) != 2 {
			break
		}
		if rec[0] != 0x80 {
			b.stitch(float64(signed8(int(rec[0]))), float64(-signed8(int(rec[1]))))
			continue
		}
		control := rec[1]
		rec = c.read(2)
		if len(rec) != 2 {
			break
		}
		switch {
		case control&1 != 0: // odd control → colour change
			b.colorChange(0, 0)
		case control == 0x04 || control == 0x02: // jump
			b.move(float64(signed8(int(rec[0]))), float64(-signed8(int(rec[1]))))
		case control == 0x10: // long stitch
			b.stitch(float64(signed8(int(rec[0]))), float64(-signed8(int(rec[1]))))
		default:
			b.end()
			return
		}
	}
	b.end()
}
