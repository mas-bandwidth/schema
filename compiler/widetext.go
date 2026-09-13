// The WIDE TEXT cross-target refusal (SPEC §4.12, §4.11): its own file, per
// the registry split — a construct's refusal adds a file beside builtin.go
// rather than growing it. A target that carries `wstring(N)` registers
// through [registerWideTextCarrier] from its own file's init; every other
// target's Generate calls [refuseWideText] and refuses a unit that declares
// one by name.
package compiler

import (
	"fmt"
	"slices"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideTextTargets is the canonical name of every registered target that
// carries `wstring(N)` on the packet wire.
var wideTextTargets []string

// tableWideTextTargets is the set that carries `wstring(N)` in a TABLE
// closure, which is a separate landing from the packet one and lands in its
// own pull request. tableWideTextNames is the same set as the refusal spells
// it, so the message and the check can never disagree.
var tableWideTextTargets = map[string]bool{
	"cpp": true, "c": true, "cs": true, "go": true, "dart": true,
}

var tableWideTextNames = []string{"C", "C++", "C#", "Dart", "Go"}

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
	// Packet storage does not imply support for table kind 33. A table
	// refusal must recommend a table carrier, never just a packet carrier.
	//
	// DART CARRIES IT ON FORM 3 AND ON NO OTHER FORM. Its table surface is
	// §3.4's fixed form, which lays wide text out as a length in CODE UNITS
	// and 2N bytes behind it, so the construct is met. Its two accelerators
	// are not taught it, and internal/codegen/darttable scopes the refusal to
	// them by name — the same way it already scopes the fixed-point and
	// 128-bit kinds — rather than refusing a unit the form carries fine.
	tableFields := ir.TableWideTextFields(u)
	if len(tableFields) > 0 && !tableWideTextTargets[target] {
		return fmt.Errorf("unit puts a wstring(N) field in a table closure (%s): table wide text is %s only today, and the %s table codec is a named follow-on; generate with --lang cpp (SPEC §4.12)", englishList(tableFields), englishList(tableWideTextNames), target)
	}
	if slices.Contains(wideTextTargets, target) {
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
