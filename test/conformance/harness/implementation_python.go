//go:build schema_leg_python

package main

// Python is admitted to `implementation --lang python` in a harness built with
// its tag (build/conformance-harness-python, make/python-ci-release.mk), as
// Goose and MOO are with theirs; the audit reads compiler.Carried("python")
// (compiler/carries_python*.go).
func init() {
	taggedImplementationLanguages = append(taggedImplementationLanguages, "python")
}

// pythonTagged is whether this build carries the tag; its untagged twin is
// implementation_python_off.go.
const pythonTagged = true
