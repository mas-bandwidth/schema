package main

import "testing"

// python is admitted in every build (a build without schema_leg_python reports
// its registration missing), and no other spelling of it is admitted.
func TestImplementationAdmitsPythonByBuild(t *testing.T) {
	t.Parallel()
	if !admittedImplementationLanguage("python") {
		t.Errorf("python is not admitted in a build where the python tag is %t", pythonTagged)
	}
	for _, lang := range []string{"Python", "python3", "py", "python "} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
