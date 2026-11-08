package check

// FixedBoundsKnown is claimed here rather than in the tableGeneratedVerbs row
// literal, on fixeddefaultbody_claim.go's rule: the shared row literal is
// appended to, never edited in place. The Go table emitter mints a
// package-level `func <T>FixedBoundsKnown<i>` for each older lineage entry
// whose writer declared ranges (internal/codegen/gotable/fixedform.go,
// emitFixedWriterBounds, docs/FIXED-FORM-ALGORITHM.md §4.5), and on
// docs/SPEC-TABLES.md §11's own rule it is claimed: a name free today becomes
// a collision the day such a table gains a lineage entry.
func init() {
	tableGeneratedVerbs = append(tableGeneratedVerbs, "FixedBoundsKnown")
}
