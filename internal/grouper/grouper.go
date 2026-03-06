package grouper

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"changeme/internal/session"

	"github.com/google/uuid"
)

const groupThreshold = 0.65

// GroupDocuments clusters indexed documents by vector proximity, extension, and path.
func GroupDocuments(docs []session.Document) []session.DocumentGroup {
	// Only consider indexed documents with a centroid.
	var indexed []session.Document

	for _, d := range docs {
		if d.Status == "indexed" && len(d.Centroid) > 0 {
			indexed = append(indexed, d)
		}
	}

	if len(indexed) == 0 {
		return nil
	}

	// Greedy clustering: assign each doc to the first compatible group.
	type group struct {
		docs     []session.Document
		centroid []float32
	}

	var groups []group

	for _, doc := range indexed {
		bestIdx := -1
		bestScore := float32(0)

		for i, g := range groups {
			score := combinedScore(doc, g.centroid, g.docs)
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}

		if bestIdx >= 0 && bestScore >= groupThreshold {
			g := &groups[bestIdx]
			g.docs = append(g.docs, doc)
			g.centroid = avgCentroid(g.docs)
		} else {
			groups = append(groups, group{
				docs:     []session.Document{doc},
				centroid: doc.Centroid,
			})
		}
	}

	// Convert to DocumentGroup.
	result := make([]session.DocumentGroup, 0, len(groups))

	for _, g := range groups {
		ids := make([]string, 0, len(g.docs))

		for _, d := range g.docs {
			ids = append(ids, d.ID)
		}

		result = append(result, session.DocumentGroup{
			ID:       uuid.New().String(),
			Label:    autoLabel(g.docs),
			DocIDs:   ids,
			Centroid: g.centroid,
		})
	}

	return result
}

// combinedScore computes a combined similarity score for a document against a group.
func combinedScore(doc session.Document, groupCentroid []float32, groupDocs []session.Document) float32 {
	vecSim := cosine(doc.Centroid, groupCentroid)

	var sameExt float32

	for _, gd := range groupDocs {
		if gd.Ext == doc.Ext {
			sameExt = 1
			break
		}
	}

	commonPath := float32(0)

	if len(groupDocs) > 0 {
		commonPath = pathSimilarity(doc.Path, groupDocs[0].Path)
	}

	return 0.6*vecSim + 0.2*sameExt + 0.2*commonPath
}

// pathSimilarity returns a score 0-1 based on common directory prefix depth.
func pathSimilarity(a, b string) float32 {
	da := filepath.Dir(a)
	db := filepath.Dir(b)

	partsA := strings.Split(da, string(filepath.Separator))
	partsB := strings.Split(db, string(filepath.Separator))

	common := 0

	for i := range partsA {
		if i >= len(partsB) {
			break
		}

		if partsA[i] == partsB[i] {
			common++
		} else {
			break
		}
	}

	maxLen := len(partsA)

	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	if maxLen == 0 {
		return 0
	}

	return float32(common) / float32(maxLen)
}

// autoLabel picks a label based on the dominant extension or common path segment.
func autoLabel(docs []session.Document) string {
	if len(docs) == 0 {
		return "Group"
	}

	// Count extensions.
	extCount := map[string]int{}

	for _, d := range docs {
		extCount[d.Ext]++
	}

	bestExt, bestCount := "", 0

	for ext, count := range extCount {
		if count > bestCount {
			bestExt = ext
			bestCount = count
		}
	}

	// Find the deepest common directory.
	dirs := make([][]string, 0, len(docs))

	for _, d := range docs {
		dir := filepath.Dir(d.Path)
		dirs = append(dirs, strings.Split(dir, string(filepath.Separator)))
	}

	var commonParts []string

	if len(dirs) > 0 {
		for i := range dirs[0] {
			val := dirs[0][i]
			all := true

			for _, parts := range dirs[1:] {
				if i >= len(parts) || parts[i] != val {
					all = false
					break
				}
			}

			if all {
				commonParts = append(commonParts, val)
			} else {
				break
			}
		}
	}

	if len(commonParts) > 0 {
		last := commonParts[len(commonParts)-1]
		if last != "" && last != "." {
			if bestExt != "" {
				return fmt.Sprintf("%s (%s)", last, strings.TrimPrefix(bestExt, "."))
			}

			return last
		}
	}

	if bestExt != "" {
		return strings.ToUpper(strings.TrimPrefix(bestExt, ".")) + " files"
	}

	return "Misc"
}

// avgCentroid returns the mean of all document centroids in the group.
func avgCentroid(docs []session.Document) []float32 {
	if len(docs) == 0 {
		return nil
	}

	dim := len(docs[0].Centroid)
	c := make([]float32, dim)

	for _, d := range docs {
		for i := range c {
			if i < len(d.Centroid) {
				c[i] += d.Centroid[i]
			}
		}
	}

	n := float32(len(docs))

	for i := range c {
		c[i] /= n
	}

	return c
}

// cosine computes cosine similarity between two vectors.
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
