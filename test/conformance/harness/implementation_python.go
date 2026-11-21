//go:build schema_leg_python

package main

// Python is admitted to `implementation --lang python` in every build; a
// harness built with its tag (build/conformance-harness-python,
// make/python-ci-release.mk) is the one where compiler.Carried("python") has
// the target's registrations (compiler/carries_python*.go), and any other
// build reports them missing.
func init() {
	taggedImplementationLanguages = append(taggedImplementationLanguages, "python")
}

// pythonTagged is whether this build carries the tag; its untagged twin is
// implementation_python_off.go.
const pythonTagged = true
