package store

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// Chunk is a text segment with its embedding.
type Chunk struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	Index      int       `json:"index"`
	Text       string    `json:"text"`
	Embedding  []float32 `json:"embedding"`
}

// ScoredChunk pairs a chunk with its similarity score.
type ScoredChunk struct {
	Chunk
	Score float32
}

// Store is an in-memory vector store backed by a JSON file on disk.
type Store struct {
	chunks []Chunk
}

// New returns an empty Store.
func New() *Store {
	return &Store{}
}

// Add appends a chunk to the store.
func (s *Store) Add(c Chunk) {
	s.chunks = append(s.chunks, c)
}

// Search returns the top-k chunks by cosine similarity that meet the threshold.
func (s *Store) Search(query []float32, topK int, threshold float32) []ScoredChunk {
	type item struct {
		chunk ScoredChunk
	}

	var results []ScoredChunk

	for _, c := range s.chunks {
		score := cosine(query, c.Embedding)
		if score >= threshold {
			results = append(results, ScoredChunk{Chunk: c, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results
}

// ForDocument returns all chunks belonging to a document.
func (s *Store) ForDocument(docID string) []Chunk {
	var out []Chunk

	for _, c := range s.chunks {
		if c.DocumentID == docID {
			out = append(out, c)
		}
	}

	return out
}

// RemoveDocument deletes all chunks for a document.
func (s *Store) RemoveDocument(docID string) {
	var keep []Chunk

	for _, c := range s.chunks {
		if c.DocumentID != docID {
			keep = append(keep, c)
		}
	}

	s.chunks = keep
}

// Save persists the store to ~/.arca/sessions/{sessionID}/chunks.json.
func (s *Store) Save(sessionID string) error {
	path, err := chunkPath(sessionID)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("store: mkdir: %w", err)
	}

	data, err := json.Marshal(s.chunks)
	if err != nil {
		return fmt.Errorf("store: marshal: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// Load reads a store from disk for the given session.
func Load(sessionID string) (*Store, error) {
	path, err := chunkPath(sessionID)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}

		return nil, fmt.Errorf("store: read: %w", err)
	}

	var chunks []Chunk

	if err := json.Unmarshal(data, &chunks); err != nil {
		return nil, fmt.Errorf("store: unmarshal: %w", err)
	}

	return &Store{chunks: chunks}, nil
}

func chunkPath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".arca", "sessions", sessionID, "chunks.json"), nil
}

// cosine returns the cosine similarity between two vectors.
func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, na, nb float64

	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}

	if na == 0 || nb == 0 {
		return 0
	}

	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
