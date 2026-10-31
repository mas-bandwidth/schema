// The MAP cross-target refusal (docs/SPEC-TABLES.md §2.8, §11): its own file,
// per the registry split — a construct's carrier registry and its refusal add
// a file beside builtin.go rather than growing it. A target that carries the
// construct registers through [registerMapCarrier] from its own file's init;
// every other target's Generate calls [refuseMaps].
package compiler

import (
	"fmt"
	"slices"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// mapTargets is the canonical name of every built-in target whose table
// backend carries a MAP (docs/SPEC-TABLES.md §2.8); refuseMaps names them.
var mapTargets []string

// registerMapCarrier is what a carrying target's file calls from its init,
// beside its registerBuiltin call.
func registerMapCarrier(name string) { mapTargets = append(mapTargets, name) }

// refuseMaps gives targets without this codec a named refusal.
// Registered carriers accept the construct.
func refuseMaps(u *ir.Unit, target string) error {
	if slices.Contains(mapTargets, target) {
		return nil
	}
	fields := ir.MapFields(u)
	if len(fields) == 0 {
		return nil
	}
	carry, flags := carriers(mapTargets)
	return fmt.Errorf("unit declares a map in a table closure (%s) — a map is %s only today, and the %s form is a named follow-on; generate with %s, or spell the lookup as a bounded array of a `{ key, value }` table and search it yourself (docs/SPEC-TABLES.md §2.8, §11, §15)",
		englishList(fields), englishList(carry), target, englishList(flags))
}

// refuseOptionalMaps is the named refusal every target but go gives a unit
// whose table closure holds an OPTIONAL MAP (docs/SPEC-TABLES.md §2.8): Go
// carries `?map[K]V` first, and a target that carries the plain map is refused
// too, because its writer would elide a present empty map as absent.
func refuseOptionalMaps(u *ir.Unit, target string) error {
	if fields := ir.OptionalMapFields(u); !carries(target, carryOptionalMap) && len(fields) > 0 {
		return fmt.Errorf("target %q refuses the construct optional map on field %s — ?map[K]V is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.8)", target, fields[0])
	}
	return nil
}
