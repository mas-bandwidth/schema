//go:build schema_leg_squirrel

package compiler

// Squirrel carries `wstring(N)` on the packet wire and in a table closure
// (docs/SPEC-TABLES.md §3, kind 33; SPEC §4.12). It registers both carriers
// from its own file's init (compiler/widetext.go): registerWideTextCarrier
// for the packet wire, and the closure set for the table wire.
func init() {
	registerWideTextCarrier("squirrel")
	tableWideTextTargets["squirrel"] = true
}
