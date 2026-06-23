package desktop

import (
	"reflect"
	"testing"
)

func TestOpenArgs(t *testing.T) {
	const path = "/x/a.pes"
	tests := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{"darwin", "open", []string{path}},
		{"windows", "cmd", []string{"/c", "start", "", path}},
		{"linux", "xdg-open", []string{path}},
		{"freebsd", "xdg-open", []string{path}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args := openArgs(tt.goos, path)
			if name != tt.wantName || !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("openArgs(%q) = %q %v, want %q %v", tt.goos, name, args, tt.wantName, tt.wantArgs)
			}
		})
	}
}

func TestDbusShowItemsArgs(t *testing.T) {
	args := dbusShowItemsArgs("/x/a.pes")
	found := false
	for _, a := range args {
		if a == "array:string:file:///x/a.pes" {
			found = true
		}
	}
	if !found {
		t.Errorf("dbusShowItemsArgs missing file URI, got %v", args)
	}
}
