package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"changeme/internal/engine"
	"changeme/internal/session"
	"changeme/internal/store"

	"github.com/google/uuid"
)

// ProgressFn is called to report progress during indexing.
type ProgressFn func(docID, docName, stage string, pct int)

// supportedExts lists file extensions that can be text-extracted.
var supportedExts = map[string]bool{
	".txt": true, ".md": true, ".mdx": true,
	".go": true, ".py": true, ".js": true, ".ts": true,
	".jsx": true, ".tsx": true, ".json": true,
	".yaml": true, ".yml": true, ".toml": true,
	".csv": true, ".xml": true, ".html": true,
	".sh": true, ".sql": true, ".rs": true,
	".java": true, ".c": true, ".cpp": true, ".h": true,
}

const (
	chunkSize    = 512 // approximate token target (chars/4)
	chunkOverlap = 64
)

// IndexPaths ingests all supported files from the given paths into the session and store.
func IndexPaths(
	ctx context.Context,
	eng *engine.Engine,
	sess *session.Session,
	st *store.Store,
	paths []string,
	onProgress ProgressFn,
) error {
	files, err := collectFiles(paths)
	if err != nil {
		return fmt.Errorf("indexer: collect files: %w", err)
	}

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := indexFile(ctx, eng, sess, st, path, onProgress); err != nil {
			// Mark document as error status and continue.
			name := filepath.Base(path)

			// Find and update the document if it exists.
			for i, d := range sess.Documents {
				if d.Path == path {
					sess.Documents[i].Status = "error"
					_ = sess.Save()

					break
				}
			}

			onProgress("", name, "error: "+err.Error(), 0)
		}
	}

	return nil
}

func collectFiles(paths []string) ([]string, error) {
	var files []string

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}

		if info.IsDir() {
			err := filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return nil
				}

				if !fi.IsDir() && supportedExts[strings.ToLower(filepath.Ext(path))] {
					files = append(files, path)
				}

				return nil
			})
			if err != nil {
				return nil, err
			}
		} else if supportedExts[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
	}

	return files, nil
}

func indexFile(
	ctx context.Context,
	eng *engine.Engine,
	sess *session.Session,
	st *store.Store,
	path string,
	onProgress ProgressFn,
) error {
	slog.Info("Indexing file", "path", path)

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	docID := uuid.New().String()
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))

	doc := session.Document{
		ID:     docID,
		Path:   path,
		Name:   name,
		Ext:    ext,
		Size:   info.Size(),
		Status: "processing",
	}
	sess.UpsertDocument(doc)
	_ = sess.Save()

	onProgress(docID, name, "reading", 5)

	// 1. Read text.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	text := string(raw)
	if !utf8.ValidString(text) {
		return fmt.Errorf("file is not valid UTF-8 text")
	}

	onProgress(docID, name, "chunking", 15)

	// 2. Chunk text.
	chunks := chunkText(text, chunkSize*4, chunkOverlap*4) // chars (approx 4 chars/token)

	if len(chunks) == 0 {
		return fmt.Errorf("no content to index")
	}

	onProgress(docID, name, "embedding", 20)

	// 3. Embed each chunk.
	embeddings := make([][]float32, 0, len(chunks))

	for i, c := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}

		emb, err := eng.Embed(ctx, c)
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", i, err)
		}

		embeddings = append(embeddings, emb)

		pct := 20 + int(float32(i+1)/float32(len(chunks))*50)
		onProgress(docID, name, "embedding", pct)

		st.Add(store.Chunk{
			ID:         uuid.New().String(),
			DocumentID: docID,
			Index:      i,
			Text:       c,
			Embedding:  emb,
		})
	}

	onProgress(docID, name, "computing centroid", 72)

	// 4. Compute centroid.
	centroid := centroidOf(embeddings)

	onProgress(docID, name, "summarizing", 78)

	// 5. Summarize (best-effort).
	summary, _ := eng.Summarize(text)

	// 6. Update document.
	doc.Status = "indexed"
	doc.ChunkCount = len(chunks)
	doc.Summary = summary
	doc.Centroid = centroid

	sess.UpsertDocument(doc)
	_ = sess.Save()

	onProgress(docID, name, "done", 100)

	return nil
}

// chunkText splits text into overlapping chunks of approximately charSize characters.
func chunkText(text string, charSize, overlap int) []string {
	runes := []rune(text)
	n := len(runes)

	if n == 0 {
		return nil
	}

	var chunks []string

	for start := 0; start < n; start += charSize - overlap {
		end := start + charSize
		if end > n {
			end = n
		}

		chunks = append(chunks, string(runes[start:end]))

		if end == n {
			break
		}
	}

	return chunks
}

// centroidOf computes the average embedding vector.
func centroidOf(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}

	dim := len(vecs[0])
	c := make([]float32, dim)

	for _, v := range vecs {
		for i := range c {
			if i < len(v) {
				c[i] += v[i]
			}
		}
	}

	n := float32(len(vecs))

	for i := range c {
		c[i] /= n
	}

	return c
}
