package main

import "slices"

// taggedImplementationLanguages are the targets behind a build tag that this
// harness carries when built with their tag (implementation_goose.go). Every
// target is admitted by name in every build; a target whose registration this
// build lacks is reported as missing, never skipped.
var taggedImplementationLanguages []string

// admittedImplementationLanguage is whether `implementation --lang` runs for
// lang: one of the 18 targets, or a tagged one this build carries.
func admittedImplementationLanguage(lang string) bool {
	return slices.Contains(everyTarget, lang) || slices.Contains(taggedImplementationLanguages, lang)
}
