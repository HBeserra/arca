package goreader

// Barudan U01 — a 0x100-byte header followed by 3-byte records whose first byte is
// a control/sign field. Port of pyembroidery U01Reader.py. Magnitudes are stored
// unsigned in the two data bytes; the sign comes from control bits 0x20 (x) and
// 0x40 (y). No thread palette (needle changes map to colour changes).

func readU01(c *cursor, b *builder) error {
	c.skip(0x80)
	c.skip(0x80) // 0x100-byte header
	for {
		if b.canceled() {
			return nil
		}
		rec := c.read(3)
		if len(rec) != 3 {
			break
		}
		ctrl := int(rec[0])
		dy := -int(rec[1])
		dx := int(rec[2])
		if ctrl&0x20 != 0 {
			dx = -dx
		}
		if ctrl&0x40 != 0 {
			dy = -dy
		}
		fx, fy := float64(dx), float64(dy)
		command := ctrl & 0b11111
		switch {
		case command == 0x00: // stitch
			b.stitch(fx, fy)
		case command == 0x01: // jump
			b.move(fx, fy)
		case command == 0x02: // fast
			b.fast()
			if dx != 0 || dy != 0 {
				b.stitch(fx, fy)
			}
		case command == 0x03: // fast + jump
			b.fast()
			if dx != 0 || dy != 0 {
				b.move(fx, fy)
			}
		case command == 0x04: // slow
			b.slow()
			if dx != 0 || dy != 0 {
				b.stitch(fx, fy)
			}
		case command == 0x05: // slow + jump
			b.slow()
			if dx != 0 || dy != 0 {
				b.move(fx, fy)
			}
		case command == 0x06, command == 0x07: // thread / bobbin trim
			b.trim()
			if dx != 0 || dy != 0 {
				b.move(fx, fy)
			}
		case command == 0x08: // stop
			b.stop(0, 0)
			if dx != 0 || dy != 0 {
				b.move(fx, fy)
			}
		case command >= 0x09 && command <= 0x17: // needle change C01–C14
			b.needleChange()
			if dx != 0 || dy != 0 {
				b.move(fx, fy)
			}
		case command == 0x18:
			b.end()
			return nil
		case ctrl == 0x2B:
			b.end()
			return nil // rare machine postfix; do not read it
		default:
			b.end()
			return nil // uncaught command
		}
	}
	b.end()
	return nil
}
