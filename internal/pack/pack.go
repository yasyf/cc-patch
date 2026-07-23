// Package pack parses an installed pack's cc-patch/pack.toml into the registry
// patches cc-patch applies. cc-patch ships no built-in patches; every patch
// arrives as a pack (see internal/packstore).
package pack

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/yasyf/cc-patch/internal/registry"
)

var patchID = regexp.MustCompile(`^[a-z0-9-]+$`)

// Manifest is a decoded pack.toml: a schema version and its patches.
type Manifest struct {
	Schema int         `toml:"schema"`
	Patch  []patchSpec `toml:"patch"`
}

type patchSpec struct {
	ID      string       `toml:"id"`
	Summary string       `toml:"summary"`
	Segment string       `toml:"segment"`
	Site    []siteSpec   `toml:"site"`
	Derive  []deriveSpec `toml:"derive"`
	Heal    healSpec     `toml:"heal"`
}

type siteSpec struct {
	Anchor  string `toml:"anchor"`
	Find    string `toml:"find"`
	FindB64 string `toml:"find_b64"`
	Drop    string `toml:"drop"`
	DropB64 string `toml:"drop_b64"`
}

type deriveSpec struct {
	Anchor  string   `toml:"anchor"`
	Pattern string   `toml:"pattern"`
	Find    any      `toml:"find"`
	Drop    any      `toml:"drop"`
	Bind    []string `toml:"bind"`
}

type healSpec struct {
	Prompt string `toml:"prompt"`
}

// Parse decodes pack.toml bytes into a Manifest without validating it; call
// Patches to validate and compile the patches.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse pack.toml: %w", err)
	}
	return m, nil
}

// Patches validates every patch and compiles it against namespace (the pack's
// "<owner>/<repo>"), yielding registry patches whose IDs are namespaced.
func (m Manifest) Patches(namespace string) ([]registry.Patch, error) {
	if m.Schema != 1 {
		return nil, fmt.Errorf("unsupported pack schema %d (want 1)", m.Schema)
	}
	out := make([]registry.Patch, 0, len(m.Patch))
	for _, p := range m.Patch {
		patch, err := p.compile(namespace)
		if err != nil {
			return nil, err
		}
		out = append(out, patch)
	}
	return out, nil
}

func (p patchSpec) compile(namespace string) (registry.Patch, error) {
	if !patchID.MatchString(p.ID) {
		return registry.Patch{}, fmt.Errorf("patch id %q must match %s", p.ID, patchID)
	}
	if strings.TrimSpace(p.Summary) == "" {
		return registry.Patch{}, fmt.Errorf("patch %q: summary is required", p.ID)
	}
	if len(p.Site) == 0 {
		return registry.Patch{}, fmt.Errorf("patch %q: at least one [[patch.site]] is required", p.ID)
	}
	segment := p.Segment
	if segment == "" {
		segment = "__BUN"
	}
	sites := make([]registry.Site, len(p.Site))
	for i, s := range p.Site {
		site, err := s.compile()
		if err != nil {
			return registry.Patch{}, fmt.Errorf("patch %q site %d (%q): %w", p.ID, i, s.Anchor, err)
		}
		sites[i] = site
	}
	derive, err := p.deriveFunc()
	if err != nil {
		return registry.Patch{}, fmt.Errorf("patch %q: %w", p.ID, err)
	}
	return registry.Patch{
		ID:          namespace + "/" + p.ID,
		Summary:     p.Summary,
		SegmentName: segment,
		Sites:       sites,
		Derive:      derive,
		HealPrompt:  strings.TrimSpace(p.Heal.Prompt),
	}, nil
}

func (s siteSpec) compile() (registry.Site, error) {
	find, err := decodeField("find", s.Find, s.FindB64)
	if err != nil {
		return registry.Site{}, err
	}
	drop, err := decodeField("drop", s.Drop, s.DropB64)
	if err != nil {
		return registry.Site{}, err
	}
	if !bytes.Contains(find, drop) {
		return registry.Site{}, fmt.Errorf("drop %q is not a substring of find %q", drop, find)
	}
	return registry.Site{Anchor: s.Anchor, Find: find, Drop: drop}, nil
}

// decodeField resolves a site field from its ascii and _b64 forms, requiring
// exactly one to be set.
func decodeField(name, ascii, b64 string) ([]byte, error) {
	switch {
	case ascii != "" && b64 != "":
		return nil, fmt.Errorf("%s: set exactly one of %s / %s_b64, not both", name, name, name)
	case ascii != "":
		return []byte(ascii), nil
	case b64 != "":
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("%s_b64: decode base64: %w", name, err)
		}
		return raw, nil
	default:
		return nil, fmt.Errorf("%s: set exactly one of %s / %s_b64", name, name, name)
	}
}

func (p patchSpec) deriveFunc() (func([]byte) ([]registry.Site, error), error) {
	if len(p.Derive) == 0 {
		return nil, nil
	}
	sites := make([]registry.DeriveSiteSpec, len(p.Derive))
	for i, d := range p.Derive {
		find, err := groupRef(d.Find)
		if err != nil {
			return nil, fmt.Errorf("derive %d (%q) find: %w", i, d.Anchor, err)
		}
		drop, err := groupRef(d.Drop)
		if err != nil {
			return nil, fmt.Errorf("derive %d (%q) drop: %w", i, d.Anchor, err)
		}
		sites[i] = registry.DeriveSiteSpec{
			Anchor:     d.Anchor,
			PatternSrc: d.Pattern,
			Find:       find,
			Drop:       drop,
			Bind:       d.Bind,
		}
	}
	spec := registry.DeriveSpec{Sites: sites}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("derive: %w", err)
	}
	return spec.DeriveFunc(), nil
}

// groupRef converts a TOML find/drop value (int64 index or string group name)
// into a registry.GroupRef.
func groupRef(v any) (registry.GroupRef, error) {
	switch t := v.(type) {
	case int64:
		return registry.GroupByIndex(int(t)), nil
	case string:
		return registry.GroupByName(t), nil
	default:
		return registry.GroupRef{}, fmt.Errorf("must be an int index or string group name, got %T", v)
	}
}
