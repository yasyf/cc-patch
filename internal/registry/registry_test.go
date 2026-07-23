package registry

import (
	"bytes"
	"testing"
)

func TestBlankIsLengthNeutral(t *testing.T) {
	find := []byte(`&&BT(_)&&!!Dn.fastMode)gn="fast"`)
	drop := []byte(`&&!!Dn.fastMode`)
	got := blank(find, drop)
	if len(got) != len(find) {
		t.Fatalf("length changed: %d != %d", len(got), len(find))
	}
	want := []byte(`&&BT(_)               )gn="fast"`)
	if !bytes.Equal(got, want) {
		t.Errorf("blank = %q, want %q", got, want)
	}
}
