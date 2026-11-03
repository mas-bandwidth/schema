package compiler

import "slices"

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
