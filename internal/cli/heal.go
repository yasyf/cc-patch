package cli

import (
	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/heal"
)

func newHealCmd() *cobra.Command {
	var all bool
	var id string
	cmd := &cobra.Command{
		Use:   "heal",
		Short: "Re-apply patches, re-deriving through Claude when an update drifts them",
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
				res, err := heal.Heal(cmd.Context(), inst, p)
				if err != nil {
					return err
				}
				state := "already patched"
				if res.Changed {
					state = "patched"
				}
				switch {
				case res.Rederived:
					state += " (re-derived via Claude)"
				case res.Derived:
					state += " (derived)"
				}
				cmd.Printf("%s  %s  %s\n", res.Version, res.PatchID, state)
			}
			return nil
		},
	}
	addSelectFlags(cmd, &all, &id)
	return cmd
}
