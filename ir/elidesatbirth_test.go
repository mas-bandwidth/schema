package ir_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElidesAtBirth holds the one statement of which fields a writer leaves
// out at their birth value (docs/SPEC-TABLES.md §22.2): a counted [Min..N]T
// with Min > 0 that is neither required (§2.3, always written) nor optional
// (rides by presence). A [..N] array, a fixed array, an unbounded []T and a
// scalar are not judged by it.
func TestElidesAtBirth(t *testing.T) {
	t.Parallel()
	counted := func(min int64) ir.Field {
		return ir.Field{Name: "few", Array: ir.ArrayCounted, ArrayMin: min, ArrayBound: 4, Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}}
	}
	required := counted(2)
	required.Required = true
	optional := counted(2)
	optional.Type.Optional = true
	fixed := counted(0)
	fixed.Array, fixed.ArrayBound = ir.ArrayFixed, 2
	list := counted(0)
	list.Array, list.ArrayBound = ir.ArrayList, 0
	scalar := counted(0)
	scalar.Array, scalar.ArrayBound = ir.ArrayNone, 0
	for _, c := range []struct {
		name  string
		field ir.Field
		want  bool
	}{
		{"[2..4]int32", counted(2), true},
		{"[1..4]int32", counted(1), true},
		{"[..4]int32", counted(0), false},
		{"[2..4]int32 | required", required, false},
		{"?[2..4]int32", optional, false},
		{"[2]int32", fixed, false},
		{"[]int32", list, false},
		{"int32", scalar, false},
	} {
		if got := c.field.ElidesAtBirth(); got != c.want {
			t.Errorf("%s: ElidesAtBirth() = %v, want %v", c.name, got, c.want)
		}
	}
}
