package main

import "testing"

// python is admitted only in a harness built with schema_leg_python, and its
// tag admits no other spelling of it.
func TestImplementationAdmitsPythonByBuild(t *testing.T) {
	t.Parallel()
	if got := admittedImplementationLanguage("python"); got != pythonTagged {
		t.Errorf("python admitted=%t in a build where the python tag is %t", got, pythonTagged)
	}
	for _, lang := range []string{"Python", "python3", "py", "python "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
