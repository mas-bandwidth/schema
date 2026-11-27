// Package languages is the one place the supported-language list is spelled
// for a reader. It is derived, never typed: the generators this build
// registers (compiler.New().Targets(), the same list `--lang` accepts) and the
// one reserved-word table (internal/check, SPEC §4.6), whose rows name every
// language, including one whose generator sits behind a build tag. The AGENTS.md
// map prints Summary, and languages_test.go holds the hand-written pages that
// state the list (README.md, llms.txt, docs/SPEC.md §1, docs/CONTRIBUTING.md,
// docs/FAQ.md) to it, so the list cannot drift again.
//
// Summary reads the same whether or not a generator is behind a build tag: a
// tag decides what one build accepts, not what the language list is, so a
// page that states the list needs no edit when a tag is dropped. A sentence
// about the default build refusing a tagged --lang is that page's own.
package languages

import (
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/check"
)

// Language is one generator as a reader names it.
type Language struct {
	Target string // the canonical --lang spelling
	Name   string // the prose name
	Lang   string // the language it generates (the checker's display name)
	Tag    string // the build tag that registers it; "" when this build does
}

// generatorNames are the names prose uses for a generator where its
// language's name is not enough. The two C# generators share the C# reserved
// words but are two generators of one language, named "C# (modern)" and
// "C# (unity)"; Unity is never listed as a language (`--lang unity` stays the
// flag).
var generatorNames = map[string]string{
	"cs":    "C# (modern)",
	"unity": "C# (unity)",
}

// languageNames are the prose names of languages whose checker display name
// (the spelling diagnostics print) is not the prose one. Moo is capitalised in
// prose; `--lang moo`, `schema_leg_moo` and file names keep their spelling.
var languageNames = map[string]string{
	"moo": "Moo",
}

// LanguageName is the prose name of a language, given the checker's display
// name (check.ReservedLanguages).
func LanguageName(lang string) string {
	if n, ok := languageNames[lang]; ok {
		return n
	}
	return lang
}

// TagPrefix starts the build tag that registers a generator left out of the
// default build: schema_leg_<target> (compiler/target_<target>.go).
const TagPrefix = "schema_leg_"

func entry(target, tag string) Language {
	lang, ok := check.ReservedLanguageOf(target)
	if !ok {
		// compiler.registerBuiltin refuses a target without a row, so this
		// is a defect in that guard, not a caller's mistake.
		panic("languages: target " + target + " has no row in internal/check's reserved-word table")
	}
	name := LanguageName(lang)
	if n, ok := generatorNames[target]; ok {
		name = n
	}
	return Language{Target: target, Name: name, Lang: lang, Tag: tag}
}

// All returns every generator: those this build registers, and each language
// of the reserved-word table that none of them generates, with the tag that
// registers it, in prose order.
func All() []Language {
	targets := compiler.New().Targets()
	var out []Language
	for _, t := range targets {
		out = append(out, entry(t, ""))
	}
	for _, lang := range check.ReservedLanguages() {
		spellings := check.ReservedTargetsOf(lang)
		if slices.ContainsFunc(spellings, func(s string) bool { return slices.Contains(targets, s) }) {
			continue
		}
		out = append(out, entry(spellings[0], TagPrefix+spellings[0]))
	}
	sortByName(out)
	return out
}

// sortByName puts the entries in prose order: alphabetical, with "C++" before
// the two C# generators, as the list is read aloud.
func sortByName(l []Language) {
	key := func(s string) string { return strings.ReplaceAll(s, "#", "~") }
	sort.Slice(l, func(i, j int) bool { return key(l[i].Name) < key(l[j].Name) })
}

// Summary is the list as prose: "C, C++, C# (modern), ... and Squirrel". It
// prints no count: the entries are targets, and two of them are one language.
func Summary() string { return summary(All()) }

func summary(entries []Language) string {
	names := make([]string, 0, len(entries))
	for _, l := range entries {
		names = append(names, l.Name)
	}
	return englishList(names)
}

// Targets is the number of generators, tagged ones included.
func Targets() int { return len(All()) }

// Languages is the number of distinct languages they generate: the two C#
// generators count once.
func Languages() int { return languages(All()) }

func languages(entries []Language) int {
	seen := map[string]bool{}
	for _, l := range entries {
		seen[l.Lang] = true
	}
	return len(seen)
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
