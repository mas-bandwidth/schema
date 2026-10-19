package ir

import (
	"reflect"
	"testing"
)

// TestTableWideFieldsFindsAWideUnionArm is §15's by-name refusal walking a
// union's arms: a table that holds `union U { wide uint128; small uint32;
// empty }` declares a wide kind, so TableWideFields must name the wide arm —
// exactly as it names a wide field of a table or closure struct — or a backend
// without the wide carrier goes on to emit a kind it does not carry
// (docs/SPEC-TABLES.md §15). The payload-free arm and the narrow one are not
// wide and must not be named.
func TestTableWideFieldsFindsAWideUnionArm(t *testing.T) {
	t.Parallel()

	un := &Union{Name: "U", Max: 3, Variants: []UnionVariant{
		{Name: "wide", F: &Field{Name: "wide", Type: FieldType{Kind: TInt, Width: 128}}},
		{Name: "small", F: &Field{Name: "small", Type: FieldType{Kind: TInt, Width: 32}}},
		{Name: "empty"},
	}}
	st := &Struct{Name: "Tab", IsTable: true, Fields: []*Field{
		{Name: "u", Type: FieldType{Kind: TNamed, Name: "U", Ref: un}},
	}}
	file := &File{Base: "Tab", Tables: []*Struct{st}}
	u := &Unit{
		Package: "probe",
		Files:   []*File{file},
		Tables:  map[string]*Struct{"Tab": st},
		Unions:  map[string]*Union{"U": un},
	}

	got := TableWideFields(u)
	want := []string{"U.wide uint128"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TableWideFields = %v, want %v", got, want)
	}
}
