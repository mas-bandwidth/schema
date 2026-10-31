package elixirtable

import (
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
// card record's slice — a variable table root, tables as list elements, bool,
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
