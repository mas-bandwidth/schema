package compiler

import "sort"

// The Squirrel leg carries `wstring(N)` on both wires: the packet type surface
// and the table surface's kind 33 (SPEC.md §4.12, docs/SPEC-TABLES.md §3).
// TABLE wide text is a separate landing from the packet one, so squirrel joins
// tableWideTextTargets and tableWideTextNames here, beside its packet
// registration.
func init() {
	registerWideTextCarrier("squirrel")
	tableWideTextTargets["squirrel"] = true
	tableWideTextNames = append(tableWideTextNames, "Squirrel")
	sort.Strings(tableWideTextNames)
}
