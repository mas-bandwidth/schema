package ir

import (
	"encoding/binary"
	"testing"
)

// These are direct IR sizing tests, not newly admitted declarations. The
// checker still refuses direct text/buffer arrays where SPEC-TABLES §2.13
// requires wrapper records. One element nevertheless owns its used-length
// prefix and bounded payload; it must not fall through to zero scalar bytes.
func TestTableFixedBufferElementBytes(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		kind                        FieldTypeKind
		bound, count, element, body int64
		array                       ArrayKind
		lengths                     []int64
	}{
		{"fixed-string", TString, 8, 2, 12, 24, ArrayFixed, []int64{0, 12}},
		{"fixed-bytes", TBytes, 5, 3, 9, 27, ArrayFixed, []int64{0, 9, 18}},
		{"fixed-wstring", TWString, 4, 2, 12, 24, ArrayFixed, []int64{0, 12}},
		{"counted-string", TString, 8, 3, 12, 40, ArrayCounted, []int64{4, 16, 28}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &Field{Name: "values", Type: FieldType{Kind: tc.kind, Size: tc.bound}, Array: tc.array, ArrayBound: tc.count}
			if got := TableFixedElementBytes(f); got != tc.element {
				t.Fatalf("element bytes=%d, want %d including the four-byte used length", got, tc.element)
			}
			if got := TableFixedFieldBytes(f); got != tc.body {
				t.Fatalf("array body bytes=%d, want %d", got, tc.body)
			}
			// An element's first four bytes hold length, then its payload.
			// Counted arrays have another four-byte count before element zero.
			start := int64(0)
			if tc.array == ArrayCounted {
				start = 4
			}
			for i, wantLength := range tc.lengths {
				lengthAt := start + int64(i)*TableFixedElementBytes(f)
				payloadAt := lengthAt + TableFixedCountBytes
				if lengthAt != wantLength || payloadAt != wantLength+4 {
					t.Fatalf("element %d: length/payload at %d/%d, want %d/%d", i, lengthAt, payloadAt, wantLength, wantLength+4)
				}
			}
			root := &Struct{Name: "Root", Fields: []*Field{
				{Name: "before", Type: FieldType{Kind: TInt, Width: 16}},
				f,
				{Name: "after", Type: FieldType{Kind: TInt, Width: 32}},
			}}
			if got := TableFixedTypeBytes(root); got != 2+tc.body+4 {
				t.Fatalf("root bytes=%d, want %d; following field starts at %d", got, 2+tc.body+4, 2+tc.body)
			}
			layout := TableFixedWalkRoot(root)
			if len(layout) != 5 || layout[0].Size != 2+tc.body+4 || layout[2].Size != tc.body || layout[3].Size != tc.element {
				t.Fatalf("layout lost the array/element byte extents: %+v", layout)
			}
			wire := TableFixedLayoutBytes(layout)
			for _, check := range []struct {
				entry int
				size  int64
			}{{0, 2 + tc.body + 4}, {2, tc.body}, {3, tc.element}} {
				at := TableFixedLayoutHeaderBytes + check.entry*TableFixedEntryBytes + 9
				if got := int64(binary.LittleEndian.Uint32(wire[at:])); got != check.size {
					t.Fatalf("layout entry %d size=%d, want %d", check.entry, got, check.size)
				}
			}
		})
	}
}
