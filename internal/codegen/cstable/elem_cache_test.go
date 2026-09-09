package cstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCsharpRootArrayElemCacheShape(t *testing.T) {
	requiredPatterns := []string{
		"int cachedElemSlots = !measure && !type.Variable ? type.RootElemSlots : 0;",
		"Span<long> rootElemSizes = stackalloc long[cachedElemSlots];",
		"long n = 1 + BodySize(value, type, ref ids, rootPayloadSizes, rootElemSizes) + 8L * ids.Count + 8;",
		"w.Byte(1); WriteBody(ref w, value, type, ref ids, true, rootPayloadSizes, rootElemSizes);",
		"static long BodySize(object value, TableTypeInfo type, ref Ids ids, scoped Span<long> rootPayloadSizes = default, scoped Span<long> rootElemSizes = default)",
		"if (f.IsArray && f.KeyId == null && f.Kind == 13 && !rootElemSizes.IsEmpty)",
		"int take = Math.Min(c, Math.Max(0, rootElemSizes.Length - elemOffset));",
		"elemCache = rootElemSizes.Slice(elemOffset, take);",
		"elemOffset += take;",
		"long payload = PayloadSize(value, f, ref ids, elemCache);",
		"static void WriteBody(ref Writer w, object value, TableTypeInfo type, ref Ids ids, bool root = false, scoped ReadOnlySpan<long> rootPayloadSizes = default, scoped ReadOnlySpan<long> rootElemSizes = default)",
		"WritePayload(ref w, value, f, ref ids, elemCache);",
		"long childBody = (!elemCache.IsEmpty && i < elemCache.Length) ? elemCache[i] : BodySize(child, f.Table, ref ids);",
		"w.Var((ulong)childBody);",
		"WriteBody(ref w, child, f.Table, ref ids);",
	}
	for _, pattern := range requiredPatterns {
		if !strings.Contains(tableWireSource, pattern) {
			t.Fatalf("tableWireSource missing expected root array element cache pattern: %q", pattern)
		}
	}
}

func TestRootElemSlots(t *testing.T) {
	cases := []struct {
		name string
		st   *ir.Struct
		want int
	}{
		{
			name: "empty",
			st:   &ir.Struct{},
			want: 0,
		},
		{
			name: "scalar arrays only",
			st: &ir.Struct{
				Fields: []*ir.Field{
					{Name: "data", Array: ir.ArrayFixed, ArrayBound: 16, Type: ir.FieldType{Kind: ir.TBytes}},
					{Name: "nums", Array: ir.ArrayCounted, ArrayBound: 32, Type: ir.FieldType{Kind: ir.TInt}},
				},
			},
			want: 0,
		},
		{
			name: "keyed table array",
			st: &ir.Struct{
				Fields: []*ir.Field{
					{
						Name:       "keyed",
						KeyEnum:    "Status",
						Array:      ir.ArrayFixed,
						ArrayBound: 8,
						Type:       ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{IsTable: true}},
					},
				},
			},
			want: 0,
		},
		{
			name: "unkeyed table arrays",
			st: &ir.Struct{
				Fields: []*ir.Field{
					{
						Name:       "entities",
						Array:      ir.ArrayCounted,
						ArrayBound: 8,
						Type:       ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{IsTable: true}},
					},
					{
						Name:       "stats",
						Array:      ir.ArrayCounted,
						ArrayBound: 80,
						Type:       ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{IsTable: true}},
					},
				},
			},
			want: 88,
		},
		{
			name: "unkeyed table list saturates at 256",
			st: &ir.Struct{
				Fields: []*ir.Field{
					{
						Name:  "items",
						Array: ir.ArrayList,
						Type:  ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{IsTable: true}},
					},
				},
			},
			want: 256,
		},
		{
			name: "unkeyed table array exceeding 256 saturates at 256",
			st: &ir.Struct{
				Fields: []*ir.Field{
					{
						Name:       "lots",
						Array:      ir.ArrayCounted,
						ArrayBound: 300,
						Type:       ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{IsTable: true}},
					},
				},
			},
			want: 256,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rootElemSlots(tc.st)
			if got != tc.want {
				t.Fatalf("rootElemSlots(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
