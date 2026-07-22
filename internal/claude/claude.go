// Package claude locates the installed Claude Code CLI on disk.
package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotInstalled indicates no Claude Code install could be resolved.
var ErrNotInstalled = errors.New("claude code install not found")

// Install is a resolved Claude Code installation.
type Install struct {
	// Launcher is ~/.local/bin/claude — the symlink auto-update repoints at each release.
	Launcher string
	// VersionsDir is the directory holding each versioned binary; auto-update drops new ones here.
	VersionsDir string
	// Binary is the real versioned executable the launcher resolves to. This is what gets patched.
	Binary string
	// Version is the binary's version, taken from its file name (e.g. "2.1.217").
	Version string
}

// Backup is the pristine-original path cc-patch keeps beside the versioned binary.
func (i Install) Backup() string { return i.Binary + ".ccpatch-orig" }

// Locate resolves the current install by following the launcher symlink.
func Locate() (Install, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Install{}, fmt.Errorf("resolve home dir: %w", err)
	}
	launcher := filepath.Join(home, ".local", "bin", "claude")
	binary, err := filepath.EvalSymlinks(launcher)
	if err != nil {
		return Install{}, fmt.Errorf("%w: resolve launcher %q: %w", ErrNotInstalled, launcher, err)
	}
	info, err := os.Stat(binary)
	if err != nil {
		return Install{}, fmt.Errorf("stat claude binary %q: %w", binary, err)
	}
	if !info.Mode().IsRegular() {
		return Install{}, fmt.Errorf("%w: %q is not a regular file", ErrNotInstalled, binary)
	}
	return Install{
		Launcher:    launcher,
		VersionsDir: filepath.Dir(binary),
		Binary:      binary,
		Version:     filepath.Base(binary),
	}, nil
}
