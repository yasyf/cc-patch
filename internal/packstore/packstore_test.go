package packstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yasyf/cc-patch/internal/registry"
	"github.com/yasyf/cc-patch/internal/store"
)

const packTOML = `
schema = 1
[[patch]]
id      = "fastmode"
summary = "Fast mode"
[[patch.site]]
anchor = "service tier"
find   = '&&BT(_)&&!!Dn.fastMode)gn="fast"'
drop   = '&&!!Dn.fastMode'
`

// makeLocalPack builds a local git repo carrying cc-patch/pack.toml and returns
// its path. git clones from a local path without touching the network.
func makeLocalPack(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "cc-patch"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "cc-patch", "pack.toml"), []byte(packTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"add", "."},
		{"commit", "-m", "pack"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return repo
}

func TestInstallFromLocalRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := makeLocalPack(t)

	patches, installed, err := installFrom(context.Background(), repo, "acme", "demo", "", autoConfirm)
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("installFrom reported not installed")
	}
	if len(patches) != 1 || patches[0].ID != "acme/demo/fastmode" {
		t.Fatalf("installFrom patches = %+v", patches)
	}

	packs, err := store.Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 {
		t.Fatalf("got %d installed packs, want 1", len(packs))
	}
	if packs[0].Owner != "acme" || packs[0].Repo != "demo" {
		t.Errorf("pack owner/repo = %s/%s", packs[0].Owner, packs[0].Repo)
	}
	if packs[0].Commit == "" {
		t.Error("recorded commit is empty")
	}

	dir, err := packDir("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cc-patch", "pack.toml")); err != nil {
		t.Errorf("pack subtree not copied: %v", err)
	}

	loaded, errs := Load()
	if len(errs) != 0 {
		t.Fatalf("Load errors: %v", errs)
	}
	if len(loaded) != 1 || loaded[0].ID != "acme/demo/fastmode" {
		t.Fatalf("Load patches = %+v", loaded)
	}
}

func TestRemoveDeletesSubtreeAndState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := makeLocalPack(t)
	if _, _, err := installFrom(context.Background(), repo, "acme", "demo", "", autoConfirm); err != nil {
		t.Fatal(err)
	}
	if err := Remove("acme", "demo"); err != nil {
		t.Fatal(err)
	}
	packs, err := store.Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 0 {
		t.Errorf("packs after remove = %+v, want none", packs)
	}
	dir, err := packDir("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("pack dir still present after remove: %v", err)
	}
}

func TestInstallDeclinedPersistsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := makeLocalPack(t)
	decline := func([]registry.Patch) (bool, error) { return false, nil }
	patches, installed, err := installFrom(context.Background(), repo, "acme", "demo", "", decline)
	if err != nil {
		t.Fatal(err)
	}
	if installed {
		t.Fatal("declined install reported installed")
	}
	if len(patches) != 1 {
		t.Fatalf("expected the pack's patches returned for display, got %+v", patches)
	}
	packs, err := store.Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 0 {
		t.Errorf("declined install recorded %d packs, want 0", len(packs))
	}
	dir, err := packDir("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("declined install left a pack dir: %v", err)
	}
}

func TestInstallRejectsSymlinkManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "cc-patch"), 0o700); err != nil {
		t.Fatal(err)
	}
	// pack.toml is a symlink to a secret outside the repo.
	secret := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(repo, "cc-patch", "pack.toml")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "core.symlinks", "true"},
		{"config", "user.email", "t@e.com"},
		{"config", "user.name", "t"},
		{"add", "."},
		{"commit", "-m", "evil"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, _, err := installFrom(context.Background(), repo, "acme", "evil", "", autoConfirm); err == nil {
		t.Fatal("expected symlink manifest to be rejected")
	}
}

func TestPackDirRejectsEscape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := packDir("..", ".."); err == nil {
		t.Fatal("expected packDir to reject an escaping owner/repo")
	}
}

func TestInstallBuiltin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	patches, installed, err := InstallBuiltin("fastmode", autoConfirm)
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("builtin not installed")
	}
	if len(patches) != 1 || patches[0].ID != "fastmode/delegated-agents" {
		t.Fatalf("builtin patches = %+v", patches)
	}
	loaded, errs := Load()
	if len(errs) != 0 {
		t.Fatalf("Load errors: %v", errs)
	}
	if len(loaded) != 1 || loaded[0].ID != "fastmode/delegated-agents" {
		t.Fatalf("Load = %+v", loaded)
	}
	if err := RemoveBuiltin("fastmode"); err != nil {
		t.Fatal(err)
	}
	packs, err := store.Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 0 {
		t.Errorf("packs after builtin uninstall = %+v", packs)
	}
}

func TestInstallBuiltinUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, _, err := InstallBuiltin("nope", autoConfirm); err == nil {
		t.Fatal("expected error for unknown builtin")
	}
}

func TestInstallBuiltinDeclinedPersistsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	decline := func([]registry.Patch) (bool, error) { return false, nil }
	_, installed, err := InstallBuiltin("fastmode", decline)
	if err != nil {
		t.Fatal(err)
	}
	if installed {
		t.Fatal("declined builtin reported installed")
	}
	packs, err := store.Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 0 {
		t.Errorf("declined builtin recorded %d packs, want 0", len(packs))
	}
}
