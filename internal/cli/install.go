package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/packstore"
	"github.com/yasyf/cc-patch/internal/registry"
)

func newInstallCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "install <owner>/<repo>[@<ref>] | <builtin> | <dir>",
		Short: "Install a patch pack — a builtin by name, a GitHub repo, or a local directory",
		Long:  "Install a patch pack.\n\nA builtin or GitHub repo is copied into the managed store and recorded. A local\ndirectory is linked instead, so it is discovered on every load and stays live as\nyou edit it; remove the link to uninstall it.",
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
			if spec.dir != "" {
				patches, lerr := packstore.LinkLocal(spec.dir, spec.repo)
				if lerr != nil {
					return lerr
				}
				for _, p := range patches {
					cmd.Printf("%s · %s · (%d sites)\n", p.ID, p.Summary, len(p.Sites))
				}
				cmd.Printf("linked %s (edits in %s take effect on the next load)\n", spec.label(), spec.dir)
				return nil
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

// packSpec is a parsed install target: a builtin by name, a local directory, or a
// remote owner/repo.
type packSpec struct {
	builtin          bool
	name             string
	dir              string
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

// parseSpec parses a local directory (a path-shaped argument), "<builtin>" (no
// slash), or "<owner>/<repo>[@<ref>]" (remote).
func parseSpec(arg string) (packSpec, error) {
	if isLocalDir(arg) {
		return parseLocalSpec(arg)
	}
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

// isLocalDir reports whether arg is a path-shaped argument naming an existing
// directory. A pack ref can never be one: packName rejects a leading dot, slash
// or tilde, so a path that does not resolve still fails as a malformed ref.
func isLocalDir(arg string) bool {
	if !strings.HasPrefix(arg, ".") && !strings.HasPrefix(arg, "/") && !strings.HasPrefix(arg, "~") {
		return false
	}
	info, err := os.Stat(expandHome(arg))
	return err == nil && info.IsDir()
}

// expandHome resolves a leading ~ against the current user's home directory,
// leaving any other path untouched.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// parseLocalSpec resolves a directory argument, naming the pack after the
// directory so its patches land under "local/<dir>".
func parseLocalSpec(arg string) (packSpec, error) {
	dir, err := filepath.Abs(expandHome(arg))
	if err != nil {
		return packSpec{}, fmt.Errorf("resolve pack dir %q: %w", arg, err)
	}
	repo := filepath.Base(dir)
	if !packName.MatchString(repo) {
		return packSpec{}, fmt.Errorf("pack dir %q has an unusable name %q", arg, repo)
	}
	return packSpec{dir: dir, owner: packstore.LocalOwner, repo: repo}, nil
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
