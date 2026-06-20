// Package filetool provides read-only filesystem access restricted to files
// indexed in the current session.
package filetool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxChunkChars is the maximum number of characters returned per read_file call.
const maxChunkChars = 8000

// allowedSet is the shared set of normalised file paths for a session.
type allowedSet map[string]struct{}

// New returns three tools — list_files, find_file, and read_file — all
// restricted to the given indexed file paths. Register all with the gateway.
func New(indexedPaths []string) (*ListTool, *FindTool, *ReadTool) {
	allowed := make(allowedSet, len(indexedPaths))
	for _, p := range indexedPaths {
		allowed[filepath.Clean(p)] = struct{}{}
	}
	return &ListTool{allowed: allowed}, &FindTool{allowed: allowed}, &ReadTool{allowed: allowed}
}

// =============================================================================
// list_files

// ListTool lists the files available in the session.
type ListTool struct{ allowed allowedSet }

func (t *ListTool) Name() string { return "list_files" }

func (t *ListTool) Schema() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "list_files",
			"description": "List all files indexed in the current session. Optionally filter by directory prefix.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Optional directory prefix to filter results.",
					},
				},
			},
		},
	}
}

func (t *ListTool) Execute(_ context.Context, args map[string]any) (string, error) {
	prefix, _ := args["path"].(string)
	prefix = filepath.Clean(prefix)

	type entry struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}

	var entries []entry
	for p := range t.allowed {
		if prefix != "." && !hasPrefix(p, prefix) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		entries = append(entries, entry{Path: p, Name: filepath.Base(p), Size: info.Size()})
	}

	b, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("marshal list: %w", err)
	}
	return string(b), nil
}

// =============================================================================
// find_file

// FindTool searches for indexed files whose name contains a given substring.
type FindTool struct{ allowed allowedSet }

func (t *FindTool) Name() string { return "find_file" }

func (t *FindTool) Schema() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "find_file",
			"description": "Search indexed session files by file name. Returns the full path(s) of files whose name contains the given query (case-insensitive). Use this when you know the file name but not its full path.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Substring to search for in file names (case-insensitive).",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

func (t *FindTool) Execute(_ context.Context, args map[string]any) (string, error) {
	query, _ := args["name"].(string)
	if query == "" {
		return "", fmt.Errorf("name is required")
	}

	lowerQuery := strings.ToLower(query)

	type match struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}

	var matches []match
	for p := range t.allowed {
		if strings.Contains(strings.ToLower(filepath.Base(p)), lowerQuery) {
			matches = append(matches, match{Path: p, Name: filepath.Base(p)})
		}
	}

	b, err := json.Marshal(matches)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(b), nil
}

// =============================================================================
// read_file

// ReadTool reads a chunk of a session file.
type ReadTool struct{ allowed allowedSet }

func (t *ReadTool) Name() string { return "read_file" }

func (t *ReadTool) Schema() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "read_file",
			"description": fmt.Sprintf(
				"Read up to %d characters of an indexed file starting at the given character offset. "+
					"The response includes has_more: if true, call again with offset = offset + limit to read the next chunk.",
				maxChunkChars,
			),
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Exact file path to read (as returned by list_files).",
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Character offset to start reading from (default 0).",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *ReadTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	clean := filepath.Clean(path)

	if _, ok := t.allowed[clean]; !ok {
		return "", fmt.Errorf("access denied: %q is not in the indexed session files", path)
	}

	offset := 0
	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset = int(n)
		case int:
			offset = n
		}
	}

	data, err := os.ReadFile(clean)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	runes := []rune(string(data))
	total := len(runes)

	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}

	end := min(offset+maxChunkChars, total)
	chunk := string(runes[offset:end])

	type result struct {
		Path      string `json:"path"`
		Offset    int    `json:"offset"`
		Limit     int    `json:"limit"`
		TotalSize int    `json:"total_size"`
		HasMore   bool   `json:"has_more"`
		Content   string `json:"content"`
	}

	b, err := json.Marshal(result{
		Path:      clean,
		Offset:    offset,
		Limit:     maxChunkChars,
		TotalSize: total,
		HasMore:   end < total,
		Content:   chunk,
	})
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// hasPrefix reports whether path starts with the given directory prefix.
func hasPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return len(path) > len(prefix) && path[len(prefix)] == filepath.Separator && path[:len(prefix)] == prefix
}
