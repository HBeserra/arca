// Package goreader is the native-Go ingest.Reader: it parses embroidery files
// directly in Go, with no Python or external process. It replaces the former
// pyembroidery subprocess (the removed internal/ingest/pyreader package).
//
// Each format decoder is a faithful port of the corresponding pyembroidery reader
// (DstReader, ExpReader, PecReader, PesReader, JefReader, SewReader, Vp3Reader,
// XxxReader, U01Reader) and the Brother/Janome thread charts. pyembroidery is
// MIT-licensed (Copyright Tatarize); the byte-level logic and colour tables here
// are derived from it. The supported set is the common home/commercial formats
// (see ingest.SupportedExts); rarer formats are intentionally out of scope.
//
// Canonical conventions (identical to the old reader.py, see internal/embroidery):
// coordinates are accumulated in the file's native 1/10 mm units and divided by 10
// to millimetres, Y points down, and each format's command set is mapped onto the
// embroidery.Command enum.
package goreader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"stitchvault/internal/embroidery"
	"stitchvault/internal/ingest"
)

// Reader implements ingest.Reader with native Go format decoders. It holds no
// state and is safe for concurrent use, so a single value can back the whole app.
type Reader struct{}

var _ ingest.Reader = (*Reader)(nil)

// New returns a Reader. It never fails and touches no disk or network (unlike the
// old pyreader, which wrote a script and bootstrapped a venv).
func New() *Reader { return &Reader{} }

// Read parses one embroidery file into a neutral Design. The context is honoured:
// it is checked before parsing and periodically inside the decode loop (see
// builder.canceled), so a batch import can abort mid-file.
func (r *Reader) Read(ctx context.Context, path string) (*embroidery.Design, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("goreader %s: %w", filepath.Base(path), err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("goreader %s: empty file (0 bytes)", filepath.Base(path))
	}

	ext := strings.ToLower(filepath.Ext(path))
	b := newBuilder(ctx)
	if err := decode(ext, newCursor(data), b); err != nil {
		return nil, fmt.Errorf("goreader %s: %w", filepath.Base(path), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d := b.toDesign(ext)
	// A file that yields no positioned stitch (only a terminating End) did not
	// parse as real embroidery data — corrupt, encrypted, or not the format its
	// extension claims. Surface it as a per-file error so the batch skips it,
	// matching the old pyembroidery "unsupported or unreadable" behaviour.
	if _, _, ok := d.Bounds(); !ok {
		return nil, fmt.Errorf("goreader %s: no stitch data (unsupported or corrupt file)", filepath.Base(path))
	}
	return d, nil
}

// decode dispatches to the format reader for ext (already lowercased, includes the
// leading dot). The set here MUST stay in sync with ingest.SupportedExts.
func decode(ext string, c *cursor, b *builder) error {
	switch ext {
	case ".dst":
		return readDST(c, b)
	case ".exp":
		return readEXP(c, b)
	case ".u01":
		return readU01(c, b)
	case ".pec":
		return readPECFile(c, b)
	case ".pes":
		return readPES(c, b)
	case ".jef":
		return readJEF(c, b)
	case ".sew":
		return readSEW(c, b)
	case ".vp3":
		return readVP3(c, b)
	case ".xxx":
		return readXXX(c, b)
	default:
		return fmt.Errorf("unsupported format %q", ext)
	}
}
