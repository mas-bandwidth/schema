package ir_test

import (
	"github.com/mas-bandwidth/schema/v2/ir"
	"testing"
)

func TestUnboundedTextFieldFacts(t *testing.T) {
	for _, optional := range []bool{false, true} {
		f := &ir.Field{Name: "value", Type: ir.FieldType{Kind: ir.TString, Unbounded: true, Optional: optional}}
		for _, projection := range []bool{false, true} {
			got := ir.BlockFieldOf(&ir.Unit{}, f, 24, projection)
			want := ir.BlockFieldFacts{ElemSize: 8, CountOffset: -1, PresentOffset: -1, Optional: optional}
			if optional {
				want.PresentOffset = 32
			}
			if got != want {
				t.Fatalf("optional=%v projection=%v got=%+v want=%+v", optional, projection, got, want)
			}
		}
	}
}
