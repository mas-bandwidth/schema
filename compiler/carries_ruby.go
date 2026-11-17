//go:build schema_leg_ruby

package compiler

// rubyCarried is every construct the Ruby target carries (docs/SPEC-TABLES.md;
// docs/PORT-RUBY.md, THE CARD MAP). It starts empty: the skeleton carries no
// table construct. A later layer that carries one adds a line in its OWN new
// file (compiler/carries_ruby_<surface>.go, tag schema_leg_ruby):
//
//	var _ = carryRuby(carryForms)
//
// Package-level variables initialise before any init() runs, so every such
// line has run when the init below registers the slice ONCE, and no later
// layer edits this file.
var rubyCarried []string

// carryRuby adds a construct to rubyCarried; its result exists only so the
// call can initialise a package-level blank variable.
func carryRuby(construct string) bool {
	rubyCarried = append(rubyCarried, construct)
	return true
}

func init() {
	registerCarries("ruby", rubyCarried...)
}
