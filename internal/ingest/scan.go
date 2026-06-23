package ingest

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// SupportedExts is the set of embroidery file extensions ScanDir collects — the
// common home and commercial machine formats the native Go reader parses
// (internal/ingest/goreader). This set MUST stay in sync with goreader.decode,
// which switches on the same extensions; extending support means adding a decoder
// there and an entry here.
var SupportedExts = map[string]bool{
	".dst": true, ".exp": true, ".jef": true, ".pec": true, ".pes": true,
	".sew": true, ".u01": true, ".vp3": true, ".xxx": true,
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
