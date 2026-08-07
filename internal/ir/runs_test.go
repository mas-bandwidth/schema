package ir

import (
	"math/big"
	"reflect"
	"testing"
)

func rangedOp(bits int64) RunOp { return RunOp{Bits: bits, Fallible: true} }
func rawOp(bits int64) RunOp    { return RunOp{Bits: bits, Fallible: false} }

// The test message shape: raw 16 + three ranged 10s (46 bits).
func TestPlanRunWriteTestShape(t *testing.T) {
	plan := PlanRun([]RunOp{rawOp(16), rangedOp(10), rangedOp(10), rangedOp(10)}, 64, false)
	if !reflect.DeepEqual(plan.ChunkBits, []int64{46}) {
		t.Fatalf("write chunks = %v, want [46]", plan.ChunkBits)
	}
	// one chunk, contiguous LSB-first placement
	wantDst := []int64{0, 16, 26, 36}
	for i, op := range plan.Ops {
		if len(op.Pieces) != 1 || op.Pieces[0].DstShift != wantDst[i] || op.Pieces[0].Chunk != 0 {
			t.Fatalf("op %d pieces = %+v, want single piece at dst %d", i, op.Pieces, wantDst[i])
		}
	}
}

func TestPlanRunReadCutsAtFallible(t *testing.T) {
	// read side: raw 16 rides with the first ranged 10; each ranged op ends
	// its chunk so validations keep their exact wire positions
	plan := PlanRun([]RunOp{rawOp(16), rangedOp(10), rangedOp(10), rangedOp(10)}, 32, true)
	if !reflect.DeepEqual(plan.ChunkBits, []int64{26, 10, 10}) {
		t.Fatalf("read chunks = %v, want [26 10 10]", plan.ChunkBits)
	}
}

// The probe_header shape after its (non-run) align: const 8 + raw 3 +
// reserved 5, then raw 64.
func TestPlanRunProbeHeaderShapes(t *testing.T) {
	head := []RunOp{
		{Bits: 8, Fallible: true}, // const
		rawOp(3),                  // version
		{Bits: 5, Fallible: true}, // reserved
	}
	w := PlanRun(head, 64, false)
	if !reflect.DeepEqual(w.ChunkBits, []int64{16}) {
		t.Fatalf("head write chunks = %v, want [16]", w.ChunkBits)
	}
	r := PlanRun(head, 32, true)
	if !reflect.DeepEqual(r.ChunkBits, []int64{8, 8}) {
		t.Fatalf("head read chunks = %v, want [8 8]", r.ChunkBits)
	}
	id := []RunOp{rawOp(64)}
	rw := PlanRun(id, 64, false)
	if !reflect.DeepEqual(rw.ChunkBits, []int64{64}) {
		t.Fatalf("id write chunks = %v, want [64]", rw.ChunkBits)
	}
	rr := PlanRun(id, 32, true)
	if !reflect.DeepEqual(rr.ChunkBits, []int64{32, 32}) {
		t.Fatalf("id read chunks = %v, want [32 32]", rr.ChunkBits)
	}
	// the 64-bit op splits across the two read chunks
	if len(rr.Ops[0].Pieces) != 2 ||
		rr.Ops[0].Pieces[0] != (RunPiece{Chunk: 0, SrcShift: 0, DstShift: 0, Bits: 32}) ||
		rr.Ops[0].Pieces[1] != (RunPiece{Chunk: 1, SrcShift: 32, DstShift: 0, Bits: 32}) {
		t.Fatalf("id read pieces = %+v", rr.Ops[0].Pieces)
	}
}

// A ranged op wider than the read cap spans chunks and still cuts after its
// own end (the runtime's up-front whole-field bounds check has the same
// granularity: the field's full width).
func TestPlanRunWideRangedRead(t *testing.T) {
	plan := PlanRun([]RunOp{rawOp(5), rangedOp(40), rawOp(7)}, 32, true)
	if !reflect.DeepEqual(plan.ChunkBits, []int64{32, 13, 7}) {
		t.Fatalf("chunks = %v, want [32 13 7]", plan.ChunkBits)
	}
	pieces := plan.Ops[1].Pieces
	if len(pieces) != 2 ||
		pieces[0] != (RunPiece{Chunk: 0, SrcShift: 0, DstShift: 5, Bits: 27}) ||
		pieces[1] != (RunPiece{Chunk: 1, SrcShift: 27, DstShift: 0, Bits: 13}) {
		t.Fatalf("ranged pieces = %+v", pieces)
	}
}

func TestRunEligibleKinds(t *testing.T) {
	ranged := &FieldItem{F: &Field{
		HasIntRange: true, IntMin: big.NewInt(0), IntMax: big.NewInt(1000),
		Type: FieldType{Kind: TInt, Signed: true, Width: 16},
	}}
	if op, ok := RunEligible(ranged); !ok || op.Bits != 10 || !op.Fallible {
		t.Fatalf("ranged = %+v %v", op, ok)
	}
	if op, ok := RunEligible(&FieldItem{F: &Field{Type: FieldType{Kind: TBits, Width: 33}}}); !ok || op.Bits != 33 || op.Fallible {
		t.Fatalf("bits(33) = %+v %v", op, ok)
	}
	if _, ok := RunEligible(&FieldItem{F: &Field{Type: FieldType{Kind: TString, Size: 10}}}); ok {
		t.Fatal("string must not be eligible")
	}
	if _, ok := RunEligible(&AlignItem{}); ok {
		t.Fatal("align must not be eligible")
	}
	if _, ok := RunEligible(&FieldItem{F: &Field{Array: ArrayFixed, ArrayBound: 4, Type: FieldType{Kind: TBits, Width: 8}}}); ok {
		t.Fatal("arrays must not be eligible")
	}
	if op, ok := RunEligible(&FieldItem{F: &Field{Type: FieldType{Kind: TFloat64}}}); !ok || op.Bits != 64 {
		t.Fatalf("float64 = %+v %v", op, ok)
	}
	if _, ok := RunEligible(&FieldItem{F: &Field{HasFloatRange: true, Type: FieldType{Kind: TFloat32}}}); ok {
		t.Fatal("compressed float must not be eligible")
	}
}
