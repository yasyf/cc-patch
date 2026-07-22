// Package registry holds the set of binary patches cc-patch knows how to apply.
package registry

import (
	"bytes"
	"fmt"

	"github.com/yasyf/cc-patch/internal/binpatch"
)

// Site is one neutralize-a-gate edit: Find is the exact byte run to locate, and
// Drop is the substring within it blanked to spaces to disable the gate.
type Site struct {
	// Anchor describes, for humans, what the site controls.
	Anchor string
	Find   []byte
	Drop   []byte
}

// Substitution renders the length-neutral edit for this site.
func (s Site) Substitution() binpatch.Substitution {
	return binpatch.Substitution{Find: s.Find, Replace: blank(s.Find, s.Drop)}
}

func blank(find, drop []byte) []byte {
	i := bytes.Index(find, drop)
	if i < 0 {
		panic(fmt.Sprintf("registry: drop %q is not a substring of find %q", drop, find))
	}
	out := bytes.Clone(find)
	for j := i; j < i+len(drop); j++ {
		out[j] = ' '
	}
	return out
}

// Patch is a named, self-describing set of sites against one Mach-O segment.
type Patch struct {
	// ID is the stable identifier used on the command line.
	ID string
	// Summary is a one-line human description.
	Summary string
	// SegmentName is the Mach-O segment the sites live in (e.g. "__BUN").
	SegmentName string
	// Sites are the version-pinned literals, proven against the current release.
	Sites []Site
	// Derive re-locates the sites structurally from the segment window, so a
	// minifier that renames locals across an update is handled without help.
	Derive func(window []byte) ([]Site, error)
}

// Substitutions renders each site to a binpatch substitution.
func Substitutions(sites []Site) []binpatch.Substitution {
	out := make([]binpatch.Substitution, len(sites))
	for i, s := range sites {
		out[i] = s.Substitution()
	}
	return out
}

// All returns every registered patch.
func All() []Patch {
	return []Patch{fastModeDelegatedAgents()}
}

// Find returns the patch with the given ID.
func Find(id string) (Patch, error) {
	for _, p := range All() {
		if p.ID == id {
			return p, nil
		}
	}
	return Patch{}, fmt.Errorf("no registered patch %q", id)
}

// fastModeDelegatedAgents enables fast mode for delegated Opus agents by blanking
// the two gates' explicit-fastMode-request requirement while keeping the
// in-scope model-eligibility gate BT (which still excludes sonnet/fable).
// Traced and runtime-verified against CC 2.1.217.
func fastModeDelegatedAgents() Patch {
	return Patch{
		ID:          "fastmode-delegated-agents",
		Summary:     "Fast mode for delegated Opus agents (subagents, teammates, workflow branches)",
		SegmentName: "__BUN",
		Sites: []Site{
			{
				Anchor: `service tier: ...&&BT(_)&&!!Dn.fastMode)gn="fast"`,
				Find:   []byte(`&&BT(_)&&!!Dn.fastMode)gn="fast"`),
				Drop:   []byte(`&&!!Dn.fastMode`),
			},
			{
				Anchor: `fast-mode beta header: ne=...&&BT(_)&&!!i.fastMode`,
				Find:   []byte(`ne=vl()&&UO()&&!pAe()&&BT(_)&&!!i.fastMode`),
				Drop:   []byte(`&&!!i.fastMode`),
			},
		},
		Derive: deriveFastMode,
	}
}
