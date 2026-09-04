// The PAYLOAD-FREE ARM's PACKET-WIRE cross-target refusal (SPEC §4.8,
// docs/SPEC-TABLES.md §2.6, §11): its own file, per the registry split — a
// construct's carrier registry and its refusal add a file beside builtin.go
// rather than growing it. A target that carries the form registers through
// [registerPacketVoidArmCarrier] from its own file's init; every other
// target's Generate calls [refusePacketVoidArms].
//
// The TABLE side of a payload-free arm is [refuseVoidArms]'s, and the two are
// separate because the wires are: a union a table closure holds has no packet
// wire at all, so a unit can carry the arm on one wire and never reach the
// other.
package compiler

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// packetVoidArmTargets is the canonical name of every built-in target whose
// PACKET backend carries a union with a payload-free arm (SPEC §4.8);
// refusePacketVoidArms names them.
var packetVoidArmTargets []string

// registerPacketVoidArmCarrier is what a carrying target's file calls from
// its init, beside its registerBuiltin call.
func registerPacketVoidArmCarrier(name string) {
	packetVoidArmTargets = append(packetVoidArmTargets, name)
}

// refusePacketVoidArms is the named refusal every target without the form
// gives a unit whose PACKET union holds a payload-free arm (SPEC §4.8): such
// an arm has no storage and rides as the tag alone, and a backend that has
// not met one would emit a member for an arm that has none.
func refusePacketVoidArms(u *ir.Unit, target string) error {
	// Unions is the PACKET unions alone: a union a table closure holds lives
	// in TableUnions and never reaches a packet backend
	var names []string
	for name, un := range u.Unions {
		for _, v := range un.Variants {
			if v.Void() {
				names = append(names, name)
				break
			}
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}
	carry, flags := carriers(packetVoidArmTargets)
	return fmt.Errorf("unit declares a union with a payload-free arm (%s) — the packet wire's payload-free arm is %s only today, and the %s form is a named follow-on; generate with %s, or give the arm a payload (SPEC §4.8, docs/SPEC-TABLES.md §11)",
		englishList(names), englishList(carry), target, englishList(flags))
}
