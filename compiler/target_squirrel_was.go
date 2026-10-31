// Squirrel carries a `was` on an enum variant, on a union arm and on a field
// of a type a table reaches (docs/SPEC-TABLES.md §5): each rides under the
// OLD name's id, so a stored value or body written before the rename reads in
// silence. The refused form is registered by appending to the per-construct
// carrier list the refuser walks.
package compiler

// init registers squirrel as a carrier of the three `was` rows
// (docs/SPEC-TABLES.md §5). The construct is not named by the cross-target
// carried registry (compiler/carries.go's carryConstructs) at this base, so
// the per-target list wasRowTargets is the register.
func init() {
	wasRowTargets = append(wasRowTargets, "squirrel")
}
