package catalogdb_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"stitchvault/internal/business/catalog"
	"stitchvault/internal/business/catalog/stores/catalogdb"
	"stitchvault/internal/embroidery"
)

func newStore(t *testing.T) *catalogdb.Store {
	t.Helper()
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := catalogdb.New(slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func rose() catalog.Design {
	return catalog.Design{
		ID:           catalog.DesignID("/a/rose.pes"),
		Path:         "/a/rose.pes",
		FileName:     "rose.pes",
		Format:       ".pes",
		WidthMM:      30, HeightMM: 10,
		StitchCount: 100, ColorChanges: 1, ColorCount: 2,
		Palette:       []embroidery.Thread{{R: 255, Description: "Red"}, {G: 255, Description: "Green"}},
		ThumbnailPath: "/thumbs/rose.png",
		FileSize:      1429,
		CreatedAt:     time.Now().Add(-time.Hour),
	}
}

func lion() catalog.Design {
	return catalog.Design{
		ID:           catalog.DesignID("/b/lion.dst"),
		Path:         "/b/lion.dst",
		FileName:     "lion.dst",
		Format:       ".dst",
		WidthMM:      80, HeightMM: 60,
		StitchCount: 5000, ColorChanges: 0, ColorCount: 1,
		Palette:       nil, // DST carries no colours
		ThumbnailPath: "/thumbs/lion.png",
		FileSize:      99000,
		CreatedAt:     time.Now(),
	}
}

func TestInsertListGet(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, d := range []catalog.Design{rose(), lion()} {
		if err := s.InsertDesign(ctx, d); err != nil {
			t.Fatalf("insert %s: %v", d.FileName, err)
		}
	}

	n, err := s.CountDesigns(ctx, catalog.Filter{})
	if err != nil || n != 2 {
		t.Fatalf("count = %d (err %v), want 2", n, err)
	}

	list, err := s.ListDesigns(ctx, catalog.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}

	got, err := s.GetDesign(ctx, catalog.DesignID("/a/rose.pes"))
	if err != nil {
		t.Fatalf("get rose: %v", err)
	}
	if got.Format != ".pes" || got.StitchCount != 100 || len(got.Palette) != 2 {
		t.Errorf("rose round-trip wrong: %+v", got)
	}
	if got.Palette[1].G != 255 {
		t.Errorf("palette colour lost in JSON round-trip: %+v", got.Palette)
	}
}

func TestFacets(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_ = s.InsertDesign(ctx, rose())
	_ = s.InsertDesign(ctx, lion())

	f, err := s.Facets(ctx)
	if err != nil {
		t.Fatalf("facets: %v", err)
	}
	if f.Total != 2 {
		t.Errorf("Total = %d, want 2", f.Total)
	}
	if f.MaxSizeMM != 80 {
		t.Errorf("MaxSizeMM = %g, want 80", f.MaxSizeMM)
	}
	if f.MaxStitches != 5000 {
		t.Errorf("MaxStitches = %d, want 5000", f.MaxStitches)
	}
	if len(f.Formats) != 2 {
		t.Errorf("Formats = %v, want 2 entries", f.Formats)
	}
}

func TestFilters(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_ = s.InsertDesign(ctx, rose())
	_ = s.InsertDesign(ctx, lion())

	cases := []struct {
		name string
		f    catalog.Filter
		want int
	}{
		{"by format pes", catalog.Filter{Formats: []string{".pes"}}, 1},
		{"by min stitches", catalog.Filter{MinStitches: 1000}, 1},
		{"by max size", catalog.Filter{MaxSizeMM: 50}, 1},
		{"by search", catalog.Filter{Search: "ROSE"}, 1},
		{"by colors >=2", catalog.Filter{MinColors: 2}, 1},
		{"no match", catalog.Filter{Formats: []string{".jef"}}, 0},
		{"all", catalog.Filter{}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := s.CountDesigns(ctx, tc.f)
			if err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != tc.want {
				t.Errorf("count = %d, want %d", n, tc.want)
			}
		})
	}
}

func TestUpsertIsIdempotent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	d := rose()
	if err := s.InsertDesign(ctx, d); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Re-import the same path with updated metadata.
	d.StitchCount = 222
	if err := s.InsertDesign(ctx, d); err != nil {
		t.Fatalf("re-insert: %v", err)
	}

	n, _ := s.CountDesigns(ctx, catalog.Filter{})
	if n != 1 {
		t.Errorf("count after re-import = %d, want 1 (idempotent)", n)
	}
	got, _ := s.GetDesign(ctx, d.ID)
	if got.StitchCount != 222 {
		t.Errorf("StitchCount = %d, want updated 222", got.StitchCount)
	}
}

func TestSettings(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if v, err := s.GetSetting(ctx, "vision_model"); err != nil || v != "" {
		t.Fatalf("unset = %q (err %v), want empty", v, err)
	}
	if err := s.SetSetting(ctx, "vision_model", "http://x/a.gguf"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetSetting(ctx, "vision_model"); v != "http://x/a.gguf" {
		t.Errorf("get = %q, want http://x/a.gguf", v)
	}
	// upsert overwrites
	if err := s.SetSetting(ctx, "vision_model", "http://y/b.gguf"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetSetting(ctx, "vision_model"); v != "http://y/b.gguf" {
		t.Errorf("upsert get = %q, want http://y/b.gguf", v)
	}
}
