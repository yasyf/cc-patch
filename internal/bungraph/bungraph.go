// Package bungraph reads the module graph a Bun `--compile --bytecode`
// executable embeds in its `__BUN` segment, so a source edit can take its
// module off JavaScriptCore bytecode and onto the patched source.
package bungraph

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrMalformed reports a segment that carries Bun's trailer but whose graph
// records do not fit inside it.
var ErrMalformed = errors.New("malformed bun module graph")

const (
	lengthPrefix    = 8
	offsetsSize     = 32
	recordSize      = 52
	pointerSize     = 8
	bytecodeField   = 24
	hashSize        = 4
	hasSourceHashes = 1 << 5
)

var trailer = []byte("\n---- Bun! ----\n")

// Module is one file of the graph, located by offsets into the segment window.
type Module struct {
	Name string
	// Start and End bound the module's source text.
	Start, End int
	// BytecodeAt is where the module record's bytecode pointer lives; the
	// module runs from bytecode while that pointer's length is non-zero.
	BytecodeAt int
	Bytecode   bool
	// HashAt is where the module's precomputed source hash lives, and Hashed
	// whether it holds one; Bun hashes the source itself when it does not.
	HashAt int
	Hashed bool
}

// Write is one in-place, length-neutral edit to the segment window.
type Write struct {
	At   int
	Data []byte
}

// Graph is the parsed module table of one segment window.
type Graph struct {
	Modules []Module
}

// Parse reads the module graph out of a `__BUN` segment window. A window
// without Bun's trailer holds no graph and yields an empty one.
func Parse(window []byte) (Graph, error) {
	if len(window) < lengthPrefix {
		return Graph{}, nil
	}
	size := binary.LittleEndian.Uint64(window)
	if size < uint64(len(trailer)+offsetsSize) || size > uint64(len(window)-lengthPrefix) { //nolint:gosec // a slice length is never negative
		return Graph{}, nil
	}
	payload := window[lengthPrefix : lengthPrefix+int(size)] //nolint:gosec // size is bounded by the window above
	if !bytes.HasSuffix(payload, trailer) {
		return Graph{}, nil
	}
	offsets := payload[len(payload)-len(trailer)-offsetsSize:]
	table, tableLen := u32(offsets, 8), u32(offsets, 12)
	flags := binary.LittleEndian.Uint32(offsets[28:])
	if tableLen%recordSize != 0 || table+tableLen > len(payload) {
		return Graph{}, fmt.Errorf("%w: module table [%d,%d) in a %d-byte payload", ErrMalformed, table, table+tableLen, len(payload))
	}
	count := tableLen / recordSize
	hashes := -1
	if flags&hasSourceHashes != 0 {
		hashes = table + tableLen
		if hashes+count*hashSize > len(payload) {
			return Graph{}, fmt.Errorf("%w: source hash table overruns the payload", ErrMalformed)
		}
	}
	g := Graph{Modules: make([]Module, count)}
	for i := range count {
		record := payload[table+i*recordSize:]
		name, nameLen := u32(record, 0), u32(record, 4)
		start, length := u32(record, 8), u32(record, 12)
		if name+nameLen > len(payload) || start+length > len(payload) {
			return Graph{}, fmt.Errorf("%w: module %d points outside the payload", ErrMalformed, i)
		}
		m := Module{
			Name:       string(payload[name : name+nameLen]),
			Start:      lengthPrefix + start,
			End:        lengthPrefix + start + length,
			BytecodeAt: lengthPrefix + table + i*recordSize + bytecodeField,
			Bytecode:   u32(record, bytecodeField+4) > 0,
			HashAt:     -1,
		}
		if hashes >= 0 {
			m.HashAt = lengthPrefix + hashes + i*hashSize
			m.Hashed = u32(payload, hashes+i*hashSize) != 0
		}
		g.Modules[i] = m
	}
	return g, nil
}

// Owner returns the module whose source text holds all of [start, end) of the
// window. A range in no module's source, or straddling two, has no owner.
func (g Graph) Owner(start, end int) (Module, bool) {
	for _, m := range g.Modules {
		if start >= m.Start && end <= m.End && m.End > m.Start {
			return m, true
		}
	}
	return Module{}, false
}

// Release returns the writes that move m onto its source text: an empty
// bytecode pointer, which Bun reads as "compile this module from source", and
// a zero source hash, which Bun reads as "hash the source when loading it",
// since an edited source no longer matches the precomputed hash. A module
// already on its source needs none.
func Release(m Module) []Write {
	var out []Write
	if m.Bytecode {
		out = append(out, Write{At: m.BytecodeAt, Data: make([]byte, pointerSize)})
	}
	if m.Hashed {
		out = append(out, Write{At: m.HashAt, Data: make([]byte, hashSize)})
	}
	return out
}

func u32(b []byte, at int) int {
	return int(binary.LittleEndian.Uint32(b[at:]))
}
