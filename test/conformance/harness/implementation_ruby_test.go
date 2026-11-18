package main

import "testing"

func TestImplementationAdmitsRubyByBuild(t *testing.T) {
	t.Parallel()
	if got := admittedImplementationLanguage("ruby"); got != rubyTagged {
		t.Errorf("ruby admitted=%t in a build where the ruby tag is %t", got, rubyTagged)
	}
	for _, lang := range []string{"Ruby", "rb", "ruby3", "ruby "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
