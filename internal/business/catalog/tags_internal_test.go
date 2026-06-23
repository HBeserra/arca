package catalog

import (
	"reflect"
	"testing"
)

// TestMergeTagsDropsMeta confirms medium tags ("embroidery", "design", "bordado")
// are filtered out while real subject tags survive, deduped and lowercased.
func TestMergeTagsDropsMeta(t *testing.T) {
	got := mergeTags(
		[]string{"Tennis", "machine-embroidery design", "EMBROIDERY", "sports"},
		[]string{"design", "bordado", "racket", "tennis"},
	)
	want := []string{"tennis", "sports", "racket"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeTags = %v, want %v", got, want)
	}
}

func TestIsMetaTag(t *testing.T) {
	for _, s := range []string{"embroidery", "machine-embroidery design", "embroidered", "design", "pattern", "bordado", "desenho", "diseño"} {
		if !isMetaTag(s) {
			t.Errorf("isMetaTag(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"tennis", "flower", "cat", "geometric", "rose"} {
		if isMetaTag(s) {
			t.Errorf("isMetaTag(%q) = true, want false", s)
		}
	}
}
