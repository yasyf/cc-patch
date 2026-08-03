package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yasyf/daemonkit/launchd"

	"github.com/yasyf/cc-patch/internal/claude"
)

func TestPlanRendersExpectedPlistsDeterministically(t *testing.T) {
	sandbox(t)
	placeFakeProgram(t)
	inst := testInstall()
	first, err := Plan(inst)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Plan(inst)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("Plan digest changed: %s != %s", first.Digest(), second.Digest())
	}

	agents := agentsByLabel(first.Agents())
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}
	assertPlistContains(
		t, agents[watchLabel],
		"<key>WatchPaths</key>",
		"<string>/Users/x/.local/share/claude/versions</string>",
		"<string>/Users/x/.local/bin/claude</string>",
		"<string>apply</string>",
		"<string>--all</string>",
	)
	assertPlistContains(
		t, agents[healLabel],
		"<key>StartCalendarInterval</key>",
		"<key>Hour</key>",
		"<integer>3</integer>",
		"<key>Minute</key>",
		"<integer>30</integer>",
		"<string>heal</string>",
	)
}

// TestPlanRegistersTheVersionStableProgramPath guards the property CHANGELOG
// 0.12.1 landed: the agents must not name the versioned install directory,
// which a package upgrade deletes out from under launchd.
func TestPlanRegistersTheVersionStableProgramPath(t *testing.T) {
	sandbox(t)
	want := placeFakeProgram(t)
	plan, err := Plan(testInstall())
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range plan.Agents() {
		if agent.Program != want {
			t.Errorf("%s program = %q, want %q", agent.Label, agent.Program, want)
		}
		if agent.Program == exe {
			t.Errorf("%s registers the invoking executable %q, not the stable copy", agent.Label, exe)
		}
		if filepath.Base(filepath.Dir(agent.Program)) != "bin" {
			t.Errorf("%s program %q does not sit in the stable bin directory", agent.Label, agent.Program)
		}
	}
}

func TestProgramPathIgnoresCallerHome(t *testing.T) {
	home := sandbox(t)
	t.Setenv("HOME", t.TempDir())
	got, err := programPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".daemonkit", "bin", "cc-patch"); got != want {
		t.Fatalf("programPath() = %q, want %q", got, want)
	}
}

func TestPlaceProgramCopiesTheExecutableThenLeavesItAlone(t *testing.T) {
	sandbox(t)
	if err := placeProgram(); err != nil {
		t.Fatal(err)
	}
	target, err := programPath()
	if err != nil {
		t.Fatal(err)
	}
	source, err := programSource()
	if err != nil {
		t.Fatal(err)
	}
	want, err := placedDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := placedDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("placed digest = %q, want the executable's %q", got, want)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("placed program mode = %v, want executable", info.Mode().Perm())
	}

	if err := placeProgram(); err != nil {
		t.Fatal(err)
	}
	repeat, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !repeat.ModTime().Equal(info.ModTime()) {
		t.Error("a second placement rewrote a copy that already held these bytes")
	}
}

func TestPlaceProgramRewritesDivergedBytes(t *testing.T) {
	sandbox(t)
	target, err := programPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // launchd refuses a program without the executable bit
	if err := os.WriteFile(target, []byte("a stale build"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := placeProgram(); err != nil {
		t.Fatal(err)
	}
	source, err := programSource()
	if err != nil {
		t.Fatal(err)
	}
	want, err := placedDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := placedDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("placed digest = %q, want the executable's %q", got, want)
	}
}

func TestInstallAppliesEveryPlannedAgent(t *testing.T) {
	sandbox(t)
	recorder := &recordingLaunchd{}
	useLaunchd(t, recorder)

	if err := Install(t.Context(), testInstall()); err != nil {
		t.Fatal(err)
	}
	applied := agentsByLabel(recorder.applied)
	if len(applied) != 2 || applied[watchLabel].Label == "" || applied[healLabel].Label == "" {
		t.Fatalf("applied agents = %#v, want watch and heal", recorder.applied)
	}
	if len(recorder.removed) != 0 {
		t.Fatalf("removed labels = %#v, want none", recorder.removed)
	}
	target, err := programPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("Install did not place the program copy: %v", err)
	}
}

func TestInstallFailsOnTheFirstRefusedAgent(t *testing.T) {
	sandbox(t)
	applyErr := errors.New("apply failed")
	recorder := &recordingLaunchd{applyErr: applyErr}
	useLaunchd(t, recorder)

	err := Install(t.Context(), testInstall())
	if !errors.Is(err, applyErr) {
		t.Fatalf("Install error = %v, want %v", err, applyErr)
	}
	if len(recorder.applied) != 1 {
		t.Fatalf("applied agents = %#v, want exactly the first", recorder.applied)
	}
}

func TestUninstallRemovesEveryLabelAndTheProgramCopy(t *testing.T) {
	sandbox(t)
	placeFakeProgram(t)
	target, err := programPath()
	if err != nil {
		t.Fatal(err)
	}
	sidecar := target + ".meta.json"
	if err := os.WriteFile(sidecar, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingLaunchd{}
	useLaunchd(t, recorder)

	if err := Uninstall(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := recorder.removed, Labels(); !slices.Equal(got, want) {
		t.Fatalf("removed labels = %#v, want %#v", got, want)
	}
	if len(recorder.applied) != 0 {
		t.Fatalf("applied agents = %#v, want none", recorder.applied)
	}
	for _, path := range []string{target, sidecar} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q survived Uninstall", path)
		}
	}
}

