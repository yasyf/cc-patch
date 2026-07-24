// Package daemon installs the launchd agents that keep the patch applied: a
// watcher that re-patches whenever Claude Code updates, and a daily heal job.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yasyf/daemonkit/service"

	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/store"
)

const (
	watchLabel             = "com.yasyf.cc-patch.watch"
	healLabel              = "com.yasyf.cc-patch.heal"
	controllerWorkerLimit  = 1
	controllerCloseTimeout = 30 * time.Second
)

type controller interface {
	Converge(context.Context, []service.Agent) error
	Close(context.Context) error
}

var openController = func(ctx context.Context, config service.ControllerConfig) (controller, error) {
	return service.NewController(ctx, config)
}

// Plan returns the exact desired agent set for an install: one watcher, one
// daily heal. Both run the current cc-patch executable.
func Plan(inst claude.Install) (service.Plan, error) {
	logDir, err := logDir()
	if err != nil {
		return service.Plan{}, err
	}
	program, err := service.CanonicalExecutable()
	if err != nil {
		return service.Plan{}, fmt.Errorf("resolve current executable: %w", err)
	}
	watch := service.Agent{
		Label:         watchLabel,
		Program:       program,
		Args:          []string{"apply", "--all"},
		LogPath:       filepath.Join(logDir, "watch.log"),
		RestartPolicy: service.NoRestart,
		WatchPaths:    []string{inst.VersionsDir, inst.Launcher},
	}
	heal := service.Agent{
		Label:                 healLabel,
		Program:               program,
		Args:                  []string{"heal", "--all"},
		LogPath:               filepath.Join(logDir, "heal.log"),
		RestartPolicy:         service.NoRestart,
		StartCalendarInterval: []service.CalendarInterval{service.Daily(3, 30)},
	}
	plan, err := service.NewPlan([]service.Agent{watch, heal})
	if err != nil {
		return service.Plan{}, fmt.Errorf("build daemon service plan: %w", err)
	}
	return plan, nil
}

// Install converges the complete daemon service plan.
func Install(ctx context.Context, inst claude.Install) error {
	plan, err := Plan(inst)
	if err != nil {
		return err
	}
	return converge(ctx, plan)
}

// Uninstall converges the daemon service plan to the empty set.
func Uninstall(ctx context.Context) error {
	plan, err := service.NewPlan(nil)
	if err != nil {
		return fmt.Errorf("build empty daemon service plan: %w", err)
	}
	return converge(ctx, plan)
}

// Labels returns the installed agents' launchd labels.
func Labels() []string { return []string{watchLabel, healLabel} }

func converge(ctx context.Context, plan service.Plan) (err error) {
	config, err := controllerConfig()
	if err != nil {
		return err
	}
	controller, err := openController(ctx, config)
	if err != nil {
		return fmt.Errorf("open daemon service controller: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), controllerCloseTimeout)
		defer cancel()
		err = errors.Join(err, controller.Close(closeCtx))
	}()
	if err := controller.Converge(ctx, plan.Agents()); err != nil {
		return fmt.Errorf("converge daemon service plan: %w", err)
	}
	return nil
}

func controllerConfig() (service.ControllerConfig, error) {
	dir, err := store.Dir()
	if err != nil {
		return service.ControllerConfig{}, err
	}
	return service.ControllerConfig{
		StatePath:   filepath.Join(dir, "daemon-services.db"),
		ProcessPath: filepath.Join(dir, "daemon-service-processes.db"),
		WorkerLimit: controllerWorkerLimit,
	}, nil
}

func logDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "cc-patch"), nil
}
