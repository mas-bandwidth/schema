//go:build schema_leg_goose

package main

// Goose is admitted to `implementation --lang goose` in every build; a harness
// built with its tag (build/conformance-harness-goose) is the one where
// compiler.Carried("goose") has the target's registrations
// (compiler/target_goose.go), and any other build reports them missing.
func init() {
	taggedImplementationLanguages = append(taggedImplementationLanguages, "goose")
}
