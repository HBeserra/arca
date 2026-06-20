// Package ingest reads embroidery files into the neutral embroidery.Design model
// and scans directories for files to import. The Reader interface is the single
// swap point between the MVP pyembroidery subprocess (internal/ingest/pyreader)
// and a future native Go parser — declared here, in the consumer package, so the
// adapters depend on it rather than the other way around.
package ingest

import (
	"context"

	"stitchvault/internal/embroidery"
)

// Reader parses one embroidery file into a neutral Design. Implementations MUST
// honour context cancellation so a batch import can be aborted mid-file.
type Reader interface {
	Read(ctx context.Context, path string) (*embroidery.Design, error)
}
