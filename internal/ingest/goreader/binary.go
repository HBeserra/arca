package goreader

// Byte-cursor and integer read helpers. These mirror pyembroidery's ReadHelper.py
// (signed8/signed16/signed24, read_int_*le/be, read_string_*) so each ported format
// reader reads bytes with the exact same width, endianness and sign conventions as
// the reference implementation. Reads are bounds-checked: past EOF a typed read
// returns ok=false (pyembroidery returns None) and a raw read returns a short slice
// (pyembroidery's stream.read returns fewer bytes), which every reader already
// guards against by checking the length / None.

// cursor is a seekable view over an in-memory file. pos may legally advance past
// len(b) (a reader can seek beyond EOF); reads from there yield nothing.
type cursor struct {
	b   []byte
	pos int
}

func newCursor(b []byte) *cursor { return &cursor{b: b} }

func (c *cursor) tell() int    { return c.pos }
func (c *cursor) seek(abs int) { c.pos = abs }  // SEEK_SET
func (c *cursor) skip(rel int) { c.pos += rel } // SEEK_CUR

// read returns up to n bytes, advancing the cursor by what it returned. It returns
// a short (possibly empty, possibly nil) slice at EOF, matching stream.read(n) —
// every reader guards by checking the returned length. A cursor seeked past EOF
// reads nothing.
func (c *cursor) read(n int) []byte {
	if n <= 0 || c.pos < 0 || c.pos >= len(c.b) {
		return nil
	}
	end := min(c.pos+n, len(c.b))
	s := c.b[c.pos:end]
	c.pos = end
	return s
}

// Unsigned / signed integer readers. The bool reports a full read (pyembroidery's
// "is not None"). Values follow ReadHelper's exact byte order.

func (c *cursor) u8() (int, bool) {
	b := c.read(1)
	if len(b) != 1 {
		return 0, false
	}
	return int(b[0]), true
}

func (c *cursor) u16le() (int, bool) {
	b := c.read(2)
	if len(b) != 2 {
		return 0, false
	}
	return int(b[0]) | int(b[1])<<8, true
}

func (c *cursor) u16be() (int, bool) {
	b := c.read(2)
	if len(b) != 2 {
		return 0, false
	}
	return int(b[1]) | int(b[0])<<8, true
}

func (c *cursor) u24le() (int, bool) {
	b := c.read(3)
	if len(b) != 3 {
		return 0, false
	}
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16, true
}

func (c *cursor) u24be() (int, bool) {
	b := c.read(3)
	if len(b) != 3 {
		return 0, false
	}
	return int(b[2]) | int(b[1])<<8 | int(b[0])<<16, true
}

func (c *cursor) u32le() (int, bool) {
	b := c.read(4)
	if len(b) != 4 {
		return 0, false
	}
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24, true
}

func (c *cursor) u32be() (int, bool) {
	b := c.read(4)
	if len(b) != 4 {
		return 0, false
	}
	return int(b[3]) | int(b[2])<<8 | int(b[1])<<16 | int(b[0])<<24, true
}

// string8 reads length bytes as UTF-8 (read_string_8). Invalid UTF-8 yields "".
func (c *cursor) string8(length int) string {
	b := c.read(length)
	return string(b)
}

// --- sign helpers (ReadHelper.signed8/12/16/24) -----------------------------

func signed8(b int) int {
	if b > 127 {
		return b - 256
	}
	return b
}

// signed7 is PEC's short-form delta (PecReader.signed7).
func signed7(b int) int {
	if b > 63 {
		return b - 128
	}
	return b
}

// signed12 is PEC's long-form delta (PecReader.signed12).
func signed12(v int) int {
	v &= 0xFFF
	if v > 0x7FF {
		return v - 0x1000
	}
	return v
}

// signed16 reinterprets a 16-bit value as signed (ReadHelper.signed16, one-arg).
func signed16(v int) int {
	v &= 0xFFFF
	if v > 0x7FFF {
		return v - 0x10000
	}
	return v
}

// signed16be combines two bytes big-endian then signs them (Vp3Reader.signed16).
func signed16be(b0, b1 int) int {
	return signed16((b0&0xFF)<<8 | (b1 & 0xFF))
}

// signed32 reinterprets a 32-bit value as signed (Vp3Reader.signed32).
func signed32(v int) int {
	v &= 0xFFFFFFFF
	if v > 0x7FFFFFFF {
		return v - 0x100000000
	}
	return v
}
