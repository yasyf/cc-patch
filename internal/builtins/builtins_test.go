package builtins

import (
	"bytes"
	"io/fs"
	"slices"
	"testing"

	"github.com/yasyf/cc-patch/internal/pack"
	"github.com/yasyf/cc-patch/internal/registry"
)

func TestOpen(t *testing.T) {
	for _, name := range []string{"fastmode", "noshadow", "workflowdefault", "worktreeguard"} {
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
	for _, want := range []string{"fastmode", "noshadow", "workflowdefault", "worktreeguard"} {
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
		if _, err := compile(t, name); err != nil {
			t.Fatalf("%s: compile: %v", name, err)
		}
	}
}

// TestNoshadowPoolEntry pins the constant-pool entry noshadow rewrites to the
// bytes Claude Code 2.1.278 carries at offset 76312836, hash included: an edit
// to pool_find that silently changes the entry's length or hash would leave the
// pool inconsistent rather than fail to apply.
func TestNoshadowPoolEntry(t *testing.T) {
	patches, err := compile(t, "noshadow")
	if err != nil {
		t.Fatal(err)
	}
	site := patches[0].Sites[0]
	wantFind := append([]byte{0x27, 0x00, 0x00, 0x80, 0xee, 0x07, 0x15, 0x00}, "  if [[ ! -x $_cc_bin ]]; then command \x00"...)
	if !bytes.Equal(site.Find, wantFind) {
		t.Errorf("Find = %q, want %q", site.Find, wantFind)
	}
	wantReplace := append([]byte{0x27, 0x00, 0x00, 0x80, 0xf8, 0x45, 0x97, 0x00}, "  if [[      $_cc_bin ]]; then command \x00"...)
	if !bytes.Equal(site.Replace, wantReplace) {
		t.Errorf("Replace = %q, want %q", site.Replace, wantReplace)
	}
	if site.Drop != nil {
		t.Errorf("Drop = %q, want nil for a pool site", site.Drop)
	}
}

func compile(t *testing.T, name string) ([]registry.Patch, error) {
	t.Helper()
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
	return m.Patches("builtin/" + name)
}
