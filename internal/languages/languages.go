// Package languages is the one place the supported-language list is spelled
// for a reader. It is derived, never typed: the generators this build
// registers (compiler.New().Targets(), the same list `--lang` accepts) and the
// one reserved-word table (internal/check, SPEC §4.6), whose rows name every
// language, including one whose generator sits behind a build tag. The AGENTS.md
// map prints Summary, and languages_test.go holds the hand-written pages that
// state the list (README.md, docs/SPEC.md §1, docs/CONTRIBUTING.md,
// docs/FAQ.md) to it, so the list cannot drift again.
package languages

import (
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
)

// Language is one generator as a reader names it.
type Language struct {
	Target string // the canonical --lang spelling
	Name   string // the prose name
	Tag    string // the build tag that registers it; "" when this build does
}

// proseNames are the names prose uses where the checker's display name is not
// one. The two C# generators share the C# reserved words but are two
// generators, named "C# (modern)" and "C# (unity)"; Unity is never listed as
// a language (`--lang unity` stays the flag).
var proseNames = map[string]string{
	"cs":    "C# (modern)",
	"unity": "C# (unity)",
}

// TagPrefix starts the build tag that registers a generator left out of the
// default build: schema_leg_<target> (compiler/target_<target>.go).
const TagPrefix = "schema_leg_"

func name(target string) string {
	if n, ok := proseNames[target]; ok {
		return n
	}
	n, ok := check.ReservedLanguageOf(target)
	if !ok {
		// compiler.registerBuiltin refuses a target without a row, so this
		// is a defect in that guard, not a caller's mistake.
		panic("languages: target " + target + " has no row in internal/check's reserved-word table")
	}
	return n
}

// All returns every language: the generators this build registers, then
// each language of the reserved-word table that none of them generates, with
// the tag that registers it. Each part is sorted by prose name.
func All() []Language {
	targets := compiler.New().Targets()
	var built, tagged []Language
	for _, t := range targets {
		built = append(built, Language{Target: t, Name: name(t)})
	}
	for _, lang := range check.ReservedLanguages() {
		spellings := check.ReservedTargetsOf(lang)
		if slices.ContainsFunc(spellings, func(s string) bool { return slices.Contains(targets, s) }) {
			continue
		}
		t := spellings[0]
		tagged = append(tagged, Language{Target: t, Name: name(t), Tag: TagPrefix + t})
	}
	byName := func(l []Language) {
		sort.Slice(l, func(i, j int) bool { return l[i].Name < l[j].Name })
	}
	byName(built)
	byName(tagged)
	return append(built, tagged...)
}

// Summary is the list as prose: "C, C++ and Go", then each language a build
// tag registers, named with its tag.
func Summary() string {
	var built, tagged []string
	for _, l := range All() {
		if l.Tag == "" {
			built = append(built, l.Name)
		} else {
			tagged = append(tagged, l.Name+" from a build with `-tags "+l.Tag+"`")
		}
	}
	s := englishList(built)
	if len(tagged) > 0 {
		s += ", and " + englishList(tagged)
	}
	return s
}

func englishList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
