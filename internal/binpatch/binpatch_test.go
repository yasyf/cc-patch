package binpatch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yasyf/cc-patch/internal/macho"
)

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
