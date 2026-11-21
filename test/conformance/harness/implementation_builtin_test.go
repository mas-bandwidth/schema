package main

import (
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// Every shipped target is admitted to `implementation --lang` in the default
// build of the harness, with its carries registration present; the admitted
// list is exactly the compiler's registered targets; no shipped target is
// listed as tagged; and no other spelling of one is admitted.
func TestImplementationAdmitsEveryShippedTarget(t *testing.T) {
	t.Parallel()
	targets := slices.Sorted(slices.Values(compiler.New().Targets()))
	if !slices.Equal(slices.Sorted(slices.Values(builtinImplementationLanguages)), targets) {
		t.Errorf("the harness admits %v, the compiler's default build registers %v", builtinImplementationLanguages, targets)
	}
	if !slices.Equal(slices.Sorted(slices.Values(everyTarget)), targets) {
		t.Errorf("everyTarget is %v, the compiler's default build registers %v", everyTarget, targets)
	}
	if len(builtinImplementationLanguages) != 18 {
		t.Errorf("the harness admits %d shipped languages, want 18", len(builtinImplementationLanguages))
	}
	for _, lang := range builtinImplementationLanguages {
		if !admittedImplementationLanguage(lang) {
			t.Errorf("%s is not admitted in the default build of the harness", lang)
		}
		if slices.Contains(taggedImplementationLanguages, lang) {
			t.Errorf("%s is listed as a tagged language; it is built in", lang)
		}
		if _, ok := compiler.Carried(lang); !ok {
			t.Errorf("compiler.Carried(%q) has no registration in the default build", lang)
		}
	}
	for _, lang := range []string{
		"", "MOO", "Moo", "moo ", "Goose", "Python", "python3", "py", "python ",
		"Ruby", "rb", "ruby3", "ruby ", "Lua", "csharp", "javascript",
	} {
		if admittedImplementationLanguage(lang) {
			t.Errorf("%q is admitted", lang)
		}
	}
}
