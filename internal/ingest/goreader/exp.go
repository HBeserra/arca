package goreader

// Melco EXP — a headerless stream of 2-byte signed stitch deltas, with 0x80 as a
// control escape. Port of pyembroidery ExpReader.py. No thread palette.

func readEXP(c *cursor, b *builder) error {
	for {
		if b.canceled() {
			return nil
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
		x := float64(signed8(int(rec[0])))
		y := float64(-signed8(int(rec[1])))
		switch control {
		case 0x80: // trim
			b.trim()
		case 0x02: // stitch (per pyembroidery: "shouldn't exist")
			b.stitch(x, y)
		case 0x04: // jump
			b.move(x, y)
		case 0x01: // color change
			b.colorChange(0, 0)
			if x != 0 || y != 0 {
				b.move(x, y)
			}
		default:
			b.end()
			return nil // uncaught control terminates the stream
		}
	}
	b.end()
	return nil
}
