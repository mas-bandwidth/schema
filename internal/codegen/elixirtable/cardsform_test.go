package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cardUnit builds a one-table unit with the given fields, so a test can ask
// whether the Elixir variable form accepts or refuses exactly those constructs.
func cardUnit(fields ...*ir.Field) *ir.Unit {
	return &ir.Unit{
		Package: "cards",
		Tables: map[string]*ir.Struct{
			"Card": {Name: "Card", IsTable: true, Fields: fields},
		},
	}
}

// TestCardConstructsAreNotRefused pins docs/SPEC-TABLES.md §2.5, §2.9: the
// card record's slice — a plain `table` root, tables as list elements, bool,
// int32, int64 and enum fields, string(N), *string and []T — is accepted by
// the backend's boundary.
func TestCardConstructsAreNotRefused(t *testing.T) {
	t.Parallel()

	unit := cardUnit(
		&ir.Field{Name: "id", Type: ir.FieldType{Kind: ir.TString, Size: 64}},
		&ir.Field{Name: "priority", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Enum{Name: "Priority"}}},
		&ir.Field{Name: "deadline_minutes", Type: ir.FieldType{Kind: ir.TInt, Width: 32}},
		&ir.Field{Name: "attempt", Type: ir.FieldType{Kind: ir.TInt, Width: 64}},
		&ir.Field{Name: "paths", Array: ir.ArrayList, Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{Name: "PathText", IsTable: true}}},
		&ir.Field{Name: "body", Type: ir.FieldType{Kind: ir.TString, Pointer: true}},
	)

	if err := refuseCardUnsupportedVariableConstructs(unit); err != nil {
		t.Errorf("card constructs were refused by the boundary: %v", err)
	}
	if _, err := Generate(unit); err != nil {
		t.Errorf("card constructs were refused: %v", err)
	}
}

// TestFixedUnitWithRefusedVariableConstructStillGenerates pins the SCOPE of
// the boundary (docs/SPEC-TABLES.md §11): a unit the fixed form owns that also
// declares a variable table carrying a construct outside the card slice still
// generates — the refusal binds a unit the variable form OWNS, not one whose
// block, cook and fixed surfaces this leg emits. Red if the refusal is applied
// unit-wide again: tables/pointers (graphdemo) is exactly this shape, and its
// generation is what the leg's table gate reads.
func TestFixedUnitWithRefusedVariableConstructStillGenerates(t *testing.T) {
	t.Parallel()

	unit := &ir.Unit{
		Package: "cards",
		Tables: map[string]*ir.Struct{
			"Card": {Name: "Card", IsTable: true, Fields: []*ir.Field{
				{Name: "by_key", MapEntry: &ir.Struct{Name: "Entry"}, Type: ir.FieldType{Kind: ir.TMap}},
			}},
			"Stamp": {Name: "Stamp", IsTable: true, FixedDeclared: true, Fields: []*ir.Field{
				{Name: "id", Type: ir.FieldType{Kind: ir.TInt, Width: 32}},
			}},
		},
	}

	if _, err := Generate(unit); err != nil {
		t.Fatalf("a fixed unit with a refused variable table was refused whole: %v", err)
	}
}

// TestOtherVariableConstructsAreRefusedByName pins docs/SPEC-TABLES.md §11:
// every variable-class construct outside the card slice is refused at
// generation, and the refusal names the table, the field and the construct.
// It is the boundary the card draws: never mis-encoded, never silently
// dropped.
func TestOtherVariableConstructsAreRefusedByName(t *testing.T) {
	t.Parallel()

	enum := &ir.Enum{Name: "Tier"}
	union := &ir.Union{Name: "Body"}

	cases := []struct {
		name     string
		field    *ir.Field
		keywords []string
	}{
		{"map", &ir.Field{Name: "by_key", MapEntry: &ir.Struct{Name: "Entry"}, Type: ir.FieldType{Kind: ir.TMap}}, []string{"map"}},
		{"optional", &ir.Field{Name: "maybe", Type: ir.FieldType{Kind: ir.TInt, Width: 32, Optional: true}}, []string{"optional"}},
		{"enum_keyed_array", &ir.Field{Name: "per_tier", KeyEnum: "Tier", KeyEnumRef: enum, Array: ir.ArrayFixed, ArrayBound: 4, Type: ir.FieldType{Kind: ir.TNamed, Ref: enum}}, []string{"enum-keyed"}},
		{"wide_int", &ir.Field{Name: "wide", Type: ir.FieldType{Kind: ir.TInt, Width: 128}}, []string{"128-bit"}},
		{"bits", &ir.Field{Name: "flags", Type: ir.FieldType{Kind: ir.TBits, Width: 12}}, []string{"bits("}},
		{"fixed", &ir.Field{Name: "ratio", Type: ir.FieldType{Kind: ir.TFixed, Width: 16}}, []string{"fixed-point"}},
		{"union_arm", &ir.Field{Name: "body", Type: ir.FieldType{Kind: ir.TNamed, Ref: union}}, []string{"union arms"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := refuseCardUnsupportedVariableConstructs(cardUnit(tc.field))
			if err == nil {
				t.Fatalf("%s: expected a refusal, got none", tc.name)
			}
			msg := err.Error()
			for _, want := range append([]string{"Card", tc.field.Name}, tc.keywords...) {
				if !strings.Contains(msg, want) {
					t.Errorf("%s: refusal %q does not name %q", tc.name, msg, want)
				}
			}
			// THE BOUNDARY IS THE BACKEND'S, not only the helper's: Generate
			// must refuse the same unit, so a hook that was never wired is a
			// green helper with a backend that still mis-encodes.
			if _, err := Generate(cardUnit(tc.field)); err == nil {
				t.Errorf("%s: Generate accepted a construct outside the card slice", tc.name)
			}
		})
	}
}
