package pack

import (
	"bytes"
	"strings"
	"testing"
)

const validPack = `
schema = 1
[[patch]]
id      = "fastmode"
summary = "Fast mode"
[[patch.site]]
anchor = "service tier"
find   = '&&BT(_)&&!!Dn.fastMode)gn="fast"'
drop   = '&&!!Dn.fastMode'
`

func TestPatchesValid(t *testing.T) {
	m, err := Parse([]byte(validPack))
	if err != nil {
		t.Fatal(err)
	}
	patches, err := m.Patches("acme/demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("got %d patches, want 1", len(patches))
	}
	p := patches[0]
	if p.ID != "acme/demo/fastmode" {
		t.Errorf("ID = %q, want acme/demo/fastmode", p.ID)
	}
	if p.SegmentName != "__BUN" {
		t.Errorf("SegmentName = %q, want __BUN (default)", p.SegmentName)
	}
	if p.Derive != nil {
		t.Error("Derive should be nil with no [[patch.derive]]")
	}
	if len(p.Sites) != 1 {
		t.Fatalf("got %d sites, want 1", len(p.Sites))
	}
	if !bytes.Equal(p.Sites[0].Find, []byte(`&&BT(_)&&!!Dn.fastMode)gn="fast"`)) {
		t.Errorf("Find = %q", p.Sites[0].Find)
	}
	if !bytes.Equal(p.Sites[0].Drop, []byte(`&&!!Dn.fastMode`)) {
		t.Errorf("Drop = %q", p.Sites[0].Drop)
	}
}

