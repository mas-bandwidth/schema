package main

import "testing"

// All 18 targets are admitted in every build, tagged or not: a build without a
// target's tag reports its registration missing rather than leaving it out.
// Any other name is never admitted.
func TestImplementationAdmitsEveryTargetInEveryBuild(t *testing.T) {
	if len(everyTarget) != 18 {
		t.Fatalf("everyTarget has %d names, want the 18 targets: %v", len(everyTarget), everyTarget)
	}
	for _, lang := range everyTarget {
		if !admittedImplementationLanguage(lang) {
			t.Errorf("%s is not admitted", lang)
		}
	}
	for _, lang := range []string{"", "Goose", "Lua", "csharp", "unity3d"} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
	if !admittedImplementationLanguage("goose") {
		t.Errorf("goose is not admitted in a build where the goose tag is %t", gooseTagged)
	}
}
