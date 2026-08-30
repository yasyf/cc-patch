package heal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/registry"
)

// TestValidateRejectsDropNotInFind proves the heal path rejects Claude-derived
// sites whose drop is not within find before Substitutions calls blank(), which
// would otherwise panic the process (and the daily-heal daemon).
func TestValidateRejectsDropNotInFind(t *testing.T) {
	sites := []registry.Site{{Anchor: "x", Find: []byte("hello"), Drop: []byte("world")}}
	if err := validate(claude.Install{}, registry.Patch{}, sites); err == nil {
		t.Fatal("expected validate to reject a drop that is not within find")
	}
}

func requireCodesignAndGo(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"codesign", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
}

func buildUnsignedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fixture")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}
	if out, err := exec.Command("codesign", "--remove-signature", bin).CombinedOutput(); err != nil {
		t.Fatalf("codesign --remove-signature: %v: %s", err, out)
	}
	return bin
}

func stubLauncher(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRederiveRejectsUnsignedBinaryBeforeExec proves the preflight turns the
// bricked-binary incident ("signal: killed") into an actionable error naming
// cc-patch restore, without ever spawning the launcher.
func TestRederiveRejectsUnsignedBinaryBeforeExec(t *testing.T) {
	requireCodesignAndGo(t)
	sentinel := filepath.Join(t.TempDir(), "executed")
	inst := claude.Install{
		Launcher: stubLauncher(t, "touch "+sentinel),
		Binary:   buildUnsignedFixture(t),
	}
	_, err := rederive(context.Background(), inst, registry.Patch{})
	if err == nil {
		t.Fatal("expected rederive to reject an unsigned binary")
	}
	if !strings.Contains(err.Error(), "cc-patch restore") {
		t.Fatalf("error does not name the remedy: %v", err)
	}
	if _, serr := os.Stat(sentinel); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("launcher was executed: stat sentinel: %v", serr)
	}
}

// TestRunClaudeTimesOut proves a hung claude -p dies at the deadline with an
// error that satisfies errors.Is(err, context.DeadlineExceeded) instead of a
// bare "signal: killed".
func TestRunClaudeTimesOut(t *testing.T) {
	prev := rederiveTimeout
	rederiveTimeout = 100 * time.Millisecond
	defer func() { rederiveTimeout = prev }()
	_, err := runClaude(context.Background(), stubLauncher(t, "exec sleep 5"), "prompt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error does not name the timeout: %v", err)
	}
}

// TestRunClaudeReadsResultEvent proves the envelope is read as the stream of
// events claude -p emits, whose result event carries the text, rather than as a
// single object with a result field.
func TestRunClaudeReadsResultEvent(t *testing.T) {
	envelope := `[{"type":"system","subtype":"init"},{"type":"assistant"},{"type":"result","subtype":"success","is_error":false,"result":"the sites"}]`
	got, err := runClaude(context.Background(), stubLauncher(t, "printf '%s' '"+envelope+"'"), "prompt")
	if err != nil {
		t.Fatalf("runClaude: %v", err)
	}
	if got != "the sites" {
		t.Fatalf("result = %q, want %q", got, "the sites")
	}
}

// TestRunClaudeSurfacesResultError proves an is_error result event fails loudly
// instead of feeding its error text downstream as re-derived sites.
func TestRunClaudeSurfacesResultError(t *testing.T) {
	envelope := `[{"type":"result","subtype":"error_max_turns","is_error":true,"result":"ran out of turns"}]`
	_, err := runClaude(context.Background(), stubLauncher(t, "printf '%s' '"+envelope+"'"), "prompt")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "error_max_turns") || !strings.Contains(err.Error(), "ran out of turns") {
		t.Fatalf("error does not carry the result event: %v", err)
	}
}

// TestRunClaudeRequiresResultEvent proves a stream that ends without a result
// event errors instead of returning empty text.
func TestRunClaudeRequiresResultEvent(t *testing.T) {
	envelope := `[{"type":"system","subtype":"init"},{"type":"assistant"}]`
	_, err := runClaude(context.Background(), stubLauncher(t, "printf '%s' '"+envelope+"'"), "prompt")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "no result event") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunClaudeSurfacesStderr proves a failing claude -p reports its stderr
// instead of a bare exit status.
func TestRunClaudeSurfacesStderr(t *testing.T) {
	_, err := runClaude(context.Background(), stubLauncher(t, `echo "boom from claude" >&2; exit 1`), "prompt")
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("crash misreported as timeout: %v", err)
	}
	if !strings.Contains(err.Error(), "boom from claude") {
		t.Fatalf("stderr not surfaced: %v", err)
	}
}

// TestCheckSitesAcceptsReplace proves a re-derived substitution site survives the
// invariant check, the path a pack with a replace site depends on for drift.
func TestCheckSitesAcceptsReplace(t *testing.T) {
	sites := []registry.Site{{Anchor: "a", Find: []byte(`x==="bash"`), Replace: []byte(`/g/.test(x)`)[:10]}}
	if err := checkSites(sites); err != nil {
		t.Fatalf("checkSites: %v", err)
	}
}

// TestCheckSitesRejectsBadSites proves each invariant pack.toml enforces at parse
// time is enforced on Claude's output too.
func TestCheckSitesRejectsBadSites(t *testing.T) {
	for name, tc := range map[string]struct {
		site registry.Site
		want string
	}{
		"replace wrong length": {registry.Site{Find: []byte("abcd"), Replace: []byte("ab")}, "differ in length"},
		"drop outside find":    {registry.Site{Find: []byte("abcd"), Drop: []byte("zz")}, "not within find"},
		"both":                 {registry.Site{Find: []byte("abcd"), Drop: []byte("ab"), Replace: []byte("wxyz")}, "both drop and replace"},
		"neither":              {registry.Site{Find: []byte("abcd")}, "neither drop nor replace"},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkSites([]registry.Site{tc.site})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
