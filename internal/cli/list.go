package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/builtins"
	"github.com/yasyf/cc-patch/internal/patchset"
	"github.com/yasyf/cc-patch/internal/store"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed pack patches and available builtins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			patches, warns, err := patchset.Load(cmd.Context())
			if err != nil {
				return err
			}
			warn(cmd, warns)
			for _, p := range patches {
				cmd.Printf("%s  (%d sites in %s)\n    %s\n", p.ID, len(p.Sites), p.SegmentName, p.Summary)
			}
			avail, err := uninstalledBuiltins()
			if err != nil {
				return err
			}
			if len(avail) > 0 {
				cmd.Printf("\nAvailable builtins (install by name): %s\n", strings.Join(avail, ", "))
			}
			return nil
		},
	}
}

func uninstalledBuiltins() ([]string, error) {
	packs, err := store.Packs()
	if err != nil {
		return nil, err
	}
	installed := map[string]bool{}
	for _, p := range packs {
		if p.Builtin {
			installed[p.Name] = true
		}
	}
	var avail []string
	for _, name := range builtins.Names() {
		if !installed[name] {
			avail = append(avail, name)
		}
	}
	return avail, nil
}
