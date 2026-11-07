package compiler

// The Squirrel leg carries the OPTIONAL MAP (docs/SPEC-TABLES.md §2.8):
// absent and present-empty are distinct, a present empty map is written,
// absent is elided, and JSON null reads as absent.
func init() {
	registerCarried("squirrel", carryOptionalMap)
}
