//go:build schema_leg_goose

package main

// Goose is admitted to `implementation --lang goose` in a harness built with
// its tag (build/conformance-harness-goose), where compiler.Carried("goose")
// has the target's registrations (compiler/target_goose.go).
func init() {
	taggedImplementationLanguages = append(taggedImplementationLanguages, "goose")
}
