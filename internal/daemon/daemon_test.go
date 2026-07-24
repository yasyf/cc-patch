package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/daemonkit/service"

	"github.com/yasyf/cc-patch/internal/claude"
)

func TestPlanRendersExpectedPlistsDeterministically(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	inst := testInstall()
	first, err := Plan(inst)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Plan(inst)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("Plan digest changed: %s != %s", first.Digest(), second.Digest())
	}

	agents := agentsByLabel(first.Agents())
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}
	assertPlistContains(
		t, agents[watchLabel],
		"<key>WatchPaths</key>",
		"<string>/Users/x/.local/share/claude/versions</string>",
		"<string>/Users/x/.local/bin/claude</string>",
		"<string>apply</string>",
		"<string>--all</string>",
	)
	assertPlistContains(
		t, agents[healLabel],
		"<key>StartCalendarInterval</key>",
		"<key>Hour</key>",
		"<integer>3</integer>",
		"<key>Minute</key>",
		"<integer>30</integer>",
		"<string>heal</string>",
	)
}

func TestInstallConvergesPlanWithExactControllerConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder := &recordingController{}
	var gotConfig service.ControllerConfig
	useController(t, recorder, &gotConfig)

	if err := Install(t.Context(), testInstall()); err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(home, ".local", "share", "cc-patch")
	wantConfig := service.ControllerConfig{
		StatePath:   filepath.Join(wantDir, "daemon-services.db"),
		ProcessPath: filepath.Join(wantDir, "daemon-service-processes.db"),
		WorkerLimit: controllerWorkerLimit,
	}
	if gotConfig != wantConfig {
		t.Fatalf("controller config = %#v, want %#v", gotConfig, wantConfig)
	}
	if got := agentsByLabel(recorder.agents); len(got) != 2 || got[watchLabel].Label == "" || got[healLabel].Label == "" {
		t.Fatalf("converged agents = %#v, want watch and heal", recorder.agents)
	}
	if !recorder.closed {
		t.Fatal("controller was not closed")
	}
}

func TestUninstallConvergesEmptyPlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder := &recordingController{}
	useController(t, recorder, nil)

	if err := Uninstall(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(recorder.agents) != 0 {
		t.Fatalf("converged agents = %#v, want empty plan", recorder.agents)
	}
	if !recorder.closed {
		t.Fatal("controller was not closed")
	}
}

func TestConvergeJoinsFailureAndClosesWithoutCallerCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	convergeErr := errors.New("converge failed")
	closeErr := errors.New("close failed")
	recorder := &recordingController{convergeErr: convergeErr, closeErr: closeErr}
	useController(t, recorder, nil)
	plan, err := service.NewPlan(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = converge(ctx, plan)
	if !errors.Is(err, convergeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("converge error = %v, want joined converge and close failures", err)
	}
	if recorder.closeContextErr != nil {
		t.Fatalf("close context error = %v, want uncanceled context", recorder.closeContextErr)
	}
	if !recorder.closeHasDeadline {
		t.Fatal("close context has no deadline")
	}
}

type recordingController struct {
	agents           []service.Agent
	convergeErr      error
	closeErr         error
	closed           bool
	closeContextErr  error
	closeHasDeadline bool
}

func (c *recordingController) Converge(_ context.Context, agents []service.Agent) error {
	c.agents = append([]service.Agent(nil), agents...)
	return c.convergeErr
}

func (c *recordingController) Close(ctx context.Context) error {
	c.closed = true
	c.closeContextErr = ctx.Err()
	_, c.closeHasDeadline = ctx.Deadline()
	return c.closeErr
}

func useController(t *testing.T, recorder controller, config *service.ControllerConfig) {
	t.Helper()
	previous := openController
	openController = func(_ context.Context, got service.ControllerConfig) (controller, error) {
		if config != nil {
			*config = got
		}
		return recorder, nil
	}
	t.Cleanup(func() { openController = previous })
}

func testInstall() claude.Install {
	return claude.Install{
		Launcher:    "/Users/x/.local/bin/claude",
		VersionsDir: "/Users/x/.local/share/claude/versions",
		Binary:      "/Users/x/.local/share/claude/versions/2.1.217",
		Version:     "2.1.217",
	}
}

func agentsByLabel(agents []service.Agent) map[string]service.Agent {
	byLabel := make(map[string]service.Agent, len(agents))
	for _, agent := range agents {
		byLabel[agent.Label] = agent
	}
	return byLabel
}

func assertPlistContains(t *testing.T, agent service.Agent, wants ...string) {
	t.Helper()
	if agent.Label == "" {
		t.Fatal("agent is missing")
	}
	plist, err := agent.Plist()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range wants {
		if !strings.Contains(string(plist), want) {
			t.Errorf("%s plist missing %q", agent.Label, want)
		}
	}
}
