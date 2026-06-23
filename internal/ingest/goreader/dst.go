package goreader

import (
	"strconv"
	"strings"

	"stitchvault/internal/embroidery"
)

// Tajima DST — a 512-byte text header followed by 3-byte ternary-encoded stitch
// records. Port of pyembroidery DstReader.py. DST carries no thread palette (colour
// info, when present, comes via TC header lines, which are rare); the renderer
// supplies default colours. pyembroidery's post-read interpolate_trims is skipped:
// it only inserts TRIM markers, which change none of StitchVault's metrics (stitch
// count, colour changes, bounds) and render identically to the jumps they follow.

func readDST(c *cursor, b *builder) error {
	dstReadHeader(c, b)
	dstReadStitches(c, b)
	return nil
}

func dstReadHeader(c *cursor, b *builder) {
	header := c.read(512)
	start := 0
	for i := 0; i <= len(header); i++ {
		if i == len(header) || header[i] == 13 || header[i] == 10 { // CR or LF
			seg := strings.TrimSpace(string(header[start:i]))
			start = i + 1
			if len(seg) > 3 {
				dstHeaderInfo(b, strings.TrimSpace(seg[0:2]), strings.TrimSpace(seg[3:]))
			}
		}
	}
}

// dstHeaderInfo maps a "XX:value" header line, mirroring DstReader.process_header_info.
func dstHeaderInfo(b *builder, prefix, value string) {
	switch prefix {
	case "LA":
		b.meta("name", value)
	case "AU":
		b.meta("author", value)
	case "CP":
		b.meta("copyright", value)
	case "TC":
		// "hex,description,catalog" — an optional extended-DST thread line.
		parts := strings.Split(value, ",")
		if len(parts) >= 1 {
			if r, g, bl, ok := parseHexColor(parts[0]); ok {
				t := embroidery.Thread{R: r, G: g, B: bl}
				if len(parts) >= 2 {
					t.Description = strings.TrimSpace(parts[1])
				}
				if len(parts) >= 3 {
					t.Catalog = strings.TrimSpace(parts[2])
				}
				b.addThread(t)
			}
		}
	default:
		b.meta(prefix, value)
	}
}

func dstReadStitches(c *cursor, b *builder) {
	sequin := false
	for {
		if b.canceled() {
			return
		}
		rec := c.read(3)
		if len(rec) != 3 {
			break
		}
		b0, b1, b2 := int(rec[0]), int(rec[1]), int(rec[2])
		dx := float64(dstDecodeDx(b0, b1, b2))
		dy := float64(dstDecodeDy(b0, b1, b2))
		switch {
		case b2&0b11110011 == 0b11110011:
			b.end()
			return
		case b2&0b11000011 == 0b11000011:
			b.colorChange(dx, dy)
		case b2&0b01000011 == 0b01000011:
			b.sequinMode(dx, dy)
			sequin = !sequin
		case b2&0b10000011 == 0b10000011:
			if sequin {
				b.sequinEject(dx, dy)
			} else {
				b.move(dx, dy)
			}
		default:
			b.stitch(dx, dy)
		}
	}
	b.end()
}

func getbit(b, pos int) int { return (b >> pos) & 1 }

// dstDecodeDx / dstDecodeDy decode the Tajima ternary delta from the 3 record bytes
// (DstReader.decode_dx/decode_dy). decode_dy returns the negated sum.
func dstDecodeDx(b0, b1, b2 int) int {
	x := 0
	x += getbit(b2, 2) * (+81)
	x += getbit(b2, 3) * (-81)
	x += getbit(b1, 2) * (+27)
	x += getbit(b1, 3) * (-27)
	x += getbit(b0, 2) * (+9)
	x += getbit(b0, 3) * (-9)
	x += getbit(b1, 0) * (+3)
	x += getbit(b1, 1) * (-3)
	x += getbit(b0, 0) * (+1)
	x += getbit(b0, 1) * (-1)
	return x
}

func dstDecodeDy(b0, b1, b2 int) int {
	y := 0
	y += getbit(b2, 5) * (+81)
	y += getbit(b2, 4) * (-81)
	y += getbit(b1, 5) * (+27)
	y += getbit(b1, 4) * (-27)
	y += getbit(b0, 5) * (+9)
	y += getbit(b0, 4) * (-9)
	y += getbit(b1, 7) * (+3)
	y += getbit(b1, 6) * (-3)
	y += getbit(b0, 7) * (+1)
	y += getbit(b0, 6) * (-1)
	return -y
}

// parseHexColor parses "RRGGBB" / "#RRGGBB" into channels.
func parseHexColor(s string) (r, g, bl uint8, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
}
