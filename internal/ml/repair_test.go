package ml
import ("encoding/json";"testing")
func TestRepairJSON(t *testing.T) {
	cases := []string{
		`{"caption":"x","tags":["a","b"]   ` + "\n\n\n",          // missing }
		`{"folders":[{"name":"A","description":"d"}`,              // missing ] and }
		`{"a":"unterminated`,                                       // missing " and }
		`{"a":1}`,                                                  // already valid
	}
	for _, c := range cases {
		var v any
		if err := unmarshalLoose(c, &v); err != nil {
			t.Errorf("unmarshalLoose(%.30q) failed: %v", c, err)
		}
	}
	// sanity: repaired equals valid JSON
	var x struct{ Caption string `json:"caption"`; Tags []string `json:"tags"` }
	if err := json.Unmarshal([]byte(repairJSON(`{"caption":"hi","tags":["a","b"]  `)), &x); err != nil || x.Caption != "hi" || len(x.Tags) != 2 {
		t.Errorf("repair roundtrip wrong: %+v err=%v", x, err)
	}
}
