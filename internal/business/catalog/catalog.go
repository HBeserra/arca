// Package catalog is the embroidery catalog domain: the stored Design record, the
// search Filter, faceting, and the Store interface its persistence must satisfy.
// The DuckDB implementation lives in stores/catalogdb. The engine that wires a
// Reader + Renderer + Store together to import and serve designs lives here too
// (engine.go).
package catalog

import (
	"context"
	"time"

	"github.com/google/uuid"

	"stitchvault/internal/embroidery"
)

// Design is one catalogued embroidery file: deterministic metadata extracted from
// the file (Phase 1) plus nullable semantic fields filled later by the LLM
// (Phase 2). It is distinct from embroidery.Design, which is the raw parsed
// geometry; a Design is what we persist and search.
type Design struct {
	ID            uuid.UUID
	Path          string // absolute source path (unique key)
	FileName      string
	Format        string // ".pes", ".dst", …
	WidthMM       float64
	HeightMM      float64
	StitchCount   int
	ColorChanges  int
	ColorCount    int
	Palette       []embroidery.Thread
	ThumbnailPath string
	FileSize      int64
	CreatedAt     time.Time

	// Phase 2 — populated by vision classification + clustering, empty until then.
	Caption         string
	Tags            []string
	Style           string
	VirtualFolderID *uuid.UUID
}

// Filter selects and pages designs for the gallery. Zero-valued bounds mean "no
// bound on that axis"; an empty Formats slice means "any format".
type Filter struct {
	Formats     []string
	MinSizeMM   float64 // filter on the larger of width/height (the design's "size")
	MaxSizeMM   float64
	MinStitches int
	MaxStitches int
	MinColors   int
	MaxColors   int
	Search      string // case-insensitive substring of the file name
	Limit       int
	Offset      int
}

// Facets describes the whole catalog for the filter sidebar: the available
// formats with counts and the maximum values that drive slider ranges.
type Facets struct {
	Total       int
	Formats     []FacetCount
	MaxSizeMM   float64 // max over GREATEST(width, height) — drives the size slider
	MaxStitches int
	MaxColors   int
}

// FacetCount is one facet value and how many designs have it.
type FacetCount struct {
	Value string
	Count int
}

// Store is the persistence contract for the catalog. Implemented by catalogdb.
type Store interface {
	InsertDesign(ctx context.Context, d Design) error
	ListDesigns(ctx context.Context, f Filter) ([]Design, error)
	CountDesigns(ctx context.Context, f Filter) (int, error)
	GetDesign(ctx context.Context, id uuid.UUID) (Design, error)
	Facets(ctx context.Context) (Facets, error)
}

// designNamespace gives stable, path-derived UUIDv5 ids so re-importing the same
// file updates its row (via INSERT OR REPLACE) instead of duplicating it.
var designNamespace = uuid.MustParse("a1d9f0c2-7b3e-5a4d-9c2f-1e6b8d4a0f33")

// DesignID returns the deterministic id for a design at the given absolute path.
func DesignID(absPath string) uuid.UUID {
	return uuid.NewSHA1(designNamespace, []byte(absPath))
}
