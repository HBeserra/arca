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
	"stitchvault/internal/ml"
	"stitchvault/internal/render"
)

// Engine wires a Reader, a Renderer and a Store into the catalog use cases:
// importing a directory of embroidery files, serving the resulting designs, and
// (when a Classifier is wired) classifying them with the local vision model.
type Engine struct {
	log      *slog.Logger
	reader   ingest.Reader
	renderer render.Renderer
	store    Store

	thumbDir  string
	thumbSize int

	cls Classifier // optional; nil disables classification/semantic search
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

// WithClassifier wires the ML capability (vision + embeddings) for Phase 2.
func WithClassifier(c Classifier) Option {
	return func(e *Engine) { e.cls = c }
}

// New creates the catalog engine. thumbDir is where rendered PNGs are written and
// from where the asset middleware serves them.
func New(log *slog.Logger, reader ingest.Reader, renderer render.Renderer, store Store, thumbDir string, opts ...Option) *Engine {
	e := &Engine{
		log:       log,
		reader:    reader,
		renderer:  renderer,
		store:     store,
		thumbDir:  thumbDir,
		thumbSize: render.DefaultSize,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ThumbDir is the directory holding rendered thumbnails (served at /thumb/).
func (e *Engine) ThumbDir() string { return e.thumbDir }

// HasClassifier reports whether classification/semantic search are available.
func (e *Engine) HasClassifier() bool { return e.cls != nil }

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

// Classify runs vision classification on a design's rendered thumbnail, embeds the
// resulting description for similarity search, and stores both. Requires a wired
// Classifier; returns the updated design.
func (e *Engine) Classify(ctx context.Context, id uuid.UUID) (Design, error) {
	if e.cls == nil {
		return Design{}, fmt.Errorf("catalog: classification unavailable (no ML engine)")
	}

	d, err := e.store.GetDesign(ctx, id)
	if err != nil {
		return Design{}, err
	}
	png, err := os.ReadFile(d.ThumbnailPath)
	if err != nil {
		return Design{}, fmt.Errorf("catalog: read thumbnail %s: %w", d.FileName, err)
	}

	c, err := e.cls.Classify(ctx, png)
	if err != nil {
		return Design{}, err
	}

	tags := mergeTags(c.Tags, c.Elements)
	emb, err := e.cls.Embed(ctx, embedText(c))
	if err != nil {
		return Design{}, fmt.Errorf("catalog: embed caption: %w", err)
	}

	if err := e.store.UpdateClassification(ctx, id, c.Caption, tags, c.Style, emb); err != nil {
		return Design{}, err
	}

	d.Caption, d.Tags, d.Style = c.Caption, tags, c.Style
	return d, nil
}

// Search performs semantic search when a Classifier is available (embeds the
// query and ranks classified designs by cosine similarity); otherwise it falls
// back to the filename substring filter.
func (e *Engine) Search(ctx context.Context, query string, f Filter) ([]Design, error) {
	query = strings.TrimSpace(query)
	if e.cls == nil || query == "" {
		f.Search = query
		return e.store.ListDesigns(ctx, f)
	}
	// Semantic ranking replaces the filename filter.
	f.Search = ""
	vec, err := e.cls.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("catalog: embed query: %w", err)
	}
	return e.store.SearchSimilar(ctx, vec, f)
}

// embedText builds the text embedded for similarity search from a classification.
func embedText(c ml.Classification) string {
	parts := []string{c.Caption}
	if c.Theme != "" {
		parts = append(parts, c.Theme)
	}
	if c.Style != "" {
		parts = append(parts, c.Style)
	}
	parts = append(parts, c.Elements...)
	parts = append(parts, c.Tags...)
	return strings.Join(parts, ". ")
}

// mergeTags lowercases, de-duplicates and concatenates tag lists.
func mergeTags(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, s := range list {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
