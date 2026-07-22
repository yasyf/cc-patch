// Package daemon installs the launchd agents that keep the patch applied: a
// watcher that re-patches whenever Claude Code updates, and a daily heal job.
package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yasyf/daemonkit/service"

	"github.com/yasyf/cc-patch/internal/claude"
)

const (
	watchLabel = "com.yasyf.cc-patch.watch"
	healLabel  = "com.yasyf.cc-patch.heal"
)

// Agents returns the exact desired agent set for an install: one watcher, one
// daily heal. Both run the current cc-patch executable.
func Agents(inst claude.Install) ([]service.Agent, error) {
	logDir, err := logDir()
	if err != nil {
		return nil, err
	}
	watch := service.Agent{
		Label:         watchLabel,
		Args:          []string{"apply", "--all"},
		LogPath:       filepath.Join(logDir, "watch.log"),
		RestartPolicy: service.NoRestart,
		WatchPaths:    []string{inst.VersionsDir, inst.Launcher},
	}
	heal := service.Agent{
		Label:                 healLabel,
		Args:                  []string{"heal", "--all"},
		LogPath:               filepath.Join(logDir, "heal.log"),
		RestartPolicy:         service.NoRestart,
		StartCalendarInterval: []service.CalendarInterval{service.Daily(3, 30)},
	}
	return []service.Agent{watch, heal}, nil
}

// Install renders and loads every agent, replacing any prior copy.
func Install(ctx context.Context, inst claude.Install) error {
	agents, err := Agents(inst)
	if err != nil {
		return err
	}
	for _, a := range agents {
		if err := load(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall unloads and removes every agent.
func Uninstall(ctx context.Context) error {
	for _, label := range []string{watchLabel, healLabel} {
		if err := unload(ctx, label); err != nil {
			return err
		}
	}
	return nil
}

// Labels returns the installed agents' launchd labels.
func Labels() []string { return []string{watchLabel, healLabel} }

func load(ctx context.Context, a service.Agent) error {
	plist, err := a.Plist()
	if err != nil {
		return fmt.Errorf("render plist for %s: %w", a.Label, err)
	}
	path, err := a.PlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.LogPath), 0o700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(path, plist, 0o600); err != nil {
		return fmt.Errorf("write plist %q: %w", path, err)
	}
	// bootout is best-effort: it fails when nothing is loaded yet.
	_ = launchctl(ctx, "bootout", serviceTarget(a.Label))
	if out, err := launchctlOut(ctx, "bootstrap", domainTarget(), path); err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %w: %s", a.Label, err, out)
	}
	if out, err := launchctlOut(ctx, "enable", serviceTarget(a.Label)); err != nil {
		return fmt.Errorf("launchctl enable %s: %w: %s", a.Label, err, out)
	}
	return nil
}

func unload(ctx context.Context, label string) error {
	path, err := service.Agent{Label: label}.PlistPath()
	if err != nil {
		return err
	}
	_ = launchctl(ctx, "bootout", serviceTarget(label))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist %q: %w", path, err)
	}
	return nil
}

func launchctl(ctx context.Context, args ...string) error {
	_, err := launchctlOut(ctx, args...)
	return err
}

func launchctlOut(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func domainTarget() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func serviceTarget(label string) string { return domainTarget() + "/" + label }

func logDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "cc-patch"), nil
}
