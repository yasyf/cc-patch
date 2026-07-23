// Package patchset merges cc-patch's built-in patches with the installed packs
// into the single set the CLI operates on, keeping registry a leaf package.
package patchset

import (
	"context"
	"fmt"

	"github.com/yasyf/cc-patch/internal/packstore"
	"github.com/yasyf/cc-patch/internal/registry"
)

// Load returns the merged patch set (built-ins plus every installed pack),
// per-pack load errors, and a hard error for store failure or an ID collision.
func Load(_ context.Context) ([]registry.Patch, []error, error) {
	packs, errs, err := packstore.Load()
	if err != nil {
		return nil, nil, err
	}
	merged, err := merge(registry.All(), packs)
	if err != nil {
		return nil, errs, err
	}
	return merged, errs, nil
}

// Find resolves a single patch by its full ID from the merged set.
func Find(ctx context.Context, id string) (registry.Patch, error) {
	patches, _, err := Load(ctx)
	if err != nil {
		return registry.Patch{}, err
	}
	for _, p := range patches {
		if p.ID == id {
			return p, nil
		}
	}
	return registry.Patch{}, fmt.Errorf("patch %q not found (see `cc-patch list`)", id)
}

func merge(built, packs []registry.Patch) ([]registry.Patch, error) {
	out := make([]registry.Patch, 0, len(built)+len(packs))
	seen := make(map[string]registry.Patch, len(built)+len(packs))
	for _, p := range append(built, packs...) {
		if prev, dup := seen[p.ID]; dup {
			return nil, fmt.Errorf("duplicate patch id %q: %q and %q both claim it", p.ID, prev.Summary, p.Summary)
		}
		seen[p.ID] = p
		out = append(out, p)
	}
	return out, nil
}
