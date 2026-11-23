package main

// REVIEW 2026-11-21 (CPP-1): a chain of unions nested inside an unknown
// field's bytes. A union arm's payload is framed (arm ref, kind, L, payload),
// so each arm is one level deeper (docs/SPEC-TABLES.md §4, binary_max_depth):
// the root body is depth 0, and the innermost union of a chain of D sits at
// depth D. 128 passes and 129 refuses, in unknown fields' bytes too. The deep
// vector holds five thousand levels: past the bound by far, and still small
// enough for the harness's own locator, which copies the enclosing chain at
// every spot. The chain sized to run a counting-blind reader out of stack
// (100000 levels, about 500 KB) is the shared hostile corpus row
// wire-union-chain-deep, which no locator walks.
func init() {
	framingVectors = append(framingVectors,
		framingVector{name: "union_chain_unknown_at_bound", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(unionChainField(128), gradeSilver, b(0))},
		framingVector{name: "union_chain_unknown_past_bound", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(unionChainField(129), gradeSilver, b(0))},
		framingVector{name: "union_chain_unknown_deep", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(unionChainField(5000), gradeSilver, b(0))},
	)
}

// unionChainField is field `zzz` (ref 16, an id Cfg does not declare) under
// kind 15 holding d unions, each one's arm (ref 16 again) the next union; the
// innermost holds no arm. Written front to back in one pass: level k's frame
// is 2 bytes, then the LEB of level k-1's length, then level k-1.
func unionChainField(d int) []byte {
	size := make([]int, d+1)
	size[0] = 1
	for k := 1; k <= d; k++ {
		size[k] = 2 + len(leb(uint64(size[k-1]))) + size[k-1]
	}
	out := make([]byte, 0, size[d]+2)
	out = append(out, 16, 15)
	for k := d; k >= 1; k-- {
		out = append(out, 16, 15)
		out = append(out, leb(uint64(size[k-1]))...)
	}
	return append(out, 0)
}
