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
	// HealPrompt is the pack author's prose telling Claude how to re-locate the
	// sites when even Derive drifts. Empty disables the claude -p heal escalation.
	HealPrompt string
}

// Substitutions renders each site to a binpatch substitution.
func Substitutions(sites []Site) []binpatch.Substitution {
	out := make([]binpatch.Substitution, len(sites))
	for i, s := range sites {
		out[i] = s.Substitution()
	}
	return out
}

// All returns the built-in patches. cc-patch ships none — every patch arrives as
// an installed pack (see internal/packstore). Kept as the seam a future built-in
// would register through, and so patchset.Load can merge built-ins with packs.
func All() []Patch { return nil }
