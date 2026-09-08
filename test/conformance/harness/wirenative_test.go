package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func nativeTestFile(body []byte, ids ...uint64) []byte {
	out := append([]byte{1}, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}
func TestNativeFramingUsesCompiledSizesAndLastNodeTable(t *testing.T) {
	leaf := &ir.Struct{Name: "Leaf", IsTable: true}
	root := &ir.Struct{Name: "Root", IsTable: true, Fields: []*ir.Field{
		{Name: "head", Type: ir.FieldType{Kind: ir.TNamed, Name: "Leaf", Ref: leaf, Pointer: true}},
		{Name: "blob", Type: ir.FieldType{Kind: ir.TBytes, Pointer: true}},
	}}
	layout := &nativeLayout{alignment: 8, records: map[uint64]nativeRecord{
		ir.TableWireId("Root"): {size: 24}, ir.TableWireId("Leaf"): {size: 40},
	}}
	r := &wireRoot{def: root, native: layout}
	// A zero-valued id is an ordinary unknown field; escape kinds must be
	// skipped before looking for the node table. Three records include one
	// unknown and a nine-byte blob with a native 8-byte header.
	nodes := []byte{3, 3, 1, 0, 4, 9, 1, 2, 3, 4, 5, 6, 7, 8, 9, 5, 1, 0}
	prefix := []byte{1, 31, 1, 99, 2, 12, byte(len(nodes))}
	body := append(append([]byte{}, prefix...), nodes...)
	ids := []uint64{0, math.MaxUint64, ir.TableWireId("Leaf"), ir.BytesWireTypeId, 0x1234}
	wire := nativeTestFile(append(append([]byte{}, body...), 0), ids...)
	if got, reason := r.nativeMeasure(wire); got != 24+40+24+4*16 || reason != 0 {
		t.Fatalf("size/reason = %d/%d", got, reason)
	}
	// Last occurrence wins even when that occurrence contains a truncated
	// first node. No decoded root or node values decide its storage.
	body = append(body, 2, 12, 3, 1, 3, 127, 0)
	wire = nativeTestFile(body, ids...)
	if got, reason := r.nativeMeasure(wire); got != 24+16 || reason != 0 {
		t.Fatalf("last node table = %d/%d", got, reason)
	}
	wire[0] = 2
	if got, reason := r.nativeMeasure(wire); got != -1 || reason != 9 {
		t.Fatalf("wrong form = %d/%d", got, reason)
	}
}
func TestNativeSequenceRefusalReasonsAndAlignment(t *testing.T) {
	f := &ir.Field{Name: "values", Array: ir.ArrayList, Type: ir.FieldType{Kind: ir.TInt, Width: 32, Signed: true}}
	l := &nativeLayout{}
	at := uint64(9)
	if !l.sequence(f, nativeCursor{data: []byte{4, 1, 0, 0, 0, 0}}, nativeSequence{size: 4, align: 4}, &at) || at != 16 {
		t.Fatalf("native element alignment = %d", at)
	}
	for _, tc := range []struct {
		data   []byte
		reason uint32
	}{{[]byte{4, 2, 0, 0, 0, 0}, 10}, {[]byte{4, 128, 128, 128, 128, 8}, 11}} {
		l.reason = 0
		at = 9
		if l.sequence(f, nativeCursor{data: tc.data}, nativeSequence{size: 4, align: 4}, &at) || l.reason != tc.reason {
			t.Fatalf("count = %v, reason %d", tc.data, l.reason)
		}
	}
}
func TestNativeLayoutRosterValidation(t *testing.T) {
	var b bytes.Buffer
	for _, v := range []uint32{2, 16, 1} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	for _, v := range []uint64{99, 40} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(1))
	for _, v := range []uint64{7, 24} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(8))
	if l, err := readNativeLayout(bytes.NewReader(b.Bytes())); err != nil || l.records[99].sequences[7].size != 24 {
		t.Fatalf("layout %v/%v", l, err)
	}
	for n := 0; n < b.Len(); n++ {
		if _, err := readNativeLayout(bytes.NewReader(b.Bytes()[:n])); err == nil {
			t.Fatalf("accepted truncated roster at %d", n)
		}
	}
	bad := append([]byte{}, b.Bytes()...)
	binary.LittleEndian.PutUint32(bad[4:], 3)
	if _, err := readNativeLayout(bytes.NewReader(bad)); err == nil {
		t.Fatal("accepted non-power-of-two alignment")
	}
}
