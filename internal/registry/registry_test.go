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

func TestSubstitutionPrefersReplace(t *testing.T) {
	site := Site{
		Find:    []byte(`{cmd:"/bin/sh",args:`),
		Replace: []byte(`{cmd:"bash"   ,args:`),
	}
	got := site.Substitution()
	if !bytes.Equal(got.Replace, site.Replace) {
		t.Errorf("Replace = %q, want %q", got.Replace, site.Replace)
	}
	if len(got.Replace) != len(got.Find) {
		t.Errorf("length changed: %d != %d", len(got.Replace), len(got.Find))
	}
}
