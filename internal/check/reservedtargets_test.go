// The §4.6 target-name safety rule, proven against EVERY supported language:
// a schema name is legal in every language the compiler supports, whatever
// target is generated (Glenn, 2026-10-21: "generated output never puts out
// keywords in Y"). One diagnostic per name lists every language it collides
// with, and the only exemption is the Go escape (a Go word carried by Go as a
// field, arm, variant or table name).
package check

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/scanner"
)

// isSchemaKeyword reports whether the schema grammar itself reserves w, so a
// source naming it outside an enum or flags body never parses.
func isSchemaKeyword(w string) bool {
	toks, errs := scanner.Scan("w", []byte(w))
	return len(errs) == 0 && len(toks) > 0 && toks[0].Kind.IsKeyword()
}

func reservedProbeErrors(t *testing.T, src string) ([]error, bool) {
	t.Helper()
	f, perrs := parser.Parse("t.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, false
	}
	_, errs := Unit([]SourceFile{{Path: "t.schema", Name: "t.schema", Base: "t", Bytes: []byte(src), AST: f}})
	return errs, true
}

// TestReservedWordsOfEveryLanguageWhateverTheTarget: a word reserved in one
// language only is refused by the one checker every command runs; there is
// no per-target narrowing left to let it through.
func TestReservedWordsOfEveryLanguageWhateverTheTarget(t *testing.T) {
	t.Parallel()
	src := "package t\ntype func {\n  a int32\n}\ntype select {\n  b int32\n}\ntable Words {\n  class int32\n  self int32\n  local int32\n  end int32\n}\n"
	errs, ok := reservedProbeErrors(t, src)
	if !ok {
		t.Fatal("parse failed")
	}
	for _, want := range []struct{ word, langs string }{
		{"func", "Go"},
		{"select", "Go"},
		{"class", "C#, C++, Dart, Java, JavaScript, Lobster, and Squirrel"},
		{"self", "Elixir, Goose, and Rust"},
		{"local", "Lua, Luau, Squirrel, and moo"},
		{"end", "Elixir, Lua, Luau, and moo"},
	} {
		needle := fmt.Sprintf("%q is a reserved word in %s ", want.word, want.langs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), needle) {
				found = true
			}
		}
		if !found {
			t.Errorf("want a refusal containing %q, got %v", needle, errs)
		}
	}

	// THE GO ESCAPE (SPEC §4.6): Go-only words as field names, and as a
	// table's name, are carried for Go, and no other language reserves them.
	if errs, _ := reservedProbeErrors(t, "package t\ntable func {\n  select int32\n  go int32\n}\n"); len(errs) > 0 {
		t.Errorf("func, select and go are reserved in Go alone, which carries them, so nothing refuses them, got %v", errs)
	}
}

// TestEveryReservedWordAsEveryNameKindIsRefused is the conformance sweep:
// every word of the one table, in every position a schema name takes, is
// refused with the full list of the languages that reserve it — Go left out
// only where the Go escape carries the word — and a word the schema grammar
// refuses first never parses at all.
func TestEveryReservedWordAsEveryNameKindIsRefused(t *testing.T) {
	t.Parallel()
	kinds := []struct {
		name    string
		escaped bool // the Go escape carries a Go word here
		prefix  string
		mk      func(w string) string
	}{
		{"package", false, "package name", func(w string) string { return "package " + w + "\ntype T { a int32 }\n" }},
		{"const", false, "declaration name", func(w string) string { return "package t\nconst " + w + " = 1\n" }},
		{"type", false, "declaration name", func(w string) string { return "package t\ntype " + w + " { a int32 }\n" }},
		{"enum", false, "declaration name", func(w string) string { return "package t\nenum " + w + " { a }\n" }},
		{"flags", false, "declaration name", func(w string) string { return "package t\nflags " + w + " { a }\n" }},
		{"union", false, "declaration name", func(w string) string { return "package t\ntype P { a int32 }\nunion " + w + " {\n  a P\n}\n" }},
		{"table", true, "declaration name", func(w string) string { return "package t\ntable " + w + " { a int32 }\n" }},
		{"type field", true, "field name", func(w string) string { return "package t\ntype T {\n  " + w + " int32\n}\n" }},
		{"table field", true, "field name", func(w string) string { return "package t\ntable T {\n  " + w + " int32\n}\n" }},
		{"union arm", true, "variant", func(w string) string { return "package t\ntype P { a int32 }\nunion U {\n  " + w + " P\n}\n" }},
		{"enum variant", true, "enum variant", func(w string) string { return "package t\nenum E { " + w + " }\n" }},
		{"flags variant", true, "flags variant", func(w string) string { return "package t\nflags F { " + w + " }\n" }},
	}
	table := ReservedWords()
	words := make([]string, 0, len(table))
	for w := range table {
		words = append(words, w)
	}
	sort.Strings(words)
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			probed := 0
			for _, w := range words {
				langs := table[w]
				if k.escaped {
					langs = slices.DeleteFunc(slices.Clone(langs), func(l string) bool { return l == "Go" })
				}
				errs, parsed := reservedProbeErrors(t, k.mk(w))
				if !parsed {
					if !isSchemaKeyword(w) {
						t.Errorf("%s %q: the probe does not parse, and the schema grammar does not reserve the word", k.name, w)
					}
					continue // the schema grammar refuses the word before any language sees it
				}
				probed++
				var refusal string
				for _, e := range errs {
					if strings.Contains(e.Error(), " is a reserved word in ") {
						refusal = e.Error()
					}
				}
				if len(langs) == 0 {
					if refusal != "" {
						t.Errorf("%s %q is reserved in Go alone, which carries it here, yet it is refused: %s", k.name, w, refusal)
					}
					continue
				}
				quoted := fmt.Sprintf("%q", w)
				if k.prefix == "variant" {
					quoted = w
				}
				want := fmt.Sprintf("%s %s is a reserved word in %s ", k.prefix, quoted, listReservedLangs(langs))
				if !strings.Contains(refusal, want) {
					t.Errorf("%s %q: want a refusal containing %q, got %v", k.name, w, want, errs)
				}
			}
			if probed < len(words)/2 {
				t.Errorf("%s: only %d of %d words reached the checker", k.name, probed, len(words))
			}
		})
	}
}

// TestEveryLanguageHasItsWords: each row of the one table names a language,
// the --lang spellings that generate it and a non-empty word list, and the
// fifteen supported languages are all there.
func TestEveryLanguageHasItsWords(t *testing.T) {
	t.Parallel()
	want := []string{"C", "C++", "C#", "Go", "Rust", "Java", "JavaScript", "Dart", "Elixir", "Lua", "Luau", "moo", "Squirrel", "Goose", "Lobster"}
	if got := ReservedLanguages(); !slices.Equal(got, want) {
		t.Fatalf("ReservedLanguages() = %v, want %v", got, want)
	}
	for _, row := range reservedLanguages {
		if len(row.targets) == 0 || len(ReservedIn(row.lang)) == 0 {
			t.Errorf("language %s has no --lang spelling or no words", row.lang)
		}
		for _, target := range row.targets {
			if lang, ok := ReservedLanguageOf(target); !ok || lang != row.lang {
				t.Errorf("ReservedLanguageOf(%q) = %q, %v; want %q", target, lang, ok, row.lang)
			}
		}
	}
	if _, ok := ReservedLanguageOf("cobol"); ok {
		t.Error("ReservedLanguageOf must refuse a language with no row")
	}
	defer func() {
		if recover() == nil {
			t.Error("ReservedIn must panic for a language with no row")
		}
	}()
	ReservedIn("COBOL")
}
