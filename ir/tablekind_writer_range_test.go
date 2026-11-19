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
	if w64, ok := TableFixedWriterField(rangedField("v", FieldType{Kind: TInt, Width: 64, Signed: true}, 0, 200), ranges[0]); !ok || w64.IntMin.Int64() != 0 || w64.IntMax.Int64() != 100 {
		t.Fatalf("widened kind i32->i64 did not resolve: ok=%v %+v", ok, w64)
	}
	if _, ok := TableFixedWriterField(rangedField("v", FieldType{Kind: TInt, Width: 64, Signed: false}, 0, 200), ranges[0]); ok {
		t.Fatal("an incompatible kind resolved")
	}
	bitsWriter := &Struct{Fields: []*Field{{Name: "b", Type: FieldType{Kind: TBits, Width: 5}}}}
	bitsRanges := TableFixedWriterRanges(bitsWriter)
	if len(bitsRanges) != 1 || bitsRanges[0].Lo != 0 || bitsRanges[0].Hi != 31 {
		t.Fatalf("bare bits(5) writer ranges: %+v", bitsRanges)
	}
	if wb, ok := TableFixedWriterField(&Field{Name: "b_new", WasName: "b", Type: FieldType{Kind: TBits, Width: 37}}, bitsRanges[0]); !ok || wb.IntMin.Int64() != 0 || wb.IntMax.Int64() != 31 {
		t.Fatalf("widened renamed bits(5)->bits(37) did not resolve: ok=%v %+v", ok, wb)
	}
	fx32 := FieldType{Kind: TFixed, Width: 32, Signed: true, FracBits: 4}
	fx128 := FieldType{Kind: TFixed, Width: 128, Signed: true, FracBits: 4}
	fxRanges := TableFixedWriterRanges(&Struct{Fields: []*Field{rangedField("fx", fx32, -32768, 32767)}})
	if wfx, ok := TableFixedWriterField(rangedField("fx", fx128, -134217728, 134217727), fxRanges[0]); !ok || wfx.IntMin.Int64() != -32768 || wfx.IntMax.Int64() != 32767 {
		t.Fatalf("widened fixed32->fixed128 did not resolve: ok=%v %+v", ok, wfx)
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
