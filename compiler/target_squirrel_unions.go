// The Squirrel target carries the union a table closure holds
// (docs/SPEC-TABLES.md §2.6): its arms are field lines of any type, a
// payload-free arm rides as the arm id with L = 0 (§2.6), and an array of
// unions is a field of the closure like any other (§2.6). The wire (§3) and
// its text (§16.2) are the table runtimes' (internal/codegen/squirreltable);
// this file only registers squirrel among the carriers.
//
// The carry registry schema-go-target-names-what-it-carries defines does not
// name this construct, so the per-target lists are the registration here:
// tableArmTargets is also the list refuseVoidArms reads, so one append carries
// both the table arm and the payload-free arm.
package compiler

func init() {
	tableArmTargets = append(tableArmTargets, "squirrel")
	unionArrayTargets = append(unionArrayTargets, "squirrel")
}
