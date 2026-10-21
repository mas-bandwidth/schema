package check

// FixedDefaultBody is claimed here rather than in the tableGeneratedVerbs row
// literal because that list is a shared row literal other cards append to, and
// its existing rows must never be edited in place. The Go table emitter mints
// a package-level `var <T>FixedDefaultBody` for a fixed table with a deprecated
// slot (internal/codegen/gotable/fixedform.go, emitFixedDefaultImage), and on
// docs/SPEC-TABLES.md §11's own rule it is claimed: it is an exported,
// user-collidable package-level `<T>`-prefixed identifier, so a name free
// today becomes a collision the day such a table gains a deprecated field.
func init() {
	tableGeneratedVerbs = append(tableGeneratedVerbs, "FixedDefaultBody")
}
