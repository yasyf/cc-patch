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
