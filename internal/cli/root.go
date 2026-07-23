// Package cli builds the cobra command tree.
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/patchset"
	"github.com/yasyf/cc-patch/internal/registry"
	"github.com/yasyf/cc-patch/internal/version"
)

// NewRootCmd builds the root command and registers its subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cc-patch",
		Short:         "Fast mode for Claude Code's delegated agents, re-applied automatically on every update.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		newApplyCmd(),
		newStatusCmd(),
		newRestoreCmd(),
		newHealCmd(),
		newListCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newUpdateCmd(),
		newInstallDaemonsCmd(),
		newUninstallDaemonsCmd(),
	)
	return root
}

// selectPatches resolves the patch set a command operates on from its flags,
// returning any per-pack load errors alongside the selected patches.
func selectPatches(ctx context.Context, all bool, id string) ([]registry.Patch, []error, error) {
	if id != "" {
		p, err := patchset.Find(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		return []registry.Patch{p}, nil, nil
	}
	if all {
		patches, warns, err := patchset.Load(ctx)
		if err != nil {
			return nil, nil, err
		}
		return patches, warns, nil
	}
	return nil, nil, fmt.Errorf("specify --all or --id <patch> (see `cc-patch list`)")
}

func warn(cmd *cobra.Command, errs []error) {
	for _, e := range errs {
		cmd.PrintErrln("warning:", e)
	}
}

func addSelectFlags(cmd *cobra.Command, all *bool, id *string) {
	cmd.Flags().BoolVar(all, "all", false, "operate on every registered patch")
	cmd.Flags().StringVar(id, "id", "", "operate on a single patch by id")
}
