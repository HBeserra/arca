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
	"stitchvault/internal/ingest/goreader"
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
// scan a directory tree → parse (native Go reader) → render thumbnails → store → query.
func TestEngineImportEndToEnd(t *testing.T) {
	reader := goreader.New()

	// Build a small tree (with a subfolder) from the shared fixtures.
	const fixtures = "../../ingest/goreader/testdata"
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

// fakeClassifier implements catalog.Classifier without any models, for testing the
// folder-generation mechanism deterministically.
type fakeClassifier struct{}

func fakeEmbed(text string) []float32 {
	v := make([]float32, ml.EmbedDim)
	for i, r := range text {
		v[i%ml.EmbedDim] += float32(r)
	}
	return v
}

func (fakeClassifier) Classify(_ context.Context, _ []byte, _ string) (ml.Classification, error) {
	return ml.Classification{Caption: "x", Tags: []string{"x"}}, nil
}
func (fakeClassifier) Embed(_ context.Context, text string) ([]float32, error) {
	return fakeEmbed(text), nil
}
func (fakeClassifier) Complete(_ context.Context, _ string, _ map[string]any) (string, error) {
	return `{"folders":[{"name":"Animals","description":"cats dogs and other animals"},{"name":"Shapes","description":"geometric shapes and patterns"}]}`, nil
}
func (fakeClassifier) Concurrency() int               { return 1 }
func (fakeClassifier) SetConcurrency(_ context.Context, _ int) {}
func (fakeClassifier) Warmup(_ context.Context) error { return nil }
func (fakeClassifier) VisionModel() string            { return "fake" }
func (fakeClassifier) SetVisionModel(_ string)        {}
func (fakeClassifier) CaptionLanguage() string        { return "pt" }
func (fakeClassifier) SetCaptionLanguage(_ string)    {}
func (fakeClassifier) Loaded() bool                   { return true }
func (fakeClassifier) Unload(_ context.Context) error { return nil }

// TestGenerateFoldersMechanism verifies taxonomy parsing, folder embedding, cosine
// assignment and persistence — no models, fully deterministic.
func TestGenerateFoldersMechanism(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "cat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := catalogdb.New(discard(), db)
	if err != nil {
		t.Fatal(err)
	}

	eng := catalog.New(discard(), nil, render.New(), store, t.TempDir(), catalog.WithClassifier(fakeClassifier{}))

	captions := map[string]string{"cat": "a fluffy cat", "dog": "a happy dog", "circle": "a blue circle shape"}
	for name, cap := range captions {
		id := catalog.DesignID("/x/" + name)
		if err := store.InsertDesign(ctx, catalog.Design{
			ID: id, Path: "/x/" + name, FileName: name + ".pes", Format: ".pes",
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateClassification(ctx, id, cap, []string{name}, "simple", "0", fakeEmbed(cap)); err != nil {
			t.Fatal(err)
		}
	}

	folders, err := eng.GenerateFolders(ctx, 2)
	if err != nil {
		t.Fatalf("GenerateFolders: %v", err)
	}
	if len(folders) != 2 {
		t.Fatalf("got %d folders, want 2", len(folders))
	}

	// Every design must be assigned, and folder counts must sum to 3.
	total := 0
	for _, f := range folders {
		total += f.Count
		n, err := store.CountDesigns(ctx, catalog.Filter{VirtualFolderID: f.ID.String()})
		if err != nil {
			t.Fatal(err)
		}
		if n != f.Count {
			t.Errorf("folder %q: filter count %d != reported %d", f.Name, n, f.Count)
		}
	}
	if total != 3 {
		t.Errorf("folder counts sum to %d, want 3 (every design assigned)", total)
	}

	// Re-generating clears and replaces (no duplicate folders).
	folders2, err := eng.GenerateFolders(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders2) != 2 {
		t.Errorf("after re-generate got %d folders, want 2 (cleared)", len(folders2))
	}
}

// TestGenerateFoldersLive runs the full headline flow with real models: classify
// several distinct designs, then ask the LLM to organize them into folders.
func TestGenerateFoldersLive(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	shapes := map[string][]embroidery.Point{
		"square":   {{X: 0, Y: 0, Cmd: embroidery.Stitch}, {X: 50, Y: 0, Cmd: embroidery.Stitch}, {X: 50, Y: 50, Cmd: embroidery.Stitch}, {X: 0, Y: 50, Cmd: embroidery.Stitch}, {X: 0, Y: 0, Cmd: embroidery.Stitch}, {X: 0, Y: 0, Cmd: embroidery.End}},
		"triangle": {{X: 0, Y: 0, Cmd: embroidery.Stitch}, {X: 50, Y: 0, Cmd: embroidery.Stitch}, {X: 25, Y: 45, Cmd: embroidery.Stitch}, {X: 0, Y: 0, Cmd: embroidery.Stitch}, {X: 0, Y: 0, Cmd: embroidery.End}},
		"cross":    {{X: 20, Y: 0, Cmd: embroidery.Stitch}, {X: 30, Y: 0, Cmd: embroidery.Stitch}, {X: 30, Y: 20, Cmd: embroidery.Stitch}, {X: 50, Y: 20, Cmd: embroidery.Stitch}, {X: 50, Y: 30, Cmd: embroidery.Stitch}, {X: 30, Y: 30, Cmd: embroidery.Stitch}, {X: 30, Y: 50, Cmd: embroidery.Stitch}, {X: 20, Y: 50, Cmd: embroidery.Stitch}, {X: 20, Y: 30, Cmd: embroidery.Stitch}, {X: 0, Y: 30, Cmd: embroidery.Stitch}, {X: 0, Y: 20, Cmd: embroidery.Stitch}, {X: 20, Y: 20, Cmd: embroidery.Stitch}, {X: 20, Y: 0, Cmd: embroidery.Stitch}, {X: 0, Y: 0, Cmd: embroidery.End}},
	}
	for name, pts := range shapes {
		id := catalog.DesignID("/x/" + name)
		d := &embroidery.Design{Format: ".pes", Threads: []embroidery.Thread{{R: 200, G: 40, B: 40}}, Stitches: pts}
		thumb := filepath.Join(thumbs, id.String()+".png")
		if err := render.New().Thumbnail(d, thumb, 512); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertDesign(ctx, catalog.Design{ID: id, Path: "/x/" + name, FileName: name + ".pes", Format: ".pes", ThumbnailPath: thumb}); err != nil {
			t.Fatal(err)
		}
		if _, err := eng.Classify(ctx, id); err != nil {
			t.Skipf("vision model unavailable: %v", err)
		}
	}

	folders, err := eng.GenerateFolders(ctx, 5)
	if err != nil {
		t.Fatalf("GenerateFolders: %v", err)
	}
	if len(folders) == 0 {
		t.Fatal("LLM proposed no folders")
	}
	total := 0
	names := make([]string, 0, len(folders))
	for _, f := range folders {
		total += f.Count
		names = append(names, f.Name)
		if f.Name == "" {
			t.Error("folder with empty name")
		}
	}
	t.Logf("LLM folders: %v", names)
	if total != len(shapes) {
		t.Errorf("assigned %d designs across folders, want %d", total, len(shapes))
	}
}
