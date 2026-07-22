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
)

// Site is a persisted patch site. []byte fields JSON-encode as base64, so the
// raw minified-bundle bytes round-trip safely.
type Site struct {
	Anchor string `json:"anchor"`
	Find   []byte `json:"find"`
	Drop   []byte `json:"drop"`
}

// State is the on-disk document: derived sites keyed by "<version>/<patchID>".
type State struct {
	Overrides map[string][]Site `json:"overrides"`
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
