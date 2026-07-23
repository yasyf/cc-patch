package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPutLoadRoundTripWithRawBytes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sites := []Site{
		{Anchor: "a", Find: []byte(`&&BT(_)&&!!Dn.fastMode)gn="fast"`), Drop: []byte(`&&!!Dn.fastMode`)},
	}
	if err := Put("2.9.9", "fastmode-delegated-agents", sites); err != nil {
		t.Fatal(err)
	}
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := state.Override("2.9.9", "fastmode-delegated-agents")
	if !ok {
		t.Fatal("override not found after Put")
	}
	if len(got) != 1 || !bytes.Equal(got[0].Find, sites[0].Find) || !bytes.Equal(got[0].Drop, sites[0].Drop) {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if _, ok := state.Override("2.9.9", "other"); ok {
		t.Error("unexpected override for different patch id")
	}
}

func TestStateSchemaFingerprintPinned(t *testing.T) {
	digest := sha256.Sum256([]byte(stateSchemaIdentity + "\x00v1\x00" + stateSchemaDescriptor))
	want := stateSchemaIdentity + "." + hex.EncodeToString(digest[:])
	if stateSchemaFingerprint != want {
		t.Fatalf("stateSchemaFingerprint = %q, want %q", stateSchemaFingerprint, want)
	}
}

