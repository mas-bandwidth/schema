package rusttable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cardUnit builds a one-table unit with the given fields, so a test can ask
// whether the Rust variable form accepts or refuses exactly those constructs.
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
// the backend.
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

// TestOtherVariableConstructsAreRefusedByName pins docs/SPEC-TABLES.md §11:
// every variable-class construct outside the card slice is refused at
// generation, and the refusal names the table, the field and the construct.
// It is the boundary the card draws: never mis-encoded, never silently
// dropped.
func TestOtherVariableConstructsAreRefusedByName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		field    *ir.Field
		keywords []string
	}{
		{"wstring", &ir.Field{Name: "title", Type: ir.FieldType{Kind: ir.TWString, Size: 16}}, []string{"wstring"}},
		{"guarded", &ir.Field{Name: "when", Guard: "armed", Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true}}, []string{"guarded"}},
		{"fixed_array", &ir.Field{Name: "slots", Array: ir.ArrayFixed, ArrayBound: 4, Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true}}, []string{"fixed-length array"}},
		{"counted_minimum", &ir.Field{Name: "some", Array: ir.ArrayCounted, ArrayMin: 1, ArrayBound: 4, Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true}}, []string{"declared minimum"}},
		{"flags", &ir.Field{Name: "perms", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Flags{Name: "Perm"}}}, []string{"flags"}},
		{"nested_type", &ir.Field{Name: "tint", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{Name: "Colour"}}}, []string{"nested type"}},
		{"compressed_float", &ir.Field{Name: "speed", HasFloatRange: true, Type: ir.FieldType{Kind: ir.TFloat32}}, []string{"compressed float"}},
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
		})
	}
}

// TestCardUnitWithOneExcludedConstructIsRefused pins the boundary on a full
// card-shaped unit: the whole card slice plus one excluded construct is
// refused, and the refusal names the table, the field and the construct, so
// the rule holds when the excluded field sits among the carried ones.
func TestCardUnitWithOneExcludedConstructIsRefused(t *testing.T) {
	t.Parallel()

	unit := cardUnit(
		&ir.Field{Name: "id", Type: ir.FieldType{Kind: ir.TString, Size: 64}},
		&ir.Field{Name: "priority", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Enum{Name: "Priority"}}},
		&ir.Field{Name: "deadline_minutes", Type: ir.FieldType{Kind: ir.TInt, Width: 32}},
		&ir.Field{Name: "attempt", Type: ir.FieldType{Kind: ir.TInt, Width: 64}},
		&ir.Field{Name: "paths", Array: ir.ArrayList, Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{Name: "PathText", IsTable: true}}},
		&ir.Field{Name: "body", Type: ir.FieldType{Kind: ir.TString, Pointer: true}},
		&ir.Field{Name: "title", Type: ir.FieldType{Kind: ir.TWString, Size: 16}},
	)

	err := refuseCardUnsupportedVariableConstructs(unit)
	if err == nil {
		t.Fatal("a card unit carrying a wstring was not refused")
	}
	for _, want := range []string{"Card", "title", "wstring"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
}

// TestGenerateForCardProducesExpectedModules tests that Generate emits the
// expected module set for a card-like unit with variable form support.
func TestGenerateForCardProducesExpectedModules(t *testing.T) {
	t.Parallel()

	unit := cardUnit(
		&ir.Field{Name: "id", Type: ir.FieldType{Kind: ir.TString, Size: 64}},
	)

	modules, err := Generate(unit)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should have build version at minimum
	if _, ok := modules["build_version.rs"]; !ok {
		t.Error("expected build_version.rs")
	}
}

// cardShapeSrc is the card record's constructs as schema source — a plain
// `table` root, a table as a list element, a bool, an int32, an int64, an
// enum, a string(N), a *string and an unbounded list of tables
// (docs/SPEC-TABLES.md §2.5, §2.9) — so a test can ask the backend what it
// does with exactly those constructs today.
const cardShapeSrc = `package cards

enum Priority { normal, high, critical, blocker }

table PathText
{
    s string(192)
}

table Card
{
    id       string(64)
    sha      string(40)
    tier     Priority
    active   bool
    deadline_minutes int32 | min = 0, max = 100000
    attempt  int64
    paths    []PathText
    body     *string
}
`

// TestCardShapeGenerationEmitsTheVariableWire pins docs/SPEC-TABLES.md §3 and
// §3.1: the card constructs generate, and the backend emits the variable table
// wire for them — the shared runtime and the per-file codec module — beside the
// record descriptors and the cooked accelerator.
func TestCardShapeGenerationEmitsTheVariableWire(t *testing.T) {
	t.Parallel()

	modules, err := Generate(unitFrom(t, cardShapeSrc))
	if err != nil {
		t.Fatalf("the card constructs were refused: %v", err)
	}

	if _, ok := modules[VariableRuntimeModule+".rs"]; !ok {
		t.Errorf("the card constructs emitted no %s; got %d modules", VariableRuntimeModule+".rs", len(modules))
	}
	if _, ok := modules["probe_table.rs"]; !ok {
		t.Errorf("the card constructs emitted no probe_table.rs; got %d modules", len(modules))
	}
	for _, want := range []string{"probe_records.rs", "probe_cook.rs", "cook_runtime.rs", "build_version.rs"} {
		if _, ok := modules[want]; !ok {
			t.Errorf("the card constructs emitted no %s; got %d modules", want, len(modules))
		}
	}
}
