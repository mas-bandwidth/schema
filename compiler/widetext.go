// wstring is retired (Glenn, 2026-10-31): kind 33 is not a construct a unit
// may declare. [refuseWideText] is the named refusal. Registration remains
// so a target file's init still links; it no longer grants acceptance.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// registerWideTextCarrier remains so each target file's init still links.
// Kind 33 is retired; the call grants nothing.
func registerWideTextCarrier(string) {}

// wstringRetired is the name the compiler gives the refusal. Tests match it.
const wstringRetired = "wstring retired"

// refuseWideText refuses every wstring. Kind 33 is retired (Glenn, 2026-10-31).
// Text is string(N), UTF-8. The target is named so a caller can see which
// generator stopped, not because one of them still carries the construct.
func refuseWideText(u *ir.Unit, target string) error {
	fields := ir.WideTextFields(u)
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf("%s: kind 33 is retired (Glenn, 2026-10-31); %s declares %s — use string(N), UTF-8 (SPEC §4.7)",
		wstringRetired, target, englishList(fields))
}
