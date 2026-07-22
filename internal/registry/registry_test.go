package registry

import (
	"bytes"
	"testing"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
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

func TestPinnedSitesAreLengthNeutral(t *testing.T) {
	for _, p := range All() {
		for i, s := range p.Sites {
			sub := s.Substitution()
			if len(sub.Find) != len(sub.Replace) {
				t.Errorf("%s site %d: find %d != replace %d", p.ID, i, len(sub.Find), len(sub.Replace))
			}
			if bytes.Equal(sub.Find, sub.Replace) {
				t.Errorf("%s site %d: replace equals find (drop did nothing)", p.ID, i)
			}
		}
	}
}

func TestDeriveToleratesRenamedLocals(t *testing.T) {
	// Same structure as CC's bundle but every local renamed.
	window := []byte(`...if(zz()&&qq()&&!rr()&&XT(mm)&&!!OP.fastMode)GG="fast";` +
		`let ee=[],NE=zz()&&qq()&&!rr()&&XT(mm)&&!!IP.fastMode;if(f2)push();if(NE)hdr()...`)
	sites, err := deriveFastMode(window)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 {
		t.Fatalf("got %d sites, want 2", len(sites))
	}
	if got := string(sites[0].Find); got != `zz()&&qq()&&!rr()&&XT(mm)&&!!OP.fastMode)GG="fast"` {
		t.Errorf("site A find = %q", got)
	}
	if got := string(sites[0].Drop); got != `&&!!OP.fastMode` {
		t.Errorf("site A drop = %q", got)
	}
	if got := string(sites[1].Find); got != `=zz()&&qq()&&!rr()&&XT(mm)&&!!IP.fastMode` {
		t.Errorf("site B find = %q", got)
	}
	if got := string(sites[1].Drop); got != `&&!!IP.fastMode` {
		t.Errorf("site B drop = %q", got)
	}
	// Every derived site must be length-neutral and actually blank something.
	for i, s := range sites {
		sub := s.Substitution()
		if len(sub.Find) != len(sub.Replace) || bytes.Equal(sub.Find, sub.Replace) {
			t.Errorf("derived site %d not a valid length-neutral edit", i)
		}
	}
}

// TestDeriveMatchesRealBundle proves structural derivation locates the sites in
// the actually-installed Claude Code binary — each derived Find must be present
// exactly once (StateUnpatched) on the pristine install.
func TestDeriveMatchesRealBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("scans the full 184MB bundle")
	}
	inst, err := claude.Locate()
	if err != nil {
		t.Skipf("no claude install: %v", err)
	}
	p := fastModeDelegatedAgents()
	window, err := binpatch.Window(inst.Binary, p.SegmentName)
	if err != nil {
		t.Skipf("read bundle window: %v", err)
	}
	derived, err := p.Derive(window)
	if err != nil {
		t.Fatalf("derive on real bundle: %v", err)
	}
	res, err := binpatch.Status(inst.Binary, p.SegmentName, Substitutions(derived))
	if err != nil {
		t.Fatalf("status with derived sites: %v", err)
	}
	for _, s := range res.Sites {
		if s.State == binpatch.StateMissing {
			t.Errorf("derived site %d not found in real binary", s.Index)
		}
	}
}
