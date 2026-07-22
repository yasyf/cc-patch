package daemon

import (
	"strings"
	"testing"

	"github.com/yasyf/cc-patch/internal/claude"
)

func TestAgentsRenderExpectedPlists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	inst := claude.Install{
		Launcher:    "/Users/x/.local/bin/claude",
		VersionsDir: "/Users/x/.local/share/claude/versions",
		Binary:      "/Users/x/.local/share/claude/versions/2.1.217",
		Version:     "2.1.217",
	}
	agents, err := Agents(inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}

	watch, err := agents[0].Plist()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>WatchPaths</key>",
		"<string>/Users/x/.local/share/claude/versions</string>",
		"<string>/Users/x/.local/bin/claude</string>",
		"<string>apply</string>",
		"<string>--all</string>",
	} {
		if !strings.Contains(string(watch), want) {
			t.Errorf("watch plist missing %q", want)
		}
	}

	heal, err := agents[1].Plist()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>StartCalendarInterval</key>",
		"<key>Hour</key>",
		"<integer>3</integer>",
		"<string>heal</string>",
	} {
		if !strings.Contains(string(heal), want) {
			t.Errorf("heal plist missing %q", want)
		}
	}
}
