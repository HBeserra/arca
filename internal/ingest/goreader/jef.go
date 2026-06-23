package goreader

// Janome JEF — a fixed header (stitch-data offset, colour count, then Janome
// thread-chart indices) followed by a 0x80-escaped 2-byte stitch stream. Port of
// pyembroidery JefReader.py, including its "colour 0 = None/stop" patch: a colour
// index of 0 is a placeholder that, when reached by a colour-change control, emits
// a Stop instead and is removed from the palette.

func readJEF(c *cursor, b *builder) error {
	stitchOffset, _ := c.u32le()
	c.skip(20)
	countColors, _ := c.u32le()
	c.skip(88)

	for range countColors {
		index, _ := c.u32le()
		if index == 0 {
			b.addNilThread() // colour 0 → None placeholder (see patch above)
		} else {
			b.addThread(janomeThreadAt(janomeJefThreads, index))
		}
	}

	c.seek(stitchOffset)
	readJEFStitches(c, b)
	return nil
}

func readJEFStitches(c *cursor, b *builder) {
	colorIndex := 1
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
		ctrl := rec[1]
		rec = c.read(2)
		if len(rec) != 2 {
			break
		}
		switch ctrl {
		case 0x02: // jump
			b.move(float64(signed8(int(rec[0]))), float64(-signed8(int(rec[1]))))
		case 0x01: // colour change (or stop, for a None/colour-0 slot)
			if colorIndex < len(b.threads) && b.threads[colorIndex] == nil {
				b.stop(0, 0)
				b.threads = append(b.threads[:colorIndex], b.threads[colorIndex+1:]...)
			} else {
				b.colorChange(0, 0)
				colorIndex++
			}
		case 0x10: // end
			b.end()
			return
		default:
			b.end()
			return
		}
	}
	b.end()
}
