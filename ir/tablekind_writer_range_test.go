package ir

import (
	"math/big"
	"testing"
)

func rangedField(name string, typ FieldType, lo, hi int64) *Field {
	return &Field{Name: name, Type: typ, HasIntRange: true, IntMin: big.NewInt(lo), IntMax: big.NewInt(hi)}
}

// A writer's range resolves onto the reader's field of the same name and kind
// as a copy carrying the writer's bounds, and onto nothing else.
func TestTableFixedWriterField(t *testing.T) {
	i32 := FieldType{Kind: TInt, Width: 32, Signed: true}
	writer := &Struct{Fields: []*Field{rangedField("v", i32, 0, 100)}}
	ranges := TableFixedWriterRanges(writer)
	if len(ranges) != 1 || ranges[0].Name != "v" || ranges[0].Lo != 0 || ranges[0].Hi != 100 {
		t.Fatalf("writer ranges: %+v", ranges)
	}
	reader := rangedField("v", i32, 0, 200)
	w, ok := TableFixedWriterField(reader, ranges[0])
	if !ok || w.IntMin.Int64() != 0 || w.IntMax.Int64() != 100 {
		t.Fatalf("widened reader: ok=%v %+v", ok, w)
	}
	if reader.IntMax.Int64() != 200 {
		t.Fatal("the reader's own field moved")
	}
	if _, ok := TableFixedWriterField(rangedField("w", i32, 0, 200), ranges[0]); ok {
		t.Fatal("a different name resolved")
	}
	if _, ok := TableFixedWriterField(rangedField("v", FieldType{Kind: TInt, Width: 64, Signed: true}, 0, 200), ranges[0]); ok {
		t.Fatal("a different kind resolved")
	}
	u8 := FieldType{Kind: TInt, Width: 8}
	held := TableFixedWriterRange{Name: "b", Kind: TableKindU8, Lo: 0, Hi: 255}
	if _, ok := TableFixedWriterField(rangedField("b", u8, 0, 10), held); ok {
		t.Fatal("a range the storage already holds resolved")
	}
	fx := FieldType{Kind: TFixed, Width: 16, Signed: true, FracBits: 4}
	raw := TableFixedWriterRanges(&Struct{Fields: []*Field{rangedField("f", fx, -2, 3)}})
	if len(raw) != 1 || raw[0].Lo != -32 || raw[0].Hi != 48 {
		t.Fatalf("fixed-point writer range is not on the raw scale: %+v", raw)
	}
	wf, ok := TableFixedWriterField(rangedField("f", fx, -4, 6), raw[0])
	if !ok || wf.IntMin.Int64() != -2 || wf.IntMax.Int64() != 3 {
		t.Fatalf("fixed-point writer field: ok=%v %+v", ok, wf)
	}
}