func TestPatchesRejects(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
	}{
		{
			name: "drop not in find",
			toml: `
schema = 1
[[patch]]
id = "x"
summary = "y"
[[patch.site]]
anchor = "a"
find = 'abc'
drop = 'zzz'
`,
			want: "not a substring",
		},
		{
			name: "schema not 1",
			toml: `
schema = 2
[[patch]]
id = "x"
summary = "y"
[[patch.site]]
anchor = "a"
find = 'abc'
drop = 'b'
`,
			want: "unsupported pack schema",
		},
		{
			name: "bad group type",
			toml: `
schema = 1
[[patch]]
id = "x"
summary = "y"
[[patch.site]]
anchor = "a"
find = 'abc'
drop = 'b'
[[patch.derive]]
anchor = "a"
pattern = 'abc'
find = 1.5
drop = 0
`,
			want: "int index or string group name",
		},
		{
			name: "bad patch id",
			toml: `
schema = 1
[[patch]]
id = "Bad_ID"
summary = "y"
[[patch.site]]
anchor = "a"
find = 'abc'
drop = 'b'
`,
			want: "must match",
		},
		{
			name: "no sites",
			toml: `
schema = 1
[[patch]]
id = "x"
summary = "y"
`,
			want: "at least one",
		},
		{
			name: "find and find_b64 both set",
			toml: `
schema = 1
[[patch]]
id = "x"
summary = "y"
[[patch.site]]
anchor = "a"
find = 'abc'
find_b64 = 'YWJj'
drop = 'b'
`,
			want: "exactly one",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Parse([]byte(tt.toml))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, err = m.Patches("acme/demo")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

const derivePack = `
schema = 1
[[patch]]
id = "fastmode"
summary = "Fast mode"
[[patch.site]]
anchor = "service tier"
find = '&&BT(_)&&!!Dn.fastMode)gn="fast"'
drop = '&&!!Dn.fastMode'
[[patch.site]]
anchor = "beta header"
find = 'ne=vl()&&UO()&&!pAe()&&BT(_)&&!!i.fastMode'
drop = '&&!!i.fastMode'
[[patch.derive]]
anchor  = "service tier"
pattern = '(?P<gate>(?:\w+\(\)&&){2}!\w+\(\)&&\w+\(\w+\))(?P<drop>&&!!\w+\.fastMode)\)\w+="fast"'
find    = 0
drop    = "drop"
bind    = ["gate"]
[[patch.derive]]
anchor  = "beta header"
pattern = '={{gate}}(?P<drop>&&!!\w+\.fastMode)'
find    = 0
drop    = "drop"
`

func TestPatchesDeriveLocatesSites(t *testing.T) {
	m, err := Parse([]byte(derivePack))
	if err != nil {
		t.Fatal(err)
	}
	patches, err := m.Patches("acme/demo")
	if err != nil {
		t.Fatal(err)
	}
	derive := patches[0].Derive
	if derive == nil {
		t.Fatal("Derive is nil for a pack with [[patch.derive]]")
	}
	window := []byte(`if(vl()&&UO()&&!pAe()&&BT(_)&&!!Dn.fastMode)gn="fast";var ne=vl()&&UO()&&!pAe()&&BT(_)&&!!i.fastMode;`)
	sites, err := derive(window)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 {
		t.Fatalf("got %d derived sites, want 2", len(sites))
	}
	if !bytes.Equal(sites[0].Drop, []byte(`&&!!Dn.fastMode`)) {
		t.Errorf("site 0 Drop = %q", sites[0].Drop)
	}
	if !bytes.Equal(sites[1].Drop, []byte(`&&!!i.fastMode`)) {
		t.Errorf("site 1 Drop = %q", sites[1].Drop)
	}
	for i, s := range sites {
		if !bytes.Contains(s.Find, s.Drop) {
			t.Errorf("site %d: Find %q does not contain Drop %q", i, s.Find, s.Drop)
		}
	}
}

const replacePack = `
schema = 1
[[patch]]
id      = "hook-shell"
summary = "Spawn hooks through bash"
[[patch.site]]
anchor  = "hook spawn"
find    = '{cmd:"/bin/sh",args:["-c",'
replace = '{cmd:"bash"   ,args:["-c",'
`

func TestPatchesReplaceSite(t *testing.T) {
	m, err := Parse([]byte(replacePack))
	if err != nil {
		t.Fatal(err)
	}
	patches, err := m.Patches("acme/demo")
	if err != nil {
		t.Fatal(err)
	}
	site := patches[0].Sites[0]
	if site.Drop != nil {
		t.Errorf("Drop = %q, want nil for a replace site", site.Drop)
	}
	want := []byte(`{cmd:"bash"   ,args:["-c",`)
	if !bytes.Equal(site.Replace, want) {
		t.Errorf("Replace = %q, want %q", site.Replace, want)
	}
	sub := site.Substitution()
	if !bytes.Equal(sub.Replace, want) {
		t.Errorf("Substitution().Replace = %q, want %q", sub.Replace, want)
	}
}

const poolPack = `
schema = 1
[[patch]]
id      = "hook-shell"
summary = "Spawn hooks through bash"
[[patch.site]]
anchor       = "hook spawn (bytecode pool)"
pool_find    = '/bin/sh'
pool_replace = 'bash'
`

func TestPatchesPoolSite(t *testing.T) {
	m, err := Parse([]byte(poolPack))
	if err != nil {
		t.Fatal(err)
	}
	patches, err := m.Patches("acme/demo")
	if err != nil {
		t.Fatal(err)
	}
	site := patches[0].Sites[0]
	if site.Drop != nil {
		t.Errorf("Drop = %q, want nil for a pool site", site.Drop)
	}
	wantFind := []byte{0x07, 0x00, 0x00, 0x80, 0x61, 0xcd, 0x60, 0x00, '/', 'b', 'i', 'n', '/', 's', 'h', 0x00}
	if !bytes.Equal(site.Find, wantFind) {
		t.Errorf("Find = %#v, want %#v", site.Find, wantFind)
	}
	wantReplace := []byte{0x04, 0x00, 0x00, 0x80, 0xbb, 0x65, 0x26, 0x00, 'b', 'a', 's', 'h', 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(site.Replace, wantReplace) {
		t.Errorf("Replace = %#v, want %#v", site.Replace, wantReplace)
	}
	sub := site.Substitution()
	if len(sub.Find) != len(sub.Replace) {
		t.Errorf("substitution is not length-neutral: %d vs %d", len(sub.Find), len(sub.Replace))
	}
}

const pinnedDerivePack = `
schema = 1
[[patch]]
id = "noshadow"
summary = "Fall back to the system tool"
[[patch.site]]
anchor       = "bytecode"
pool_find    = '/bin/sh'
pool_replace = 'bash'
[[patch.site]]
anchor = "source"
find   = 'sh=${e}'
drop   = 'sh'
[[patch.derive]]
anchor = "bytecode"
pinned = true
[[patch.derive]]
anchor  = "source"
pattern = '(?P<drop>sh)=\$\{\w+\}'
find    = 0
drop    = "drop"
`

// TestPatchesPinnedDeriveReemitsItsSite proves a derive covers every site even
// when one of them is a pool entry no pattern can render: a derive replaces the
// whole site list, so a site it omits is silently left unpatched.
func TestPatchesPinnedDeriveReemitsItsSite(t *testing.T) {
	m, err := Parse([]byte(pinnedDerivePack))
	if err != nil {
		t.Fatal(err)
	}
	patches, err := m.Patches("acme/demo")
	if err != nil {
		t.Fatal(err)
	}
	p := patches[0]
	sites, err := p.Derive([]byte(`var sh=${qq};`))
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != len(p.Sites) {
		t.Fatalf("derived %d sites for %d pinned sites", len(sites), len(p.Sites))
	}
	if !bytes.Equal(sites[0].Find, p.Sites[0].Find) || !bytes.Equal(sites[0].Replace, p.Sites[0].Replace) {
		t.Errorf("pinned derive site = %+v, want the pool site %+v verbatim", sites[0], p.Sites[0])
	}
	if !bytes.Equal(sites[1].Drop, []byte("sh")) {
		t.Errorf("site 1 Drop = %q, want the pattern-derived drop", sites[1].Drop)
	}
}

// TestPatchesRejectsPinnedWithPatternFields proves a pinned block that also
// carries pattern fields fails to compile rather than having them silently
// dropped, which would read as a working pattern that never runs.
func TestPatchesRejectsPinnedWithPatternFields(t *testing.T) {
	for _, extra := range []string{
		`pattern = 'sh=\$\{\w+\}'`,
		"find    = 0",
		`drop    = "drop"`,
		`replace = '{{lead}}x'`,
		`bind    = ["lead"]`,
	} {
		t.Run(extra, func(t *testing.T) {
			src := strings.Replace(pinnedDerivePack, `anchor = "bytecode"
pinned = true`, `anchor = "bytecode"
pinned = true
`+extra, 1)
			m, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Patches("acme/demo"); err == nil {
				t.Fatal("a pinned derive carrying pattern fields should not compile")
			} else if !strings.Contains(err.Error(), "pinned takes no") {
				t.Errorf("error = %q, want it to name the pinned exclusivity rule", err)
			}
		})
	}
}

func TestPatchesRejectsPartialDerive(t *testing.T) {
	src := strings.Replace(pinnedDerivePack, `[[patch.derive]]
anchor = "bytecode"
pinned = true
`, "", 1)
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Patches("acme/demo")
	if err == nil {
		t.Fatal("a derive covering 1 of 2 sites should not compile")
	}
	if !strings.Contains(err.Error(), "must cover every site") {
		t.Errorf("error = %q, want it to name the coverage rule", err)
	}
}

func TestPatchesPoolRejects(t *testing.T) {
	tests := []struct {
		name, site, want string
	}{
		{
			name: "replacement overruns the entry",
			site: "pool_find = '/bin/sh'\npool_replace = '/usr/bin/bash'",
			want: "does not fit",
		},
		{
			name: "only one half set",
			site: "pool_find = '/bin/sh'",
			want: "set both",
		},
		{
			name: "mixed with a find site",
			site: "pool_find = '/bin/sh'\npool_replace = 'bash'\nfind = 'abcd'\nreplace = 'wxyz'",
			want: "stand in for",
		},
		{
			name: "not latin-1",
			site: "pool_find = '/bin/sh'\npool_replace = '/bin/中'",
			want: "not Latin-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "schema = 1\n[[patch]]\nid = \"p\"\nsummary = \"s\"\n[[patch.site]]\nanchor = \"a\"\n" + tt.site + "\n"
			m, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Patches("acme/demo")
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q missing %q", err, tt.want)
			}
		})
	}
}

func TestPatchesReplaceRejects(t *testing.T) {
	tests := []struct {
		name, site, want string
	}{
		{
			name: "length mismatch",
			site: "find = 'abcd'\nreplace = 'ab'",
			want: "differ in length",
		},
		{
			name: "drop and replace",
			site: "find = 'abcd'\nreplace = 'wxyz'\ndrop = 'bc'",
			want: "not both",
		},
		{
			name: "neither drop nor replace",
			site: "find = 'abcd'",
			want: "set exactly one of drop",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "schema = 1\n[[patch]]\nid = \"p\"\nsummary = \"s\"\n[[patch.site]]\nanchor = \"a\"\n" + tt.site + "\n"
			m, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Patches("acme/demo")
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q missing %q", err, tt.want)
			}
		})
	}
}
