package catalog_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"stitchvault/internal/business/catalog"
	"stitchvault/internal/business/catalog/stores/catalogdb"
	"stitchvault/internal/render"
)

// TestConsolidateOriginals checks the one-time pass that copies catalogued originals
// into ~/.stitchvault/originals: an accessible source is copied + its path rewritten
// (source kept), a missing source is skipped untouched, and a second run is a no-op.
func TestConsolidateOriginals(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("duckdb", filepath.Join(root, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := catalogdb.New(discard(), db)
	if err != nil {
		t.Fatal(err)
	}
	thumbs := filepath.Join(root, "thumbs")
	eng := catalog.New(discard(), nil, render.New(), store, thumbs)
	ctx := context.Background()
	originals := filepath.Join(root, "originals")

	// A design whose source file is accessible, outside the originals dir.
	srcDir := filepath.Join(root, "ext")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "a.pes")
	if err := os.WriteFile(src, []byte("stitchdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	idA := catalog.DesignID(src)
	mustInsert(t, store, src, "a.pes")

	// A design whose source is gone (e.g. removed USB).
	missing := filepath.Join(srcDir, "gone.pes")
	idB := catalog.DesignID(missing)
	mustInsert(t, store, missing, "gone.pes")

	copied, skipped, err := eng.ConsolidateOriginals(ctx)
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if copied != 1 || skipped != 1 {
		t.Fatalf("copied=%d skipped=%d, want 1/1", copied, skipped)
	}

	// idA: path rewritten under originals, file copied, source kept.
	dA, _ := store.GetDesign(ctx, idA)
	if filepath.Dir(dA.Path) != originals {
		t.Errorf("idA path = %q, want under %q", dA.Path, originals)
	}
	if _, err := os.Stat(dA.Path); err != nil {
		t.Errorf("copied original missing: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source must be kept: %v", err)
	}

	// idB: missing source left pointing where it was.
	dB, _ := store.GetDesign(ctx, idB)
	if dB.Path != missing {
		t.Errorf("idB path = %q, want unchanged %q", dB.Path, missing)
	}

	// Idempotent.
	if copied2, _, _ := eng.ConsolidateOriginals(ctx); copied2 != 0 {
		t.Errorf("second run copied = %d, want 0", copied2)
	}
}

func mustInsert(t *testing.T, store *catalogdb.Store, path, name string) {
	t.Helper()
	if err := store.InsertDesign(context.Background(), catalog.Design{
		ID:        catalog.DesignID(path), // deterministic; matches the caller's id
		Path:      path,
		FileName:  name,
		Format:    ".pes",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("insert %s: %v", name, err)
	}
}
