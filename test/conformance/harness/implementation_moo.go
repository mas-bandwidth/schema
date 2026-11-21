//go:build schema_leg_moo

package main

// MOO is admitted to `implementation --lang moo` in every build; a harness
// built with its tag (build/conformance-harness-moo) is the one where
// compiler.Carried("moo") has the target's registrations
// (compiler/carries_moo.go), and any other build reports them missing.
func init() {
	taggedImplementationLanguages = append(taggedImplementationLanguages, "moo")
}
