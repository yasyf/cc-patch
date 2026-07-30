package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Ref
		wantErr bool
	}{
		{name: "simple", in: "anthropics/claude-code#123", want: Ref{Owner: "anthropics", Repo: "claude-code", Number: 123}},
		{name: "trims space", in: "  yasyf/cc-patch#7  ", want: Ref{Owner: "yasyf", Repo: "cc-patch", Number: 7}},
		{name: "dots in repo", in: "a/b.c#1", want: Ref{Owner: "a", Repo: "b.c", Number: 1}},
		{name: "no hash", in: "anthropics/claude-code", wantErr: true},
		{name: "no owner", in: "claude-code#1", wantErr: true},
		{name: "zero number", in: "a/b#0", wantErr: true},
		{name: "non-numeric", in: "a/b#x", wantErr: true},
		{name: "url", in: "https://github.com/a/b/issues/1", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRefRendering(t *testing.T) {
	r := Ref{Owner: "anthropics", Repo: "claude-code", Number: 42}
	if got, want := r.String(), "anthropics/claude-code#42"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := r.URL(), "https://github.com/anthropics/claude-code/issues/42"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
	if !(Ref{}).Zero() {
		t.Error("zero Ref should report Zero()")
	}
	if r.Zero() {
		t.Error("populated Ref should not report Zero()")
	}
	if got := (Ref{}).String(); got != "" {
		t.Errorf("zero Ref String() = %q, want empty", got)
	}
}

func TestFetch(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    bool
		wantErr bool
	}{
		{name: "open", status: http.StatusOK, body: `{"state":"open"}`, want: false},
		{name: "closed", status: http.StatusOK, body: `{"state":"closed"}`, want: true},
		{name: "not found", status: http.StatusNotFound, body: `{}`, wantErr: true},
		{name: "rate limited", status: http.StatusForbidden, body: `{}`, wantErr: true},
		{name: "malformed", status: http.StatusOK, body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			prev := apiBase
			t.Cleanup(func() { apiBase = prev })
			apiBase = srv.URL

			ref := Ref{Owner: "anthropics", Repo: "claude-code", Number: 9}
			got, err := Fetch(context.Background(), ref)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Fetch() = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Fetch(): %v", err)
			}
			if got != tt.want {
				t.Errorf("Fetch() = %v, want %v", got, tt.want)
			}
			if want := "/repos/anthropics/claude-code/issues/9"; gotPath != want {
				t.Errorf("requested %q, want %q", gotPath, want)
			}
		})
	}
}

func TestRetiredZeroRefSkipsLookup(t *testing.T) {
	prev := apiBase
	t.Cleanup(func() { apiBase = prev })
	apiBase = "http://127.0.0.1:0"

	retired, err := Retired(context.Background(), Ref{})
	if err != nil {
		t.Fatalf("Retired(zero): %v", err)
	}
	if retired {
		t.Error("zero Ref should never retire")
	}
}
