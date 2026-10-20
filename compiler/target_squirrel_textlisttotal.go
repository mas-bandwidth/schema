//go:build schema_leg_squirrel

// The Squirrel target carries the TEXT LIST TOTAL (docs/SPEC-TABLES.md §2.13).
// The carrier decision lives in the shared registry (compiler/carries.go), one
// registration per target; this file is the Squirrel leg's whole share of the
// rule.
package compiler

func init() {
	registerCarried("squirrel", carryTextListTotal) // §2.13, `| total = N`
}
