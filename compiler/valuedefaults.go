// The STRING, BYTES and FLAGS DEFAULT's cross-target refusal (SPEC §4.2): the
// packet and table support are separate because a packet default initializes
// storage, while a table default also governs elision and absent-field reads.
// [refuseUnported] reaches the refusal for every port.
package compiler

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueDefaultTargets is the canonical name of every built-in target whose
// backends carry a string, bytes or flags default on both wires.
//
// A PORT EARNS THIS LIST BY ANSWERING "ABSENT FIELD" WITH THE DECLARED VALUE
// on every table form it emits, and by writing the same value where its own
// form has a template. It is not a claim about the value surface: a port whose
// packet codec already spells the default (packetValueDefaultTargets) still
// initializes a fresh value correctly and can still be missing the table half.
var valueDefaultTargets = []string{"cpp", "c", "cs"}

// registerTableValueDefaultCarrier registers a port on the TABLE half of the
// list above. A port calls it from its own target file's init, beside the
// packet call, so a target stays one file (docs/CONTRIBUTING.md, "Adding a
// language").
func registerTableValueDefaultCarrier(name string) {
	valueDefaultTargets = append(valueDefaultTargets, name)
}

// packetValueDefaultTargets names the packet carriers independently of the
// table carriers. A port registers here from its own target file's init.
var packetValueDefaultTargets = []string{"cpp"}

func registerPacketValueDefaultCarrier(name string) {
	packetValueDefaultTargets = append(packetValueDefaultTargets, name)
}

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
	// A plain type can also supply table storage. Follow the same closure the
	// table emitter uses, including arrays, union arms, pointers and map entries.
	closure := ir.TableClosure(u)
	var tableNames []string
	for _, name := range names {
		owner, _, _ := strings.Cut(name, ".") // ValueDefaultFields returns Decl.field
		if closure[owner] {
			tableNames = append(tableNames, name)
		}
	}
	if len(tableNames) > 0 && !slices.Contains(valueDefaultTargets, target) {
		carry, flags := carriers(valueDefaultTargets)
		return fmt.Errorf("unit puts a string, bytes or flags default in a table closure (%s): table-wire defaults are %s only today, and the %s form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
			englishList(tableNames), englishList(carry), target, englishList(flags))
	}
	if slices.Contains(packetValueDefaultTargets, target) {
		return nil
	}
	carry, flags := carriers(packetValueDefaultTargets)
	return fmt.Errorf("unit declares a string, bytes or flags default (%s): packet-wire defaults are %s only today, and the %s form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
		englishList(names), englishList(carry), target, englishList(flags))
}
