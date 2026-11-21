package main

import "slices"

// taggedImplementationLanguages are the targets behind a build tag that
// `implementation --lang` admits by name when this harness is built with
// their tag (implementation_goose.go). The untagged run over every language
// stays the original nine.
var taggedImplementationLanguages []string

// builtinImplementationLanguages are the targets past the original nine that
// `implementation --lang` admits by name in every build, because their
// compiler target and carries registration are built in
// (implementation_moo.go).
var builtinImplementationLanguages []string

// admittedImplementationLanguage is whether `implementation --lang` runs for
// lang: an original-nine language, a built-in one, or a tagged one this build
// carries.
func admittedImplementationLanguage(lang string) bool {
	return slices.Contains(originalNine, lang) ||
		slices.Contains(builtinImplementationLanguages, lang) ||
		slices.Contains(taggedImplementationLanguages, lang)
}
