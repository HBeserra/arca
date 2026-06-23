package catalog

import (
	"strings"
	"testing"
)

// TestShortFolderName locks the hard caps that keep LLM-proposed folder names from
// overflowing the sidebar: at most 3 words, whitespace collapsed, with a rune cap.
func TestShortFolderName(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"one word", "Animais", "Animais"},
		{"two words kept", "Animais Fofos", "Animais Fofos"},
		{"three words kept", "Bichos da Floresta", "Bichos da Floresta"},
		{"four words clamped to three", "Animais da Floresta Encantada", "Animais da Floresta"},
		{"collapses inner whitespace", "  Flores   do   Campo  ", "Flores do Campo"},
		{"long single word char-capped", strings.Repeat("a", 50), strings.Repeat("a", 28)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortFolderName(tt.in); got != tt.want {
				t.Errorf("shortFolderName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
