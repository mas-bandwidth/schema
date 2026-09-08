// The `was` ROWS' cross-target refusal (docs/SPEC-TABLES.md §5): a `was` on an
// enum variant, on a union arm, or on a field of a `type` a table reaches is
// carried by C++, C# and the tool. Targets without the form name the follow-on
// rather than hashing the declared name where the wire carries the alias.
// A `was` on a TABLE's own field is every port's already, and a `was` on a
// table declaration names a node type id the ports' fixed class never writes.
package compiler

import (
	"fmt"
	"slices"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// wasRowTargets is the canonical name of every built-in target whose backends
// carry the three; refuseWasRows names them.
var wasRowTargets = []string{"cpp", "cs"}

// refuseWasRows is the named refusal every target without the form gives a
// unit whose table closure carries a variant, arm or type-field `was`.
func refuseWasRows(u *ir.Unit, target string) error {
	if slices.Contains(wasRowTargets, target) {
		return nil
	}
	names := ir.WasRows(u)
	if len(names) == 0 {
		return nil
	}
	carry, flags := carriers(wasRowTargets)
	return fmt.Errorf("unit declares was on an enum variant, a union arm or a type's field (%s): the three are %s only today, and the %s form is a named follow-on; generate with %s (docs/SPEC-TABLES.md §5)",
		englishList(names), englishList(carry), target, englishList(flags))
}
