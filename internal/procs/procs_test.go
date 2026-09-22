package procs

import (
	"context"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yasyf/cc-patch/internal/claude"
)

func TestClassifySeparatesMappedInodes(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	found := []candidate{
		{pid: 10, started: start},
		{pid: 11, started: start.Add(time.Minute)},
		{pid: 12, started: start.Add(2 * time.Minute)},
	}
	mapped := map[int][]file{
		10: {{device: 1, inode: 41}, {device: 2, inode: 42}},
		11: {{device: 1, inode: 42}, {device: 1, inode: 99}},
	}
	got := classify(found, mapped, file{device: 1, inode: 42})
	want := []Process{
		{PID: 11, Started: start.Add(time.Minute), Current: true},
		{PID: 10, Started: start, Current: false},
	}
	if len(got) != len(want) {
		t.Fatalf("classify() reported %d processes, want %d (pid 12 is gone)", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("classify()[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestParseTextFilesReadsLsofFields(t *testing.T) {
	out := []byte("p69074\nftxt\nD0x1000012\ni654751880\nftxt\nD0x1000013\ni1152921500312573255\n" +
		"p80822\nftxt\nD0x1000012\ni754526962\n")
	mapped, err := parseTextFiles(out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int][]file{
		69074: {{device: 0x1000012, inode: 654751880}, {device: 0x1000013, inode: 1152921500312573255}},
		80822: {{device: 0x1000012, inode: 754526962}},
	}
	if !maps.EqualFunc(mapped, want, slices.Equal) {
		t.Errorf("parseTextFiles() = %v, want %v", mapped, want)
	}
}

// TestTextFilesErrorsOnAnUnreadableProcess proves the gate: pid 1 is running and
// lsof reports nothing for it, so its absence may not pass for an exit.
func TestTextFilesErrorsOnAnUnreadableProcess(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which can read pid 1")
	}
	if _, err := textFiles(t.Context(), []candidate{{pid: 1}}); err == nil {
		t.Fatal("textFiles() accepted pid 1, which lsof does not report and which has not exited")
	}
}

func TestTextFilesOmitsAnExitedProcess(t *testing.T) {
	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	mapped, err := textFiles(t.Context(), []candidate{{pid: pid}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mapped[pid]; ok {
		t.Errorf("textFiles() reported files for exited pid %d", pid)
	}
}

// TestInspectCallsAProcessFromBeforeTheWriteStale is the failure cc-patch status
// used to hide: an in-place patch reaches no process that is already running, so
// a process exec'd before the binary was replaced must report stale while one
// exec'd after it reports current.
func TestInspectCallsAProcessFromBeforeTheWriteStale(t *testing.T) {
	for _, tool := range []string{"go", "lsof"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	dir := t.TempDir()
	versions := filepath.Join(dir, "versions")
	if err := os.Mkdir(versions, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(versions, "9.9.9")
	launcher := filepath.Join(dir, "claude")
	fixture := buildFixture(t)
	publish(t, fixture, binary)
	if err := os.Symlink(binary, launcher); err != nil {
		t.Fatal(err)
	}

	stalePID := run(t, launcher)
	publish(t, fixture, binary)
	currentPID := run(t, launcher)

	report, err := Inspect(context.Background(), claude.Install{
		Launcher:    launcher,
		VersionsDir: versions,
		Binary:      binary,
		Version:     "9.9.9",
	})
	if err != nil {
		t.Fatal(err)
	}
	stale, current := reported(t, report, stalePID), reported(t, report, currentPID)
	if stale.Current {
		t.Errorf("pid %d exec'd the binary the write replaced, want Current false", stalePID)
	}
	if !current.Current {
		t.Errorf("pid %d exec'd the binary the write published, want Current true", currentPID)
	}
	if !stale.Started.Before(report.Written) {
		t.Errorf("pid %d started %s but the binary was written %s; the fixture must predate the write",
			stalePID, stale.Started.Format(time.RFC3339Nano), report.Written.Format(time.RFC3339Nano))
	}
	if !current.Started.After(report.Written) {
		t.Errorf("pid %d started %s but the binary was written %s; the control must follow the write",
			currentPID, current.Started.Format(time.RFC3339Nano), report.Written.Format(time.RFC3339Nano))
	}
	if !contains(report.Stale(), stalePID) {
		t.Errorf("Stale() omitted pid %d", stalePID)
	}
	if contains(report.Stale(), currentPID) {
		t.Errorf("Stale() named pid %d, which maps the published binary", currentPID)
	}
}

func contains(procs []Process, pid int) bool {
	for _, p := range procs {
		if p.PID == pid {
			return true
		}
	}
	return false
}

func reported(t *testing.T, report Report, pid int) Process {
	t.Helper()
	for _, p := range report.Processes {
		if p.PID == pid {
			return p
		}
	}
	t.Fatalf("pid %d is running the fixture binary but Inspect did not report it", pid)
	return Process{}
}

// buildFixture builds a Mach-O binary that announces its exec on stdout and
// then blocks, so the test can prove the process mapped the file it published.
func buildFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := "package main\n\nimport (\n\t\"os\"\n\t\"time\"\n)\n\nfunc main() {\n\tif _, err := os.Stdout.Write([]byte(\"x\")); err != nil {\n\t\tos.Exit(1)\n\t}\n\ttime.Sleep(time.Hour)\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "fixture")
	cmd := exec.Command("go", "build", "-o", fixture, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}
	return fixture
}

// publish installs a fresh copy of the fixture at path the way binpatch does —
// a new file renamed over the old one — so the path gains a new inode.
func publish(t *testing.T, fixture, path string) {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	pending := path + ".pending"
	if err := os.WriteFile(pending, data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(pending, path); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, binary string) int {
	t.Helper()
	cmd := exec.Command(binary)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %q: %v", binary, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if _, err := io.ReadFull(stdout, make([]byte, 1)); err != nil {
		t.Fatalf("%q never reached main: %v", binary, err)
	}
	return cmd.Process.Pid
}
