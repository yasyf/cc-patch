package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/packstore"
	"github.com/yasyf/cc-patch/internal/store"
)

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [<owner>/<repo>]",
		Short: "Re-clone installed packs to pick up upstream changes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			packs, err := store.Packs()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				spec, err := parseSpec(args[0])
				if err != nil {
					return err
				}
				if spec.builtin {
					cmd.Printf("%s is a builtin pack; it updates with cc-patch\n", spec.name)
					return nil
				}
				ref, err := refFor(packs, spec.owner, spec.repo)
				if err != nil {
					return err
				}
				return update(cmd, spec.owner, spec.repo, ref)
			}
			for _, p := range packs {
				if p.Builtin {
					continue
				}
				if err := update(cmd, p.Owner, p.Repo, p.Ref); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func update(cmd *cobra.Command, owner, repo, ref string) error {
	if err := packstore.Update(cmd.Context(), owner, repo, ref); err != nil {
		return fmt.Errorf("update %s/%s: %w", owner, repo, err)
	}
	cmd.Printf("updated %s/%s\n", owner, repo)
	return nil
}

func refFor(packs []store.InstalledPack, owner, repo string) (string, error) {
	for _, p := range packs {
		if p.Owner == owner && p.Repo == repo {
			return p.Ref, nil
		}
	}
	return "", fmt.Errorf("pack %s/%s is not installed", owner, repo)
}
