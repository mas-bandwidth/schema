// The UNBOUNDED ARRAY cross-target refusal (docs/SPEC-TABLES.md §2.9, §11):
// its own file, per the registry split: a construct's carrier registry and
// its refusal add a file beside builtin.go rather than growing it. A target
// that carries the construct registers through [registerListCarrier] from its
// own file's init, and every other target's Generate calls [refuseLists].
package compiler

import (
	"fmt"
	"slices"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// listTargets is the canonical name of every built-in target whose table
// backend carries an UNBOUNDED ARRAY (docs/SPEC-TABLES.md §2.9). refuseLists
// names them.
var listTargets []string

// registerListCarrier is what a carrying target's file calls from its init,
// beside its registerBuiltin call.
func registerListCarrier(name string) { listTargets = append(listTargets, name) }

// refuseLists gives targets without this codec a named refusal.
// Registered carriers accept the construct.
func refuseLists(u *ir.Unit, target string) error {
	if slices.Contains(listTargets, target) {
		return nil
	}
	fields := ir.ListFields(u)
	if len(fields) == 0 {
		return nil
	}
	carry, flags := carriers(listTargets)
	return fmt.Errorf("unit declares an unbounded array in a table closure (%s): a []T is %s only today, and the %s form is a named follow-on. Generate with %s, or declare the array at a bound, [..N]T, which is the same bytes (docs/SPEC-TABLES.md §2.9, §11, §15)",
		englishList(fields), englishList(carry), target, englishList(flags))
}

// THE TOOL'S COOK AND UNCOOK HALVES CARRY THE UNBOUNDED ARRAY (schema#380,
// docs/SPEC-TABLES.md §2.9, §7.6). There is no `refuseToolLists` any more:
// `internal/tablecook` lays the element arrays out in the holder's node
// extent, PRE-ORDER, exactly as the C++ reference does — the same
// `ir.ListElementLayout` numbers, the same alignment floor on the record, the
// same "the extent written is the extent measured" check before a header is
// written — and reads them back through the deltas it wrote. `cook-check`
// carries §7.4's element-array clause beside them.
//
// The MAP's tool cook half is still owed and still refused by name
// ([refuseToolMaps]): the two constructs share the extent but a map adds the
// sort, the entry array's key order and the two reader events, and none of
// that is what a list needed.
