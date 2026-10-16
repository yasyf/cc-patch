// Package macho locates named segments inside a Mach-O binary, resolving their
// absolute file offset for both thin and universal (fat) images.
package macho

import (
	"debug/macho"
	"errors"
	"fmt"
	"io"
)

// Segment is one Mach-O segment's byte range within the file.
type Segment struct {
	Name   string
	Offset int64
	Size   int64
}

// FindSegment returns the named segment's file range within the bytes r holds:
// thin images directly, universal images through their arm64 slice.
func FindSegment(r io.ReaderAt, name string) (Segment, error) {
	if f, err := macho.NewFile(r); err == nil {
		return findIn(f, 0, name)
	}
	fat, err := macho.NewFatFile(r)
	if err != nil {
		return Segment{}, fmt.Errorf("parse mach-o: %w", err)
	}
	for _, arch := range fat.Arches {
		if arch.Cpu == macho.CpuArm64 {
			return findIn(arch.File, int64(arch.Offset), name)
		}
	}
	return Segment{}, errors.New("mach-o: no arm64 slice")
}

func findIn(f *macho.File, base int64, name string) (Segment, error) {
	for _, load := range f.Loads {
		seg, ok := load.(*macho.Segment)
		if !ok || seg.Name != name {
			continue
		}
		//nolint:gosec // mach-o segment offsets are bounded by the on-disk file size
		return Segment{Name: name, Offset: base + int64(seg.Offset), Size: int64(seg.Filesz)}, nil
	}
	return Segment{}, fmt.Errorf("mach-o: segment %q not found", name)
}
