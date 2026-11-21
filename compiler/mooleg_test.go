package compiler

import (
	"slices"
	"strings"
)

// The cross-target carrier gates name the MOO leg: moo is a built-in target
// of the default build (compiler/target_moo.go), so every carrier list and
// refusal spelling holds it.

// withMooLeg is a sorted carrier list with moo in it.
func withMooLeg(carriers []string) []string {
	return slices.Sorted(slices.Values(append(slices.Clone(carriers), "moo")))
}

// mooLegSpelled is a refusal's carrier list as it reads with the moo leg
// built in: moo sorts right after luau.
func mooLegSpelled(s string) string {
	if strings.Contains(s, " luau, ") {
		return strings.Replace(s, " luau, ", " luau, moo, ", 1)
	}
	return strings.Replace(s, " luau and ", " luau, moo and ", 1)
}
