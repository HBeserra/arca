// Package filetool provides utilities for file operations such as reading, writing, and managing files, folders, and paths.
// It abstracts away common file handling tasks to simplify interactions with the filesystem for the ai.
package filetool

type (
	Tool struct {
		storage Storage
	}

	FileInfo struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		IsFolder bool   `json:"isFolder"`
		Size     int64  `json:"size"`
	}

	Storage interface {
		ListFiles(path string) ([]FileInfo, error)
	}
)

func New(storage Storage) *Tool {
	return &Tool{storage: storage}
}

func (t *Tool) Name() string {
	return "filetool"
}

func (t *Tool) Description() string {
	return "A tool for managing files and folders, allowing you to list files, read file contents, and perform basic file operations."
}
