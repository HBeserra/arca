package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"stitchvault/internal/ingest"
	"stitchvault/internal/render"
)

// DefaultVisionModel is the GGUF vision model used for Phase-2 classification.
// Swap this constant (or pass WithVisionModel) to use a different VLM; for a bare
// model id the Kronk resolver auto-discovers the matching mmproj. Unused in the
// Phase-1 vertical slice — the config point exists so Phase 2 plugs straight in.
const DefaultVisionModel = "ggml-org/Qwen2.5-VL-3B-Instruct-GGUF"

// Engine wires a Reader, a Renderer and a Store into the catalog use cases:
// importing a directory of embroidery files and serving the resulting designs.
type Engine struct {
	log      *slog.Logger
	reader   ingest.Reader
	renderer render.Renderer
	store    Store

	thumbDir  string
	thumbSize int

	// Phase-2 vision config (held but unused in Phase 1).
	visionModel   string
	visionGrammar string
}

// Option configures the Engine.
type Option func(*Engine)

// WithThumbSize sets the thumbnail edge in pixels.
func WithThumbSize(px int) Option {
	return func(e *Engine) {
		if px > 0 {
			e.thumbSize = px
		}
	}
}

// WithVisionModel overrides the Phase-2 vision model source (id or URL).
func WithVisionModel(source string) Option {
	return func(e *Engine) {
		if source != "" {
			e.visionModel = source
		}
	}
}

// WithVisionGrammar sets the GBNF grammar file constraining Phase-2 classification.
func WithVisionGrammar(path string) Option {
	return func(e *Engine) { e.visionGrammar = path }
}

// New creates the catalog engine. thumbDir is where rendered PNGs are written and
// from where the asset middleware serves them.
func New(log *slog.Logger, reader ingest.Reader, renderer render.Renderer, store Store, thumbDir string, opts ...Option) *Engine {
	e := &Engine{
		log:         log,
		reader:      reader,
		renderer:    renderer,
		store:       store,
		thumbDir:    thumbDir,
		thumbSize:   render.DefaultSize,
		visionModel: DefaultVisionModel,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ThumbDir is the directory holding rendered thumbnails (served at /thumb/).
func (e *Engine) ThumbDir() string { return e.thumbDir }

// VisionModel reports the configured Phase-2 vision model source.
func (e *Engine) VisionModel() string { return e.visionModel }

// Scan expands the given roots (files and/or directories) into a de-duplicated
// list of absolute embroidery file paths.
func (e *Engine) Scan(roots []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}

	add := func(p string) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}

	for _, root := range roots {
		fi, err := os.Stat(root)
		if err != nil {
			e.log.Warn("scan: skipping unreadable root", "path", root, "err", err)
			continue
		}
		if fi.IsDir() {
			found, err := ingest.ScanDir(root)
			if err != nil {
				return nil, fmt.Errorf("scan %s: %w", root, err)
			}
			for _, f := range found {
				add(f)
			}
		} else if ingest.IsSupported(root) {
			add(root)
		}
	}
	return files, nil
}

// Prepare runs the expensive, parallelizable part of import for one file: parse →
// render a thumbnail → extract deterministic metadata. It does NOT touch the
// store, so many Prepare calls can run concurrently; persistence is serialized
// separately via Save (DuckDB writes are best kept single-writer). The thumbnail
// is named by the design's deterministic id.
func (e *Engine) Prepare(ctx context.Context, path string) (Design, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	des, err := e.reader.Read(ctx, abs)
	if err != nil {
		return Design{}, err
	}

	id := DesignID(abs)
	thumbPath := filepath.Join(e.thumbDir, id.String()+".png")
	if err := e.renderer.Thumbnail(des, thumbPath, e.thumbSize); err != nil {
		return Design{}, fmt.Errorf("render %s: %w", filepath.Base(abs), err)
	}

	format := des.Format
	if format == "" {
		format = strings.ToLower(filepath.Ext(abs))
	}

	var fileSize int64
	if fi, err := os.Stat(abs); err == nil {
		fileSize = fi.Size()
	}

	w, h := des.SizeMM()
	return Design{
		ID:            id,
		Path:          abs,
		FileName:      filepath.Base(abs),
		Format:        format,
		WidthMM:       w,
		HeightMM:      h,
		StitchCount:   des.StitchCount(),
		ColorChanges:  des.ColorChanges(),
		ColorCount:    des.ColorCount(),
		Palette:       des.Palette(),
		ThumbnailPath: thumbPath,
		FileSize:      fileSize,
		CreatedAt:     time.Now(),
	}, nil
}

// Save persists a prepared design. Call it from a single goroutine to keep DuckDB
// writes serialized.
func (e *Engine) Save(ctx context.Context, d Design) error {
	return e.store.InsertDesign(ctx, d)
}

// ImportFile prepares and saves one file in sequence. Convenient for single-file
// use and tests; batch imports use Prepare (parallel) + Save (serial) directly.
func (e *Engine) ImportFile(ctx context.Context, path string) (Design, error) {
	d, err := e.Prepare(ctx, path)
	if err != nil {
		return Design{}, err
	}
	if err := e.Save(ctx, d); err != nil {
		return Design{}, err
	}
	return d, nil
}

// List, Count, Get and Facets delegate to the store; they exist so callers depend
// on the engine, not the persistence layer.

func (e *Engine) List(ctx context.Context, f Filter) ([]Design, error) {
	return e.store.ListDesigns(ctx, f)
}

func (e *Engine) Count(ctx context.Context, f Filter) (int, error) {
	return e.store.CountDesigns(ctx, f)
}

func (e *Engine) Get(ctx context.Context, id uuid.UUID) (Design, error) {
	return e.store.GetDesign(ctx, id)
}

func (e *Engine) Facets(ctx context.Context) (Facets, error) {
	return e.store.Facets(ctx)
}
