// The UNBOUNDED ARRAY cross-target refusal (docs/SPEC-TABLES.md §2.9, §11):
// its own file, per the registry split: a construct's carrier registry and
// its refusal add a file beside builtin.go rather than growing it. A target
// that carries the construct registers through [registerListCarrier] from its
// own file's init, and every other target's Generate calls [refuseLists].
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// listTargets is the canonical name of every built-in target whose table
// backend carries an UNBOUNDED ARRAY (docs/SPEC-TABLES.md §2.9). refuseLists
// names them.
var listTargets []string

// registerListCarrier is what a carrying target's file calls from its init,
// beside its registerBuiltin call.
func registerListCarrier(name string) { listTargets = append(listTargets, name) }

// refuseLists is the named refusal every PORT gives a unit whose table
// closure declares a `[]T` (docs/SPEC-TABLES.md §2.9, §11, §15).
//
// An unbounded array is a VARIABLE-CLASS construct, and the variable class is
// the C++ reference's alone — the arena, the region, the node extent and the
// walks a list's elements ride in are all the reference's. So the reference
// carries the codec, registers through [registerListCarrier] from its own
// init and never reaches here, and every port refuses loudly rather than
// emitting a codec that never met an element array.
func refuseLists(u *ir.Unit, target string) error {
	fields := ir.ListFields(u)
	if len(fields) == 0 {
		return nil
	}
	carry, flags := carriers(listTargets)
	return fmt.Errorf("unit declares an unbounded array in a table closure (%s): a []T is %s only today, and the %s form is a named follow-on. Generate with %s, or declare the array at a bound, [..N]T, which is the same bytes (docs/SPEC-TABLES.md §2.9, §11, §15)",
		englishList(fields), englishList(carry), target, englishList(flags))
}

// refuseToolLists is the TOOL's COOK refusal (docs/SPEC-TABLES.md §2.9, §15),
// the map's own, one construct over: a unit whose table closure declares a
// `[]T` is refused by name at the tool's COOK and UNCOOK surfaces, because
// internal/tablecook does not lay out the element arrays yet. `cook-check`
// is not among them: its scan carries §7.4's element-array clause, and a cook
// the C++ reference wrote is checked there.
//
// It is here, at the surface, rather than in the engine, and it is NAMED
// rather than left to the layout. Without it the engine lays out a region
// short of the element arrays and a reader meets a slot pointing past its
// holder's extent, which is a corrupt file with nothing saying who wrote it.
func refuseToolLists(u *ir.Unit) error {
	fields := ir.ListFields(u)
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf("unit declares an unbounded array in a table closure (%s): the tool's WIRE and TEXT halves carry the construct and `cook-check` reads one, and its COOK half does not, so this command would lay out a region short of the element arrays rather than refusing. The C++ reference carries the cook (--lang cpp) (docs/SPEC-TABLES.md §2.9, §15)",
		englishList(fields))
}
