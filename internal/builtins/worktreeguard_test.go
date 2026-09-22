package builtins

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/registry"
)

const (
	guardHome = "/home/tester"
	guardCwd  = "/home/tester/.claude/worktrees/repo/lane"
)

type homeState int

const (
	homeSet homeState = iota
	homeUnset
)

// result is what the resolver answered: a resolved directory, or a refusal the
// guard reports as a path it cannot verify.
type result struct {
	Refused bool   `json:"refused"`
	Path    string `json:"path"`
}

func resolved(path string) result { return result{Path: path} }

var refused = result{Refused: true}

type resolverCase struct {
	name string
	word string
	// absolute and glob are the resolver's mustBeAbsolute and hasUnquotedGlob flags.
	absolute bool
	glob     bool
	home     homeState
	before   result
	after    result
}

// The pinned bytes refuse every tilde, so the guard reports a location computed
// at runtime; the patched bytes resolve a leading `~/` and let the guard judge
// the real directory.
var resolverCases = []resolverCase{
	{
		name:   "an unrelated repository",
		word:   "~/Code/other-repo",
		before: refused,
		after:  resolved("/home/tester/Code/other-repo"),
	},
	{
		name:   "a sibling worktree of this repository",
		word:   "~/.claude/worktrees/repo/other",
		before: refused,
		after:  resolved("/home/tester/.claude/worktrees/repo/other"),
	},
	{
		name:   "the shared checkout",
		word:   "~/Code/repo",
		before: refused,
		after:  resolved("/home/tester/Code/repo"),
	},
	{
		name:   "a bare tilde",
		word:   "~",
		before: refused,
		after:  refused,
	},
	{
		name:   "another user's home directory",
		word:   "~root/Code/repo",
		before: refused,
		after:  refused,
	},
	{
		name:   "a tilde that does not lead",
		word:   "/tmp/a~b",
		before: refused,
		after:  refused,
	},
	{
		name:   "a home-relative path with HOME unset",
		word:   "~/Code/repo",
		home:   homeUnset,
		before: refused,
		after:  refused,
	},
	{
		name:   "a glob under the home directory",
		word:   "~/Code/*/src",
		glob:   true,
		before: refused,
		after:  refused,
	},
	{
		name:   "a NUL byte",
		word:   "/tmp/a\x00b",
		before: refused,
		after:  refused,
	},
	{
		name:   "a brace expansion",
		word:   "/tmp/a{}b",
		before: refused,
		after:  refused,
	},
	{
		name:   "a device path",
		word:   "/dev/fd/3",
		before: refused,
		after:  refused,
	},
	{
		name:   "a volume path",
		word:   "/Volumes/disk/repo",
		before: refused,
		after:  refused,
	},
	{
		name:   "a relative path",
		word:   "sub/dir",
		before: resolved("/home/tester/.claude/worktrees/repo/lane/sub/dir"),
		after:  resolved("/home/tester/.claude/worktrees/repo/lane/sub/dir"),
	},
	{
		name:     "a relative path where the caller demands an absolute one",
		word:     "sub/dir",
		absolute: true,
		before:   refused,
		after:    refused,
	},
}

func TestExpandLeadingTilde(t *testing.T) {
	site := resolverSite(t)
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node runs the resolver under test: %v", err)
	}
	harness, err := filepath.Abs(filepath.Join("testdata", "resolver-harness.mjs"))
	if err != nil {
		t.Fatal(err)
	}

	for _, variant := range []struct {
		name string
		js   []byte
		want func(resolverCase) result
	}{
		{"pinned", site.Find, func(c resolverCase) result { return c.before }},
		{"patched", site.Replace, func(c resolverCase) result { return c.after }},
	} {
		t.Run(variant.name, func(t *testing.T) {
			dir := t.TempDir()
			resolver := filepath.Join(dir, "resolver.js")
			if err := os.WriteFile(resolver, variant.js, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, home := range []homeState{homeSet, homeUnset} {
				cases := homeCases(home)
				if len(cases) == 0 {
					continue
				}
				got := runResolver(t, node, harness, dir, resolver, home, cases)
				for i, c := range cases {
					if got[i] != variant.want(c) {
						t.Errorf("%s: resolver answered %+v, want %+v", c.name, got[i], variant.want(c))
					}
				}
			}
		})
	}
}

func homeCases(home homeState) []resolverCase {
	var out []resolverCase
	for _, c := range resolverCases {
		if c.home == home {
			out = append(out, c)
		}
	}
	return out
}

func runResolver(t *testing.T, node, harness, dir, resolver string, home homeState, cases []resolverCase) []result {
	t.Helper()
	args := make([][4]any, len(cases))
	for i, c := range cases {
		args[i] = [4]any{c.word, guardCwd, c.absolute, c.glob}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	casesPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(casesPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(node, harness, resolver, casesPath)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "HOME=")
	})
	if home == homeSet {
		cmd.Env = append(cmd.Env, "HOME="+guardHome)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node %s: %v", resolver, err)
	}

	var got []result
	decoder := json.NewDecoder(bytes.NewReader(out))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(got) != len(cases) {
		t.Fatalf("got %d results, want %d", len(got), len(cases))
	}
	return got
}

func resolverSite(t *testing.T) registry.Site {
	t.Helper()
	return sites(t, "expand-leading-tilde")[0]
}

func sites(t *testing.T, id string) []registry.Site {
	t.Helper()
	patches, err := compile(t, "worktreeguard")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	i := slices.IndexFunc(patches, func(p registry.Patch) bool {
		return strings.HasSuffix(p.ID, "/"+id)
	})
	if i < 0 {
		t.Fatalf("worktreeguard has no %s patch", id)
	}
	if len(patches[i].Sites) != 1 {
		t.Fatalf("%s has %d sites, want 1", id, len(patches[i].Sites))
	}
	return patches[i].Sites
}

// TestWorktreeGuardDerivesFromRealBundle proves each pinned site is re-locatable
// by its derive pattern in the installed release, and that the derive renders the
// pinned edit rather than some other length-neutral one.
func TestWorktreeGuardDerivesFromRealBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("scans the full bundle")
	}
	inst, err := claude.Locate()
	if err != nil {
		t.Skipf("no claude install: %v", err)
	}
	binary := inst.Binary
	if _, err := os.Stat(inst.Backup()); err == nil {
		binary = inst.Backup()
	}
	patches, err := compile(t, "worktreeguard")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, p := range patches {
		t.Run(p.ID, func(t *testing.T) {
			window, err := binpatch.Window(binary, p.SegmentName)
			if err != nil {
				t.Skipf("read bundle window: %v", err)
			}
			derived, err := p.Derive(window)
			if err != nil {
				t.Fatalf("derive on real bundle: %v", err)
			}
			res, err := binpatch.Status(binary, p.SegmentName, registry.Substitutions(derived))
			if err != nil {
				t.Fatalf("status with derived sites: %v", err)
			}
			for _, s := range res.Sites {
				if s.State == binpatch.StateMissing {
					t.Errorf("derived site %d not found in the real binary", s.Index)
				}
			}
			if len(derived) != len(p.Sites) {
				t.Fatalf("derived %d sites, want %d", len(derived), len(p.Sites))
			}
			for i, d := range derived {
				want := p.Sites[i].Substitution()
				if got := d.Substitution(); !bytes.Equal(got.Find, want.Find) || !bytes.Equal(got.Replace, want.Replace) {
					t.Errorf("derived site %d renders %q -> %q, want %q -> %q", i, got.Find, got.Replace, want.Find, want.Replace)
				}
			}
		})
	}
}
