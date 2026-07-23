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
