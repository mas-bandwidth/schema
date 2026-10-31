// Squirrel carries the UNBOUNDED ARRAY in a table closure (docs/SPEC-TABLES.md
// §2.9, §3, §16.2). The target registers through [registerListCarrier], so its
// Generate stops refusing the construct and the table runtime
// (internal/codegen/squirreltable) reads and writes the count the data holds.
//
// The construct registry in compiler/carries.go leaves the unbounded array out
// of its keys, so the per-target list in compiler/tableslists.go is the
// registration this target uses.
package compiler

func init() {
	registerListCarrier("squirrel")
}
