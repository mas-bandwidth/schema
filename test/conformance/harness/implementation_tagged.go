package main

import "slices"

// builtinImplementationLanguages are the eighteen shipped targets, which
// `implementation --lang` admits by name in every build of the harness: each
// one's compiler target and carries registration are built in, with no build
// tag (Glenn, 2026-11-21: "all languages, by default, all the time.";
// implementation_builtin_test.go holds this list to the compiler's targets).
// The untagged run over every language stays the original nine.
var builtinImplementationLanguages = []string{
	"c", "cpp", "cs", "dart", "elixir", "go", "goose", "java", "js",
	"lobster", "lua", "luau", "moo", "python", "ruby", "rust", "squirrel", "unity",
}

// taggedImplementationLanguages are legs still being written by
// `schema new-leg`, behind their own schema_leg_<name> tag, that
// `implementation --lang` admits by name only when this harness is built with
// that tag. No shipped target is ever listed here.
var taggedImplementationLanguages []string

// admittedImplementationLanguage is whether `implementation --lang` runs for
// lang: a shipped language, or a new leg's tagged one this build carries.
func admittedImplementationLanguage(lang string) bool {
	return slices.Contains(builtinImplementationLanguages, lang) ||
		slices.Contains(taggedImplementationLanguages, lang)
}
