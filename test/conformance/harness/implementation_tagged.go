package main

import "slices"

// `implementation --lang` admits every one of the eighteen shipped targets
// (everyTarget, coverage_complete.go) by name in every build of the harness:
// each one's compiler target and carries registration are built in, with no
// build tag (2026-11-21, docs/rulings.json every-target-default-build;
// implementation_builtin_test.go holds everyTarget to the compiler's targets).

// taggedImplementationLanguages are legs still being written by
// `schema new-leg`, behind their own schema_leg_<name> tag, that
// `implementation --lang` admits by name only when this harness is built with
// that tag. No shipped target is ever listed here.
var taggedImplementationLanguages []string

// admittedImplementationLanguage is whether `implementation --lang` runs for
// lang: a shipped target, or a new leg's tagged one this build carries.
func admittedImplementationLanguage(lang string) bool {
	return slices.Contains(everyTarget, lang) ||
		slices.Contains(taggedImplementationLanguages, lang)
}
