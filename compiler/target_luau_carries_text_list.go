//go:build schema_leg_luau

// The Luau target carries the TEXT LIST construct (docs/SPEC-TABLES.md
// §2.13): [..N]string(M), [..N]?string(M), []string(M) and []?string(M).
// Only this construct is registered here — the array root (§16.10) and a
// text list's total still have their own cards — so every other target keeps
// refusing it by the same name.
package compiler

import "github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"

func init() {
	registerCarried("luau", carryTextList)
	// Carrying the text list makes the central text-form gate step over the
	// constructs this target does not carry yet, so the table surface keeps
	// refusing them by the same name (docs/SPEC-TABLES.md §2.13.1, §16.10).
	luautable.EnforceCarryRefusals()
}
