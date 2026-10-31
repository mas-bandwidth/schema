//go:build schema_leg_goose

package compiler

// gooseCarried is every construct the Goose target carries (docs/SPEC-TABLES.md;
// docs/PORT-GOOSE.md, THE CARD MAP). It starts empty: the skeleton carries
// no table construct. A later card that carries one adds a line in its OWN
// new file (compiler/carries_goose_<surface>.go, tag schema_leg_goose):
//
//	var _ = carryGoose(carryForms)
//
// Package-level variables initialise before any init() runs, so every such
// line has run when the init below registers the slice ONCE, and no later
// card edits this file.
var gooseCarried []string

// carryGoose adds a construct to gooseCarried; its result exists only so the
// call can initialise a package-level blank variable.
func carryGoose(construct string) bool {
	gooseCarried = append(gooseCarried, construct)
	return true
}

func init() {
	registerCarries("goose", gooseCarried...)
}
