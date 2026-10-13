package ir

import "testing"

// TestTableFixedSupportedAdmitsDirectAndPayloadFreeArms pins the shared
// fixed-form gate's treatment of a union arm. docs/SPEC-TABLES.md §2.6,
// "Union arms are field lines", says an arm's type is any type a FIELD's is
// and an arm may also carry no payload at all: so a direct scalar, text,
// bytes or bounded-array arm rides as its own field type under the arm's
// position, a payload-free arm is the tag enum value with no storage, and a
// by-value struct arm recurses. What the arm position refuses is the shapes
// whose arm position already spends what they buy — a pointer, a map, an
// unbounded array, a keyed array and an optional. The gate is the one place
// this is decided (ir.TableFixedSupported); no other walk may grow a second
// copy of the rule.
func TestTableFixedSupportedAdmitsDirectAndPayloadFreeArms(t *testing.T) {
	t.Parallel()

	leaf := &Struct{Name: "Leaf", Fields: []*Field{
		{Name: "n", Type: FieldType{Kind: TInt, Width: 32}},
	}}
	// badLeaf holds a pointer, so its own closure is not one the form carries
	// (§5.5): the recursion a struct arm does must actually run, and an arm
	// naming this type must be refused for the type, not merely admitted
	// because the arm itself is a named type.
	badLeaf := &Struct{Name: "Bad", Fields: []*Field{
		{Name: "p", Type: FieldType{Kind: TNamed, Name: "Bad", Pointer: true}},
	}}

	// unionTable builds a fixed table holding ONE union field whose single
	// arm is f, so the gate's arm branch is the only thing under test.
	unionTable := func(f *Field) *Struct {
		un := &Union{Name: "Pick", Max: 1, Variants: []UnionVariant{
			{Name: "a", F: f},
		}}
		return &Struct{Name: "Root", IsTable: true, Fields: []*Field{
			{Name: "u", Type: FieldType{Kind: TNamed, Name: "Pick", Ref: un}},
		}}
	}

	arm := func(t FieldType) *Field {
		return &Field{Name: "a", Type: t}
	}

	tests := []struct {
		name string
		st   *Struct
		want bool
	}{
		// Admitted: an arm's type is any type a field's is (§2.6).
		{"direct scalar arm", unionTable(arm(FieldType{Kind: TInt, Width: 32})), true},
		{"direct string arm", unionTable(arm(FieldType{Kind: TString, Size: 16})), true},
		{"bytes arm", unionTable(arm(FieldType{Kind: TBytes, Size: 16})), true},
		{"bounded array arm", unionTable(&Field{
			Name: "a", Array: ArrayFixed, ArrayBound: 8,
			Type: FieldType{Kind: TInt, Width: 32},
		}), true},
		// A payload-free arm has no storage at all: the tag alone is its
		// whole storage, V.F == nil (§2.6).
		{"payload-free arm", unionTable(nil), true},
		// A by-value struct arm recurses into the closure.
		{"struct arm", unionTable(arm(FieldType{Kind: TNamed, Name: "Leaf", Ref: leaf})), true},

		// Refused: the arm position already spends what these would buy (§2.6).
		{"pointer arm", unionTable(arm(FieldType{Kind: TNamed, Name: "Leaf", Ref: leaf, Pointer: true})), false},
		{"map arm", unionTable(&Field{
			Name: "a", Type: FieldType{Kind: TMap},
			MapEntry: &Struct{Name: "PickEntry"},
		}), false},
		{"list arm", unionTable(&Field{
			Name: "a", Array: ArrayList,
			Type: FieldType{Kind: TInt, Width: 32},
		}), false},
		{"keyed-array arm", unionTable(&Field{
			Name: "a", Array: ArrayFixed, ArrayBound: 2, KeyEnum: "Mode",
			Type: FieldType{Kind: TInt, Width: 32},
		}), false},
		{"optional arm", unionTable(arm(FieldType{Kind: TInt, Width: 32, Optional: true})), false},
		// The struct arm's admission is not vacuous: it recurses into the
		// named struct's own closure, so one naming a pointer holder is refused.
		{"struct arm with unfixed closure", unionTable(arm(FieldType{Kind: TNamed, Name: "Bad", Ref: badLeaf})), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := TableFixedSupported(tt.st, 0); got != tt.want {
				t.Fatalf("TableFixedSupported = %v, want %v (§2.6: an arm is a field line, refused only where the arm position already spends what it buys)", got, tt.want)
			}
		})
	}
}
