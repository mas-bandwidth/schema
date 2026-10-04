package cstable

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestIsChildScalarArray(t *testing.T) {
	leafStruct := &ir.Struct{
		Name: "Leaf",
		Fields: []*ir.Field{
			{Name: "value", Type: ir.FieldType{Kind: ir.TInt, Width: 32}},
		},
	}
	nonLeafStruct := &ir.Struct{
		Name: "NonLeaf",
		Fields: []*ir.Field{
			{Name: "text", Type: ir.FieldType{Kind: ir.TString}},
		},
	}

	tests := []struct {
		name      string
		field     *ir.Field
		wantOk    bool
		wantMatch *ir.Struct
	}{
		{
			name: "normal fixed scalar array",
			field: &ir.Field{
				Name:       "rows",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct},
			},
			wantOk:    true,
			wantMatch: leafStruct,
		},
		{
			name: "normal counted scalar array",
			field: &ir.Field{
				Name:       "items",
				Array:      ir.ArrayCounted,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct},
			},
			wantOk:    true,
			wantMatch: leafStruct,
		},
		{
			name: "guarded fixed scalar array",
			field: &ir.Field{
				Name:       "rows",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				Guard:      "on",
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct},
			},
			wantOk: false,
		},
		{
			name: "guarded counted scalar array",
			field: &ir.Field{
				Name:       "items",
				Array:      ir.ArrayCounted,
				ArrayBound: 4,
				Guard:      "on",
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct},
			},
			wantOk: false,
		},
		{
			name: "optional fixed scalar array",
			field: &ir.Field{
				Name:       "rows",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct, Optional: true},
			},
			wantOk: false,
		},
		{
			name: "optional counted scalar array",
			field: &ir.Field{
				Name:       "items",
				Array:      ir.ArrayCounted,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct, Optional: true},
			},
			wantOk: false,
		},
		{
			name: "pointer scalar array",
			field: &ir.Field{
				Name:       "rows",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct, Pointer: true},
			},
			wantOk: false,
		},
		{
			name: "keyed scalar array",
			field: &ir.Field{
				Name:       "rows",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				KeyEnum:    "Slot",
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct},
			},
			wantOk: false,
		},
		{
			name: "non-scalar leaf struct array",
			field: &ir.Field{
				Name:       "rows",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: nonLeafStruct},
			},
			wantOk: false,
		},
		{
			name: "primitive array not TNamed",
			field: &ir.Field{
				Name:       "nums",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TInt, Width: 32},
			},
			wantOk: false,
		},
		{
			name: "map field",
			field: &ir.Field{
				Name:       "mapField",
				Array:      ir.ArrayFixed,
				ArrayBound: 4,
				MapEntry:   &ir.Struct{Name: "Entry"},
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct},
			},
			wantOk: false,
		},
		{
			name: "list scalar array",
			field: &ir.Field{
				Name:       "listField",
				Array:      ir.ArrayList,
				ArrayBound: 4,
				Type:       ir.FieldType{Kind: ir.TNamed, Ref: leafStruct},
			},
			wantOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, ok := isChildScalarArray(tc.field)
			if ok != tc.wantOk {
				t.Fatalf("isChildScalarArray() ok = %v, want %v", ok, tc.wantOk)
			}
			if tc.wantOk && st != tc.wantMatch {
				t.Fatalf("isChildScalarArray() st = %v, want %v", st, tc.wantMatch)
			}
			if !tc.wantOk && st != nil {
				t.Fatalf("isChildScalarArray() returned non-nil struct on failure: %v", st)
			}
		})
	}
}

func TestKnownOrdinal(t *testing.T) {
	g := &tableGen{
		idOrdinal: map[uint64]int{
			0x1111: 0,
			0x2222: 1,
		},
	}
	f1 := &ir.Field{Name: "field1"}
	f2 := &ir.Field{Name: "field2"}
	if got := g.knownOrdinal(f1, 0x1111); got != 0 {
		t.Fatalf("knownOrdinal(f1, 0x1111) = %d, want 0", got)
	}
	if got := g.knownOrdinal(f2, 0x2222); got != 1 {
		t.Fatalf("knownOrdinal(f2, 0x2222) = %d, want 1", got)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic for unregistered field, got none")
		}
		wantMsg := fmt.Sprintf("table field missingField (%d) missing from wire ID table; schema compiler invariant violated", uint64(0x9999))
		if msg, ok := r.(string); !ok || msg != wantMsg {
			t.Fatalf("panic = %v, want %q", r, wantMsg)
		}
	}()

	fMissing := &ir.Field{Name: "missingField"}
	g.knownOrdinal(fMissing, 0x9999)
}
