package cli

import (
	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/patcher"
)

func newApplyCmd() *cobra.Command {
	var all bool
	var id string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Patch the installed Claude Code binary and re-sign it",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := claude.Locate()
			if err != nil {
				return err
			}
			patches, warns, err := selectPatches(cmd.Context(), all, id)
			if err != nil {
				return err
			}
			warn(cmd, warns)
			for _, p := range patches {
				out, err := patcher.Apply(cmd.Context(), inst, p)
				if err != nil {
					return err
				}
				state := "already patched"
				if out.Changed {
					state = "patched"
				}
				if out.Derived {
					state += " (derived)"
				}
				cmd.Printf("%s  %s  %s\n", out.Version, out.PatchID, state)
			}
			return nil
		},
	}
	addSelectFlags(cmd, &all, &id)
	return cmd
}
