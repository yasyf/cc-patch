// Package daemon installs the launchd agents that keep the patch applied: a
// watcher that re-patches whenever Claude Code updates, and a daily heal job.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/yasyf/daemonkit/durable"
	"github.com/yasyf/daemonkit/launchd"

	"github.com/yasyf/cc-patch/internal/claude"
)

const (
	watchLabel = "com.yasyf.cc-patch.watch"
	healLabel  = "com.yasyf.cc-patch.heal"

	// programName is the leaf of the version-stable program path. It is the
	// name daemonkit v0.20's service.StableProgram used, because the plists
	// already on disk register that exact absolute path.
	programName = "cc-patch"

	// homeEnvOverride mirrors daemonkit's realhome seam. daemonkit resolves
	// every durable path through the passwd database and never reads HOME, so
	// this is the only variable that relocates the program copy with it.
	homeEnvOverride = "DAEMONKIT_HOME"
)

var (
	applyAgent = func(ctx context.Context, agent launchd.Agent) error {
		return launchd.Apply(ctx, launchctl, agent)
	}
	removeAgent = func(ctx context.Context, label string) error {
		return launchd.Remove(ctx, launchctl, label)
	}
	launchctl launchd.Runner = execLaunchctl
)

// Plan returns the exact desired agent set for an install: one watcher, one
// daily heal. Both run the copy at the version-stable program path, which
// Install places before planning — launchd.NewPlan requires a resident program.
func Plan(inst claude.Install) (launchd.Plan, error) {
	logDir, err := logDir()
	if err != nil {
		return launchd.Plan{}, err
	}
	program, err := program()
	if err != nil {
		return launchd.Plan{}, err
	}
	watch := launchd.Agent{
		Label:         watchLabel,
		Program:       program,
		Args:          []string{"apply", "--all"},
		LogPath:       filepath.Join(logDir, "watch.log"),
		RestartPolicy: launchd.NoRestart,
		WatchPaths:    []string{inst.VersionsDir, inst.Launcher},
	}
	heal := launchd.Agent{
		Label:                 healLabel,
		Program:               program,
		Args:                  []string{"heal", "--all"},
		LogPath:               filepath.Join(logDir, "heal.log"),
		RestartPolicy:         launchd.NoRestart,
		StartCalendarInterval: []launchd.CalendarInterval{launchd.Daily(3, 30)},
	}
	plan, err := launchd.NewPlan([]launchd.Agent{watch, heal})
	if err != nil {
		return launchd.Plan{}, fmt.Errorf("build daemon agent plan: %w", err)
	}
	return plan, nil
}

// Install places the program copy, then installs or repairs every planned agent
// and kickstarts it.
func Install(ctx context.Context, inst claude.Install) error {
	if err := placeProgram(); err != nil {
		return err
	}
	plan, err := Plan(inst)
	if err != nil {
		return err
	}
	for _, agent := range plan.Agents() {
		if err := applyAgent(ctx, agent); err != nil {
			return fmt.Errorf("apply daemon agent %q: %w", agent.Label, err)
		}
	}
	return nil
}

// Uninstall boots out and deletes every agent this package installs, then
// removes the program copy. Every label is attempted, so one refusal does not
// strand the rest.
func Uninstall(ctx context.Context) error {
	var errs []error
	for _, label := range Labels() {
		if err := removeAgent(ctx, label); err != nil {
			errs = append(errs, fmt.Errorf("remove daemon agent %q: %w", label, err))
		}
	}
	if err := removeProgram(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Labels returns the installed agents' launchd labels.
func Labels() []string { return []string{watchLabel, healLabel} }

// placeProgram makes the version-stable program path hold this executable's
// bytes, writing only when it does not already, so re-converging a settled
// system leaves the inode launchd already knows alone.
func placeProgram() error {
	source, err := programSource()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read program source %q: %w", source, err)
	}
	target, err := programPath()
	if err != nil {
		return err
	}
	held, err := placedDigest(target)
	if err != nil {
		return err
	}
	if held == digest(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create program directory: %w", err)
	}
	if err := durable.WriteFile(target, data, 0o700); err != nil {
		return fmt.Errorf("place program at %q: %w", target, err)
	}
	return nil
}

// removeProgram deletes the program copy and the sidecar daemonkit v0.20 wrote
// beside it. An absent file is not a failure.
func removeProgram() error {
	target, err := programPath()
	if err != nil {
		return err
	}
	for _, path := range []string{target, target + ".meta.json"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove program %q: %w", path, err)
		}
	}
	return nil
}

// program is the path the agents register: the placed copy, its directory
// symlink-resolved because launchd refuses a symlink anywhere in a program
// path. The final component is never followed, so a link substituted for the
// copy cannot be registered in its place.
func program() (string, error) {
	target, err := programPath()
	if err != nil {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", fmt.Errorf("resolve program directory: %w", err)
	}
	resolved := filepath.Join(dir, filepath.Base(target))
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect program %q: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("program %q is not a regular file", resolved)
	}
	return resolved, nil
}

// programPath is ~/.daemonkit/bin/cc-patch: outside any versioned install
// directory, so a package upgrade that deletes the directory this binary was
// installed into cannot delete the program launchd runs.
func programPath() (string, error) {
	home, err := realHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".daemonkit", "bin", programName), nil
}

func programSource() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve current executable %q: %w", exe, err)
	}
	return resolved, nil
}

func placedDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read program %q: %w", path, err)
	}
	return digest(data), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// realHome mirrors daemonkit's realhome.Dir: the invoking user's home from the
// passwd database, never HOME, so Homebrew postinstall's sandboxed environment
// cannot redirect these paths into a directory that is about to vanish.
func realHome() (string, error) {
	if override := os.Getenv(homeEnvOverride); override != "" {
		return override, nil
	}
	current, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolve passwd entry: %w", err)
	}
	if current.HomeDir == "" {
		return "", fmt.Errorf("passwd entry for uid %s has no home directory", current.Uid)
	}
	return current.HomeDir, nil
}

func execLaunchctl(ctx context.Context, path string, args ...string) (string, int, error) {
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode(), nil
	}
	if err != nil {
		return string(out), 0, err
	}
	return string(out), 0, nil
}

func logDir() (string, error) {
	home, err := realHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "cc-patch"), nil
}
