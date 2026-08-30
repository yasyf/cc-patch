package registry

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
)

// fastmodeDeriveSpec is the DSL form of the former Go-coded deriveFastMode. It is
// the golden fixture for the derive DSL: the fastmode pack's pack.toml must encode
// exactly these two sites, cross-pinned via the "gate" binding.
func fastmodeDeriveSpec() DeriveSpec {
	return DeriveSpec{Sites: []DeriveSiteSpec{
		{
			Anchor:     "service tier",
			PatternSrc: `(?P<gate>(?:\w+\(\)&&){2}!\w+\(\)&&\w+\(\w+\))(?P<drop>&&!!\w+\.fastMode)\)\w+="fast"`,
			Find:       GroupByIndex(0),
			Drop:       ref(GroupByName("drop")),
			Bind:       []string{"gate"},
		},
		{
			Anchor:     "beta header",
			PatternSrc: `={{gate}}(?P<drop>&&!!\w+\.fastMode)`,
			Find:       GroupByIndex(0),
			Drop:       ref(GroupByName("drop")),
		},
	}}
}

func fastmodePatch() Patch {
	return Patch{
		ID:          "fastmode",
		Summary:     "Fast mode for delegated Opus agents",
		SegmentName: "__BUN",
		Sites: []Site{
			{Anchor: "service tier", Find: []byte(`&&BT(_)&&!!Dn.fastMode)gn="fast"`), Drop: []byte(`&&!!Dn.fastMode`)},
			{Anchor: "beta header", Find: []byte(`ne=vl()&&UO()&&!pAe()&&BT(_)&&!!i.fastMode`), Drop: []byte(`&&!!i.fastMode`)},
		},
		Derive: fastmodeDeriveSpec().DeriveFunc(),
	}
}

func TestDeriveSpecValidates(t *testing.T) {
	if err := fastmodeDeriveSpec().Validate(); err != nil {
		t.Fatalf("fastmode spec should validate: %v", err)
	}
}

func TestDeriveDSLToleratesRenamedLocals(t *testing.T) {
	// Same structure as CC's bundle but every local renamed.
	window := []byte(`...if(zz()&&qq()&&!rr()&&XT(mm)&&!!OP.fastMode)GG="fast";` +
		`let ee=[],NE=zz()&&qq()&&!rr()&&XT(mm)&&!!IP.fastMode;if(f2)push();if(NE)hdr()...`)
	sites, err := fastmodeDeriveSpec().DeriveFunc()(window)
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
	for i, s := range sites {
		sub := s.Substitution()
		if len(sub.Find) != len(sub.Replace) || bytes.Equal(sub.Find, sub.Replace) {
			t.Errorf("derived site %d not a valid length-neutral edit", i)
		}
	}
}

func TestDerivePinnedSitesAreLengthNeutral(t *testing.T) {
	for i, s := range fastmodePatch().Sites {
		sub := s.Substitution()
		if len(sub.Find) != len(sub.Replace) {
			t.Errorf("site %d: find %d != replace %d", i, len(sub.Find), len(sub.Replace))
		}
		if bytes.Equal(sub.Find, sub.Replace) {
			t.Errorf("site %d: replace equals find (drop did nothing)", i)
		}
	}
}

func TestValidateRejectsUnboundInterpolation(t *testing.T) {
	spec := DeriveSpec{Sites: []DeriveSiteSpec{
		{Anchor: "x", PatternSrc: `={{gate}}`, Find: GroupByIndex(0), Drop: ref(GroupByIndex(0))},
	}}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected error for unbound {{gate}}")
	}
}

func TestValidateRejectsMissingGroup(t *testing.T) {
	spec := DeriveSpec{Sites: []DeriveSiteSpec{
		{Anchor: "x", PatternSrc: `abc`, Find: GroupByName("nope"), Drop: ref(GroupByIndex(0))},
	}}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected error for missing named group")
	}
}

