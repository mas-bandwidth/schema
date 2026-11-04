//go:build schema_leg_python

package compiler

// pythonCarried is every construct the Python target carries
// (docs/SPEC-TABLES.md; docs/PORT-PYTHON.md, THE LAYERS). It starts empty: the
// skeleton carries no table construct. A later layer that carries one adds a
// line in its OWN new file (compiler/carries_python_<surface>.go, tag
// schema_leg_python):
//
//	var _ = carryPython(carryForms)
//
// Package-level variables initialise before any init() runs, so every such
// line has run when the init below registers the slice ONCE, and no later
// layer edits this file.
var pythonCarried []string

// carryPython adds a construct to pythonCarried; its result exists only so the
// call can initialise a package-level blank variable.
func carryPython(construct string) bool {
	pythonCarried = append(pythonCarried, construct)
	return true
}

func init() {
	registerCarries("python", pythonCarried...)
}