// TestUninstallFallsBackToLegacyRemovalOnErrNotOwned covers the markerless
// plists every pre-v0.21 cc-patch install left behind: launchd.Remove refuses
// them, and without the fallback both agents stay loaded forever.
func TestUninstallFallsBackToLegacyRemovalOnErrNotOwned(t *testing.T) {
	sandbox(t)
	plists := make(map[string]string, len(Labels()))
	for _, label := range Labels() {
		path, err := launchd.Agent{Label: label}.PlistPath()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("<plist>markerless</plist>"), 0o600); err != nil {
			t.Fatal(err)
		}
		plists[label] = path
	}
	useLaunchd(t, &recordingLaunchd{removeErr: launchd.ErrNotOwned})
	runner := &recordingLaunchctl{}
	useLaunchctl(t, runner)

	if err := Uninstall(t.Context()); err != nil {
		t.Fatalf("Uninstall error = %v, want the legacy removal to succeed", err)
	}
	for label, path := range plists {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("legacy plist for %q survived: %v", label, err)
		}
		if !runner.bootedOut(label) {
			t.Errorf("%q was never booted out; calls = %#v", label, runner.calls)
		}
	}
}

func TestUninstallAttemptsEveryLabelAndJoinsFailures(t *testing.T) {
	sandbox(t)
	removeErr := errors.New("remove failed")
	recorder := &recordingLaunchd{removeErr: removeErr}
	useLaunchd(t, recorder)

	err := Uninstall(t.Context())
	if !errors.Is(err, removeErr) {
		t.Fatalf("Uninstall error = %v, want %v", err, removeErr)
	}
	if got, want := recorder.removed, Labels(); !slices.Equal(got, want) {
		t.Fatalf("removed labels = %#v, want every label attempted %#v", got, want)
	}
	for _, label := range Labels() {
		if !strings.Contains(err.Error(), label) {
			t.Errorf("Uninstall error %v does not name %q", err, label)
		}
	}
}

type recordingLaunchd struct {
	applied   []launchd.Agent
	removed   []string
	applyErr  error
	removeErr error
}

func (r *recordingLaunchd) apply(_ context.Context, agent launchd.Agent) error {
	r.applied = append(r.applied, agent)
	return r.applyErr
}

func (r *recordingLaunchd) remove(_ context.Context, label string) error {
	r.removed = append(r.removed, label)
	return r.removeErr
}

func useLaunchd(t *testing.T, recorder *recordingLaunchd) {
	t.Helper()
	previousApply, previousRemove := applyAgent, removeAgent
	applyAgent, removeAgent = recorder.apply, recorder.remove
	t.Cleanup(func() { applyAgent, removeAgent = previousApply, previousRemove })
}

type recordingLaunchctl struct {
	calls [][]string
}

func (r *recordingLaunchctl) run(_ context.Context, path string, args ...string) (string, int, error) {
	r.calls = append(r.calls, append([]string{path}, args...))
	return "", 0, nil
}

func (r *recordingLaunchctl) bootedOut(label string) bool {
	return slices.ContainsFunc(r.calls, func(call []string) bool {
		return len(call) == 3 && call[1] == "bootout" && strings.HasSuffix(call[2], "/"+label)
	})
}

func useLaunchctl(t *testing.T, runner *recordingLaunchctl) {
	t.Helper()
	previous := launchctl
	launchctl = runner.run
	t.Cleanup(func() { launchctl = previous })
}

// sandbox relocates every daemonkit-reached path — the program copy, the plist
// directory, the log directory — into a temp home. HOME is not the seam:
// daemonkit resolves through the passwd database and never reads it.
func sandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(homeEnvOverride, home)
	return home
}

// placeFakeProgram writes an executable stand-in at the stable program path, so
// a test that only needs launchd.NewPlan's residency check satisfied does not
// copy the whole test binary.
func placeFakeProgram(t *testing.T) string {
	t.Helper()
	target, err := programPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // launchd refuses a program without the executable bit
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := program()
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func testInstall() claude.Install {
	return claude.Install{
		Launcher:    "/Users/x/.local/bin/claude",
		VersionsDir: "/Users/x/.local/share/claude/versions",
		Binary:      "/Users/x/.local/share/claude/versions/2.1.217",
		Version:     "2.1.217",
	}
}

func agentsByLabel(agents []launchd.Agent) map[string]launchd.Agent {
	byLabel := make(map[string]launchd.Agent, len(agents))
	for _, agent := range agents {
		byLabel[agent.Label] = agent
	}
	return byLabel
}

func assertPlistContains(t *testing.T, agent launchd.Agent, wants ...string) {
	t.Helper()
	if agent.Label == "" {
		t.Fatal("agent is missing")
	}
	plist, err := agent.Plist()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range wants {
		if !strings.Contains(string(plist), want) {
			t.Errorf("%s plist missing %q", agent.Label, want)
		}
	}
}
