//go:build schema_leg_luau

// The Luau target carries `| required` (docs/SPEC-TABLES.md §2.3): the
// construct is registered from this file, beside the target's own
// registration, so every other target keeps refusing it by name and no
// shared refusal line is edited.
package compiler

func init() {
	registerCarried("luau", carryRequired)
}
