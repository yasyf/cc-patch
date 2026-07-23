package builtins

import (
	"io/fs"
	"slices"
	"testing"

	"github.com/yasyf/cc-patch/internal/pack"
)

func TestOpen(t *testing.T) {
	for _, name := range []string{"fastmode", "tasktools"} {
		if _, ok := Open(name); !ok {
			t.Errorf("%s builtin should resolve", name)
		}
	}
	// fs.Sub runs fs.ValidPath, so traversal and empty names never resolve.
	for _, name := range []string{"../etc", "..", ".", "", "nonexistent", "packs"} {
		if _, ok := Open(name); ok {
			t.Errorf("Open(%q) should not resolve", name)
		}
	}
}

func TestNames(t *testing.T) {
	names := Names()
	for _, want := range []string{"fastmode", "tasktools"} {
		if !slices.Contains(names, want) {
			t.Errorf("Names() = %v, want to contain %s", names, want)
		}
	}
}

// TestCompile proves every shipped builtin pack.toml parses and compiles, so a
// malformed pinned site or derive pattern fails the build rather than a user's
// install.
func TestCompile(t *testing.T) {
	for _, name := range Names() {
		fsys, ok := Open(name)
		if !ok {
			t.Fatalf("Open(%q) should resolve", name)
		}
		data, err := fs.ReadFile(fsys, "pack.toml")
		if err != nil {
			t.Fatalf("%s: read pack.toml: %v", name, err)
		}
		m, err := pack.Parse(data)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if _, err := m.Patches("builtin/" + name); err != nil {
			t.Fatalf("%s: compile: %v", name, err)
		}
	}
}
