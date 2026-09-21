// Package jscpool encodes entries of the JavaScriptCore string constant pool a
// Bun-compiled executable carries, so a patch can rewrite a pooled string rather
// than the retained JS source that only appears to spell it.
package jscpool

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
)

// ErrNotLatin1 reports a string the pool cannot hold in its 8-bit form.
var ErrNotLatin1 = errors.New("string is not Latin-1")

// ErrTooLong reports a replacement wider than the entry it must fit inside.
var ErrTooLong = errors.New("replacement does not fit the pooled entry")

const (
	headerSize = 8
	alignment  = 4
	eightBit   = 1 << 31
	hashMask   = 1<<24 - 1
	maxLength  = 1<<24 - 1
	// nonZeroHash is what WTF substitutes when a hash masks down to zero, which
	// it reserves to mean "not computed yet".
	nonZeroHash = 1 << 23
)

// secret is RapidHash's, as WTF vendors it in Source/WTF/wtf/text/RapidHash.h.
var secret = [3]uint64{0x2d358dccaa6c78a5, 0x8bb84b93962eacc9, 0x4b33a62ed433d4a3}

// Edit is a length-neutral rewrite of one pool entry.
type Edit struct {
	Find    []byte
	Replace []byte
}

// Rewrite renders the edit that replaces the pooled string old with replacement:
// a new length, a recomputed hash, and the characters NUL-padded back out to the
// width old occupied.
func Rewrite(old, replacement string) (Edit, error) {
	find, err := Entry(old)
	if err != nil {
		return Edit{}, fmt.Errorf("pooled string: %w", err)
	}
	replace, err := entry(replacement, len(find)-headerSize)
	if err != nil {
		return Edit{}, fmt.Errorf("replacement: %w", err)
	}
	return Edit{Find: find, Replace: replace}, nil
}

// Entry encodes s as the pool holds it: an 8-bit flag and the length, the hash
// WTF precomputes, then the characters padded with NULs to a four-byte boundary.
func Entry(s string) ([]byte, error) {
	return entry(s, width(len(s)))
}

// Hash returns the 24-bit hash WTF stores for a Latin-1 string, the value
// StringHasher::computeHashAndMaskTop8Bits yields.
func Hash(chars []byte) uint32 {
	h := uint32(rapidHash(chars) & hashMask) //nolint:gosec // hashMask bounds the value to 24 bits
	if h == 0 {
		return nonZeroHash
	}
	return h
}

func entry(s string, width int) ([]byte, error) {
	chars, err := latin1(s)
	if err != nil {
		return nil, err
	}
	if len(chars) > width {
		return nil, fmt.Errorf("%w: %q needs %d bytes, the entry holds %d", ErrTooLong, s, len(chars), width)
	}
	if len(chars) > maxLength {
		return nil, fmt.Errorf("%w: %d characters overruns the header's 24-bit length", ErrTooLong, len(chars))
	}
	out := make([]byte, headerSize+width)
	binary.LittleEndian.PutUint32(out, eightBit|uint32(len(chars))) //nolint:gosec // the length is bounded above
	binary.LittleEndian.PutUint32(out[4:], Hash(chars))
	copy(out[headerSize:], chars)
	return out, nil
}

func width(n int) int {
	return (n + alignment - 1) / alignment * alignment
}

func latin1(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return nil, fmt.Errorf("%w: %q holds %q", ErrNotLatin1, s, r)
		}
		out = append(out, byte(r)) //nolint:gosec // the rune is bounded to Latin-1 above
	}
	return out, nil
}

func rapidHash(data []byte) uint64 {
	n := uint64(len(data))
	seed := mix(secret[0], secret[1]) ^ n
	var a, b uint64
	switch {
	case n >= 4 && n <= 16:
		delta := (n & 24) >> (n >> 3)
		a = read32(data)<<32 | read32(data[n-4:])
		b = read32(data[delta:])<<32 | read32(data[n-4-delta:])
	case n > 0 && n < 4:
		a = readSmall(data)
	case n > 16:
		rest := data
		if len(rest) > 48 {
			see1, see2 := seed, seed
			for len(rest) >= 48 {
				seed = mix(read64(rest)^secret[0], read64(rest[8:])^seed)
				see1 = mix(read64(rest[16:])^secret[1], read64(rest[24:])^see1)
				see2 = mix(read64(rest[32:])^secret[2], read64(rest[40:])^see2)
				rest = rest[48:]
			}
			seed ^= see1 ^ see2
		}
		if len(rest) > 16 {
			seed = mix(read64(rest)^secret[2], read64(rest[8:])^seed^secret[1])
			if len(rest) > 32 {
				seed = mix(read64(rest[16:])^secret[2], read64(rest[24:])^seed)
			}
		}
		a = read64(data[n-16:])
		b = read64(data[n-8:])
	}
	a ^= secret[1]
	b ^= seed
	hi, lo := bits.Mul64(a, b)
	return mix(lo^secret[0]^n, hi^secret[1])
}

func mix(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	return lo ^ hi
}

func read64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

func read32(b []byte) uint64 { return uint64(binary.LittleEndian.Uint32(b)) }

func readSmall(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[len(b)>>1])<<32 | uint64(b[len(b)-1])
}
