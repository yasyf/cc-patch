// Package binpatch applies length-neutral byte substitutions to a Mach-O binary
// and re-signs it ad-hoc, keeping a pristine backup for rollback.
package binpatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yasyf/cc-patch/internal/macho"
)

// ErrDrift means a substitution's Find bytes were not present and the site was
// not already patched — the target has shifted and the substitution must be
// re-derived.
var ErrDrift = errors.New("patch site not found (binary drifted)")

// Substitution is one length-neutral edit: every occurrence context of Find is
// replaced by Replace, which must be the same length.
type Substitution struct {
	Find    []byte
	Replace []byte
}

// State is the resolved condition of one substitution against a binary.
type State int

const (
	// StatePatched means Replace is already present and Find is absent.
	StatePatched State = iota
	// StateUnpatched means Find is present exactly once, ready to apply.
	StateUnpatched
	// StateMissing means neither Find nor Replace is present (drift).
	StateMissing
)

func (s State) String() string {
	switch s {
	case StatePatched:
		return "patched"
	case StateUnpatched:
		return "unpatched"
	case StateMissing:
		return "missing"
	default:
		return "unknown"
	}
}

// SiteStatus reports one substitution's resolved state.
type SiteStatus struct {
	Index int
	State State
}

// Result is the outcome of Apply or Status.
type Result struct {
	// Changed is true when Apply wrote bytes and re-signed.
	Changed bool
	Sites   []SiteStatus
}

// Patched reports whether every site is in StatePatched.
func (r Result) Patched() bool {
	for _, s := range r.Sites {
		if s.State != StatePatched {
			return false
		}
	}
	return len(r.Sites) > 0
}

type resolved struct {
	sub    Substitution
	state  State
	offset int64
}

func evaluate(data []byte, seg macho.Segment, subs []Substitution) ([]resolved, error) {
	if seg.Offset < 0 || seg.Offset+seg.Size > int64(len(data)) {
		return nil, fmt.Errorf("segment %s [%d,%d) outside file of %d bytes", seg.Name, seg.Offset, seg.Offset+seg.Size, len(data))
	}
	window := data[seg.Offset : seg.Offset+seg.Size]
	out := make([]resolved, len(subs))
	for i, sub := range subs {
		if len(sub.Find) != len(sub.Replace) {
			return nil, fmt.Errorf("substitution %d: find (%d bytes) and replace (%d bytes) differ in length", i, len(sub.Find), len(sub.Replace))
		}
		nFind := bytes.Count(window, sub.Find)
		switch {
		case nFind == 1:
			rel := bytes.Index(window, sub.Find)
			out[i] = resolved{sub: sub, state: StateUnpatched, offset: seg.Offset + int64(rel)}
		case nFind == 0 && bytes.Contains(window, sub.Replace):
			out[i] = resolved{sub: sub, state: StatePatched}
		case nFind == 0:
			out[i] = resolved{sub: sub, state: StateMissing}
		default:
			return nil, fmt.Errorf("substitution %d: %d matches for find in %s (expected 1)", i, nFind, seg.Name)
		}
	}
	return out, nil
}

func statuses(rs []resolved) []SiteStatus {
	out := make([]SiteStatus, len(rs))
	for i, r := range rs {
		out[i] = SiteStatus{Index: i, State: r.state}
	}
	return out
}

// Window returns a copy of the named segment's bytes, for structural derivation.
func Window(binary, segName string) ([]byte, error) {
	seg, err := macho.FindSegment(binary, segName)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return nil, fmt.Errorf("read binary %q: %w", binary, err)
	}
	if seg.Offset < 0 || seg.Offset+seg.Size > int64(len(data)) {
		return nil, fmt.Errorf("segment %s [%d,%d) outside file of %d bytes", seg.Name, seg.Offset, seg.Offset+seg.Size, len(data))
	}
	return bytes.Clone(data[seg.Offset : seg.Offset+seg.Size]), nil
}

// Status resolves every substitution against the binary without modifying it.
func Status(binary, segName string, subs []Substitution) (Result, error) {
	seg, err := macho.FindSegment(binary, segName)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return Result{}, fmt.Errorf("read binary %q: %w", binary, err)
	}
	rs, err := evaluate(data, seg, subs)
	if err != nil {
		return Result{}, err
	}
	return Result{Sites: statuses(rs)}, nil
}

// Apply patches every unpatched site in place and re-signs the binary. It backs
// the pristine original up to backup before the first write, is idempotent when
// already patched, and returns ErrDrift when any site is missing.
func Apply(ctx context.Context, binary, backup, segName string, subs []Substitution) (Result, error) {
	seg, err := macho.FindSegment(binary, segName)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return Result{}, fmt.Errorf("read binary %q: %w", binary, err)
	}
	rs, err := evaluate(data, seg, subs)
	if err != nil {
		return Result{}, err
	}
	var missing []int
	var pending []resolved
	for i, r := range rs {
		switch r.state {
		case StateMissing:
			missing = append(missing, i)
		case StateUnpatched:
			pending = append(pending, r)
		}
	}
	if len(missing) > 0 {
		return Result{Sites: statuses(rs)}, fmt.Errorf("%w: sites %v absent in %s", ErrDrift, missing, segName)
	}
	if len(pending) == 0 {
		return Result{Changed: false, Sites: statuses(rs)}, nil
	}
	if err := ensureBackup(binary, backup); err != nil {
		return Result{}, err
	}
	for _, r := range pending {
		copy(data[r.offset:r.offset+int64(len(r.sub.Replace))], r.sub.Replace)
	}
	if err := writeInPlace(binary, data); err != nil {
		return Result{}, err
	}
	if err := resign(ctx, binary); err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Sites: statuses(rs)}, nil
}

// Restore rewrites the binary from its pristine backup and removes the backup.
// The restored file carries the original vendor signature, so no re-sign runs.
func Restore(binary, backup string) error {
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("read backup %q: %w", backup, err)
	}
	if err := writeInPlace(binary, data); err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("remove backup %q: %w", backup, err)
	}
	return nil
}

func ensureBackup(binary, backup string) error {
	if _, err := os.Stat(backup); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat backup %q: %w", backup, err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("read binary for backup: %w", err)
	}
	if err := writeAtomic(backup, data, 0o755); err != nil {
		return fmt.Errorf("write backup %q: %w", backup, err)
	}
	return nil
}

// writeInPlace replaces the binary via a same-directory temp file and rename, so
// a currently-executing copy (ETXTBSY) is never written through.
func writeInPlace(binary string, data []byte) error {
	return writeAtomic(binary, data, 0o755)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ccpatch-*")
	if err != nil {
		return fmt.Errorf("create temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename over %q: %w", path, err)
	}
	return nil
}

func resign(ctx context.Context, binary string) error {
	// The in-place edit invalidates the vendor hardened-runtime signature; replace
	// it with an ad-hoc signature so the kernel will still exec the binary.
	_ = exec.CommandContext(ctx, "codesign", "--remove-signature", binary).Run()
	out, err := exec.CommandContext(ctx, "codesign", "--force", "--sign", "-", binary).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign %q: %w: %s", binary, err, strings.TrimSpace(string(out)))
	}
	return nil
}
