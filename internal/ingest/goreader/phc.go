package goreader

// Brother PHC — a Brother machine "card" format that wraps the same PEC stitch
// encoding as PES/PEC inside a larger container header. Port of pyembroidery
// PhcReader.py: read the Brother colour palette, then walk the container's nested
// section offsets to the PEC stitch block and decode it. The embedded thumbnail
// graphics pyembroidery parses are skipped here — the next read is an absolute
// seek, so the graphics cursor position is never used.
//
// pyembroidery cannot WRITE phc, so there is no generated golden fixture (like
// SEW). Validated during development against a real #PHC0009 file: an exact match
// to pyembroidery on all 966 stitches, both threads, and the bounding box.

func readPHC(c *cursor, b *builder) error {
	c.seek(0x4A)
	c.u8()    // pec graphic icon height (only feeds the skipped thumbnail)
	c.skip(1) // reserved
	c.u8()    // pec graphic byte stride (only feeds the skipped thumbnail)

	colorCount, _ := c.u16le()
	for range colorCount {
		idx, ok := c.u8()
		if !ok {
			break // file terminated before the expected colour list end
		}
		b.addThread(brotherThreads[idx%len(brotherThreads)])
	}

	// Walk the container header to the PEC stitch block.
	c.seek(0x2B)
	pecAdd, _ := c.u8() // size of the pre-graphics, post-copyright header
	c.skip(4)           // 0x30: graphics-end size
	pecOffset, _ := c.u16le()

	c.seek(pecOffset + pecAdd)
	bytesInSection, _ := c.u16le() // primary bounds block
	c.skip(bytesInSection)
	bytesInSection2, _ := c.u32le() // sectional bounds block
	c.skip(bytesInSection2 + 10)
	colorCount2, _ := c.u8()
	c.skip(colorCount2 + 0x1D)

	readPECStitches(c, b)
	b.interpolateDuplicateColorAsStop()
	return nil
}
