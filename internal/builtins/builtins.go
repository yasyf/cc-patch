// Package builtins embeds the declarative packs cc-patch ships in its own repo,
// installable by short name (e.g. `cc-patch install fastmode`). Builtins use the
// same pack.toml format as remote packs; they just live in the binary and update
// with it rather than being cloned.
package builtins

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed packs
var packsFS embed.FS

// Open returns the embedded pack tree for name, rooted so pack.toml is at its
// root, and reports whether such a builtin exists.
func Open(name string) (fs.FS, bool) {
	sub, err := fs.Sub(packsFS, "packs/"+name)
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "pack.toml"); err != nil {
		return nil, false
	}
	return sub, true
}

// Names lists the available builtin pack names, sorted.
func Names() []string {
	entries, err := packsFS.ReadDir("packs")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
