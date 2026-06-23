// Package ingest reads embroidery files into the neutral embroidery.Design model
// and scans directories for files to import. The Reader interface keeps the parser
// behind a single seam — implemented by the native Go reader in
// internal/ingest/goreader — declared here, in the consumer package, so the adapter
// depends on it rather than the other way around.
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
