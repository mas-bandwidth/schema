package ctable

// THE ARRAY BOUND THE SIZING WALK AND THE C RUNTIME SHARE.
//
// A compiled plan walks an array's element once per slot, so the sizing walk
// must stop at the same bound the reader's own layout validation holds a record
// to. That bound is the fixed form's 65536-byte record ceiling
// (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md §1.1): an element is
// at least one byte, so no lawful array field can hold more than that many
// elements. This test probes an array AT the bound and one element past it, and
// pins the sizing walk's answer to the same number the C runtime holds.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCFixedStrideBoundAgrees(t *testing.T) {
	t.Parallel()

	if ir.TableFixedRecordMaxBytes != 65536 {
		t.Fatalf("the fixed record bound is %d, want the spec's 65536", ir.TableFixedRecordMaxBytes)
	}
	if !strings.Contains(tableFixedRuntime, "kTableFixedRecordMaxBytes = 65536") {
		t.Fatal("the C runtime does not hold a record to the spec's 65536-byte bound")
	}

	for _, tc := range []struct {
		name     string
		elements int
		hostile  bool
	}{
		{"at the bound", ir.TableFixedRecordMaxBytes, false},
		{"one over the bound", ir.TableFixedRecordMaxBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := fixedStrideBoundCompiler(tc.elements)
			c.matchChildren(0, 0, 0, 0, fixedNoGuard, 0)
			if c.hostile != tc.hostile {
				t.Fatalf("a %d-element array: hostile=%v, want %v", tc.elements, c.hostile, tc.hostile)
			}
			if tc.hostile {
				if len(c.plan) != 0 {
					t.Fatalf("a %d-element array pushed %d plan entries before the bound", tc.elements, len(c.plan))
				}
				return
			}
			// One entry per element: the walk has not folded anything yet, and
			// an array at the bound must be sized whole.
			if len(c.plan) != tc.elements {
				t.Fatalf("a %d-element array sized %d entries, want %d", tc.elements, len(c.plan), tc.elements)
			}
		})
	}
}

// fixedStrideBoundCompiler is a writer table holding one array of `elements`
// u8 slots, with the reader's walk over the same shape.
func fixedStrideBoundCompiler(elements int) *fixedStrideCompiler {
	writer := []fixedLineageLayoutEntry{
		{id: 1, kind: uint8(ir.TableKindTable), size: uint32(elements), children: 1},
		{id: 2, kind: uint8(ir.TableKindArray), size: uint32(elements), children: 1},
		{id: 3, kind: uint8(ir.TableKindU8), size: 1},
	}
	mine := []ir.TableFixedLayoutEntry{
		{ID: 1, Kind: ir.TableKindTable, Size: int64(elements), Children: 1},
		{ID: 2, Kind: ir.TableKindArray, Size: int64(elements), Children: 1, Dst: ir.TableFixedDstSpec{Stride1: true}},
		{ID: 3, Kind: ir.TableKindU8, Size: 1},
	}
	dst := []fixedPlanDstRow{
		{},
		{dst: 0, stride: 1},
		{},
	}
	return &fixedStrideCompiler{writer: writer, mine: mine, dst: dst, record: uint32(elements)}
}
