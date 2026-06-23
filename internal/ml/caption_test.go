package ml

import "testing"

// TestCleanCaption locks the removal of redundant embroidery/dimension filler,
// including the exact cases the user reported.
func TestCleanCaption(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			"en featuring + dimensions",
			"A machine-embroidery design featuring a tennis player in action, with the text 'SPT05' and dimensions of 40.8 x 54.9 mm.",
			"Tennis player in action, with the text 'SPT05'",
		},
		{
			"en design of a child",
			"A machine-embroidery design of a child in a bonnet and dress.",
			"Child in a bonnet and dress",
		},
		{
			"pt imagem mostra + faixa de bordado",
			"A imagem mostra um pássaro em uma faixa de bordado.",
			"Pássaro",
		},
		{
			"en this is a … of",
			"This is a machine-embroidery design of a lotus flower.",
			"Lotus flower",
		},
		{"subject with pattern kept", "Floral pattern with roses", "Floral pattern with roses"},
		{"already clean is recapitalized", "tennis player serving", "Tennis player serving"},
		{"pt bordado de", "Bordado de uma rosa vermelha.", "Rosa vermelha"},
		{"meta-only has no subject to recover", "machine-embroidery design", "Machine-embroidery design"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanCaption(tt.in); got != tt.want {
				t.Errorf("cleanCaption(%q)\n  = %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}
