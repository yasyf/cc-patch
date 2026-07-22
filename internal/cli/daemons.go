package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/daemon"
)

func newInstallDaemonsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-daemons",
		Short: "Install the launchd agents that re-patch on update and heal daily",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := claude.Locate()
			if err != nil {
				return err
			}
			if err := daemon.Install(cmd.Context(), inst); err != nil {
				return err
			}
			cmd.Printf("installed: %s\n", strings.Join(daemon.Labels(), ", "))
			return nil
		},
	}
}

func newUninstallDaemonsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall-daemons",
		Short: "Remove the launchd agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := daemon.Uninstall(cmd.Context()); err != nil {
				return err
			}
			cmd.Printf("removed: %s\n", strings.Join(daemon.Labels(), ", "))
			return nil
		},
	}
}
