package ir_test

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestTableFixedSplitByWidthBitsLeafIsItsStorageWidth pins the identity
// plan's element width for a fixed-table `bits(N)` leaf. A bits(N) rides at
// its declared STORAGE width (ir.TableFixedStorageBytes: 4 bytes for N <= 32,
// 8 above), so a big-endian reader must reverse the whole storage word: an
// element width of 1 or 2 for bits(8) or bits(12) swaps nothing, or each half,
// and lands 0x05000000 for a lawful 5 (docs/SPEC-TABLES.md §3.4: the wire is
// little-endian on every host). The C and C++ legs both emit this width as
// the plan entry's `elem`, and tools/fixedtwin holds the two plans equal.
func TestTableFixedSplitByWidthBitsLeafIsItsStorageWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bits int
		size int64
		elem int64
	}{
		{8, 4, 4},
		{12, 4, 4},
		{17, 4, 4},
		{40, 8, 8},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("bits(%d)", tc.bits), func(t *testing.T) {
			t.Parallel()
			src := fmt.Sprintf("package bitsu\n\nfixed table T\n{\n    a bits(%d)\n}\n", tc.bits)
			u := unitFrom(t, src)
			st := u.Tables["T"]
			if st == nil {
				t.Fatal("table T not in the unit")
			}
			entries := ir.TableFixedWalkRoot(st)
			plan, guarded, _ := ir.TableFixedBuildPlanPresentDefaultsWithPool(u, st)
			split := ir.TableFixedSplitByWidth(entries, plan, guarded)
			copies := 0
			for i, e := range split.Plan {
				if e.Op != ir.TableFixedOpCopy {
					continue
				}
				copies++
				if e.Size != tc.size {
					t.Errorf("copy entry %d: size %d, want the %d-byte storage width", i, e.Size, tc.size)
				}
				if split.Elems[i] != tc.elem {
					t.Errorf("copy entry %d: elem %d, want %d (the leaf's storage width)", i, split.Elems[i], tc.elem)
				}
			}
			if copies != 1 {
				t.Fatalf("got %d copy entries, want 1: %+v", copies, split.Plan)
			}
		})
	}
}

// TestTableFixedSplitByWidthMixedBitsLeavesSplitAtStorageWidth holds the
// split across neighbouring leaves: a bits(8), a bits(12) and a uint32 share
// one storage width, so they coalesce at elem 4, and a bits(40) beside them is
// elem 8. Every byte of the record is covered by exactly the width its leaf is
// stored at.
func TestTableFixedSplitByWidthMixedBitsLeavesSplitAtStorageWidth(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package bitsu\n\nfixed table T\n{\n    a bits(8)\n    b bits(12)\n    c uint32\n    d bits(40)\n}\n")
	st := u.Tables["T"]
	entries := ir.TableFixedWalkRoot(st)
	plan, guarded, _ := ir.TableFixedBuildPlanPresentDefaultsWithPool(u, st)
	split := ir.TableFixedSplitByWidth(entries, plan, guarded)
	at := map[int64]int64{}
	for i, e := range split.Plan {
		if e.Op != ir.TableFixedOpCopy {
			continue
		}
		for b := e.Src; b < e.Src+e.Size; b++ {
			at[b] = split.Elems[i]
		}
	}
	for b := int64(0); b < 12; b++ {
		if at[b] != 4 {
			t.Errorf("byte %d: elem %d, want 4", b, at[b])
		}
	}
	for b := int64(12); b < 20; b++ {
		if at[b] != 8 {
			t.Errorf("byte %d: elem %d, want 8", b, at[b])
		}
	}
}
