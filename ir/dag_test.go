package ir

import (
	"fmt"
	"testing"
)

// TestSharedTypeDAGIsMeasuredOnce pins the shared-DAG bound. A two-child
// chain `type ItemN { left Item(N-1); right Item(N-1) }` is O(N) source and,
// measured once per edge, O(2^N) work. Each named declaration is measured
// once per walk, so a depth-24 chain stays a small multiple of its
// declaration count. The counter caps itself at 1<<20: the unmemoized walk
// is about 2^24 visits and must fail before it finishes them.
//
// The numbers are the depth-4 chain computed by hand. The leaf is one
// uint64: 64 wire bits and 8 storage bytes at alignment 8. Each level is
// two by-value fields of the previous struct, so both the wire width and
// the record size double and nothing pads.
//
//	depth 4: MaxBits = 64<<4 = 1024, size = 8<<4 = 128.
//
// Depth 24 is that result doubled once per further level.
func TestSharedTypeDAGIsMeasuredOnce(t *testing.T) {
	t.Parallel()

	const handBits int64 = 1024
	const handSize int64 = 128
	d4 := twoChildDAG(4)
	if got := MaxBitsStruct(d4[4]); got != handBits {
		t.Fatalf("depth-4 MaxBits = %d, want %d", got, handBits)
	}
	if got := RecordLayout(&Unit{}, d4[4]).Size; got != handSize {
		t.Fatalf("depth-4 layout size = %d, want %d", got, handSize)
	}

	const depth = 24
	dag := twoChildDAG(depth)
	decls := depth + 1
	owned := make(map[*Struct]bool, decls)
	for _, st := range dag {
		owned[st] = true
	}
	const visitCap = 1 << 20
	var visits int
	note := func(st *Struct) {
		if !owned[st] {
			return
		}
		visits++
		if visits > visitCap {
			panic("struct-visit-cap")
		}
	}
	structMeasureNote.Store(&note)
	defer structMeasureNote.Store(nil)
	defer func() {
		if rec := recover(); rec != nil {
			if s, ok := rec.(string); ok && s == "struct-visit-cap" {
				t.Fatalf("struct visits exceeded %d on a %d-declaration DAG", visitCap, decls)
			}
			panic(rec)
		}
	}()

	root := dag[depth]
	bits := MaxBitsStruct(root)
	size := RecordLayout(&Unit{}, root).Size
	// one bits walk and one layout walk, each declaration once per walk,
	// with room for a second look at a declaration inside the same walk
	if visits > 4*decls {
		t.Fatalf("struct visits = %d, want at most %d (4 x %d declarations)", visits, 4*decls, decls)
	}
	if visits < decls {
		t.Fatalf("struct visits = %d, want at least the %d declarations", visits, decls)
	}
	wantBits := handBits << (depth - 4)
	wantSize := handSize << (depth - 4)
	if bits != wantBits {
		t.Fatalf("depth-24 MaxBits = %d, want %d (depth-4 %d doubled per level)", bits, wantBits, handBits)
	}
	if size != wantSize {
		t.Fatalf("depth-24 layout size = %d, want %d (depth-4 %d doubled per level)", size, wantSize, handSize)
	}
}

// twoChildDAG is Item0..Item[depth]: Item0 holds one uint64, and each later
// struct holds left and right fields of the previous struct, the same pointer
// twice. O(depth) declarations, and a walk that re-enters on every edge is
// exponential.
func twoChildDAG(depth int) []*Struct {
	leaf := &Field{Name: "v", Type: FieldType{Kind: TInt, Width: 64}}
	cur := &Struct{
		Name:   "Item0",
		Fields: []*Field{leaf},
		Items:  []Item{&FieldItem{F: leaf}},
	}
	out := []*Struct{cur}
	for n := 1; n <= depth; n++ {
		prev := cur
		left := &Field{Name: "left", Type: FieldType{Kind: TNamed, Name: prev.Name, Ref: prev}}
		right := &Field{Name: "right", Type: FieldType{Kind: TNamed, Name: prev.Name, Ref: prev}}
		cur = &Struct{
			Name:   fmt.Sprintf("Item%d", n),
			Fields: []*Field{left, right},
			Items:  []Item{&FieldItem{F: left}, &FieldItem{F: right}},
		}
		out = append(out, cur)
	}
	return out
}
