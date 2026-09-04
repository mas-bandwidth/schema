// The OPTIONAL ARRAY cross-target refusal (docs/SPEC-TABLES.md §2.3, §11): its
// own file, per the registry split — a construct's carrier registry and its
// refusal add a file beside builtin.go rather than growing it. A target that
// carries the form registers through [registerOptionalArrayCarrier] from its
// own file's init; every other target's Generate calls
// [refuseOptionalArrays].
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// optionalArrayTargets is the canonical name of every built-in target whose
// table backend carries an OPTIONAL ARRAY (docs/SPEC-TABLES.md §2.3);
// refuseOptionalArrays names them.
var optionalArrayTargets []string

// registerOptionalArrayCarrier is what a carrying target's file calls from
// its init, beside its registerBuiltin call.
func registerOptionalArrayCarrier(name string) {
	optionalArrayTargets = append(optionalArrayTargets, name)
}

// refuseOptionalArrays is the named refusal every target without the form
// gives a unit whose table closure holds an OPTIONAL ARRAY (docs/SPEC-TABLES.md
// §2.3, §11): the targets that carry it are named, and each remaining port's
// is a named follow-on — refused loudly here rather than emitted as a
// fixed-class codec that never met the presence companion beside an array.
func refuseOptionalArrays(u *ir.Unit, target string) error {
	fields := ir.TableOptionalArrays(u)
	if len(fields) == 0 {
		return nil
	}
	carry, flags := carriers(optionalArrayTargets)
	return fmt.Errorf("unit declares an optional array in a table closure (%s) — an optional array is %s only today, and the %s form is a named follow-on; generate with %s, or wrap the array in a table and make that optional (docs/SPEC-TABLES.md §2.3, §11)",
		englishList(fields), englishList(carry), target, englishList(flags))
}
