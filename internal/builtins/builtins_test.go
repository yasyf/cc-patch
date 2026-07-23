package builtins

import (
	"slices"
	"testing"
)

func TestOpen(t *testing.T) {
	if _, ok := Open("fastmode"); !ok {
		t.Fatal("fastmode builtin should resolve")
	}
	// fs.Sub runs fs.ValidPath, so traversal and empty names never resolve.
	for _, name := range []string{"../etc", "..", ".", "", "nonexistent", "packs"} {
		if _, ok := Open(name); ok {
			t.Errorf("Open(%q) should not resolve", name)
		}
	}
}

func TestNames(t *testing.T) {
	if names := Names(); !slices.Contains(names, "fastmode") {
		t.Errorf("Names() = %v, want to contain fastmode", names)
	}
}
