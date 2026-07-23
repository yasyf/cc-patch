package patchset

import (
	"strings"
	"testing"

	"github.com/yasyf/cc-patch/internal/registry"
)

func TestMergeDuplicateIDErrors(t *testing.T) {
	a := registry.Patch{ID: "acme/demo/fastmode", Summary: "first"}
	b := registry.Patch{ID: "acme/demo/fastmode", Summary: "second"}
	_, err := merge(nil, []registry.Patch{a, b})
	if err == nil {
		t.Fatal("expected a duplicate-id error, got nil")
	}
	for _, want := range []string{"acme/demo/fastmode", "first", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestMergeDistinctIDs(t *testing.T) {
	built := []registry.Patch{{ID: "builtin/a", Summary: "a"}}
	packs := []registry.Patch{{ID: "acme/demo/fastmode", Summary: "b"}}
	got, err := merge(built, packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d patches, want 2", len(got))
	}
}
