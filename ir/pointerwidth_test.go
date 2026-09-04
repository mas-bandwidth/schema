// The width helpers stop at pointer edges (docs/SPEC-TABLES.md §3.1): a
// pointer rides as a u32 node index and no pointer edge is a nesting level, so
// a width is the REFERENCE's, never the referent's. Following one by value
// read a legal `table Node { next *Node }` as an infinite nesting and killed
// the process with a stack overflow, which no caller could recover.
//
// The corpus below carries every pointer spelling the language has beside a
// by-value nesting, because the fix must stop at pointer edges WITHOUT
// stopping at the by-value edges the same helpers exist to follow.
package ir_test

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const pointerCorpus = `package ptrtest

type Vec {
    x float32
    y float32
}

table Node {
    value    int32 | min = 0, max = 100
    next     *Node
    children [..4]*Node
    blob     *bytes
    position Vec
}

table ByValue {
    position Vec
}
`

func loadPointerCorpus(t *testing.T) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Ptr.schema", []byte(pointerCorpus))
	if len(perrs) > 0 {
		t.Fatalf("test corpus does not parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "Ptr.schema",
		Name:  "Ptr.schema",
		Base:  "Ptr",
		Bytes: []byte(pointerCorpus),
		AST:   f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("test corpus does not check: %v", cerrs[0])
	}
	return u
}

// withinBounds runs f and fails if it has not finished in time. A descent
// through pointer edges either overflows the stack (which takes the process
// down, red on its own) or runs long; the bound turns the second into a
// failure rather than a hang.
func withinBounds(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s did not finish in 30s: the walk is following pointer edges", what)
	}
}

func TestPointerRecursiveTableHasAFiniteWidth(t *testing.T) {
	u := loadPointerCorpus(t)
	node := u.Tables["Node"]
	if node == nil {
		t.Fatal("the corpus declares table Node")
	}

	var bits int64
	withinBounds(t, "ir.MaxBitsStruct", func() { bits = ir.MaxBitsStruct(node) })

	// value 7 bits (the range [0, 100]), next 32, children 3 count bits plus
	// 4 x 32, blob 32, position 64 (docs/SPEC-TABLES.md §3.1)
	if want := int64(7 + 32 + (3 + 4*32) + 32 + 64); bits != want {
		t.Errorf("MaxBitsStruct(Node) = %d, want %d", bits, want)
	}
	if got, want := ir.MaxBytes(bits), int64(40); got != want {
		t.Errorf("MaxBytes = %d, want %d", got, want)
	}

	// the alignment analysis walks the same edges and must stop at the same
	// places
	withinBounds(t, "ir.AlignedFixedByteArrays", func() { ir.AlignedFixedByteArrays(node) })

	// so does the storage layout, which already stopped: a pointer slot is
	// eight bytes (§6.3), not its referent's record
	var size int64
	withinBounds(t, "ir.RecordLayout", func() { size = ir.RecordLayout(u, node).Size })
	if want := int64(72); size != want {
		t.Errorf("sizeof(Node) = %d, want %d", size, want)
	}
}

// A BY-VALUE edge is still followed: the stop is at pointer edges only, and a
// helper that stopped at both would report a nested type as costing nothing.
func TestByValueNestingStillDescends(t *testing.T) {
	u := loadPointerCorpus(t)
	byValue := u.Tables["ByValue"]
	if byValue == nil {
		t.Fatal("the corpus declares table ByValue")
	}
	if got, want := ir.MaxBitsStruct(byValue), int64(64); got != want {
		t.Errorf("MaxBitsStruct(ByValue) = %d, want %d — a by-value nesting must still be counted", got, want)
	}
}

func TestPointerFieldWidthIsTheReference(t *testing.T) {
	u := loadPointerCorpus(t)
	node := u.Tables["Node"]
	for _, f := range node.Fields {
		if !f.Type.Pointer || f.Array != ir.ArrayNone {
			continue
		}
		if got := ir.MaxBitsField(f); got != ir.PointerWireBits {
			t.Errorf("MaxBitsField(%s) = %d, want %d", f.Name, got, ir.PointerWireBits)
		}
	}
}
