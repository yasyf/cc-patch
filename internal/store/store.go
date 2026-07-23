// Package store persists cc-patch runtime state — patch sites that heal
// re-derived for a specific Claude Code version — so later apply runs reuse them
// without re-invoking Claude.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Site is a persisted patch site. []byte fields JSON-encode as base64, so the
// raw minified-bundle bytes round-trip safely.
type Site struct {
	Anchor string `json:"anchor"`
	Find   []byte `json:"find"`
	Drop   []byte `json:"drop"`
}

// State is the on-disk document: heal-derived sites keyed by
// "<version>/<patchID>", plus the installed packs.
type State struct {
	Overrides map[string][]Site `json:"overrides"`
	Packs     []InstalledPack   `json:"packs"`
}

// InstalledPack records a pack cc-patch has installed: a builtin (embedded in the
// binary, identified by Name) or a remote repo cloned under PacksDir.
type InstalledPack struct {
	Name    string `json:"name,omitempty"`
	Builtin bool   `json:"builtin,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// Namespace is the pack's identity: its builtin name, or "<owner>/<repo>".
func (p InstalledPack) Namespace() string {
	if p.Builtin {
		return p.Name
	}
	return p.Owner + "/" + p.Repo
}

func key(version, patchID string) string { return version + "/" + patchID }

// Dir is cc-patch's private state directory (~/.local/share/cc-patch).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "cc-patch"), nil
}

// PacksDir is where installed pack repos live (Dir()/packs).
func PacksDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "packs"), nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

// Load reads the persisted state, returning an empty state when none exists.
func Load() (State, error) {
	p, err := path()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return State{Overrides: map[string][]Site{}}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state %q: %w", p, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", p, err)
	}
	if s.Overrides == nil {
		s.Overrides = map[string][]Site{}
	}
	return s, nil
}

// Override returns the derived sites persisted for a version+patch, if any.
func (s State) Override(version, patchID string) ([]Site, bool) {
	sites, ok := s.Overrides[key(version, patchID)]
	return sites, ok
}

// Put records derived sites for a version+patch and writes the state to disk.
func Put(version, patchID string, sites []Site) error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.Overrides[key(version, patchID)] = sites
	return save(s)
}

// Packs returns the installed packs.
func Packs() ([]InstalledPack, error) {
	s, err := Load()
	if err != nil {
		return nil, err
	}
	return s.Packs, nil
}

// AddPack records an installed pack, replacing any prior entry with the same
// namespace.
func AddPack(p InstalledPack) error {
	s, err := Load()
	if err != nil {
		return err
	}
	for i, existing := range s.Packs {
		if existing.Namespace() == p.Namespace() {
			s.Packs[i] = p
			return save(s)
		}
	}
	s.Packs = append(s.Packs, p)
	return save(s)
}

// RemovePack drops the installed-pack entry with the given namespace.
func RemovePack(namespace string) error {
	s, err := Load()
	if err != nil {
		return err
	}
	kept := make([]InstalledPack, 0, len(s.Packs))
	for _, p := range s.Packs {
		if p.Namespace() == namespace {
			continue
		}
		kept = append(kept, p)
	}
	s.Packs = kept
	return save(s)
}

// RemoveOverridesForPatchIDs drops every heal override for the given patch ids
// across all versions. Keys are "<version>/<patchID>" (versions carry no slash),
// so matching the exact patchID avoids conflating a builtin namespace ("fastmode")
// with a remote whose owner shares that name ("fastmode/repo").
func RemoveOverridesForPatchIDs(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	s, err := Load()
	if err != nil {
		return err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	for k := range s.Overrides {
		if _, patchID, ok := strings.Cut(k, "/"); ok && want[patchID] {
			delete(s.Overrides, k)
		}
	}
	return save(s)
}

func save(s State) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir %q: %w", dir, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("write state %q: %w", p, err)
	}
	return nil
}
