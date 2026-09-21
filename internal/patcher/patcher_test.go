package patcher

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/registry"
)

const (
	firstMarker  = "ccpatch_patcher_first_3d9b1a7c"
	secondMarker = "ccpatch_patcher_second_8c4f2e6a"
)

// fixtureBinary builds the signed Mach-O once for the package: every test here
// needs one, and a build plus codesign per test dominates the suite's runtime.
var fixtureBinary = sync.OnceValues(buildFixture)

func fixtureInstall(t *testing.T) claude.Install {
	t.Helper()
	for _, tool := range []string{"codesign", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	src, err := fixtureBinary()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "2.9.9")
	if err := os.WriteFile(bin, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return claude.Install{Binary: bin, VersionsDir: filepath.Dir(bin), Version: "2.9.9"}
}

func buildFixture() (string, error) {
	dir, err := os.MkdirTemp("", "ccpatch-patcher-fixture-")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0o600); err != nil {
		return "", err
	}
	src := `package main

import "os"

const (
	first  = "` + firstMarker + `"
	second = "` + secondMarker + `"
)

func main() {
	if len(os.Args) > 1 {
		os.Stdout.WriteString(first)
		os.Stdout.WriteString(second)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "fixture")
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %w: %s", err, out)
	}
	return bin, nil
}

func blank(s string) []byte { return bytes.Repeat([]byte(" "), len(s)) }

// twoSitePatch pins both sites to literals the fixture does not carry, so Apply
// always falls through to derive.
func twoSitePatch(derive func([]byte) ([]registry.Site, error)) registry.Patch {
	return registry.Patch{
		ID:          "acme/demo/two-sites",
		Summary:     "two sites",
		SegmentName: "__TEXT",
		Sites: []registry.Site{
			{Anchor: "first", Find: []byte("drifted_first_literal"), Replace: blank("drifted_first_literal")},
			{Anchor: "second", Find: []byte("drifted_second_literal"), Replace: blank("drifted_second_literal")},
		},
		Derive: derive,
	}
}

func derivedSite(anchor, marker string) registry.Site {
	return registry.Site{Anchor: anchor + " (derived)", Find: []byte(marker), Replace: blank(marker)}
}

// TestApplyRefusesShortDerive proves a derive that covers only some of a patch's
// sites fails loudly. Apply replaces the whole site list with what Derive
// returns, so a short list would patch one site, report "patched (derived)", and
// persist that short list as the version's override.
func TestApplyRefusesShortDerive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	inst := fixtureInstall(t)
	pristine, err := os.ReadFile(inst.Binary)
	if err != nil {
		t.Fatal(err)
	}
	p := twoSitePatch(func([]byte) ([]registry.Site, error) {
		return []registry.Site{derivedSite("first", firstMarker)}, nil
	})

	_, err = Apply(context.Background(), inst, p)
	if err == nil {
		t.Fatal("Apply accepted a derive covering 1 of 2 sites")
	}
	if !strings.Contains(err.Error(), "second") {
		t.Errorf("error = %q, want it to name the uncovered anchor %q", err, "second")
	}

	after, err := os.ReadFile(inst.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pristine, after) {
		t.Error("binary changed despite the refusal")
	}
	if _, err := os.Stat(inst.Backup()); err == nil {
		t.Error("backup written despite the refusal")
	}
}

func TestApplyAcceptsFullDerive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	inst := fixtureInstall(t)
	p := twoSitePatch(func([]byte) ([]registry.Site, error) {
		return []registry.Site{
			derivedSite("first", firstMarker),
			derivedSite("second", secondMarker),
		}, nil
	})

	out, err := Apply(context.Background(), inst, p)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Changed || !out.Derived {
		t.Fatalf("outcome = %+v, want a changed, derived apply", out)
	}
	patched, err := os.ReadFile(inst.Binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{firstMarker, secondMarker} {
		if bytes.Contains(patched, []byte(marker)) {
			t.Errorf("%s survived the apply", marker)
		}
	}
	if err := exec.Command(inst.Binary).Run(); err != nil {
		t.Errorf("patched binary no longer executes: %v", err)
	}
}

func TestUncovered(t *testing.T) {
	pinned := []registry.Site{{Anchor: "first"}, {Anchor: "second"}}
	tests := []struct {
		name    string
		derived []registry.Site
		want    []string
	}{
		{name: "none", derived: []registry.Site{{Anchor: "first (derived)"}, {Anchor: "second (derived)"}}},
		{name: "pinned site re-emitted verbatim", derived: []registry.Site{{Anchor: "first"}, {Anchor: "second (derived)"}}},
		{name: "one missing", derived: []registry.Site{{Anchor: "first (derived)"}}, want: []string{"second"}},
		{name: "all missing", derived: nil, want: []string{"first", "second"}},
		{name: "wrong anchor", derived: []registry.Site{{Anchor: "third (derived)"}}, want: []string{"first", "second"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uncovered(pinned, tt.derived); !slices.Equal(got, tt.want) {
				t.Errorf("uncovered() = %v, want %v", got, tt.want)
			}
		})
	}
}
