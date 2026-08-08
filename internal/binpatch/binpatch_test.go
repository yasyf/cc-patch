package binpatch

import (
	"bytes"
	"context"
	debugmacho "debug/macho"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/cc-patch/internal/macho"
)

const (
	fixtureMarker       = "ccpatch_test_marker_7f3a9c1e"
	fixtureSecondMarker = "ccpatch_second_mark_4e2b8d6f"
	lcCodeSignature     = 0x1d
)

func requireCodesignAndGo(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"codesign", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
}

func buildFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `package main

import "os"

const (
	marker  = "` + fixtureMarker + `"
	marker2 = "` + fixtureSecondMarker + `"
)

func main() {
	if len(os.Args) > 1 {
		os.Stdout.WriteString(marker)
		os.Stdout.WriteString(marker2)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fixture")
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}
	if err := resign(context.Background(), bin); err != nil {
		t.Fatal(err)
	}
	return bin
}

func blanked(needle string) []byte {
	return bytes.Repeat([]byte(" "), len(needle))
}

func mustUniqueIndex(t *testing.T, data []byte, needle string) int {
	t.Helper()
	if n := bytes.Count(data, []byte(needle)); n != 1 {
		t.Fatalf("%q occurs %d times, want 1", needle, n)
	}
	return bytes.Index(data, []byte(needle))
}

func corruptSignatureBlob(t *testing.T, path string) {
	t.Helper()
	f, err := debugmacho.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, load := range f.Loads {
		lb, ok := load.(debugmacho.LoadBytes)
		if !ok || len(lb) < 16 || binary.LittleEndian.Uint32(lb[0:4]) != lcCodeSignature {
			continue
		}
		dataoff := binary.LittleEndian.Uint32(lb[8:12])
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Overwrites the superblob magic so codesign reports a format error
		// rather than an absent signature.
		binary.LittleEndian.PutUint32(data[dataoff:], 0xdeadbeef)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("no LC_CODE_SIGNATURE load command")
}

func TestVerifySignature(t *testing.T) {
	requireCodesignAndGo(t)
	bin := buildFixture(t)
	unsigned := bin + ".unsigned"
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unsigned, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("codesign", "--remove-signature", unsigned).CombinedOutput(); err != nil {
		t.Fatalf("codesign --remove-signature: %v: %s", err, out)
	}
	malformed := bin + ".malformed"
	if err := os.WriteFile(malformed, data, 0o600); err != nil {
		t.Fatal(err)
	}
	corruptSignatureBlob(t, malformed)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"signed", bin, false},
		{"signature removed", unsigned, true},
		{"signature malformed", malformed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifySignature(context.Background(), tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifySignature() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplySigningFailureLeavesLiveBinaryUntouched(t *testing.T) {
	requireCodesignAndGo(t)
	bin := buildFixture(t)
	before, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}

	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "codesign")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexit 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shim, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	subs := []Substitution{{Find: []byte(fixtureMarker), Replace: blanked(fixtureMarker)}}
	if _, err := Apply(context.Background(), bin, bin+".ccpatch-orig", "__TEXT", subs); err == nil {
		t.Fatal("expected Apply to fail under a failing codesign")
	}

	after, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("live binary changed despite signing failure")
	}
	if err := exec.Command(bin).Run(); err != nil {
		t.Errorf("binary no longer executes: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(bin))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ccpatch-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestApplyRepairsPatchedBinaryWithInvalidSignature(t *testing.T) {
	requireCodesignAndGo(t)
	ctx := context.Background()
	bin := buildFixture(t)
	pristine, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	backup := bin + ".ccpatch-orig"

	corrupted := bytes.Clone(pristine)
	idx := mustUniqueIndex(t, corrupted, fixtureMarker)
	copy(corrupted[idx:], blanked(fixtureMarker))
	if err := os.WriteFile(bin, corrupted, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(ctx, bin); err == nil {
		t.Fatal("corrupted binary unexpectedly verifies")
	}

	subs := []Substitution{{Find: []byte(fixtureMarker), Replace: blanked(fixtureMarker)}}
	res, err := Apply(ctx, bin, backup, "__TEXT", subs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("Changed = false, want true")
	}
	if err := VerifySignature(ctx, bin); err != nil {
		t.Errorf("repaired binary fails verification: %v", err)
	}
	if err := exec.Command(bin).Run(); err != nil {
		t.Errorf("repaired binary no longer executes: %v", err)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte(fixtureMarker)) {
		t.Error("marker still present after repair")
	}
	if !bytes.Contains(got, blanked(fixtureMarker)) {
		t.Error("blanked marker absent after repair")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("repair should not create a backup, stat err = %v", err)
	}
}

func TestApplyRepairWithPartialSubsPreservesSiblingPatches(t *testing.T) {
	requireCodesignAndGo(t)
	ctx := context.Background()
	bin := buildFixture(t)
	pristine, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	window, err := Window(bin, "__TEXT")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{fixtureMarker, fixtureSecondMarker} {
		if n := bytes.Count(window, []byte(m)); n != 1 {
			t.Fatalf("%q occurs %d times in __TEXT, want 1", m, n)
		}
	}
	backup := bin + ".ccpatch-orig"
	if err := os.WriteFile(backup, pristine, 0o600); err != nil {
		t.Fatal(err)
	}

	corrupted := bytes.Clone(pristine)
	idx1 := mustUniqueIndex(t, corrupted, fixtureMarker)
	idx2 := mustUniqueIndex(t, corrupted, fixtureSecondMarker)
	copy(corrupted[idx1:], blanked(fixtureMarker))
	copy(corrupted[idx2:], blanked(fixtureSecondMarker))
	if err := os.WriteFile(bin, corrupted, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(ctx, bin); err == nil {
		t.Fatal("corrupted binary unexpectedly verifies")
	}

	subs := []Substitution{{Find: []byte(fixtureMarker), Replace: blanked(fixtureMarker)}}
	res, err := Apply(ctx, bin, backup, "__TEXT", subs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("Changed = false, want true")
	}
	if err := VerifySignature(ctx, bin); err != nil {
		t.Errorf("repaired binary fails verification: %v", err)
	}
	if err := exec.Command(bin).Run(); err != nil {
		t.Errorf("repaired binary no longer executes: %v", err)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte(fixtureSecondMarker)) {
		t.Error("repair with partial subs reverted the sibling patch")
	}
	if !bytes.Equal(got[idx2:idx2+len(fixtureSecondMarker)], blanked(fixtureSecondMarker)) {
		t.Error("sibling blanking absent after repair")
	}
	if bytes.Contains(got, []byte(fixtureMarker)) {
		t.Error("marker still present after repair")
	}
}

func TestResignRecoversMalformedSignature(t *testing.T) {
	requireCodesignAndGo(t)
	ctx := context.Background()
	bin := buildFixture(t)
	corruptSignatureBlob(t, bin)
	err := VerifySignature(ctx, bin)
	if err == nil {
		t.Fatal("malformed signature unexpectedly verifies")
	}
	if !strings.Contains(err.Error(), "invalid or unsupported format") {
		t.Fatalf("corruption did not produce a malformed signature: %v", err)
	}
	if err := resign(ctx, bin); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(ctx, bin); err != nil {
		t.Errorf("re-signed binary fails verification: %v", err)
	}
	if err := exec.Command(bin).Run(); err != nil {
		t.Errorf("re-signed binary no longer executes: %v", err)
	}
}

func TestEvaluateStates(t *testing.T) {
	// window: [pre][FIND][mid][REPLACE-of-other][post]
	data := []byte("XXfind_oneYYbar    ZZ")
	//              0         1         2
	//              0123456789012345678901
	seg := macho.Segment{Name: "__T", Offset: 2, Size: int64(len(data)) - 2}

	tests := []struct {
		name string
		sub  Substitution
		want State
	}{
		{"unpatched", Substitution{Find: []byte("find_one"), Replace: []byte("XXXXXXXX")}, StateUnpatched},
		{"patched", Substitution{Find: []byte("absent7"), Replace: []byte("bar    ")}, StatePatched},
		{"missing", Substitution{Find: []byte("absent__"), Replace: []byte("nowhere_")}, StateMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluate(data, seg, []Substitution{tt.sub})
			if err != nil {
				t.Fatal(err)
			}
			if got[0].state != tt.want {
				t.Errorf("state = %v, want %v", got[0].state, tt.want)
			}
		})
	}
}

func TestEvaluateRejectsLengthMismatch(t *testing.T) {
	data := []byte("hello world")
	seg := macho.Segment{Name: "__T", Offset: 0, Size: int64(len(data))}
	if _, err := evaluate(data, seg, []Substitution{{Find: []byte("hi"), Replace: []byte("bye")}}); err == nil {
		t.Fatal("expected length-mismatch error")
	}
}

func TestEvaluateRejectsAmbiguous(t *testing.T) {
	data := []byte("ababab")
	seg := macho.Segment{Name: "__T", Offset: 0, Size: int64(len(data))}
	if _, err := evaluate(data, seg, []Substitution{{Find: []byte("ab"), Replace: []byte("cd")}}); err == nil {
		t.Fatal("expected ambiguous-match error")
	}
}

func TestBackupAndRestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	backup := bin + ".orig"
	original := []byte("the original bytes")
	if err := os.WriteFile(bin, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureBackup(bin, backup); err != nil {
		t.Fatal(err)
	}
	// ensureBackup is a no-op the second time even if the binary changed.
	if err := os.WriteFile(bin, []byte("mutated__________"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureBackup(bin, backup); err != nil {
		t.Fatal(err)
	}
	if err := Restore(bin, backup); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("restored = %q, want %q", got, original)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("backup should be removed after restore, stat err = %v", err)
	}
}
