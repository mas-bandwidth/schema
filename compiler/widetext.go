// The WIDE TEXT cross-target refusal (SPEC §4.12, §4.11): its own file, per
// the registry split — a construct's refusal adds a file beside builtin.go
// rather than growing it. A target that carries `wstring(N)` registers
// through [registerWideTextCarrier] from its own file's init; every other
// target's Generate calls [refuseWideText] and refuses a unit that declares
// one by name.
package compiler

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideTextTargets is the canonical name of every registered target that
// carries `wstring(N)` on the packet wire.
var wideTextTargets []string

// registerWideTextCarrier is what a carrying target's file calls from its
// init, beside its registerBuiltin call.
func registerWideTextCarrier(name string) {
	wideTextTargets = append(wideTextTargets, name)
	sort.Strings(wideTextTargets)
}

// refuseWideText is the named refusal every target without the construct
// gives a unit that declares a `wstring(N)` field (SPEC §4.12).
//
// Wide text is storage plus a codec, not a spelling of `string(N)`: the
// storage is a buffer of UTF-16 CODE UNITS with a used length beside it, the
// wire is a length and one 32-bit group per unit with NO ALIGNMENT anywhere,
// and the reader owes five refusals over content the narrow type has never
// met. A backend that has not been taught any of that would emit a member
// with no type and a wire that skips the field, which is a silently truncated
// packet — so every target that has not landed the codec refuses here, by
// name, and the ones that have are named as the way through.
func refuseWideText(u *ir.Unit, target string) error {
	fields := ir.WideTextFields(u)
	if len(fields) == 0 {
		return nil
	}
	carry, flags := carriers(wideTextTargets)
	if len(carry) == 0 {
		return fmt.Errorf("unit declares a wstring(N) field (%s) — no code generator carries wide text yet, %s included; declare the field string(N), whose payload is UTF-8 at a byte bound (SPEC §4.7, §4.12)",
			englishList(fields), target)
	}
	return fmt.Errorf("unit declares a wstring(N) field (%s) — wide text is %s only today, and the %s codec is a named follow-on; generate with %s, or declare the field string(N), whose payload is UTF-8 at a byte bound (SPEC §4.7, §4.12)",
		englishList(fields), englishList(carry), target, englishList(flags))
}
