package main

// REVIEW 2026-11-21 (CPP-1): a chain of unions nested inside an unknown
// field's bytes. A union arm's payload is framed (arm ref, kind, L, payload),
// so each arm is one level deeper (docs/SPEC-TABLES.md §4, binary_max_depth):
// the root body is depth 0, and the innermost union of a chain of D sits at
// depth D. 128 passes and 129 refuses, in unknown fields' bytes too. The deep
// vector is sized so a reader that counts the levels refuses it at once, and
// one that does not recurses five thousand levels before its strict walk.
func init() {
	framingVectors = append(framingVectors,
		framingVector{name: "union_chain_unknown_at_bound", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(unionChainField(128), gradeSilver, b(0))},
		framingVector{name: "union_chain_unknown_past_bound", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(unionChainField(129), gradeSilver, b(0))},
		framingVector{name: "union_chain_unknown_deep", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(unionChainField(5000), gradeSilver, b(0))},
	)
}

// unionChainField is field `zzz` (ref 16, an id Cfg does not declare) under
// kind 15 holding d unions, each one's arm (ref 16 again) the next union; the
// innermost holds no arm.
func unionChainField(d int) []byte {
	value := b(0)
	for i := 0; i < d; i++ {
		value = cat(b(16, 15), leb(uint64(len(value))), value)
	}
	return cat(b(16, 15), value)
}
