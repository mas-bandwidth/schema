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

// refuseMaps is the named refusal every target without the construct gives a
// unit whose table closure declares a MAP (docs/SPEC-TABLES.md §2.8, §11).
//
// TODAY THAT IS EVERY TARGET, and the refusal says so rather than naming a
// carrier that does not exist: the language takes the declaration — the parser
// reads `map[K]V`, the checker holds every rule §2.8 states, and the generated
// `{ key, value }` entry is a table of the closure with its record and its two
// constant ids — and the CODECS are the same issue's next PR (schema#380). A
// unit that declares a map is refused loudly here rather than emitted as a
// codec that never met the entry, its sort or its ascending check.
//
// When the C++ reference's codec lands, target_cpp.go registers through
// [registerMapCarrier] from its own init and drops its refuseMaps call, and
// this message names it the way every other construct's does.
func refuseMaps(u *ir.Unit, target string) error {
	fields := ir.MapFields(u)
	if len(fields) == 0 {
		return nil
	}
	if len(mapTargets) == 0 {
		return fmt.Errorf("unit declares a map in a table closure (%s) — the language takes the declaration and no backend carries the codec yet, including %s: the construct's C++ reference and tool halves are schema#380's next PR; spell the lookup as a bounded array of a `{ key, value }` table and search it yourself, or wait for that PR (docs/SPEC-TABLES.md §2.8, §11, §15)",
			englishList(fields), target)
	}
	carry, flags := carriers(mapTargets)
	return fmt.Errorf("unit declares a map in a table closure (%s) — a map is %s only today, and the %s form is a named follow-on; generate with %s, or spell the lookup as a bounded array of a `{ key, value }` table and search it yourself (docs/SPEC-TABLES.md §2.8, §11, §15)",
		englishList(fields), englishList(carry), target, englishList(flags))
}
