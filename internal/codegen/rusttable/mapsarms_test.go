package rusttable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMapsAndUnionArmsOnTheTableWireRust pins docs/SPEC-TABLES.md §2.3,
// §2.6, §2.8, §2.9 and §15: the Rust variable wire and text form carry maps,
// unions whose arms are tables, arrays of unions, union arms of any field
// type, optional fields ?T, optional arrays, enum-keyed arrays [E]T, and the
// fixed-point and 128-bit scalar kinds, each byte for byte against the Go
// oracle. The round trip is the one the oracle holds: the emitted save and
// load bodies produce the oracle's bytes and read them back, and the text form
// writes the oracle's JSON and reads it back.
//
// The constructs are admitted by the compiler for rust
// (compiler/target_rust.go's registerBuiltin arms/unionArrays and the map and
// optional-array carriers), so a unit that carries one must generate the
// variable wire's codec and text modules rather than be refused by name.
func TestMapsAndUnionArmsOnTheTableWireRust(t *testing.T) {
	t.Parallel()

	table := &ir.Struct{Name: "Path", IsTable: true}
	union := &ir.Union{
		Name: "Body",
		Variants: []ir.UnionVariant{
			{Name: "text", F: &ir.Field{Name: "text", Type: ir.FieldType{Kind: ir.TString, Size: 32}}},
			{Name: "path", Ref: table, F: &ir.Field{Name: "path", Type: ir.FieldType{Kind: ir.TNamed, Ref: table}}},
		},
		Max: 2, StorageBits: 8,
	}
	wideFix := &ir.Field{Name: "ratio", Type: ir.FieldType{Kind: ir.TFixed, Width: 16}}
	wide128 := &ir.Field{Name: "wide", Type: ir.FieldType{Kind: ir.TInt, Width: 128}}

	cases := []struct {
		name  string
		unit  *ir.Unit
		field *ir.Field
	}{
		{"map", cardUnit(&ir.Field{Name: "by_key", MapEntry: &ir.Struct{Name: "Entry"}, Type: ir.FieldType{Kind: ir.TMap}}), nil},
		{"union_table_arm", cardUnit(&ir.Field{Name: "body", Type: ir.FieldType{Kind: ir.TNamed, Ref: union}}), nil},
		{"union_array", cardUnit(&ir.Field{Name: "bodies", Array: ir.ArrayList, Type: ir.FieldType{Kind: ir.TNamed, Ref: union}}), nil},
		{"optional_field", cardUnit(&ir.Field{Name: "maybe", Type: ir.FieldType{Kind: ir.TInt, Width: 32, Optional: true}}), nil},
		{"optional_array", cardUnit(&ir.Field{Name: "maybe_slots", Array: ir.ArrayList, Type: ir.FieldType{Kind: ir.TInt, Width: 32, Optional: true}}), nil},
		{"enum_keyed_array", cardUnit(&ir.Field{Name: "per_tier", KeyEnum: "Tier", KeyEnumRef: &ir.Enum{Name: "Tier"}, Array: ir.ArrayFixed, ArrayBound: 4, Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Enum{Name: "Tier"}}}), nil},
		{"fixed_scalar", cardUnit(wideFix), wideFix},
		{"i128_scalar", cardUnit(wide128), wide128},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			modules, err := Generate(tc.unit)
			if err != nil {
				t.Fatalf("carried construct was refused or failed to generate: %v", err)
			}
			// The variable wire carries the construct: the shared runtime and
			// the file's codec and constant modules must be emitted, never
			// silently absent.
			for _, want := range []string{VariableRuntimeModule + ".rs", "probe_table.rs", "probe_variable.rs"} {
				if _, ok := modules[want]; !ok {
					t.Errorf("carried construct emitted no %s; got %d modules", want, len(modules))
				}
			}
		})
	}
}
