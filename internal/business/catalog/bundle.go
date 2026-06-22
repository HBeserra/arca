package catalog

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A .svault bundle is a portable copy of the whole catalog: the processed index
// (metadata + captions + embeddings), the rendered thumbnails, and the original
// embroidery files. Importing it on another (weaker) computer restores a fully
// usable, searchable catalog WITHOUT re-parsing or re-classifying anything.
//
// Layout (zip):
//   manifest.json          {app, version, createdAt, designCount}
//   catalog.json           {designs:[…+embedding], folders:[…], settings:{…}}
//   thumbnails/<id>.png     rendered thumbnails
//   designs/<id><ext>       original embroidery files

const bundleVersion = 1

type bundleManifest struct {
	App         string `json:"app"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"createdAt"`
	DesignCount int    `json:"designCount"`
}

type bundleFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// bundleDesign is a full design record plus its embedding and which files the
// bundle carries for it. The embedded Design's fields are flattened into the JSON.
type bundleDesign struct {
	Design
	Embedding []float32 `json:"embedding,omitempty"`
	FileExt   string    `json:"fileExt"`
	HasFile   bool      `json:"hasFile"`
	HasThumb  bool      `json:"hasThumb"`
}

type bundleData struct {
	Designs  []bundleDesign    `json:"designs"`
	Folders  []bundleFolder    `json:"folders"`
	Settings map[string]string `json:"settings"`
}

// importedDir is where Import extracts original files (sibling of thumbnails).
func (e *Engine) importedDir() string {
	return filepath.Join(filepath.Dir(e.thumbDir), "imported")
}

// Export writes the whole catalog as a .svault bundle to w. progress(done,total)
// is called as each design is added (nil to skip).
func (e *Engine) Export(ctx context.Context, w io.Writer, progress func(done, total int)) error {
	designs, err := e.store.ListDesigns(ctx, Filter{Limit: 1 << 30})
	if err != nil {
		return fmt.Errorf("export: list designs: %w", err)
	}
	embByID := map[uuid.UUID][]float32{}
	if vecs, err := e.store.ListForClustering(ctx); err == nil {
		for _, v := range vecs {
			embByID[v.ID] = v.Embedding
		}
	}
	folders, _ := e.store.ListVirtualFolders(ctx)

	data := bundleData{Settings: map[string]string{}}
	for _, key := range []string{"caption_language"} {
		if v, _ := e.store.GetSetting(ctx, key); v != "" {
			data.Settings[key] = v
		}
	}
	for _, f := range folders {
		data.Folders = append(data.Folders, bundleFolder{ID: f.ID.String(), Name: f.Name})
	}

	zw := zip.NewWriter(w)
	total := len(designs)
	for i, d := range designs {
		if err := ctx.Err(); err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(d.Path))
		bd := bundleDesign{Design: d, Embedding: embByID[d.ID], FileExt: ext}
		if d.ThumbnailPath != "" {
			if err := addFileToZip(zw, d.ThumbnailPath, "thumbnails/"+d.ID.String()+".png"); err == nil {
				bd.HasThumb = true
			}
		}
		if d.Path != "" {
			if err := addFileToZip(zw, d.Path, "designs/"+d.ID.String()+ext); err == nil {
				bd.HasFile = true
			}
		}
		data.Designs = append(data.Designs, bd)
		if progress != nil {
			progress(i+1, total)
		}
	}

	manifest := bundleManifest{App: "StitchVault", Version: bundleVersion, CreatedAt: time.Now().Format(time.RFC3339), DesignCount: total}
	if err := writeJSONToZip(zw, "manifest.json", manifest); err != nil {
		return err
	}
	if err := writeJSONToZip(zw, "catalog.json", data); err != nil {
		return err
	}
	return zw.Close()
}

// Import merges a .svault bundle into the catalog: it upserts designs by id
// (non-destructive), extracts thumbnails into the thumbnails dir and original
// files into the imported dir, and rewrites paths to the local machine.
func (e *Engine) Import(ctx context.Context, r io.ReaderAt, size int64, progress func(done, total int)) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("import: open bundle: %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}

	var data bundleData
	if err := readJSONFromZip(files, "catalog.json", &data); err != nil {
		return fmt.Errorf("import: read catalog: %w", err)
	}

	for _, f := range data.Folders {
		if id, err := uuid.Parse(f.ID); err == nil {
			_ = e.store.CreateVirtualFolder(ctx, id, f.Name) // best-effort (may already exist)
		}
	}

	imported := e.importedDir()
	if err := os.MkdirAll(imported, 0o755); err != nil {
		return fmt.Errorf("import: mkdir: %w", err)
	}

	total := len(data.Designs)
	for i, bd := range data.Designs {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := bd.Design
		if bd.HasThumb {
			dst := filepath.Join(e.thumbDir, d.ID.String()+".png")
			if extractZipFile(files, "thumbnails/"+d.ID.String()+".png", dst, 0o644) == nil {
				d.ThumbnailPath = dst
			}
		}
		if bd.HasFile {
			dst := filepath.Join(imported, d.ID.String()+bd.FileExt)
			if extractZipFile(files, "designs/"+d.ID.String()+bd.FileExt, dst, 0o644) == nil {
				d.Path = dst
			}
		}
		if err := e.store.InsertDesign(ctx, d); err != nil {
			return fmt.Errorf("import: insert %s: %w", d.FileName, err)
		}
		if len(bd.Embedding) > 0 {
			_ = e.store.UpdateClassification(ctx, d.ID, d.Caption, d.Tags, d.Style, bd.Embedding)
		}
		if progress != nil {
			progress(i+1, total)
		}
	}

	if lang := data.Settings["caption_language"]; lang != "" {
		_ = e.store.SetSetting(ctx, "caption_language", lang)
		if e.cls != nil {
			e.cls.SetCaptionLanguage(lang)
		}
	}
	return nil
}

// ─── zip helpers ─────────────────────────────────────────────────────────────

func addFileToZip(zw *zip.Writer, srcPath, name string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

func writeJSONToZip(zw *zip.Writer, name string, v any) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func readJSONFromZip(files map[string]*zip.File, name string, dst any) error {
	f, ok := files[name]
	if !ok {
		return fmt.Errorf("missing %q in bundle", name)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(rc).Decode(dst)
}

func extractZipFile(files map[string]*zip.File, name, dst string, perm os.FileMode) error {
	f, ok := files[name]
	if !ok {
		return fmt.Errorf("missing %q", name)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
