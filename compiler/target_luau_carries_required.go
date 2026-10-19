//go:build schema_leg_luau

// The Luau target carries `| required` (docs/SPEC-TABLES.md §2.3): the
// construct is registered from this file, beside the target's own
// registration, so every other target keeps refusing it by name and no
// shared refusal line is edited.
package compiler

func init() {
	registerCarried("luau", carryRequired)
}

// registerCarried adds constructs to a target whose base registration already
// exists in compiler/carries.go. The standard (docs/SPEC-TABLES.md) is
// unchanged: it only records that this target now carries the construct.
func registerCarried(target string, constructs ...string) {
	set, ok := targetCarries[target]
	if !ok {
		panic("compiler: cannot carry constructs for an unregistered target " + target)
	}
	for _, construct := range constructs {
		if _, known := knownCarryConstructs[construct]; !known {
			panic("compiler: unknown carried construct " + construct)
		}
		set[construct] = struct{}{}
	}
}
