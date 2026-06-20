package catalog_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/marcboeker/go-duckdb/v2"

	"stitchvault/internal/business/catalog"
	"stitchvault/internal/business/catalog/stores/catalogdb"
	"stitchvault/internal/ingest/pyreader"
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
