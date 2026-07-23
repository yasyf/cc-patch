package cli

import "testing"

func TestParseSpec(t *testing.T) {
	tests := []struct {
		arg              string
		builtin          bool
		name             string
		owner, repo, ref string
		wantErr          bool
	}{
		{arg: "yasyf/cc-patch-fastmode", owner: "yasyf", repo: "cc-patch-fastmode"},
		{arg: "yasyf/foo@v1.2.3", owner: "yasyf", repo: "foo", ref: "v1.2.3"},
		{arg: "a.b/c_d.e", owner: "a.b", repo: "c_d.e"},
		{arg: "fastmode", builtin: true, name: "fastmode"},
		{arg: "opus-max-thinking", builtin: true, name: "opus-max-thinking"},
		{arg: "fastmode@v1", wantErr: true},
		{arg: "../..", wantErr: true},
		{arg: "../evil", wantErr: true},
		{arg: "yasyf/..", wantErr: true},
		{arg: "yasyf/a/b", wantErr: true},
		{arg: "-flag/repo", wantErr: true},
		{arg: "yasyf/-flag", wantErr: true},
		{arg: "-flag", wantErr: true},
		{arg: "..", wantErr: true},
		{arg: "/repo", wantErr: true},
		{arg: "owner/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			spec, err := parseSpec(tt.arg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSpec(%q) = %+v, want error", tt.arg, spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSpec(%q) unexpected error: %v", tt.arg, err)
			}
			if spec.builtin != tt.builtin || spec.name != tt.name || spec.owner != tt.owner || spec.repo != tt.repo || spec.ref != tt.ref {
				t.Errorf("parseSpec(%q) = %+v, want builtin=%v name=%q owner=%q repo=%q ref=%q",
					tt.arg, spec, tt.builtin, tt.name, tt.owner, tt.repo, tt.ref)
			}
		})
	}
}
