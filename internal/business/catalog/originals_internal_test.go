package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreOriginal checks the copy-into-~/.stitchvault/originals helper: it copies
// the file, lowercases the extension in the stored name, never deletes the source,
// and is a no-op for a file already under the originals dir.
func TestStoreOriginal(t *testing.T) {
	root := t.TempDir()
	e := &Engine{thumbDir: filepath.Join(root, "thumbnails")}
	originals := e.originalsDir() // root/originals

	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "design.PES")
	content := []byte("hello-stitches")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	id := DesignID(src)
	dst, err := e.storeOriginal(src, id, filepath.Ext(src))
	if err != nil {
		t.Fatalf("storeOriginal: %v", err)
	}
	if want := filepath.Join(originals, id.String()+".pes"); dst != want {
		t.Errorf("dst = %q, want %q (lowercased ext)", dst, want)
	}
	if got, _ := os.ReadFile(dst); string(got) != string(content) {
		t.Errorf("copied content mismatch: %q", got)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source must not be deleted: %v", err)
	}

	// Already under originals → returned unchanged, no error, no duplicate.
	again, err := e.storeOriginal(dst, id, filepath.Ext(dst))
	if err != nil {
		t.Fatalf("storeOriginal(already-stored): %v", err)
	}
	if again != dst {
		t.Errorf("already-stored: got %q, want unchanged %q", again, dst)
	}
}
