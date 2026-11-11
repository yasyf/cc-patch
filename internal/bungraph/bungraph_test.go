package bungraph

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

type fixtureModule struct {
	name     string
	source   string
	bytecode bool
	hash     uint32
}

func buildWindow(modules []fixtureModule, flags uint32) []byte {
	var payload []byte
	type span struct{ off, n int }
	names := make([]span, len(modules))
	sources := make([]span, len(modules))
	for i, m := range modules {
		names[i] = span{len(payload), len(m.name)}
		payload = append(payload, m.name...)
		sources[i] = span{len(payload), len(m.source)}
		payload = append(payload, m.source...)
	}
	table := len(payload)
	for i, m := range modules {
		record := make([]byte, recordSize)
		put(record, 0, names[i].off)
		put(record, 4, names[i].n)
		put(record, 8, sources[i].off)
		put(record, 12, sources[i].n)
		if m.bytecode {
			binary.LittleEndian.PutUint32(record[24:], 7)
			binary.LittleEndian.PutUint32(record[28:], 99)
		}
		payload = append(payload, record...)
	}
	tableLen := len(payload) - table
	if flags&hasSourceHashes != 0 {
		for _, m := range modules {
			payload = binary.LittleEndian.AppendUint32(payload, m.hash)
		}
	}
	offsets := make([]byte, offsetsSize)
	put(offsets, 8, table)
	put(offsets, 12, tableLen)
	binary.LittleEndian.PutUint32(offsets[28:], flags)
	payload = append(payload, offsets...)
	payload = append(payload, trailer...)
	window := binary.LittleEndian.AppendUint64(nil, uint64(len(payload)))
	return append(window, payload...)
}

func TestParseWithoutTrailer(t *testing.T) {
	for _, window := range [][]byte{nil, []byte("short"), bytes.Repeat([]byte{0xff}, 64)} {
		g, err := Parse(window)
		if err != nil || len(g.Modules) != 0 {
			t.Errorf("Parse(%q) = %d modules, %v; want 0, nil", window, len(g.Modules), err)
		}
	}
}

func TestParseModules(t *testing.T) {
	window := buildWindow([]fixtureModule{
		{name: "/$bunfs/root/a.js", source: "let a=1;", bytecode: true, hash: 0xabc},
		{name: "/$bunfs/root/b.txt", source: "plain"},
	}, hasSourceHashes)
	g, err := Parse(window)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Modules) != 2 {
		t.Fatalf("got %d modules, want 2", len(g.Modules))
	}
	a, b := g.Modules[0], g.Modules[1]
	if a.Name != "/$bunfs/root/a.js" || string(window[a.Start:a.End]) != "let a=1;" || !a.Bytecode || !a.Hashed {
		t.Errorf("module a = %+v", a)
	}
	if b.Name != "/$bunfs/root/b.txt" || string(window[b.Start:b.End]) != "plain" || b.Bytecode || b.Hashed {
		t.Errorf("module b = %+v", b)
	}
	if got := binary.LittleEndian.Uint32(window[a.BytecodeAt+4:]); got != 99 {
		t.Errorf("bytecode length at BytecodeAt+4 = %d, want 99", got)
	}
	if got := binary.LittleEndian.Uint32(window[a.HashAt:]); got != 0xabc {
		t.Errorf("hash at HashAt = %#x, want 0xabc", got)
	}
}

func TestParseWithoutHashes(t *testing.T) {
	g, err := Parse(buildWindow([]fixtureModule{{name: "a", source: "x", bytecode: true}}, 0))
	if err != nil {
		t.Fatal(err)
	}
	if m := g.Modules[0]; m.HashAt != -1 || m.Hashed {
		t.Errorf("module = %+v, want no hash slot", m)
	}
	if got := Release(g.Modules[0]); len(got) != 1 || got[0].At != g.Modules[0].BytecodeAt {
		t.Errorf("Release = %+v, want only the bytecode pointer", got)
	}
}

func TestParseMalformedTable(t *testing.T) {
	window := buildWindow([]fixtureModule{{name: "a", source: "x", bytecode: true}}, 0)
	offsets := len(window) - len(trailer) - offsetsSize
	binary.LittleEndian.PutUint32(window[offsets+12:], recordSize+1)
	if _, err := Parse(window); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Parse = %v, want ErrMalformed", err)
	}
}

func TestOwner(t *testing.T) {
	window := buildWindow([]fixtureModule{
		{name: "a", source: "first module", bytecode: true},
		{name: "b", source: "second module", bytecode: true},
	}, 0)
	g, err := Parse(window)
	if err != nil {
		t.Fatal(err)
	}
	a, b := g.Modules[0], g.Modules[1]
	tests := []struct {
		name       string
		start, end int
		want       string
	}{
		{"inside a", a.Start + 1, a.Start + 5, "a"},
		{"all of b", b.Start, b.End, "b"},
		{"straddles a and b", a.End - 2, b.Start + 2, ""},
		{"outside every source", b.End + 1, b.End + 3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ok := g.Owner(tt.start, tt.end)
			if got := map[bool]string{true: m.Name, false: ""}[ok]; got != tt.want {
				t.Errorf("Owner(%d, %d) = %q, want %q", tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestRelease(t *testing.T) {
	window := buildWindow([]fixtureModule{
		{name: "a", source: "x", bytecode: true, hash: 5},
		{name: "b", source: "y"},
	}, hasSourceHashes)
	g, err := Parse(window)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range Release(g.Modules[0]) {
		copy(window[w.At:], w.Data)
	}
	after, err := Parse(window)
	if err != nil {
		t.Fatal(err)
	}
	if m := after.Modules[0]; m.Bytecode || m.Hashed {
		t.Errorf("released module = %+v, want neither bytecode nor hash", m)
	}
	if got := Release(after.Modules[0]); len(got) != 0 {
		t.Errorf("Release of a released module = %+v, want none", got)
	}
	if got := Release(g.Modules[1]); len(got) != 0 {
		t.Errorf("Release of a source-only module = %+v, want none", got)
	}
}

func put(b []byte, at, v int) {
	binary.LittleEndian.PutUint32(b[at:], uint32(v)) //nolint:gosec // fixture offsets are small
}
