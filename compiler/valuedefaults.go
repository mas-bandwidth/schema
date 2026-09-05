// The STRING, BYTES and FLAGS DEFAULT's cross-target refusal (SPEC §4.2): the
// C++ reference carries the three on both wires, and every other target names
// the follow-on rather than emitting a fresh value that is not the declared
// one. The refusal lives here, beside the other cross-target refusals, and
// [refuseUnported] reaches it for every port.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueDefaultTargets is the canonical name of every built-in target whose
// backends carry a string, bytes or flags default; refuseValueDefaults names
// them.
var valueDefaultTargets = []string{"cpp"}

// refuseValueDefaults is the named refusal every target without the form
// gives a unit whose fields carry a string, bytes or flags default (SPEC
// §4.2): a port that has not met one would initialize the field to empty or
// zero where the schema says otherwise, and on the table wire would elide the
// wrong value.
func refuseValueDefaults(u *ir.Unit, target string) error {
	names := ir.ValueDefaultFields(u)
	if len(names) == 0 {
		return nil
	}
	carry, flags := carriers(valueDefaultTargets)
	return fmt.Errorf("unit declares a string, bytes or flags default (%s): the three are %s only today, and the %s form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
		englishList(names), englishList(carry), target, englishList(flags))
}
