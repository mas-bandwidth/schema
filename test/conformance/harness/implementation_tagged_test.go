package main

import "testing"

// The tagged mechanism a `schema new-leg` leg uses, held on a made-up leg that
// is no shipped name: tagprobe is admitted only in a harness built with
// schema_leg_tagprobe (implementation_tagprobe_on_test.go), never otherwise
// (implementation_tagprobe_off_test.go); the shipped targets are admitted in
// both builds; any other name never.
func TestImplementationAdmitsTaggedLanguagesByBuild(t *testing.T) {
	for _, lang := range builtinImplementationLanguages {
		if !admittedImplementationLanguage(lang) {
			t.Errorf("%s is not admitted", lang)
		}
	}
	for _, lang := range []string{"", "zig", "Tagprobe", "tagprobe "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
	if got := admittedImplementationLanguage("tagprobe"); got != tagprobeTagged {
		t.Errorf("tagprobe admitted=%t in a build where the tagprobe tag is %t", got, tagprobeTagged)
	}
}
