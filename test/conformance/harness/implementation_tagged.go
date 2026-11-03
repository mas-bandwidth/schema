package main

import "slices"

// taggedImplementationLanguages are the targets behind a build tag that
// `implementation --lang` admits by name when this harness is built with
// their tag (implementation_goose.go). The untagged run over every language
// stays the original nine.
var taggedImplementationLanguages []string

// admittedImplementationLanguage is whether `implementation --lang` runs for
// lang: an original-nine language, or a tagged one this build carries.
func admittedImplementationLanguage(lang string) bool {
	return slices.Contains(originalNine, lang) || slices.Contains(taggedImplementationLanguages, lang)
}
