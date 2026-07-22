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
