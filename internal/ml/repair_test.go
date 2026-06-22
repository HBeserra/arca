package ml

import (
	"encoding/json"
	"testing"
)

func TestRepairJSON(t *testing.T) {
	cases := []string{
		`{"caption":"x","tags":["a","b"]   ` + "\n\n\n", // missing }
		`{"folders":[{"name":"A","description":"d"}`,    // missing ] and }
		`{"a":"unterminated`,                            // missing " and }
		`{"a":1}`,                                       // already valid
	}
	for _, c := range cases {
		var v any
		if err := unmarshalLoose(c, &v); err != nil {
			t.Errorf("unmarshalLoose(%.30q) failed: %v", c, err)
		}
	}
	// sanity: repaired equals valid JSON
	var x struct {
		Caption string   `json:"caption"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(repairJSON(`{"caption":"hi","tags":["a","b"]  `)), &x); err != nil || x.Caption != "hi" || len(x.Tags) != 2 {
		t.Errorf("repair roundtrip wrong: %+v err=%v", x, err)
	}
}

// TestRepairJSONControlChars covers raw control characters inside string literals
// (small VLMs emit raw newlines, illegal in JSON) and the parroted-prompt shape
// reported on VA08063.dst.
func TestRepairJSONControlChars(t *testing.T) {
	t.Run("raw newline preserved", func(t *testing.T) {
		bad := "{\"caption\": \"A bird\nin flight\", \"tags\": [\"bird\"]}"
		var out struct {
			Caption string   `json:"caption"`
			Tags    []string `json:"tags"`
		}
		if err := unmarshalLoose(bad, &out); err != nil {
			t.Fatalf("still invalid: %v (repaired=%q)", err, repairJSON(bad))
		}
		if out.Caption != "A bird\nin flight" {
			t.Errorf("caption = %q, want newline preserved", out.Caption)
		}
		if len(out.Tags) != 1 || out.Tags[0] != "bird" {
			t.Errorf("tags = %v, want [bird]", out.Tags)
		}
	})

	t.Run("parroted prompt with newline, unterminated", func(t *testing.T) {
		bad := "{ \"caption\": \"A bird in flight\", \"elements\": [\"the visual style; the overall theme;\nthe mood;"
		var out struct {
			Caption string `json:"caption"`
		}
		if err := unmarshalLoose(bad, &out); err != nil {
			t.Fatalf("still invalid: %v (repaired=%q)", err, repairJSON(bad))
		}
		if out.Caption != "A bird in flight" {
			t.Errorf("caption = %q, want \"A bird in flight\"", out.Caption)
		}
	})
}

// TestSalvageCaption covers the last-resort caption extraction from JSON that's
// broken beyond repairJSON (single-quoted tags, unterminated caption string).
func TestSalvageCaption(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"caption": "Western cowboy boot with cactus and spurs design.}, tags: 'cowboy', 'spur'`, "Western cowboy boot with cactus and spurs design."},
		{`{"caption": "A red rose", "tags": ["rose"]}`, "A red rose"},
		{"{\"caption\": \"unterminated\nmore", "unterminated"},
		{`{"tags": ["x"]}`, ""},
		{`total garbage`, ""},
	}
	for _, c := range cases {
		if got := salvageCaption(c.in); got != c.want {
			t.Errorf("salvageCaption(%.50q) = %q, want %q", c.in, got, c.want)
		}
	}
}
