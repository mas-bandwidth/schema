package tablepack

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestZeroByteDagChainIsLinear pins the shared-DAG bound of the zero-byte walk.
// A node that names the next one twice makes a walk that re-enters on every
// reference descend into the same node once per reference — O(2^N) on a
// depth-N chain — until a visited set bounds it to once per node. The work is
// bounded by a node count, never by wall-clock time: a full walk over a 64-node
// chain must enter each of its 64 shared nodes exactly once, and a NUL planted
// in the deepest node must still be found.
func TestZeroByteDagChainIsLinear(t *testing.T) {
	t.Parallel()

	const depth = 64

	// Walk the whole graph: with no NUL to prune the search, a walk that
	// re-enters shared nodes does exponential work before returning. Entering
	// each node once keeps it linear, and the entry count pins that.
	full := diamondZeroByteChain(depth, false)
	w := &zeroByteWalk{visited: map[*tabletext.Instance]bool{}}
	w.instancePath(full[depth], "")
	if w.nodes != depth {
		t.Fatalf("zero-byte walk entered %d nodes on a %d-node chain; want %d (once per node, not per reference)", w.nodes, depth, depth)
	}

	// A NUL planted in the deepest node is still found.
	nul := diamondZeroByteChain(depth, true)
	w2 := &zeroByteWalk{visited: map[*tabletext.Instance]bool{}}
	got := w2.instancePath(nul[depth], "")
	want := strings.Repeat("left.", depth) + "s"
	if got != want {
		t.Fatalf("zero-byte walk found %q, want %q (the NUL in the deepest node)", got, want)
	}
}

// diamondZeroByteChain builds Item0..Item[depth]: Item0 holds one string (a NUL
// when nul is true), and each later instance holds left and right fields, both
// pointers to the previous instance — the same node twice. O(depth) instances,
// and a walk that re-enters a node for every reference is exponential, while
// one that enters each node once is linear.
func diamondZeroByteChain(depth int, nul bool) []*tabletext.Instance {
	chain := make([]*tabletext.Instance, depth+1)
	leaf := []byte{'a', 'b'}
	if nul {
		leaf = []byte{'a', 0, 'b'}
	}
	chain[0] = &tabletext.Instance{
		Def: &ir.Struct{Name: "Item0"},
		Fields: []tabletext.Field{
			{Def: &ir.Field{Name: "s", Type: ir.FieldType{Kind: ir.TString, Size: 8}}, Cell: tabletext.Cell{Str: leaf}},
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
