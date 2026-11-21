package main

import "testing"

// ruby is admitted in every build (a build without its tag reports its
// registration missing), and no other spelling of it is admitted.
func TestImplementationAdmitsRubyByBuild(t *testing.T) {
	t.Parallel()
	if !admittedImplementationLanguage("ruby") {
		t.Errorf("ruby is not admitted in a build where the ruby tag is %t", rubyTagged)
	}
	for _, lang := range []string{"Ruby", "rb", "ruby3", "ruby "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
