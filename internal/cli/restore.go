package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
)

func newRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore",
		Short: "Restore the pristine, vendor-signed Claude Code binary from backup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := claude.Locate()
			if err != nil {
				return err
			}
			if _, err := os.Stat(inst.Backup()); err != nil {
				return fmt.Errorf("no backup at %q (nothing to restore): %w", inst.Backup(), err)
			}
			if err := binpatch.Restore(inst.Binary, inst.Backup()); err != nil {
				return err
			}
			cmd.Printf("%s  restored from backup\n", inst.Version)
			return nil
		},
	}
}
