package main

import "testing"

// The original nine are admitted in every build; goose only in a harness
// built with schema_leg_goose; any other name never.
func TestImplementationAdmitsTaggedLanguagesByBuild(t *testing.T) {
	for _, lang := range originalNine {
		if !admittedImplementationLanguage(lang) {
			t.Errorf("%s is not admitted", lang)
		}
	}
	for _, lang := range []string{"lua", "", "Goose"} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
	if got := admittedImplementationLanguage("goose"); got != gooseTagged {
		t.Errorf("goose admitted=%t in a build where the goose tag is %t", got, gooseTagged)
	}
}