func TestStateEncodingIsExact(t *testing.T) {
	data, err := encodeState(State{
		Overrides: map[string][]Site{
			"2.9.9/demo": {{Anchor: "a", Find: []byte("find-drop"), Drop: []byte("drop")}},
		},
		Packs: []InstalledPack{{Owner: "acme", Repo: "demo", Commit: "abc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":"dev.yasyf.cc-patch.state","schemaVersion":1,"schemaFingerprint":"dev.yasyf.cc-patch.state.3fe1393c998608169f67bdc07db3e0654dcc8825d173f02844ea09da4edffbaa","payload":{"overrides":{"2.9.9/demo":[{"anchor":"a","find":"ZmluZC1kcm9w","drop":"ZHJvcA=="}]},"packs":[{"name":"","builtin":false,"owner":"acme","repo":"demo","ref":"","commit":"abc"}]}}`
	if string(data) != want {
		t.Fatalf("encoded state = %s, want %s", data, want)
	}
	decoded, err := decodeState(data, "state.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Packs[0]; got.Owner != "acme" || got.Repo != "demo" || got.Commit != "abc" {
		t.Fatalf("decoded pack = %+v", got)
	}
}

func TestLoadRejectsNonExactState(t *testing.T) {
	valid := validStateJSON()
	validPayload := `{"overrides":{},"packs":[]}`
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "malformed", data: `not json`},
		{name: "legacy bare state", data: validPayload},
		{name: "missing schema", data: strings.Replace(valid, `"schema":"`+stateSchemaIdentity+`",`, "", 1)},
		{name: "null schema", data: strings.Replace(valid, `"schema":"`+stateSchemaIdentity+`"`, `"schema":null`, 1)},
		{name: "wrong schema", data: strings.Replace(valid, stateSchemaIdentity, "dev.yasyf.foreign", 1)},
		{name: "missing version", data: strings.Replace(valid, `"schemaVersion":1,`, "", 1)},
		{name: "null version", data: strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":null`, 1)},
		{name: "old version", data: strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":0`, 1)},
		{name: "future version", data: strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":2`, 1)},
		{name: "missing fingerprint", data: strings.Replace(valid, `"schemaFingerprint":"`+stateSchemaFingerprint+`",`, "", 1)},
		{name: "null fingerprint", data: strings.Replace(valid, `"schemaFingerprint":"`+stateSchemaFingerprint+`"`, `"schemaFingerprint":null`, 1)},
		{name: "wrong fingerprint", data: strings.Replace(valid, stateSchemaFingerprint, stateSchemaIdentity+".stale", 1)},
		{name: "missing payload", data: strings.Replace(valid, `,"payload":`+validPayload, "", 1)},
		{name: "null payload", data: strings.Replace(valid, validPayload, `null`, 1)},
		{name: "missing overrides", data: strings.Replace(valid, `"overrides":{},`, "", 1)},
		{name: "null overrides", data: strings.Replace(valid, `"overrides":{}`, `"overrides":null`, 1)},
		{name: "missing packs", data: strings.Replace(valid, `,"packs":[]`, "", 1)},
		{name: "null packs", data: strings.Replace(valid, `"packs":[]`, `"packs":null`, 1)},
		{name: "unknown envelope field", data: strings.TrimSuffix(valid, "}") + `,"legacy":true}`},
		{name: "duplicate envelope field", data: strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1)},
		{name: "unknown payload field", data: strings.Replace(valid, `"packs":[]`, `"packs":[],"legacy":true`, 1)},
		{name: "duplicate payload field", data: strings.Replace(valid, `"packs":[]`, `"packs":[],"packs":[]`, 1)},
		{name: "trailing JSON", data: valid + ` {}`},
		{name: "null override sites", data: stateJSON(`{"demo":null}`, `[]`)},
		{name: "missing site field", data: stateJSON(`{"demo":[{"anchor":"a","find":"Zg=="}]}`, `[]`)},
		{name: "null site field", data: stateJSON(`{"demo":[{"anchor":"a","find":"Zg==","drop":null}]}`, `[]`)},
		{name: "unknown site field", data: stateJSON(`{"demo":[{"anchor":"a","find":"Zg==","drop":"ZA==","legacy":true}]}`, `[]`)},
		{name: "duplicate site field", data: stateJSON(`{"demo":[{"anchor":"a","anchor":"b","find":"Zg==","drop":"ZA=="}]}`, `[]`)},
		{name: "empty sites", data: stateJSON(`{"2.9.9/demo":[]}`, `[]`)},
		{name: "drop outside find", data: stateJSON(`{"2.9.9/demo":[{"anchor":"a","find":"Zg==","drop":"ZA=="}]}`, `[]`)},
		{name: "null pack", data: stateJSON(`{}`, `[null]`)},
		{name: "missing pack field", data: stateJSON(`{}`, `[{"name":"","builtin":false,"owner":"a","repo":"b","ref":""}]`)},
		{name: "null pack field", data: stateJSON(`{}`, `[{"name":"","builtin":false,"owner":"a","repo":"b","ref":"","commit":null}]`)},
		{name: "unknown pack field", data: stateJSON(`{}`, `[{"name":"","builtin":false,"owner":"a","repo":"b","ref":"","commit":"c","legacy":true}]`)},
		{name: "duplicate pack field", data: stateJSON(`{}`, `[{"name":"","builtin":false,"owner":"a","owner":"b","repo":"b","ref":"","commit":"c"}]`)},
		{name: "duplicate pack namespace", data: stateJSON(`{}`, `[{"name":"","builtin":false,"owner":"a","repo":"b","ref":"","commit":"c"},{"name":"","builtin":false,"owner":"a","repo":"b","ref":"main","commit":"d"}]`)},
		{name: "mixed builtin and remote pack", data: stateJSON(`{}`, `[{"name":"fastmode","builtin":true,"owner":"a","repo":"b","ref":"","commit":"c"}]`)},
		{name: "remote traversal", data: stateJSON(`{}`, `[{"name":"","builtin":false,"owner":"..","repo":"b","ref":"","commit":"c"}]`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			writeRawState(t, tc.data)
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %s", tc.data)
			}
		})
	}
}

func TestLoadMissingIsCanonicalEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Overrides == nil || len(state.Overrides) != 0 {
		t.Fatalf("Overrides = %#v, want non-nil empty map", state.Overrides)
	}
	if state.Packs == nil || len(state.Packs) != 0 {
		t.Fatalf("Packs = %#v, want non-nil empty slice", state.Packs)
	}
}

func TestSaveIsAtomicDurableAndPrivate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Put("2.9.9", "demo", []Site{{Anchor: "a", Find: []byte("find-drop"), Drop: []byte("drop")}}); err != nil {
		t.Fatal(err)
	}
	p, err := path()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("state directory mode = %o, want 700", got)
	}
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".state-") {
			t.Fatalf("temporary state leaked: %s", entry.Name())
		}
	}
}

func TestRejectedMutationPreservesPriorDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Put("2.9.9", "good", []Site{{Anchor: "a", Find: []byte("find-drop"), Drop: []byte("drop")}}); err != nil {
		t.Fatal(err)
	}
	p, err := path()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Put("2.9.9", "bad", []Site{{Anchor: "a", Find: []byte("find"), Drop: []byte("outside")}}); err == nil {
		t.Fatal("Put accepted an invalid site")
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("rejected mutation changed state: before=%s after=%s", before, after)
	}
}

func TestConcurrentMutationsDoNotLoseUpdates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const count = 32
	var wait sync.WaitGroup
	for i := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			id := fmt.Sprintf("patch-%02d", i)
			if err := Put("2.9.9", id, []Site{{Anchor: id, Find: []byte("find-drop"), Drop: []byte("drop")}}); err != nil {
				t.Errorf("Put(%s): %v", id, err)
			}
		}()
	}
	wait.Wait()
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Overrides) != count {
		t.Fatalf("overrides = %d, want %d", len(state.Overrides), count)
	}
}

func TestEncodeRejectsNilSemanticCollections(t *testing.T) {
	for _, state := range []State{
		{},
		{Overrides: map[string][]Site{}, Packs: nil},
		{Overrides: map[string][]Site{"demo": nil}, Packs: []InstalledPack{}},
		{Overrides: map[string][]Site{"demo": {{Anchor: "a", Find: nil, Drop: []byte("drop")}}}, Packs: []InstalledPack{}},
	} {
		if _, err := encodeState(state); err == nil {
			t.Fatalf("encodeState accepted %#v", state)
		}
	}
}

func TestPackRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := InstalledPack{Owner: "acme", Repo: "demo", Ref: "main", Commit: "abc123"}
	b := InstalledPack{Owner: "acme", Repo: "other", Commit: "def456"}
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
	if err := RemovePack("acme/demo", nil); err != nil {
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
	if err := AddPack(InstalledPack{Owner: "acme", Repo: "demo", Commit: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := RemovePack("fastmode", nil); err != nil {
		t.Fatal(err)
	}
	packs, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 || packs[0].Builtin {
		t.Fatalf("after remove got %+v, want only the remote pack", packs)
	}
}

func TestRemovePackAndOverridesIsOneMutation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := AddPack(InstalledPack{Name: "fastmode", Builtin: true}); err != nil {
		t.Fatal(err)
	}
	sites := []Site{{Anchor: "a", Find: []byte("abc"), Drop: []byte("b")}}
	for _, id := range []string{"fastmode/delegated-agents", "fastmode/repo/foo", "acme/other/foo"} {
		if err := Put("2.9.9", id, sites); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemovePack("fastmode", []string{"fastmode/delegated-agents"}); err != nil {
		t.Fatal(err)
	}
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Packs) != 0 {
		t.Fatalf("packs = %+v, want empty", state.Packs)
	}
	if _, ok := state.Override("2.9.9", "fastmode/delegated-agents"); ok {
		t.Error("builtin override should have been pruned")
	}
	if _, ok := state.Override("2.9.9", "fastmode/repo/foo"); !ok {
		t.Error("remote fastmode/repo override should have survived")
	}
	if _, ok := state.Override("2.9.9", "acme/other/foo"); !ok {
		t.Error("unrelated override should have survived")
	}
}

func validStateJSON() string { return stateJSON(`{}`, `[]`) }

func stateJSON(overrides, packs string) string {
	return fmt.Sprintf(
		`{"schema":"%s","schemaVersion":1,"schemaFingerprint":"%s","payload":{"overrides":%s,"packs":%s}}`,
		stateSchemaIdentity,
		stateSchemaFingerprint,
		overrides,
		packs,
	)
}

func writeRawState(t *testing.T, data string) {
	t.Helper()
	p, err := path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func findPack(packs []InstalledPack, owner, repo string) InstalledPack {
	for _, pack := range packs {
		if pack.Owner == owner && pack.Repo == repo {
			return pack
		}
	}
	return InstalledPack{}
}
