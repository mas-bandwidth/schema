package main

import "testing"

// moo is admitted in every build (a build without schema_leg_moo reports its
// registration missing), and no other spelling of it is admitted.
func TestImplementationAdmitsMooByBuild(t *testing.T) {
	if !admittedImplementationLanguage("moo") {
		t.Errorf("moo is not admitted in a build where the moo tag is %t", mooTagged)
	}
	for _, lang := range []string{"MOO", "Moo", "moo "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
