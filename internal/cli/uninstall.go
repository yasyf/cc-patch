package cli

import (
	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/packstore"
)

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <owner>/<repo> | <builtin>",
		Short: "Remove an installed patch pack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := parseSpec(args[0])
			if err != nil {
				return err
			}
			if spec.builtin {
				err = packstore.RemoveBuiltin(spec.name)
			} else {
				err = packstore.Remove(spec.owner, spec.repo)
			}
			if err != nil {
				return err
			}
			cmd.Printf("uninstalled %s\n", spec.label())
			return nil
		},
	}
}
