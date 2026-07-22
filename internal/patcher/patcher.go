// Package patcher applies registered patches to the installed Claude Code
// binary, falling back from pinned literals to structural derivation on drift.
package patcher

import (
	"context"
	"errors"
	"fmt"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/registry"
	"github.com/yasyf/cc-patch/internal/store"
)

// overrideSites returns heal-derived sites persisted for this install's version.
func overrideSites(inst claude.Install, p registry.Patch) ([]registry.Site, bool, error) {
	st, err := store.Load()
	if err != nil {
		return nil, false, err
	}
	saved, ok := st.Override(inst.Version, p.ID)
	if !ok {
		return nil, false, nil
	}
	sites := make([]registry.Site, len(saved))
	for i, s := range saved {
		sites[i] = registry.Site{Anchor: s.Anchor, Find: s.Find, Drop: s.Drop}
	}
	return sites, true, nil
}

// ErrDrift means neither the pinned sites nor structural derivation could locate
// a patch's sites — the binary has drifted beyond what cc-patch can self-derive.
var ErrDrift = errors.New("patch drifted beyond derivation")

// Outcome reports one patch applied against one install.
type Outcome struct {
	PatchID string
	Version string
	Changed bool
	// Derived is true when the sites came from structural derivation, not the pinned literals.
	Derived bool
	Result  binpatch.Result
}

// Apply applies one patch to the install: pinned sites first, structural
// derivation on drift. Idempotent when already patched.
func Apply(ctx context.Context, inst claude.Install, p registry.Patch) (Outcome, error) {
	if sites, ok, err := overrideSites(inst, p); err != nil {
		return Outcome{}, err
	} else if ok {
		res, err := binpatch.Apply(ctx, inst.Binary, inst.Backup(), p.SegmentName, registry.Substitutions(sites))
		if err != nil {
			return Outcome{}, fmt.Errorf("apply %s (stored sites): %w", p.ID, err)
		}
		return Outcome{PatchID: p.ID, Version: inst.Version, Changed: res.Changed, Derived: true, Result: res}, nil
	}
	res, err := binpatch.Apply(ctx, inst.Binary, inst.Backup(), p.SegmentName, registry.Substitutions(p.Sites))
	if err == nil {
		return Outcome{PatchID: p.ID, Version: inst.Version, Changed: res.Changed, Result: res}, nil
	}
	if !errors.Is(err, binpatch.ErrDrift) || p.Derive == nil {
		return Outcome{}, err
	}
	sites, derr := deriveSites(inst, p)
	if derr != nil {
		return Outcome{}, fmt.Errorf("%w: %s: %w", ErrDrift, p.ID, derr)
	}
	res, err = binpatch.Apply(ctx, inst.Binary, inst.Backup(), p.SegmentName, registry.Substitutions(sites))
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: %s (derived sites): %w", ErrDrift, p.ID, err)
	}
	return Outcome{PatchID: p.ID, Version: inst.Version, Changed: res.Changed, Derived: true, Result: res}, nil
}

// Status resolves one patch against the install without modifying it: pinned
// sites first, structural derivation on drift.
func Status(inst claude.Install, p registry.Patch) (Outcome, error) {
	if sites, ok, err := overrideSites(inst, p); err != nil {
		return Outcome{}, err
	} else if ok {
		res, err := binpatch.Status(inst.Binary, p.SegmentName, registry.Substitutions(sites))
		if err != nil {
			return Outcome{}, fmt.Errorf("status %s (stored sites): %w", p.ID, err)
		}
		return Outcome{PatchID: p.ID, Version: inst.Version, Derived: true, Result: res}, nil
	}
	res, err := binpatch.Status(inst.Binary, p.SegmentName, registry.Substitutions(p.Sites))
	if err == nil {
		return Outcome{PatchID: p.ID, Version: inst.Version, Result: res}, nil
	}
	if p.Derive == nil {
		return Outcome{}, err
	}
	sites, derr := deriveSites(inst, p)
	if derr != nil {
		return Outcome{}, fmt.Errorf("%w: %s: %w", ErrDrift, p.ID, derr)
	}
	res, err = binpatch.Status(inst.Binary, p.SegmentName, registry.Substitutions(sites))
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: %s (derived sites): %w", ErrDrift, p.ID, err)
	}
	return Outcome{PatchID: p.ID, Version: inst.Version, Derived: true, Result: res}, nil
}

func deriveSites(inst claude.Install, p registry.Patch) ([]registry.Site, error) {
	window, err := binpatch.Window(inst.Binary, p.SegmentName)
	if err != nil {
		return nil, err
	}
	return p.Derive(window)
}
