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

// refuseToolMaps is the TOOL's COOK refusal (docs/SPEC-TABLES.md §2.8, §15): a unit
// whose table closure declares a map is refused by name at the tool's COOK and
// UNCOOK surfaces, because internal/tablecook does not lay out the entry
// arrays yet. `cook-check` is not among them: its scan refuses a map SLOT by
// name where it meets one (internal/tablecook), so a cook of a map-free root
// in a unit that declares a map elsewhere is checked as any other is.
//
// It is here, at the surface, rather than in the engine, and it is NAMED rather
// than left to the layout. Without it the engine lays out a region short of
// the entry arrays and a reader meets a slot pointing past its holder's
// extent, which is a corrupt file with nothing saying who wrote it. A refusal
// that says which is which is the whole difference.
func refuseToolMaps(u *ir.Unit) error {
	fields := ir.MapFields(u)
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf("unit declares a map in a table closure (%s) — the tool's WIRE and TEXT halves carry the construct, and its COOK half does not, so this command would lay out a region short of the entry arrays rather than refusing; the C++ reference carries the cook (--lang cpp), and the tool's half is schema#380's next PR (docs/SPEC-TABLES.md §2.8, §15)",
		englishList(fields))
}