func TestDeriveRejectsMultipleMatches(t *testing.T) {
	spec := DeriveSpec{Sites: []DeriveSiteSpec{
		{Anchor: "x", PatternSrc: `(?P<d>a)b`, Find: GroupByIndex(0), Drop: ref(GroupByName("d"))},
	}}
	if _, err := spec.DeriveFunc()([]byte("ab-ab")); err == nil {
		t.Fatal("expected error when pattern matches twice")
	}
}

// TestDeriveDSLMatchesRealBundle proves the DSL derive locates the sites in the
// actually-installed Claude Code binary — each derived Find must be present
// exactly once (not StateMissing) on the pristine install.
func TestDeriveDSLMatchesRealBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("scans the full bundle")
	}
	inst, err := claude.Locate()
	if err != nil {
		t.Skipf("no claude install: %v", err)
	}
	// Derive against the pristine bytes: prefer the backup, since the live binary
	// may already be fastmode-patched (the derive sites blanked to spaces).
	binary := inst.Binary
	if _, err := os.Stat(inst.Backup()); err == nil {
		binary = inst.Backup()
	}
	p := fastmodePatch()
	window, err := binpatch.Window(binary, p.SegmentName)
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

func ref(g GroupRef) *GroupRef { return &g }

func replaceSpec(pattern, replace string) DeriveSpec {
	return DeriveSpec{Sites: []DeriveSiteSpec{
		{Anchor: "guard", PatternSrc: pattern, Find: GroupByIndex(0), Replace: replace},
	}}
}

// TestDeriveReplaceRendersTemplate proves a derive can substitute rather than
// blank, replaying captured groups so a site whose every identifier is minified
// survives a rename without falling through to heal.
func TestDeriveReplaceRendersTemplate(t *testing.T) {
	spec := replaceSpec(`(?P<lead>let \w+=)\w+==="bash"\?(?P<guard>\w+\((?P<cmd>\w+)\)):null;`, `{{lead}}/git/.test({{cmd}})?{{guard}}:0;`)
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	sites, err := spec.DeriveFunc()([]byte(`x;let wn=r==="bash"?Wgt(e):null;y`))
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got, want := string(sites[0].Find), `let wn=r==="bash"?Wgt(e):null;`; got != want {
		t.Fatalf("find = %q, want %q", got, want)
	}
	if got, want := string(sites[0].Replace), `let wn=/git/.test(e)?Wgt(e):0;`; got != want {
		t.Fatalf("replace = %q, want %q", got, want)
	}
	if sites[0].Drop != nil {
		t.Fatalf("replace site carries a drop: %q", sites[0].Drop)
	}
}

// TestDeriveReplaceRejectsLengthChange proves a rename that widens a captured
// group is reported instead of yielding an edit that resizes the segment.
func TestDeriveReplaceRejectsLengthChange(t *testing.T) {
	spec := replaceSpec(`(?P<lead>let \w+=)\w+==="bash"\?(?P<guard>\w+\((?P<cmd>\w+)\)):null;`, `{{lead}}/git/.test({{cmd}})?{{guard}}:0;`)
	_, err := spec.DeriveFunc()([]byte(`let wn=r==="bash"?Wgt(eee):null;`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "differ in length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateRejectsMalformedReplaceSites proves the drop/replace invariants are
// caught statically rather than at derive time against a user's binary.
func TestValidateRejectsMalformedReplaceSites(t *testing.T) {
	for name, tc := range map[string]struct {
		site DeriveSiteSpec
		want string
	}{
		"unknown token": {DeriveSiteSpec{PatternSrc: `(?P<a>x)`, Find: GroupByIndex(0), Replace: `{{nope}}`}, "neither a group in this pattern"},
		"both":          {DeriveSiteSpec{PatternSrc: `(?P<a>x)`, Find: GroupByIndex(0), Drop: ref(GroupByName("a")), Replace: `{{a}}`}, "both drop and replace"},
		"neither":       {DeriveSiteSpec{PatternSrc: `(?P<a>x)`, Find: GroupByIndex(0)}, "neither drop nor replace"},
	} {
		t.Run(name, func(t *testing.T) {
			err := DeriveSpec{Sites: []DeriveSiteSpec{tc.site}}.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
