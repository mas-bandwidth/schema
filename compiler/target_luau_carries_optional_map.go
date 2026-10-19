//go:build schema_leg_luau

// Luau carries the OPTIONAL MAP, `?map[K]V` (docs/SPEC-TABLES.md §2.8): the
// construct's three values — absent, present-empty and present-with-entries —
// are the map's beside a presence companion, as §2.3 spells every `?T`. The
// construct is registered here, in the target's own file, so no shared
// refusal line is edited and every target that does not register it keeps
// refusing by name.
package compiler

func init() {
	registerCarried("luau", carryOptionalMap)
}
