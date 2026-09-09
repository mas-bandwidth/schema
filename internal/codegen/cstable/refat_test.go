package cstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCsharpRefAtAndInliningShape(t *testing.T) {
	requiredPatterns := []string{
		// Writer inlining
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public void Byte(byte v)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public void Raw(ReadOnlySpan<byte> v)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public void Fixed(ulong v, int n)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public void Header(ulong reference, byte kind)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public void Var(ulong v)",

		// Reader inlining
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public bool Has(int n)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public byte Byte()",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public ulong Fixed(int n)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public bool Var(out ulong v)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public bool Ref(out ulong reference, out ulong id)",

		// Ids struct members
		"public Span<ushort> Slots;",
		"public Span<short> OrdinalOf;",
		"public Span<int> Index;",

		// Ids methods
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public ulong RefAt(int ordinal, ulong id)",
		"if ((uint)ordinal < (uint)Slots.Length)",
		"ushort s = Slots[ordinal];",
		"if (s != 0) { return s; }",
		"return AddAt(ordinal, id);",

		"[MethodImpl(MethodImplOptions.NoInlining)]\n        public ulong AddAt(int ordinal, ulong id)",
		"ulong r = Ref(id);",
		"Slots[ordinal] = (ushort)r;",
		"OrdinalOf[idx] = (short)ordinal;",

		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public void Truncate(int mark)",
		"Count--;",
		"short o = OrdinalOf[Count];",
		"if ((uint)o < (uint)Slots.Length) { Slots[o] = 0; }",

		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public ulong Ref(ulong id)",
		"[MethodImpl(MethodImplOptions.AggressiveInlining)]\n        public ulong Reference(ulong id)",
	}
	for _, pattern := range requiredPatterns {
		if !strings.Contains(tableWireSource, pattern) {
			t.Fatalf("tableWireSource missing expected pattern: %q", pattern)
		}
	}
}

func TestCsharpKnownOrdinal(t *testing.T) {
	u := &ir.Unit{
		Package: "probe",
		Files: []*ir.File{
			{
				Base: "Probe",
				Tables: []*ir.Struct{
					{
						Name:    "Root",
						IsTable: true,
						Fields: []*ir.Field{
							{
								Name: "alpha",
								Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true},
							},
							{
								Name: "beta",
								Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true},
							},
						},
					},
				},
			},
		},
	}
	ords := wireIdOrdinals(u)
	wireIds := ir.TableWireIds(u)
	if len(ords) != len(wireIds) {
		t.Fatalf("wireIdOrdinals len = %d, want %d", len(ords), len(wireIds))
	}
	for i, id := range wireIds {
		if ords[id] != i {
			t.Errorf("ord for id 0x%016x = %d, want %d", id, ords[id], i)
		}
	}

	g := &tableGen{unit: u, idOrdinal: ords}
	for i, id := range wireIds {
		if got := g.knownOrdinal(id); got != i {
			t.Errorf("g.knownOrdinal(0x%016x) = %d, want %d", id, got, i)
		}
	}
}
