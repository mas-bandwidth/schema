package main

// MOO is admitted to `implementation --lang moo` in every build of the
// harness: the moo target is built in, so compiler.Carried("moo") always has
// the target's registrations (compiler/carries_moo.go).
func init() {
	builtinImplementationLanguages = append(builtinImplementationLanguages, "moo")
}
