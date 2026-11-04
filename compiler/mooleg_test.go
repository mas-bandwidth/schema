package compiler

import (
	"slices"
	"strings"
)

// The cross-target carrier gates name the MOO leg only where the target is
// built in (`-tags schema_leg_moo`, compiler/target_moo.go). mooLegBuilt is
// that tag's answer (mooleg_on_test.go, mooleg_off_test.go).

// withMooLeg is a sorted carrier list with moo in it when the leg is built.
func withMooLeg(carriers []string) []string {
	if !mooLegBuilt {
		return carriers
	}
	return slices.Sorted(slices.Values(append(slices.Clone(carriers), "moo")))
}

// mooLegSpelled is a refusal's carrier list as it reads with the moo leg
// built: moo sorts right after luau.
func mooLegSpelled(s string) string {
	if !mooLegBuilt {
		return s
	}
	if strings.Contains(s, " luau, ") {
		return strings.Replace(s, " luau, ", " luau, moo, ", 1)
	}
	return strings.Replace(s, " luau and ", " luau, moo and ", 1)
}
