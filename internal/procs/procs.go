// Package procs reports the Claude Code processes this user is running and
// whether each maps the binary now on disk. A patch edits a file, so it reaches
// only the processes that exec after it.
package procs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/yasyf/cc-patch/internal/claude"
)

// argcSize is the int32 argument count KERN_PROCARGS2 writes before the path.
const argcSize = 4

// Process is one running Claude Code process.
type Process struct {
	PID     int
	Started time.Time
	// The patcher publishes a patched binary by renaming a fresh file over the
	// old one, so a false Current proves the process predates the last write.
	Current bool
}

// Report is the running-process view of one install.
type Report struct {
	// Written is the binary's mtime — when the patcher last replaced it.
	Written   time.Time
	Processes []Process
	// Unidentified counts the running processes whose executable the kernel would
	// not name, so the process list is never silently short.
	Unidentified int
}

// Stale returns the processes that do not map the binary on disk.
func (r Report) Stale() []Process {
	var stale []Process
	for _, p := range r.Processes {
		if !p.Current {
			stale = append(stale, p)
		}
	}
	return stale
}

type candidate struct {
	pid     int
	started time.Time
}

// file identifies one file on this machine. An inode number is only unique
// within its device.
type file struct {
	device int64
	inode  uint64
}

// Inspect resolves every Claude Code process this user is running against the
// install's binary, newest first.
func Inspect(ctx context.Context, inst claude.Install) (Report, error) {
	info, err := os.Stat(inst.Binary)
	if err != nil {
		return Report{}, fmt.Errorf("stat binary %q: %w", inst.Binary, err)
	}
	found, unidentified, err := candidates(inst)
	if err != nil {
		return Report{}, err
	}
	report := Report{Written: info.ModTime(), Unidentified: unidentified}
	if len(found) == 0 {
		return report, nil
	}
	mapped, err := textFiles(ctx, found)
	if err != nil {
		return Report{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	report.Processes = classify(found, mapped, file{device: int64(stat.Dev), inode: stat.Ino})
	return report, nil
}

func classify(found []candidate, mapped map[int][]file, binary file) []Process {
	procs := make([]Process, 0, len(found))
	for _, c := range found {
		files, ok := mapped[c.pid]
		if !ok {
			// textFiles proved this pid is gone.
			continue
		}
		procs = append(procs, Process{PID: c.pid, Started: c.started, Current: slices.Contains(files, binary)})
	}
	slices.SortFunc(procs, func(a, b Process) int { return b.Started.Compare(a.Started) })
	return procs
}

// candidates finds the processes exec'd through the install's launcher. A
// process's accounting name is no use here: the kernel takes it from the
// resolved version file, so it names a version rather than the install, and
// names nothing at all once that file is deleted.
func candidates(inst claude.Install) ([]candidate, int, error) {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
	if err != nil {
		return nil, 0, fmt.Errorf("enumerate this user's processes: %w", err)
	}
	var found []candidate
	unidentified := 0
	for _, p := range all {
		pid := int(p.Proc.P_pid)
		path, err := execPath(pid)
		if err != nil {
			unidentified++
			continue
		}
		if path != inst.Launcher {
			continue
		}
		found = append(found, candidate{pid: pid, started: time.Unix(p.Proc.P_starttime.Unix())})
	}
	return found, unidentified, nil
}

// execPath reads the path a process's exec was given, which the kernel keeps in
// front of the saved argument vector.
func execPath(pid int) (string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", fmt.Errorf("read arguments of pid %d: %w", pid, err)
	}
	path, _, _ := bytes.Cut(raw[argcSize:], []byte{0})
	return string(path), nil
}

// textFiles maps each candidate pid to the executables it has mapped,
// separating a process on the current binary from one still running the file the
// patcher renamed away.
func textFiles(ctx context.Context, found []candidate) (map[int][]file, error) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return nil, fmt.Errorf("find lsof: %w", err)
	}
	pids := make([]string, len(found))
	for i, c := range found {
		pids[i] = strconv.Itoa(c.pid)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lsof, "-p", strings.Join(pids, ","), "-a", "-d", "txt", "-FpDi")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// lsof exits 1 when a listed pid has gone as well as when it fails, so the
	// exit check below is the gate: only a pid the kernel says is gone may be
	// missing from the output.
	_ = cmd.Run()
	mapped, err := parseTextFiles(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	for _, c := range found {
		if _, ok := mapped[c.pid]; ok {
			continue
		}
		if errors.Is(syscall.Kill(c.pid, 0), syscall.ESRCH) {
			continue
		}
		return nil, fmt.Errorf("lsof mapped no executable for pid %d, which has not exited: %s", c.pid, strings.TrimSpace(stderr.String()))
	}
	return mapped, nil
}

func parseTextFiles(out []byte) (map[int][]file, error) {
	mapped := map[int][]file{}
	var pid int
	var current file
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		field, value := line[0], line[1:]
		switch field {
		case 'p':
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parse lsof pid field %q: %w", line, err)
			}
			pid = parsed
		case 'f':
			current = file{}
		case 'D':
			device, err := strconv.ParseInt(value, 0, 64)
			if err != nil {
				return nil, fmt.Errorf("parse lsof device field %q: %w", line, err)
			}
			current.device = device
		case 'i':
			inode, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse lsof inode field %q: %w", line, err)
			}
			current.inode = inode
			mapped[pid] = append(mapped[pid], current)
		}
	}
	return mapped, nil
}
