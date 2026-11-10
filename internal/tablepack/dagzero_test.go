package tablepack

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestZeroByteDagChainIsLinear pins the shared-DAG bound of the zero-byte
// walk. A node that names the next one twice makes a walk that re-enters on
// every reference descend into the same node once per reference — O(2^N) on
// a depth-N chain — until a visited set bounds it to once per node. The work
// is bounded by a node count (a 64-node chain must enter each of its 64 nodes
// at most once), never by wall-clock time, and a NUL planted in the deepest
// node must still be found.
func TestZeroByteDagChainIsLinear(t *testing.T) {
	t.Parallel()

	const depth = 64
	chain := diamondZeroByteChain(depth)

	w := &zeroByteWalk{visited: map[*tabletext.Instance]bool{}}
	got := w.instancePath(chain[depth], "")

	if w.nodes != depth {
		t.Fatalf("zero-byte walk entered %d nodes on a %d-node chain; want %d (once per node, not per reference)", w.nodes, depth, depth)
	}
	want := strings.Repeat("left.", depth) + "s"
	if got != want {
		t.Fatalf("zero-byte walk found %q, want %q (the NUL in the deepest node)", got, want)
	}
}

// diamondZeroByteChain builds Item0..Item[depth]: Item0 holds one string that
// carries a NUL, and each later instance holds left and right fields, both
// pointers to the previous instance — the same node twice. O(depth)
// instances, and a walk that re-enters a node for every reference is
// exponential.
func diamondZeroByteChain(depth int) []*tabletext.Instance {
	chain := make([]*tabletext.Instance, depth+1)
	leafType := &ir.Struct{Name: "Item0"}
	sField := &ir.Field{Name: "s", Type: ir.FieldType{Kind: ir.TString, Size: 8}}
	chain[0] = &tabletext.Instance{
		Def: leafType,
		Fields: []tabletext.Field{
			{Def: sField, Cell: tabletext.Cell{Str: []byte{'a', 0, 'b'}}},
		},
	}
	for i := 1; i <= depth; i++ {
		typ := &ir.Struct{Name: fmt.Sprintf("Item%d", i)}
		left := &ir.Field{Name: "left", Type: ir.FieldType{Kind: ir.TNamed, Name: typ.Name, Pointer: true}}
		right := &ir.Field{Name: "right", Type: ir.FieldType{Kind: ir.TNamed, Name: typ.Name, Pointer: true}}
		chain[i] = &tabletext.Instance{
			Def: typ,
			Fields: []tabletext.Field{
				{Def: left, Cell: tabletext.Cell{Node: chain[i-1]}},
				{Def: right, Cell: tabletext.Cell{Node: chain[i-1]}},
			},
		}
	}
	return chain
}
