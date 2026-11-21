package main

import (
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// moo is admitted in every build of the harness, with its carries
// registration present, and no other spelling of it is admitted.
func TestImplementationAdmitsMooByBuild(t *testing.T) {
	if !admittedImplementationLanguage("moo") {
		t.Error("moo is not admitted in the default build of the harness")
	}
	if slices.Contains(taggedImplementationLanguages, "moo") {
		t.Error("moo is listed as a tagged language; it is built in")
	}
	if _, ok := compiler.Carried("moo"); !ok {
		t.Error("compiler.Carried(\"moo\") has no registration in the default build")
	}
	for _, lang := range []string{"MOO", "Moo", "moo "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
