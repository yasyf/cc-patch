package cli

import (
	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/patcher"
	"github.com/yasyf/cc-patch/internal/registry"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether each patch is applied to the installed binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := claude.Locate()
			if err != nil {
				return err
			}
			cmd.Printf("claude %s at %s\n", inst.Version, inst.Binary)
			for _, p := range registry.All() {
				out, err := patcher.Status(inst, p)
				if err != nil {
					cmd.Printf("  %s  error: %v\n", p.ID, err)
					continue
				}
				cmd.Printf("  %s  %s\n", p.ID, summarize(out.Result))
			}
			return nil
		},
	}
}

func summarize(r binpatch.Result) string {
	patched, unpatched, missing := 0, 0, 0
	for _, s := range r.Sites {
		switch s.State {
		case binpatch.StatePatched:
			patched++
		case binpatch.StateUnpatched:
			unpatched++
		case binpatch.StateMissing:
			missing++
		}
	}
	switch {
	case missing > 0:
		return "drifted"
	case unpatched == 0:
		return "patched"
	case patched == 0:
		return "not patched"
	default:
		return "partial"
	}
}
