package cli

import (
	"errors"
	"slices"
	"testing"

	"github.com/yasyf/cc-patch/internal/registry"
)

func TestEachPatchRunsEverySelection(t *testing.T) {
	drift := errors.New("patch drifted beyond derivation")
	tests := []struct {
		name    string
		failing []string
		wantRun []string
		wantErr []error
	}{
		{name: "all succeed", wantRun: []string{"a", "b", "c"}},
		{name: "first fails", failing: []string{"a"}, wantRun: []string{"a", "b", "c"}, wantErr: []error{drift}},
		{name: "middle fails", failing: []string{"b"}, wantRun: []string{"a", "b", "c"}, wantErr: []error{drift}},
		{name: "every patch fails", failing: []string{"a", "b", "c"}, wantRun: []string{"a", "b", "c"}, wantErr: []error{drift}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patches := []registry.Patch{{ID: "a"}, {ID: "b"}, {ID: "c"}}
			var ran []string
			err := eachPatch(patches, func(p registry.Patch) error {
				ran = append(ran, p.ID)
				if slices.Contains(tt.failing, p.ID) {
					return drift
				}
				return nil
			})
			if !slices.Equal(ran, tt.wantRun) {
				t.Errorf("ran %v, want %v", ran, tt.wantRun)
			}
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("err = nil, want the failures joined")
			}
			for _, want := range tt.wantErr {
				if !errors.Is(err, want) {
					t.Errorf("err = %v, want it to wrap %v", err, want)
				}
			}
		})
	}
}
