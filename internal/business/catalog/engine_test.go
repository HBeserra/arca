package catalog_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"stitchvault/internal/business/catalog"
	"stitchvault/internal/business/catalog/stores/catalogdb"
	"stitchvault/internal/embroidery"
	"stitchvault/internal/ingest/pyreader"
	"stitchvault/internal/ml"
	"stitchvault/internal/render"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// TestEngineImportEndToEnd exercises the full Phase-1 pipeline through the engine:
// scan a directory tree → parse (pyreader) → render thumbnails → store → query.
func TestEngineImportEndToEnd(t *testing.T) {
	reader, err := pyreader.New()
	if err != nil {
		t.Skipf("pyreader unavailable: %v", err)
	}
	if err := reader.Check(context.Background()); err != nil {
		t.Skipf("pyembroidery not installed: %v", err)
	}

	// Build a small tree (with a subfolder) from the shared fixtures.
	const fixtures = "../../ingest/pyreader/testdata"
	root := t.TempDir()
	copyFile(t, filepath.Join(fixtures, "sample.pes"), filepath.Join(root, "flower.pes"))
	copyFile(t, filepath.Join(fixtures, "sample.dst"), filepath.Join(root, "lion.dst"))
	sub := filepath.Join(root, "animals")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(fixtures, "sample.pes"), filepath.Join(sub, "rose.pes"))

	db, err := sql.Open("duckdb", filepath.Join(root, "cat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := catalogdb.New(discard(), db)
	if err != nil {
		t.Fatal(err)
	}

	thumbs := filepath.Join(root, "thumbs")
	eng := catalog.New(discard(), reader, render.New(), store, thumbs)
	ctx := context.Background()

	files, err := eng.Scan([]string{root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("scan found %d files, want 3 (recursion into subfolder)", len(files))
	}

	for _, f := range files {
		if _, err := eng.ImportFile(ctx, f); err != nil {
			t.Fatalf("import %s: %v", filepath.Base(f), err)
		}
	}

	list, err := eng.List(ctx, catalog.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list returned %d designs, want 3", len(list))
	}

	for _, d := range list {
		if d.ThumbnailPath == "" {
			t.Errorf("%s: empty thumbnail path", d.FileName)
			continue
		}
		if _, err := os.Stat(d.ThumbnailPath); err != nil {
			t.Errorf("%s: thumbnail not written: %v", d.FileName, err)
		}
		if d.StitchCount == 0 || d.WidthMM == 0 {
			t.Errorf("%s: missing metadata (stitches=%d width=%.1f)", d.FileName, d.StitchCount, d.WidthMM)
		}
	}

	fc, err := eng.Facets(ctx)
	if err != nil {
		t.Fatalf("facets: %v", err)
	}
	if fc.Total != 3 {
		t.Errorf("facets total = %d, want 3", fc.Total)
	}

	formatCounts := map[string]int{}
	for _, f := range fc.Formats {
		formatCounts[f.Value] = f.Count
	}
	if formatCounts[".pes"] != 2 || formatCounts[".dst"] != 1 {
		t.Errorf("format facets = %v, want .pes:2 .dst:1", formatCounts)
	}

	// A format filter narrows the result set.
	pes, err := eng.Count(ctx, catalog.Filter{Formats: []string{".pes"}})
	if err != nil {
		t.Fatal(err)
	}
	if pes != 2 {
		t.Errorf("count(.pes) = %d, want 2", pes)
	}
}

// TestClassifyAndSearchLive verifies the Phase-2 catalog flow end-to-end: classify
// a design's thumbnail with the vision model, persist caption/tags/embedding, and
// retrieve it via semantic search. Skipped in -short; needs the local models.
func TestClassifyAndSearchLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live model test in -short mode")
	}

	root := t.TempDir()
	db, err := sql.Open("duckdb", filepath.Join(root, "cat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := catalogdb.New(discard(), db)
	if err != nil {
		t.Fatal(err)
	}

	thumbs := filepath.Join(root, "thumbs")
	mlEng := ml.New(discard())
	t.Cleanup(func() { mlEng.Close(context.Background()) })
	eng := catalog.New(discard(), nil, render.New(), store, thumbs, catalog.WithClassifier(mlEng))

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	// Insert a design with a rendered thumbnail (a red square + green triangle).
	id := catalog.DesignID("/x/shapes.pes")
	design := &embroidery.Design{
		Format:  ".pes",
		Threads: []embroidery.Thread{{R: 220}, {G: 160}},
		Stitches: []embroidery.Point{
			{X: 0, Y: 0, Cmd: embroidery.Stitch}, {X: 40, Y: 0, Cmd: embroidery.Stitch},
			{X: 40, Y: 40, Cmd: embroidery.Stitch}, {X: 0, Y: 40, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.Stitch}, {X: 0, Y: 0, Cmd: embroidery.ColorChange},
			{X: 60, Y: 0, Cmd: embroidery.Stitch}, {X: 100, Y: 0, Cmd: embroidery.Stitch},
			{X: 80, Y: 36, Cmd: embroidery.Stitch}, {X: 60, Y: 0, Cmd: embroidery.Stitch},
			{X: 0, Y: 0, Cmd: embroidery.End},
		},
	}
	thumbPath := filepath.Join(thumbs, id.String()+".png")
	if err := render.New().Thumbnail(design, thumbPath, 512); err != nil {
		t.Fatal(err)
	}
	w, h := design.SizeMM()
	if err := store.InsertDesign(ctx, catalog.Design{
		ID: id, Path: "/x/shapes.pes", FileName: "shapes.pes", Format: ".pes",
		WidthMM: w, HeightMM: h, StitchCount: design.StitchCount(), ColorCount: design.ColorCount(),
		ThumbnailPath: thumbPath,
	}); err != nil {
		t.Fatal(err)
	}

	// Classify (loads the vision + embedding models).
	updated, err := eng.Classify(ctx, id)
	if err != nil {
		t.Skipf("vision model unavailable: %v", err)
	}
	t.Logf("caption=%q tags=%v style=%q", updated.Caption, updated.Tags, updated.Style)
	if updated.Caption == "" || len(updated.Tags) == 0 {
		t.Fatalf("classification incomplete: %+v", updated)
	}

	// Persisted?
	got, err := store.GetDesign(ctx, id)
	if err != nil || got.Caption == "" {
		t.Fatalf("caption not persisted: %v", err)
	}

	// Semantic search finds it.
	res, err := eng.Search(ctx, "geometric shapes square and triangle", catalog.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	found := false
	for _, r := range res {
		if r.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("semantic search returned %d results, none matching the classified design", len(res))
	}
}
