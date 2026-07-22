package cli

import (
	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/registry"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the registered patches",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, p := range registry.All() {
				cmd.Printf("%s  (%d sites in %s)\n    %s\n", p.ID, len(p.Sites), p.SegmentName, p.Summary)
			}
			return nil
		},
	}
}
