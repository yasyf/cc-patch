package store

import (
	"bytes"
	"testing"
)

func TestPutLoadRoundTripWithRawBytes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Find/Drop carry raw minified bytes including quotes and operators.
	sites := []Site{
		{Anchor: "a", Find: []byte(`&&BT(_)&&!!Dn.fastMode)gn="fast"`), Drop: []byte(`&&!!Dn.fastMode`)},
	}
	if err := Put("2.9.9", "fastmode-delegated-agents", sites); err != nil {
		t.Fatal(err)
	}
	st, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := st.Override("2.9.9", "fastmode-delegated-agents")
	if !ok {
		t.Fatal("override not found after Put")
	}
	if len(got) != 1 || !bytes.Equal(got[0].Find, sites[0].Find) || !bytes.Equal(got[0].Drop, sites[0].Drop) {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if _, ok := st.Override("2.9.9", "other"); ok {
		t.Error("unexpected override for different patch id")
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Overrides) != 0 {
		t.Errorf("expected empty overrides, got %v", st.Overrides)
	}
}

func TestPackRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := InstalledPack{Owner: "acme", Repo: "demo", Ref: "main", Commit: "abc123"}
	b := InstalledPack{Owner: "acme", Repo: "other", Ref: "", Commit: "def456"}
	if err := AddPack(a); err != nil {
		t.Fatal(err)
	}
	if err := AddPack(b); err != nil {
		t.Fatal(err)
	}
	packs, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 2 {
		t.Fatalf("got %d packs, want 2", len(packs))
	}

	// AddPack replaces the entry for the same owner/repo rather than duplicating.
	updated := InstalledPack{Owner: "acme", Repo: "demo", Ref: "v2", Commit: "999"}
	if err := AddPack(updated); err != nil {
		t.Fatal(err)
	}
	packs, err = Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 2 {
		t.Fatalf("after re-add got %d packs, want 2", len(packs))
	}
	if got := findPack(packs, "acme", "demo"); got.Commit != "999" || got.Ref != "v2" {
		t.Errorf("replace failed: %+v", got)
	}

	if err := RemovePack("acme/demo"); err != nil {
		t.Fatal(err)
	}
	packs, err = Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 || packs[0].Repo != "other" {
		t.Fatalf("after remove got %+v, want only acme/other", packs)
	}
}

func TestBuiltinPackRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := AddPack(InstalledPack{Name: "fastmode", Builtin: true}); err != nil {
		t.Fatal(err)
	}
	// A remote pack whose owner/repo would join to the same string must not clash.
	if err := AddPack(InstalledPack{Owner: "acme", Repo: "demo"}); err != nil {
		t.Fatal(err)
	}
	packs, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 2 {
		t.Fatalf("got %d packs, want 2", len(packs))
	}
	if err := RemovePack("fastmode"); err != nil {
		t.Fatal(err)
	}
	packs, err = Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 || packs[0].Builtin {
		t.Fatalf("after remove got %+v, want only the remote pack", packs)
	}
}

func TestRemoveOverridesForPatchIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sites := []Site{{Anchor: "a", Find: []byte("abc"), Drop: []byte("b")}}
	// A builtin patch id ("fastmode/…") and a remote pack whose owner shares the
	// builtin's name ("fastmode/repo/…") — removing one must not touch the other.
	for _, id := range []string{"fastmode/delegated-agents", "fastmode/repo/foo", "acme/other/foo"} {
		if err := Put("2.9.9", id, sites); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveOverridesForPatchIDs([]string{"fastmode/delegated-agents"}); err != nil {
		t.Fatal(err)
	}
	st, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Override("2.9.9", "fastmode/delegated-agents"); ok {
		t.Error("builtin override should have been pruned")
	}
	if _, ok := st.Override("2.9.9", "fastmode/repo/foo"); !ok {
		t.Error("remote fastmode/repo override should have survived the builtin removal")
	}
	if _, ok := st.Override("2.9.9", "acme/other/foo"); !ok {
		t.Error("unrelated override should have survived")
	}
}

func findPack(packs []InstalledPack, owner, repo string) InstalledPack {
	for _, p := range packs {
		if p.Owner == owner && p.Repo == repo {
			return p
		}
	}
	return InstalledPack{}
}
