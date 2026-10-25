package cstable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A null report is DISCARDED, not constructed: the C# read path owns no
// memory (docs/PORTING.md I1, docs/SPEC-TABLES.md "What allocates, and what
// never does"). TableWire.Load once allocated a fresh TableReport for a null
// report, which made a null-report read the one allocating path; the cached
// ignored ledger is the fix, and this holds it.
func TestNullReportLoadDoesNotAllocate(t *testing.T) {
	if strings.Contains(tableWireSource, "if (report == null) { report = new TableReport(); }") {
		t.Fatal("TableWire.Load constructs a TableReport for a null report — a null-report read allocates")
	}
	if !strings.Contains(tableWireSource, "if (report == null) { report = Ignored; }") {
		t.Fatal("TableWire.Load does not route a null report through the cached ignored ledger")
	}
	if !strings.Contains(tableWireSource, "public static readonly TableReport Ignored = new TableReport();") {
		t.Fatal("TableWire does not cache the ignored ledger a null report needs")
	}
}

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

func TestCsInliningStopsAtTheVariableClass(t *testing.T) {
	t.Parallel()

	fCount := &ir.Field{
		Name: "count",
		Type: ir.FieldType{Kind: ir.TInt, Width: 32},
	}
	fixedRow := &ir.Struct{
		Name:          "FixedRow",
		IsTable:       true,
		FixedDeclared: true,
		Fields:        []*ir.Field{fCount},
		Items:         []ir.Item{&ir.FieldItem{F: fCount}},
	}
	fLabel := &ir.Field{
		Name: "label",
		Type: ir.FieldType{Kind: ir.TString},
	}
	fChild := &ir.Field{
		Name: "child",
		Type: ir.FieldType{Kind: ir.TNamed, Ref: fixedRow},
	}
	varRow := &ir.Struct{
		Name:    "VarRow",
		IsTable: true,
		Fields:  []*ir.Field{fLabel, fChild},
		Items:   []ir.Item{&ir.FieldItem{F: fLabel}, &ir.FieldItem{F: fChild}},
	}
	file := &ir.File{
		Base:   "Testpkg",
		Tables: []*ir.Struct{fixedRow, varRow},
	}
	u := &ir.Unit{
		Package:  "testpkg",
		Files:    []*ir.File{file},
		DeclFile: map[string]string{"FixedRow": "Testpkg", "VarRow": "Testpkg"},
		Structs:  map[string]*ir.Struct{},
		Tables:   map[string]*ir.Struct{"FixedRow": fixedRow, "VarRow": varRow},
	}

	varTables := ir.VariableTables(u)
	if varTables["FixedRow"] {
		t.Fatal("FixedRow classified as Variable-class; expected Fixed-class")
	}
	if !varTables["VarRow"] {
		t.Fatal("VarRow classified as Fixed-class; expected Variable-class")
	}

	out, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	srcBytes, ok := out["TestpkgTable.cs"]
	if !ok {
		t.Fatalf("Generate() missing TestpkgTable.cs; got keys %v", out)
	}
	src := string(srcBytes)

	const attr = "[MethodImpl(MethodImplOptions.AggressiveInlining)]"

	wantInlinedPrimitives := []string{
		"        " + attr + "\n        public static float TableBitsToFloat(uint bits)",
		"        " + attr + "\n        public static uint TableFloatToBits(float value)",
		"        " + attr + "\n        public static double TableBitsToDouble(ulong bits)",
		"        " + attr + "\n        public static ulong TableDoubleToBits(double value)",
		"                " + attr + "\n                public ulong Reference(ulong id)",
		"                " + attr + "\n                public ulong Ref(ulong id)",
		"                " + attr + "\n                public ulong RefAt(int ordinal, ulong id)",
		"                " + attr + "\n                public void Byte(byte v)",
		"                " + attr + "\n                public void Raw(ReadOnlySpan<byte> v)",
		"                " + attr + "\n                public void Fixed(ulong v, int n)",
		"                " + attr + "\n                public void Header(ulong reference, byte kind)",
		"                " + attr + "\n                public void HeaderAt(int ordinal, ulong id, byte kind, ref Ids ids)",
		"                " + attr + "\n                public void Var(ulong v)",
		"                " + attr + "\n                public bool Has(int n)",
		"                " + attr + "\n                public byte Byte()",
		"                " + attr + "\n                public ulong Fixed(int n)",
		"                " + attr + "\n                public bool Var(out ulong v)",
		"                " + attr + "\n                public bool Ref(out ulong reference, out ulong id)",
	}
	for _, want := range wantInlinedPrimitives {
		if !strings.Contains(src, want) {
			t.Errorf("primitive helper missing AggressiveInlining:\n%s", want)
		}
	}

	mustNotInline := []string{
		"public static long VarRowLoadMeasure(",
		"public static long VarRowMeasure(",
		"public static long VarRowSave(",
		"public static TableWire.Verdict VarRowLoadVerdict(",
		"public static bool VarRowLoad(",
		"public static void TableReset(VarRow value)",
		"public static bool FixedRowCollectTyped(",
		"public static long FixedRowBodySizeTyped(",
		"public static void FixedRowWriteBodyTyped(",
		"public static long FixedRowSaveTyped(",
		"public static long FixedRowMeasure(",
		"public static long FixedRowSave(",
		"public static TableWire.Verdict FixedRowLoadVerdict(",
		"public static bool FixedRowLoad(",
		"public static void TableReset(FixedRow value)",
		"static void FixedRowFixedWriteBody(",
		"public static long FixedRowFixedMeasure(",
		"public static long FixedRowFixedSave(",
		"public static long FixedRowFixedLoad(",
	}
	for _, sig := range mustNotInline {
		if !strings.Contains(src, sig) {
			t.Errorf("expected generated method %q in TestpkgTable.cs", sig)
			continue
		}
		for _, indent := range []string{"    ", "        "} {
			bad := attr + "\n" + indent + sig
			if strings.Contains(src, bad) {
				t.Errorf("method %q unexpectedly carries %s", sig, attr)
			}
		}
	}

	// Counted by the option name, not the attribute spelling, so a planted
	// attribute is caught however it is qualified
	// ([global::System.Runtime.CompilerServices.MethodImpl(...)] included).
	if got, want := strings.Count(src, "MethodImplOptions.AggressiveInlining"), len(wantInlinedPrimitives); got != want {
		t.Errorf("AggressiveInlining count = %d, want %d (primitive helpers only; Fixed-class body attributes reverted in bench/results/cs-inlining-405.md)", got, want)
	}
}
