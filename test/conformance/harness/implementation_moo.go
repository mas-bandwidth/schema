//go:build schema_leg_moo

package main

// MOO is admitted to `implementation --lang moo` in a harness built with its
// tag (build/conformance-harness-moo), where compiler.Carried("moo") has the
// target's registrations (compiler/carries_moo.go).
func init() {
	taggedImplementationLanguages = append(taggedImplementationLanguages, "moo")
}
