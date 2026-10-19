//go:build schema_leg_luau

package compiler

// The Luau target carries `?T` over a variable closure (docs/SPEC-TABLES.md
// §2.3): a walk that follows pointer edges visits T's edges only when the
// presence companion is set, and an absent value owns no node. The
// registration lives in the Luau target's own carries file, so no shared
// refusal line is edited and every other target keeps refusing the construct
// by name.
func init() {
	registerCarried("luau", carryOptionalVariableClosure)
}
