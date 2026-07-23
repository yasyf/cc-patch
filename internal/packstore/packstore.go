// Package packstore installs, updates, removes, and loads patch packs — git
// repos carrying cc-patch/pack.toml — under the store's packs directory.
package packstore

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yasyf/cc-patch/internal/builtins"
	"github.com/yasyf/cc-patch/internal/pack"
	"github.com/yasyf/cc-patch/internal/registry"
	"github.com/yasyf/cc-patch/internal/store"
)

// Install clones github.com/<owner>/<repo> (at ref when set), validates its pack,
// and — only if confirm returns true — copies it under PacksDir and records it.
// It returns the pack's patches, whether it was recorded, and any error. Nothing
// is persisted until confirm approves, so a decline or interrupt leaves no state.
func Install(ctx context.Context, owner, repo, ref string, confirm func([]registry.Patch) (bool, error)) ([]registry.Patch, bool, error) {
	return installFrom(ctx, cloneURL(owner, repo), owner, repo, ref, confirm)
}

// Update re-clones an installed pack to refresh its subtree and commit.
func Update(ctx context.Context, owner, repo, ref string) error {
	_, _, err := installFrom(ctx, cloneURL(owner, repo), owner, repo, ref, autoConfirm)
	return err
}

func autoConfirm([]registry.Patch) (bool, error) { return true, nil }

// InstallBuiltin records the embedded builtin pack named name — only if confirm
// returns true — and returns its patches. Builtins live in the binary, so there
// is nothing to clone or copy; they update when cc-patch itself updates.
func InstallBuiltin(name string, confirm func([]registry.Patch) (bool, error)) ([]registry.Patch, bool, error) {
	fsys, ok := builtins.Open(name)
	if !ok {
		return nil, false, fmt.Errorf("no builtin pack %q (see `cc-patch list`)", name)
	}
	patches, err := compilePackFS(fsys, name)
	if err != nil {
		return nil, false, err
	}
	ok, err = confirm(patches)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return patches, false, nil
	}
	if err := store.AddPack(store.InstalledPack{Name: name, Builtin: true}); err != nil {
		return nil, false, err
	}
	return patches, true, nil
}

// Remove deletes a remote pack's subtree, its installed-pack record, and any heal
// overrides scoped to it.
func Remove(owner, repo string) error {
	p := store.InstalledPack{Owner: owner, Repo: repo}
	ids := installedPatchIDs(p) // read patch ids before deleting the subtree
	dir, err := packDir(owner, repo)
	if err != nil {
		return err
	}
	// Commit the authoritative removal before deleting inert pack bytes. If
	// cleanup fails, an unreferenced directory is safe; the inverse leaves live
	// state pointing at missing code.
	if err := store.RemovePack(p.Namespace(), ids); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove pack dir %q: %w", dir, err)
	}
	return nil
}

// RemoveBuiltin drops a builtin pack's record and its heal overrides. The pack
// stays embedded in the binary; only its installed status is cleared.
func RemoveBuiltin(name string) error {
	ids := installedPatchIDs(store.InstalledPack{Name: name, Builtin: true})
	return store.RemovePack(name, ids)
}

// installedPatchIDs best-effort loads a pack's patch ids so its heal overrides can
// be pruned exactly; a pack that no longer compiles simply prunes nothing.
func installedPatchIDs(p store.InstalledPack) []string {
	patches, err := loadPack(p)
	if err != nil {
		return nil
	}
	ids := make([]string, len(patches))
	for i, pt := range patches {
		ids[i] = pt.ID
	}
	return ids
}

// Load compiles every installed pack's patches. An invalid pack remains an
// isolated warning; an invalid authoritative store is a hard error.
func Load() ([]registry.Patch, []error, error) {
	packs, err := store.Packs()
	if err != nil {
		return nil, nil, err
	}
	var patches []registry.Patch
	var errs []error
	for _, p := range packs {
		loaded, lerr := loadPack(p)
		if lerr != nil {
			errs = append(errs, fmt.Errorf("pack %s: %w", p.Namespace(), lerr))
			continue
		}
		patches = append(patches, loaded...)
	}
	return patches, errs, nil
}

