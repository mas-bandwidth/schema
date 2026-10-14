package jstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMapsAndUnionArmsOnTheTableWireJavaScript is the card's carry step
// (docs/SPEC-TABLES.md §2.3, §2.4, §2.6, §2.8, §3, §3.1): the JavaScript
// variable form carries maps, union arms of any field type, optional ?T,
// enum-keyed arrays [E]T, optional arrays and 128-bit scalars on the table wire,
// byte for byte against the Go oracle (internal/codegen/gotable,
// internal/tablewire), for the tables/maps, tables/arms, tables/messages,
// tables/scalars and examples-wide corpora.
//
// RED FIRST: the refusal layer still names every one of these constructs, so a
// single-table unit made of them is refused before a byte is round-tripped. The
// byte-for-byte corpus body lands with the codec.
func TestMapsAndUnionArmsOnTheTableWireJavaScript(t *testing.T) {
	t.Parallel()

	field := func(name string, typ ir.FieldType) *ir.Field { return &ir.Field{Name: name, Type: typ} }

	for _, tc := range []struct {
		name  string
		field *ir.Field
	}{
		{"map", &ir.Field{Name: "m", Type: ir.FieldType{Kind: ir.TMap}, MapEntry: &ir.Struct{Name: "m_entry"}}},
		{"optional", field("opt", ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32, Optional: true})},
		{"enumkeyed", &ir.Field{Name: "ships", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 8}, KeyEnum: "ShipType", KeyEnumRef: &ir.Enum{Name: "ShipType", Max: 4}}},
		{"unionarm", &ir.Field{Name: "arm", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Union{Name: "Pick"}}}},
		{"uint128", field("wide", ir.FieldType{Kind: ir.TInt, Signed: false, Width: 128})},
	} {
		st := &ir.Struct{Name: "Probe", IsTable: true, Fields: []*ir.Field{tc.field}}
		if got := tableVariableRefusals(st); len(got) != 0 {
			t.Errorf("%s: the JavaScript variable wire still refuses a construct this card carries: %q", tc.name, got)
			continue
		}
		if _, err := generateVariableFiles(unitWith(st), nil); err != nil && strings.Contains(err.Error(), "Probe."+tc.field.Name) {
			t.Errorf("%s: generateVariableFiles still refuses Probe.%s: %v", tc.name, tc.field.Name, err)
		}
	}
}
