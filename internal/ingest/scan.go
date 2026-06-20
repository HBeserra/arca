package ingest

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// SupportedExts is the set of embroidery file extensions ScanDir collects. It is
// a pragmatic subset of the 40+ formats pyembroidery can read — the common home
// and commercial machine formats. Extending it is just adding an entry here.
var SupportedExts = map[string]bool{
	".pes": true, ".pec": true, ".dst": true, ".exp": true, ".jef": true,
	".vp3": true, ".vip": true, ".xxx": true, ".hus": true, ".sew": true,
	".pcs": true, ".csd": true, ".dsb": true, ".jpx": true, ".u01": true,
	".shv": true, ".emd": true, ".phb": true, ".phc": true, ".pcm": true,
}

// IsSupported reports whether path has a recognized embroidery extension.
func IsSupported(path string) bool {
	return SupportedExts[strings.ToLower(filepath.Ext(path))]
}

// ScanDir walks root recursively and returns absolute paths of every supported
// embroidery file, skipping hidden files and directories (names starting with
// "."). Unreadable entries are skipped rather than aborting the whole walk, so a
// single permission error deep in a tree does not lose the rest of the import.
func ScanDir(root string) ([]string, error) {
	var out []string

	err := filepath.WalkDir(root, func(path string, de fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip this entry, keep walking
		}

		name := de.Name()
		if de.IsDir() {
			if path != root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasPrefix(name, ".") || !IsSupported(path) {
			return nil
		}

		if abs, err := filepath.Abs(path); err == nil {
			out = append(out, abs)
		} else {
			out = append(out, path)
		}
		return nil
	})

	return out, err
}
