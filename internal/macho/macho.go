// Package macho locates named segments inside a Mach-O binary, resolving their
// absolute file offset for both thin and universal (fat) images.
package macho

import (
	"debug/macho"
	"fmt"
)

// Segment is one Mach-O segment's byte range within the file.
type Segment struct {
	Name   string
	Offset int64
	Size   int64
}

// FindSegment returns the file range of the named segment. Thin images resolve
// directly; universal images use their arm64 slice, offsetting by its position
// in the fat container.
func FindSegment(path, name string) (Segment, error) {
	if f, err := macho.Open(path); err == nil {
		defer func() { _ = f.Close() }()
		return findIn(f, 0, name)
	}
	fat, err := macho.OpenFat(path)
	if err != nil {
		return Segment{}, fmt.Errorf("open mach-o %q: %w", path, err)
	}
	defer func() { _ = fat.Close() }()
	for _, arch := range fat.Arches {
		if arch.Cpu == macho.CpuArm64 {
			return findIn(arch.File, int64(arch.Offset), name)
		}
	}
	return Segment{}, fmt.Errorf("mach-o %q: no arm64 slice", path)
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
