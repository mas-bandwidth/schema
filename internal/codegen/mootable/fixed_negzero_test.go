package mootable_test

// A COMPRESSED FLOAT'S NEGATIVE ZERO READS AS +0 (docs/SPEC-TABLES.md §3.3,
// §3.4; docs/FIXED-FORM-ALGORITHM.md §4.6; docs/rulings.json
// fixed-compressed-negzero). The fixed form carries the float itself, so a
// compressed field's -0 lands as the value +0, refusing nothing and counting
// nothing, and a read-then-save writes the canonical +0 image. A PLAIN float's
// -0 is carried bit-exact.

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedHeaderBytes is the fixed form's file header (docs/SPEC-TABLES.md §3.4);
// the layout's length follows it.
const fixedHeaderBytes = 16

// The shared vectors (fixedvector.NegativeZero): the reference writer's file
// holding the sign bit alone in test/tables/FNZ.schema's compressed `value`.
// The leg's load lands one record whose value is 0 with the sign bit clear,
// refuses nothing and moves no counter, and its save writes the same file
// with that field's bytes cleared.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	corpus := fixedCorpus(t)
	g := fixedUnit(t, readTestSchema(t, "FNZ"), nil)
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			file := corpusFile(t, corpus, vec.File)
			layoutBytes := int(binary.LittleEndian.Uint32(file[fixedHeaderBytes:]))
			valueAt := fixedHeaderBytes + 4 + layoutBytes + 8 + 4
			if got := binary.LittleEndian.Uint32(file[valueAt:]); got != 0x80000000 {
				t.Fatalf("%s does not hold a compressed float's -0 at byte %d: %#08x", filepath.Join(corpus, vec.File), valueAt, got)
			}
			got := evoLoad(t, g, "FNZTable", vec.Root, file, -1)
			if got.head != "ok 1 u0 k0 c0 w0 d0 -" {
				t.Fatalf("the vector does not land one record with every counter at zero: %s", got.head)
			}
			if got.dump["r0.value"] != "0x00000000" {
				t.Errorf("r0.value landed %s, want +0 (0x00000000, the sign bit clear)", got.dump["r0.value"])
			}
			want := clone(file)
			clear(want[valueAt : valueAt+4])
			if !bytes.Equal(got.saved, want) {
				t.Errorf("the read-then-save image is not the canonical +0 image:\n got %x\nwant %x", got.saved, want)
			}
		})
	}
}
