package catalog_test

import (
	"bytes"
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
	"stitchvault/internal/embroidery"
)

func tempEngine(t *testing.T) (*catalog.Engine, catalog.Store, string) {
	t.Helper()
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := catalogdb.New(log, db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	thumbs := t.TempDir()
	eng := catalog.New(log, nil, nil, store, thumbs)
	return eng, store, thumbs
}

// TestBundleRoundTrip exports a catalog (design + embedding + thumbnail + original
// file + folder + setting) and imports it into a fresh engine, verifying the data,
// the extracted files, and the rewritten original path.
func TestBundleRoundTrip(t *testing.T) {
	ctx := context.Background()
	srcEng, srcStore, srcThumbs := tempEngine(t)

	// An original embroidery file + its rendered thumbnail on disk.
	origPath := filepath.Join(t.TempDir(), "rose.pes")
	if err := os.WriteFile(origPath, []byte("PESDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := catalog.DesignID(origPath)
	thumbPath := filepath.Join(srcThumbs, id.String()+".png")
	if err := os.WriteFile(thumbPath, []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := catalog.Design{
		ID: id, Path: origPath, FileName: "rose.pes", Format: ".pes",
		WidthMM: 30, HeightMM: 10, StitchCount: 100, ColorCount: 2,
		Palette: []embroidery.Thread{{R: 255}}, ThumbnailPath: thumbPath,
		Caption: "uma rosa vermelha", Tags: []string{"flor", "rosa"}, Style: "vetorial",
	}
	if err := srcStore.InsertDesign(ctx, d); err != nil {
		t.Fatal(err)
	}
	emb := make([]float32, 768)
	emb[0], emb[767] = 0.5, -0.25
	if err := srcStore.UpdateClassification(ctx, id, d.Caption, d.Tags, d.Style, emb); err != nil {
		t.Fatal(err)
	}
	fid := catalog.DesignID("/folder/flores")
	_ = srcStore.CreateVirtualFolder(ctx, fid, "Flores")
	_ = srcStore.SetSetting(ctx, "caption_language", "pt")

	// Export to an in-memory bundle.
	var buf bytes.Buffer
	if err := srcEng.Export(ctx, &buf, nil); err != nil {
		t.Fatalf("export: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("export produced an empty bundle")
	}

	// Import into a fresh engine/store/thumbs.
	dstEng, dstStore, dstThumbs := tempEngine(t)
	if err := dstEng.Import(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil); err != nil {
		t.Fatalf("import: %v", err)
	}

	got, err := dstStore.GetDesign(ctx, id)
	if err != nil {
		t.Fatalf("get restored design: %v", err)
	}
	if got.Caption != "uma rosa vermelha" || len(got.Tags) != 2 || got.Style != "vetorial" {
		t.Errorf("design metadata not restored: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dstThumbs, id.String()+".png")); err != nil {
		t.Errorf("thumbnail not extracted: %v", err)
	}
	if got.Path == origPath {
		t.Errorf("original path not rewritten (still %q)", got.Path)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Errorf("original file not extracted to %q: %v", got.Path, err)
	}
	if vecs, _ := dstStore.ListForClustering(ctx); len(vecs) != 1 || len(vecs[0].Embedding) != 768 {
		t.Errorf("embedding not restored: %d vectors", len(vecs))
	}
	if folders, _ := dstStore.ListVirtualFolders(ctx); len(folders) != 1 || folders[0].Name != "Flores" {
		t.Errorf("folder not restored: %+v", folders)
	}
	if v, _ := dstStore.GetSetting(ctx, "caption_language"); v != "pt" {
		t.Errorf("setting not restored: %q", v)
	}
}
