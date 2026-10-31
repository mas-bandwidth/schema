package rusttable

import (
	"math/big"
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
// dropped. A guarded field (an if group) left this list when the variable
// wire learned to gate it (variableguard.go, TestGuardAndOptionalEdgesRust).
func TestOtherVariableConstructsAreRefusedByName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		field    *ir.Field
		keywords []string
	}{
		{"counted_minimum", &ir.Field{Name: "some", Array: ir.ArrayCounted, ArrayMin: 1, ArrayBound: 4, Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true}}, []string{"declared minimum"}},
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

// TestFixedClassConstructsAreCarried pins the constructs the fixed class
// brought onto the Rust variable wire (docs/SPEC-TABLES.md §2, §2.2, §3): a
// fixed-length array [N]T, a flags mask (kind 9, its set bits' names in text),
// a `type` nested by value (kind 13, a body like a table's) and the
// fixed-point family (kinds 20–29, its raw storage integer). They were
// refused by name before vt/fixed-class/rust; TestFixedClassOnTableWireRust
// holds their bytes to the oracle.
func TestFixedClassConstructsAreCarried(t *testing.T) {
	t.Parallel()

	for _, f := range []*ir.Field{
		{Name: "three", Array: ir.ArrayFixed, ArrayBound: 3, Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true}},
		{Name: "perms", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Flags{Name: "Perm"}}},
		{Name: "tint", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{Name: "Colour"}}},
		// the fixed-point family rides as its raw storage integer (§3, kinds
		// 20–29); TestFixedPointRidesItsRawStorage pins storage and clamp
		{Name: "rate", Type: ir.FieldType{Kind: ir.TFixed, Width: 32, IntBits: 16, FracBits: 16, Signed: true}},
	} {
		if err := refuseCardUnsupportedVariableConstructs(cardUnit(f)); err != nil {
			t.Errorf("%s: the fixed class's construct was refused: %v", f.Name, err)
		}
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
		&ir.Field{Name: "speed", HasFloatRange: true, Type: ir.FieldType{Kind: ir.TFloat32}},
	)

	err := refuseCardUnsupportedVariableConstructs(unit)
	if err == nil {
		t.Fatal("a card unit carrying a compressed float was not refused")
	}
	for _, want := range []string{"Card", "speed", "compressed float"} {
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

// TestFixedPointRidesItsRawStorage pins the fixed-point family on the Rust
// variable wire (docs/SPEC-TABLES.md §3, §4): a fixed(I, F) is the raw integer
// its I+F storage holds, under the fixed kind of that width and signedness, and
// its declared whole-unit range clamps on the RAW scale — the bounds shifted by
// F — never on the whole units. tables-rust-wide-clamp-negative-control is the
// reversed witness on the conformance rows.
func TestFixedPointRidesItsRawStorage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		f       ir.FieldType
		storage string
		kind    int
	}{
		{ir.FieldType{Kind: ir.TFixed, Width: 8, IntBits: 4, FracBits: 4, Signed: true}, "i8", ir.TableKindFixed8},
		{ir.FieldType{Kind: ir.TFixed, Width: 32, IntBits: 16, FracBits: 16, Signed: true}, "i32", ir.TableKindFixed32},
		{ir.FieldType{Kind: ir.TFixed, Width: 16, IntBits: 8, FracBits: 8}, "u16", ir.TableKindUFixed16},
		{ir.FieldType{Kind: ir.TFixed, Width: 128, IntBits: 112, FracBits: 16}, "u128", ir.TableKindUFixed128},
	}
	for _, c := range cases {
		f := &ir.Field{Name: "x", Type: c.f}
		if got := scalarRust(f); got != c.storage {
			t.Errorf("%s: storage %s, want %s", ir.FieldTypeSpelling(f), got, c.storage)
		}
		if got := scalarWireKind(f); got != c.kind {
			t.Errorf("%s: kind %d, want %d", ir.FieldTypeSpelling(f), got, c.kind)
		}
	}

	angle := &ir.Field{Name: "angle", HasIntRange: true, IntMin: big.NewInt(-180), IntMax: big.NewInt(180),
		Type: ir.FieldType{Kind: ir.TFixed, Width: 32, IntBits: 16, FracBits: 16, Signed: true}}
	got := clampStmt(angle, 64, true, "i64", "report.clamped", "")
	for _, want := range []string{"-11796480i64", "11796480i64"} {
		if !strings.Contains(got, want) {
			t.Errorf("fixed(16, 16) in [-180, 180] clamps as %q, missing the raw bound %s", got, want)
		}
	}
	if strings.Contains(got, " 180i64") {
		t.Errorf("fixed(16, 16) clamps on the whole-unit scale: %q", got)
	}
}
