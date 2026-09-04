// The MAP cross-target refusal (docs/SPEC-TABLES.md §2.8, §11): its own file,
// per the registry split — a construct's carrier registry and its refusal add
// a file beside builtin.go rather than growing it. A target that carries the
// construct registers through [registerMapCarrier] from its own file's init;
// every other target's Generate calls [refuseMaps].
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// mapTargets is the canonical name of every built-in target whose table
// backend carries a MAP (docs/SPEC-TABLES.md §2.8); refuseMaps names them.
var mapTargets []string

// registerMapCarrier is what a carrying target's file calls from its init,
// beside its registerBuiltin call.
func registerMapCarrier(name string) { mapTargets = append(mapTargets, name) }

// refuseMaps is the named refusal every PORT gives a unit whose table closure
// declares a MAP (docs/SPEC-TABLES.md §2.8, §11, §15).
//
// A map is a VARIABLE-CLASS construct, and the variable class is the C++
// reference's alone — the arena, the region, the node extent and the walks a
// map's entries ride in are all the reference's. So the reference carries the
// codec, registers through [registerMapCarrier] from its own init and never
// reaches here, and every port refuses loudly rather than emitting a codec
// that never met the entry, its sort or its ascending check.
func refuseMaps(u *ir.Unit, target string) error {
	fields := ir.MapFields(u)
	if len(fields) == 0 {
		return nil
	}
	carry, flags := carriers(mapTargets)
	return fmt.Errorf("unit declares a map in a table closure (%s) — a map is %s only today, and the %s form is a named follow-on; generate with %s, or spell the lookup as a bounded array of a `{ key, value }` table and search it yourself (docs/SPEC-TABLES.md §2.8, §11, §15)",
		englishList(fields), englishList(carry), target, englishList(flags))
}

// refuseToolMaps is the TOOL's COOK refusal (docs/SPEC-TABLES.md §2.8, §15): a unit
// whose table closure declares a map is refused by name at every table surface
// the tool has — pack, unpack, cook, cook-check and uncook — because
// internal/tablewire does not carry the construct yet.
//
// It is here, at the surface, rather than in the engine, and it is NAMED rather
// than left to the decoder. Without it the engine meets a kind 14 field whose
// element kind is 13, decodes nothing into a slot it has no shape for, and
// reports FRAMING DAMAGE — an answer that sends its reader looking for a
// corrupt file when the file is fine and the reader is the one that is short.
// A refusal that says which is which is the whole difference.
func refuseToolMaps(u *ir.Unit) error {
	fields := ir.MapFields(u)
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf("unit declares a map in a table closure (%s) — the tool's WIRE and TEXT halves carry the construct, and its COOK half does not, so this command would lay out a region short of the entry arrays rather than refusing; the C++ reference carries the cook (--lang cpp), and the tool's half is schema#380's next PR (docs/SPEC-TABLES.md §2.8, §15)",
		englishList(fields))
}
