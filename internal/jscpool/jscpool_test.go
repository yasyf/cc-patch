package jscpool

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// pooled are entries read out of a real Bun-compiled Claude Code binary
// (2.1.274), spanning every branch of the hash: the 1-3 byte tail, the 4-16 byte
// pair, the 17-48 byte body and the 48-byte striped loop.
var pooled = []struct {
	s    string
	hash uint32
}{
	{"/s", 0x76811d},
	{"/c", 0x9076ef},
	{"cmd.exe", 0x32f6d8},
	{"/bin/sh", 0x60cd61},
	{"SHELL", 0x87b47d},
	{"COMSPEC", 0xc72fbb},
	{"gate_blocked", 0xfb4fbd},
	{". `claude attach ", 0x66581e},
	{"` opens the original.", 0x1cdc29},
	{". The original conversation is unchanged.", 0x55d8a2},
	{", so this started a copy as ", 0x2cc937},
	{" is already running in the background, so this started a copy as ", 0xff5df4},
	{" is open in another Claude Code process, so this started a copy as ", 0x96be28},
	{"note: another background session already uses the id ", 0xed4994},
	{". To continue a session under its own id, pass its full session id (lowercase, as `claude agents --json` prints it) to --resume.", 0x6d4142},
}

func TestHashMatchesPooledEntries(t *testing.T) {
	for _, tt := range pooled {
		t.Run(tt.s, func(t *testing.T) {
			if got := Hash([]byte(tt.s)); got != tt.hash {
				t.Errorf("Hash(%q) = %#06x, want %#06x", tt.s, got, tt.hash)
			}
		})
	}
}

func TestEntryEncodesPooledBytes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []byte
	}{
		{
			name: "padded to the boundary",
			s:    "/bin/sh",
			want: []byte{0x07, 0x00, 0x00, 0x80, 0x61, 0xcd, 0x60, 0x00, '/', 'b', 'i', 'n', '/', 's', 'h', 0x00},
		},
		{
			name: "already on the boundary",
			s:    "gate_blocked",
			want: []byte{0x0c, 0x00, 0x00, 0x80, 0xbd, 0x4f, 0xfb, 0x00, 'g', 'a', 't', 'e', '_', 'b', 'l', 'o', 'c', 'k', 'e', 'd'},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Entry(tt.s)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Entry(%q) = %#v, want %#v", tt.s, got, tt.want)
			}
		})
	}
}

func TestEntryRoundTrips(t *testing.T) {
	for _, tt := range pooled {
		t.Run(tt.s, func(t *testing.T) {
			raw, err := Entry(tt.s)
			if err != nil {
				t.Fatal(err)
			}
			length, hash, chars := decode(t, raw)
			if length != len(tt.s) {
				t.Errorf("length = %d, want %d", length, len(tt.s))
			}
			if hash != tt.hash {
				t.Errorf("hash = %#06x, want %#06x", hash, tt.hash)
			}
			if string(chars[:length]) != tt.s {
				t.Errorf("chars = %q, want %q", chars[:length], tt.s)
			}
			if !bytes.Equal(chars[length:], make([]byte, len(chars)-length)) {
				t.Errorf("padding = %#v, want NULs", chars[length:])
			}
			if len(raw)%alignment != 0 {
				t.Errorf("entry is %d bytes, not a multiple of %d", len(raw), alignment)
			}
		})
	}
}

func TestRewriteShrinksInPlace(t *testing.T) {
	edit, err := Rewrite("/bin/sh", "bash")
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.Find) != len(edit.Replace) {
		t.Fatalf("Find is %d bytes, Replace is %d — the edit must be length-neutral", len(edit.Find), len(edit.Replace))
	}
	length, hash, chars := decode(t, edit.Replace)
	if length != 4 {
		t.Errorf("length = %d, want 4", length)
	}
	// bashHash is pinned independently of Hash, so the first string this site
	// type was written to write is checked against an outside value rather than
	// against the implementation that produced it.
	const bashHash = 0x2665bb
	if hash != bashHash {
		t.Errorf("hash = %#06x, want %#06x", hash, bashHash)
	}
	if hash == Hash([]byte("/bin/sh")) {
		t.Error("hash is still the replaced string's")
	}
	if string(chars[:4]) != "bash" {
		t.Errorf("chars = %q, want bash", chars[:4])
	}
	if !bytes.Equal(chars[4:], make([]byte, len(chars)-4)) {
		t.Errorf("tail = %#v, want NUL padding", chars[4:])
	}
}

func TestRewriteWidensWithinTheBoundary(t *testing.T) {
	edit, err := Rewrite("/bin/sh", "/bin/zsh")
	if err != nil {
		t.Fatal(err)
	}
	length, _, chars := decode(t, edit.Replace)
	if length != 8 {
		t.Errorf("length = %d, want 8 — an entry padded to 8 bytes holds one more character", length)
	}
	if string(chars) != "/bin/zsh" {
		t.Errorf("chars = %q, want /bin/zsh", chars)
	}
}

func TestRewriteRejects(t *testing.T) {
	tests := []struct {
		name             string
		old, replacement string
		want             error
	}{
		{"replacement overruns the entry", "/bin/sh", "/usr/bin/bash", ErrTooLong},
		{"replacement is not latin-1", "/bin/sh", "/bin/中", ErrNotLatin1},
		{"pooled string is not latin-1", "/bin/中", "bash", ErrNotLatin1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Rewrite(tt.old, tt.replacement)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestHashAvoidsZero(t *testing.T) {
	for _, s := range []string{"", "a", "ab", "abc"} {
		if got := Hash([]byte(s)); got == 0 {
			t.Errorf("Hash(%q) = 0, which WTF reserves for an uncomputed hash", s)
		}
	}
}

func TestEntryFillsEveryWidth(t *testing.T) {
	for n := 1; n <= 64; n++ {
		s := strings.Repeat("x", n)
		raw, err := Entry(s)
		if err != nil {
			t.Fatal(err)
		}
		if want := headerSize + width(n); len(raw) != want {
			t.Errorf("Entry(%d chars) is %d bytes, want %d", n, len(raw), want)
		}
	}
}

func decode(t *testing.T, raw []byte) (length int, hash uint32, chars []byte) {
	t.Helper()
	if len(raw) < headerSize {
		t.Fatalf("entry is %d bytes, shorter than its header", len(raw))
	}
	header := binary.LittleEndian.Uint32(raw)
	if header&eightBit == 0 {
		t.Errorf("header %#08x does not carry the 8-bit flag", header)
	}
	return int(header &^ eightBit), binary.LittleEndian.Uint32(raw[4:]), raw[headerSize:]
}
