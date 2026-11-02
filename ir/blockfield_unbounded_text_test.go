package ir_test

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Unbounded text occupies one reference slot, never an inline buffer plus
// length companion (SPEC-TABLES §2.13.1). Its optional presence stays separate.
func TestBlockFieldOfUnboundedTextSlot(t *testing.T) {
	for _, optional := range []bool{false, true} {
		for _, offset := range []int64{0, 16} {
			t.Run(fmt.Sprintf("optional_%t_offset_%d", optional, offset), func(t *testing.T) {
				f := &ir.Field{Name: "value", Type: ir.FieldType{Kind: ir.TString, Unbounded: true, Optional: optional}}
				got := ir.BlockFieldOf(&ir.Unit{}, f, offset, false)
				present := int64(-1)
				if optional {
					present = offset + 8
				}
				if got.ElemSize != 8 || got.CountOffset != -1 || got.PresentOffset != present || got.Optional != optional || got.Counted || got.IsArray || got.ArrayBound != 0 {
					t.Fatalf("unbounded text slot facts=%+v, want 8-byte slot with presence at %d", got, present)
				}
			})
		}
	}
}
