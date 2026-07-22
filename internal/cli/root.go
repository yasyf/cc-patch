// Package cli builds the cobra command tree.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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
		newInstallDaemonsCmd(),
		newUninstallDaemonsCmd(),
	)
	return root
}

// selectPatches resolves the patch set a command operates on from its flags.
func selectPatches(all bool, id string) ([]registry.Patch, error) {
	if id != "" {
		p, err := registry.Find(id)
		if err != nil {
			return nil, err
		}
		return []registry.Patch{p}, nil
	}
	if all {
		return registry.All(), nil
	}
	return nil, fmt.Errorf("specify --all or --id <patch> (see `cc-patch list`)")
}

func addSelectFlags(cmd *cobra.Command, all *bool, id *string) {
	cmd.Flags().BoolVar(all, "all", false, "operate on every registered patch")
	cmd.Flags().StringVar(id, "id", "", "operate on a single patch by id")
}
