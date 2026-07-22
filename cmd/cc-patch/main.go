// Command cc-patch: Fast mode for Claude Code's delegated agents, re-applied automatically on every update.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yasyf/cc-patch/internal/cli"
	applog "github.com/yasyf/cc-patch/internal/log"
)

func main() {
	applog.Setup()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		// Minimal error handling: report on stderr and exit non-zero. As the CLI
		// grows, map typed errors to exit codes here (see STYLEGUIDE.md § Error Handling).
		fmt.Fprintln(os.Stderr, "cc-patch:", err)
		os.Exit(1)
	}
}
