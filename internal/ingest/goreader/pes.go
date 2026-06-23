package goreader

import (
	"fmt"
	"strings"

	"stitchvault/internal/embroidery"
)

// Brother PES — a versioned wrapper around an embedded PEC block. Port of
// pyembroidery PesReader.py. Every version stores a 32-bit little-endian pointer to
// the PEC block at offset 8; the version header (v5+) may additionally carry an
// authoritative thread list, which is handed to the PEC colour mapping. Like
// pyembroidery we read the header for threads/metadata, then jump to the PEC block
// for the stitches regardless of version.

func readPES(c *cursor, b *builder) error {
	magic := c.string8(8)
	if !strings.HasPrefix(magic, "#PE") {
		return fmt.Errorf("not a valid PES file (bad header — likely encrypted or corrupt)")
	}
	if magic == "#PEC0001" { // a PEC file carrying a .pes extension
		readPEC(c, b, nil)
		b.interpolateDuplicateColorAsStop()
		return nil
	}

	pecBlock, ok := c.u32le()
	if !ok {
		return fmt.Errorf("truncated PES header")
	}

	var chart []embroidery.Thread
	switch magic {
	case "#PES0100":
		b.meta("version", "10")
		chart = readPESHeaderV10(c, b)
	case "#PES0090":
		b.meta("version", "9")
		chart = readPESHeaderV9(c, b)
	case "#PES0080":
		b.meta("version", "8")
		chart = readPESHeaderV8(c, b)
	case "#PES0070":
		b.meta("version", "7")
		chart = readPESHeaderV7(c, b)
	case "#PES0060":
		b.meta("version", "6")
		chart = readPESHeaderV6(c, b)
	case "#PES0050", "#PES0055", "#PES0056":
		b.meta("version", "5")
		chart = readPESHeaderV5(c, b)
	case "#PES0040":
		b.meta("version", "4")
		readPESHeaderV4(c, b)
	case "#PES0030":
		b.meta("version", "3")
	case "#PES0022":
		b.meta("version", "2.2")
	case "#PES0020":
		b.meta("version", "2")
	case "#PES0001":
		b.meta("version", "1")
	default:
		// Unrecognised header — still read the PEC block by its pointer.
	}

	c.seek(pecBlock)
	readPEC(c, b, chart)
	b.interpolateDuplicateColorAsStop()
	return nil
}

// readPESString reads a one-byte length prefix then that many UTF-8 bytes.
func readPESString(c *cursor) string {
	length, ok := c.u8()
	if !ok || length == 0 {
		return ""
	}
	return c.string8(length)
}

func readPESMetadata(c *cursor, b *builder) {
	b.meta("name", readPESString(c))
	b.meta("category", readPESString(c))
	b.meta("author", readPESString(c))
	b.meta("keywords", readPESString(c))
	b.meta("comments", readPESString(c))
}

func readPESThread(c *cursor, chart *[]embroidery.Thread) {
	catalog := readPESString(c)
	color, _ := c.u24be() // 0xFF000000 | RGB; only RGB is kept
	c.skip(5)
	desc := readPESString(c)
	brand := readPESString(c)
	readPESString(c) // thread chart name; unused
	*chart = append(*chart, embroidery.Thread{
		R:           uint8(color >> 16),
		G:           uint8(color >> 8),
		B:           uint8(color),
		Description: desc,
		Brand:       brand,
		Catalog:     catalog,
	})
}

// readPESThreadSection reads the trailing thread count + threads shared by v5–v10.
// A non-zero programmable-fill / motif / feather count aborts the thread read, as
// in pyembroidery (the layout past those is not modelled).
func readPESThreadSection(c *cursor) []embroidery.Thread {
	var chart []embroidery.Thread
	if v, _ := c.u16le(); v != 0 {
		return chart
	}
	if v, _ := c.u16le(); v != 0 {
		return chart
	}
	if v, _ := c.u16le(); v != 0 {
		return chart
	}
	count, _ := c.u16le()
	for range count {
		readPESThread(c, &chart)
	}
	return chart
}

func readPESHeaderV4(c *cursor, b *builder) {
	c.skip(4)
	readPESMetadata(c, b)
}

func readPESHeaderV5(c *cursor, b *builder) []embroidery.Thread {
	c.skip(4)
	readPESMetadata(c, b)
	c.skip(24)
	b.meta("image", readPESString(c))
	c.skip(24)
	return readPESThreadSection(c)
}

func readPESHeaderV6(c *cursor, b *builder) []embroidery.Thread {
	c.skip(4)
	readPESMetadata(c, b)
	c.skip(36)
	b.meta("image_file", readPESString(c))
	c.skip(24)
	return readPESThreadSection(c)
}

func readPESHeaderV7(c *cursor, b *builder) []embroidery.Thread {
	c.skip(4)
	readPESMetadata(c, b)
	c.skip(36)
	b.meta("image_file", readPESString(c))
	c.skip(24)
	return readPESThreadSection(c)
}

func readPESHeaderV8(c *cursor, b *builder) []embroidery.Thread {
	c.skip(4)
	readPESMetadata(c, b)
	c.skip(38)
	b.meta("image_file", readPESString(c))
	c.skip(26)
	return readPESThreadSection(c)
}

func readPESHeaderV9(c *cursor, b *builder) []embroidery.Thread {
	c.skip(4)
	readPESMetadata(c, b)
	c.skip(14)
	b.meta("hoop_name", readPESString(c))
	c.skip(30)
	b.meta("image_file", readPESString(c))
	c.skip(34)
	return readPESThreadSection(c)
}

func readPESHeaderV10(c *cursor, b *builder) []embroidery.Thread {
	c.skip(4)
	readPESMetadata(c, b)
	c.skip(14)
	b.meta("hoop_name", readPESString(c))
	c.skip(38)
	b.meta("image_file", readPESString(c))
	c.skip(34)
	return readPESThreadSection(c)
}