func installFrom(ctx context.Context, cloneURL, owner, repo, ref string, confirm func([]registry.Patch) (bool, error)) ([]registry.Patch, bool, error) {
	tmp, err := os.MkdirTemp("", "cc-patch-pack-*")
	if err != nil {
		return nil, false, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, cloneURL, tmp)
	if _, err := git(ctx, "", args...); err != nil {
		return nil, false, fmt.Errorf("clone pack %s: %w", cloneURL, err)
	}

	src := filepath.Join(tmp, "cc-patch")
	patches, err := compilePackFile(filepath.Join(src, "pack.toml"), owner+"/"+repo)
	if err != nil {
		return nil, false, err
	}

	// Confirm before anything is persisted, so a decline or interrupt leaves no
	// pack recorded that the daemon would then auto-apply.
	ok, err := confirm(patches)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return patches, false, nil
	}

	commit, err := git(ctx, tmp, "rev-parse", "HEAD")
	if err != nil {
		return nil, false, fmt.Errorf("resolve pack commit: %w", err)
	}
	commit = strings.TrimSpace(commit)

	dir, err := packDir(owner, repo)
	if err != nil {
		return nil, false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, false, fmt.Errorf("clear pack dir %q: %w", dir, err)
	}
	if err := copyTree(filepath.Join(dir, "cc-patch"), src); err != nil {
		return nil, false, fmt.Errorf("copy pack subtree: %w", err)
	}

	if err := store.AddPack(store.InstalledPack{Owner: owner, Repo: repo, Ref: ref, Commit: commit}); err != nil {
		return nil, false, err
	}
	return patches, true, nil
}

// copyTree copies src into dst, rejecting symlinks and other non-regular entries
// so an untrusted pack repo can't smuggle a symlink into the managed store. The
// walk is scoped through os.Root, which refuses any symlink traversal out of src.
func copyTree(dst, src string) error {
	root, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	rootFS := root.FS()
	return fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, path)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type().IsRegular():
			data, err := fs.ReadFile(rootFS, path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o600)
		default:
			return fmt.Errorf("pack entry %q is not a regular file", path)
		}
	})
}

func loadPack(p store.InstalledPack) ([]registry.Patch, error) {
	if p.Builtin {
		fsys, ok := builtins.Open(p.Name)
		if !ok {
			return nil, fmt.Errorf("no builtin pack %q", p.Name)
		}
		return compilePackFS(fsys, p.Name)
	}
	dir, err := packDir(p.Owner, p.Repo)
	if err != nil {
		return nil, err
	}
	return compilePackFile(filepath.Join(dir, "cc-patch", "pack.toml"), p.Namespace())
}

func compilePackFile(manifestPath, namespace string) ([]registry.Patch, error) {
	fi, err := os.Lstat(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("stat pack manifest: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("pack manifest %q is a symlink", manifestPath)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read pack manifest: %w", err)
	}
	return compileManifest(data, namespace)
}

func compilePackFS(fsys fs.FS, namespace string) ([]registry.Patch, error) {
	data, err := fs.ReadFile(fsys, "pack.toml")
	if err != nil {
		return nil, fmt.Errorf("read builtin manifest: %w", err)
	}
	return compileManifest(data, namespace)
}

func compileManifest(data []byte, namespace string) ([]registry.Patch, error) {
	manifest, err := pack.Parse(data)
	if err != nil {
		return nil, err
	}
	return manifest.Patches(namespace)
}

func packDir(owner, repo string) (string, error) {
	base, err := store.PacksDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, owner, repo)
	// Defense in depth: reject an owner/repo that escapes the store even if the
	// CLI's name validation is ever bypassed — packDir gates destructive ops.
	rel, err := filepath.Rel(filepath.Clean(base), dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("pack %q/%q escapes the store", owner, repo)
	}
	return dir, nil
}

func cloneURL(owner, repo string) string {
	return "https://github.com/" + owner + "/" + repo
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return string(out), nil
}
