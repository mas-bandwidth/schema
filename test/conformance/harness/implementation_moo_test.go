package main

import "testing"

// moo is admitted only in a harness built with schema_leg_moo, and its tag
// admits no other spelling of it.
func TestImplementationAdmitsMooByBuild(t *testing.T) {
	if got := admittedImplementationLanguage("moo"); got != mooTagged {
		t.Errorf("moo admitted=%t in a build where the moo tag is %t", got, mooTagged)
	}
	for _, lang := range []string{"MOO", "Moo", "moo "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
