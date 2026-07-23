package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/packstore"
	"github.com/yasyf/cc-patch/internal/registry"
)

func newInstallCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "install <owner>/<repo>[@<ref>] | <builtin>",
		Short: "Install a patch pack — a builtin by name, or a GitHub repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := parseSpec(args[0])
			if err != nil {
				return err
			}
			confirmInstall := func(patches []registry.Patch) (bool, error) {
				for _, p := range patches {
					cmd.Printf("%s · %s · (%d sites)\n", p.ID, p.Summary, len(p.Sites))
				}
				if yes {
					return true, nil
				}
				cmd.Print("Install and auto-apply these patches? [y/N] ")
				return confirm(cmd.InOrStdin()), nil
			}
			var installed bool
			if spec.builtin {
				_, installed, err = packstore.InstallBuiltin(spec.name, confirmInstall)
			} else {
				_, installed, err = packstore.Install(cmd.Context(), spec.owner, spec.repo, spec.ref, confirmInstall)
			}
			if err != nil {
				return err
			}
			if !installed {
				cmd.Println("aborted")
				return nil
			}
			cmd.Printf("installed %s\n", spec.label())
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}

// packSpec is a parsed install target: a builtin by name, or a remote owner/repo.
type packSpec struct {
	builtin          bool
	name             string
	owner, repo, ref string
}

func (s packSpec) label() string {
	if s.builtin {
		return s.name
	}
	return s.owner + "/" + s.repo
}

// packName matches GitHub-style owner/repo segments and builtin names, rejecting
// "..", leading dashes, and path separators so a name can never escape the store.
var packName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// parseSpec parses "<owner>/<repo>[@<ref>]" (remote) or "<builtin>" (no slash).
func parseSpec(arg string) (packSpec, error) {
	rest := arg
	var ref string
	if i := strings.Index(rest, "@"); i >= 0 {
		ref = rest[i+1:]
		rest = rest[:i]
	}
	if !strings.Contains(rest, "/") {
		if !packName.MatchString(rest) {
			return packSpec{}, fmt.Errorf("invalid builtin name %q", arg)
		}
		if ref != "" {
			return packSpec{}, fmt.Errorf("builtin pack %q takes no @ref", arg)
		}
		return packSpec{builtin: true, name: rest}, nil
	}
	owner, repo, ok := strings.Cut(rest, "/")
	if !ok || !packName.MatchString(owner) || !packName.MatchString(repo) {
		return packSpec{}, fmt.Errorf("expected <owner>/<repo>[@<ref>] or a builtin name, got %q", arg)
	}
	return packSpec{owner: owner, repo: repo, ref: ref}, nil
}

func confirm(r io.Reader) bool {
	var s string
	_, _ = fmt.Fscanln(r, &s)
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
