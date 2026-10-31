package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHostileZeroEscapeRows pins the three `\u0000` rows of the hostile
// battery to docs/SPEC-TABLES.md §16.3's zero rule: a string the read would
// PLACE in text storage refuses the read (`text_holds_zero: <Table>.<field>`),
// while a string it only SKIPS stores nothing and is not the event. The rule is
// held first in Go and carried by C, Rust, Dart, Java, moo, Lobster, Squirrel,
// Lua, Luau, Elixir and Goose, so the two refusals state the standard's answer with
// `carried-by=go,c,rust,dart,java,moo,lobster,squirrel,lua,luau,elixir,goose` and every other port keeps
// its BASE counters; the skipped value is the clean §4 report with the unknown
// key counted.
//
// A Go answer of the clean counters on the value row must score red, because
// the exemption is a different expectation and never a skip.
func TestHostileZeroEscapeRows(t *testing.T) {
	t.Parallel()

	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}

	// the counters every non-carrying port answers: it stores the zero byte as
	// text, which is what its reader did before the ruling and still does
	const baseCounters = "0,0,0,0,0,false,read"

	refusals := []string{"str-zero-escape-value", "str-zero-escape-map-key"}
	for _, caseName := range refusals {
		row := findHostile(t, m, caseName)
		if !row.Refused || row.Verdict() != "refused" {
			t.Fatalf("%s: the standard verdict is %q; §16.3 refuses a zero placed in text", caseName, row.Verdict())
		}
		// both rows are carried by Squirrel too: the map-key row's unit,
		// tables/maps, declares wide text, which its descriptors now carry
		wantCarried := "go,c,rust,dart,java,moo,lobster,squirrel,lua,luau,elixir,goose"
		if strings.Join(row.CarriedBy, ",") != wantCarried {
			t.Fatalf("%s: carried-by=%v; want %s: Go held the rule first, C, Rust, Dart, Java, moo, Lobster, Squirrel, Lua, Luau, Elixir and Goose carry it, and every other port keeps its behaviour", caseName, row.CarriedBy, wantCarried)
		}
		if row.Uncarried != baseCounters {
			t.Fatalf("%s: uncarried=%q; every other port stores the zero and answers %q", caseName, row.Uncarried, baseCounters)
		}
		if got := row.VerdictFor("go"); got != "refused" {
			t.Fatalf("%s: Go owes %q, got %q", caseName, "refused", got)
		}
		if got := row.VerdictFor("c"); got != "refused" {
			t.Fatalf("%s: C owes %q, got %q", caseName, "refused", got)
		}
		for _, port := range []string{"rust", "lobster", "lua", "luau", "elixir"} {
			if got := row.VerdictFor(port); got != "refused" {
				t.Fatalf("%s: %s owes %q, got %q", caseName, port, "refused", got)
			}
		}
		if got := row.VerdictFor("dart"); got != "refused" {
			t.Fatalf("%s: Dart owes %q, got %q", caseName, "refused", got)
		}
		if got := row.VerdictFor("squirrel"); got != "refused" {
			t.Fatalf("%s: Squirrel owes %q, got %q", caseName, "refused", got)
		}
		if got := row.VerdictFor("cpp"); got != baseCounters {
			t.Fatalf("%s: cpp owes %q, got %q", caseName, baseCounters, got)
		}

		// the harness's own expectations, not a second copy of the rule
		byLang := map[string]expectation{}
		exemptNamed := map[string]bool{}
		for _, lang := range []string{"go", "rust", "cpp"} {
			legWant, exemptions := hostileExpectations(m, lang)
			for _, e := range legWant {
				if e.name == caseName {
					byLang[lang] = e
				}
			}
			for _, line := range exemptions {
				if strings.Contains(line, caseName) {
					exemptNamed[lang] = true
				}
			}
		}
		if exemptNamed["go"] || exemptNamed["rust"] {
			t.Fatalf("%s: Go and Rust carry the rule and must not be exempted", caseName)
		}
		if !exemptNamed["cpp"] {
			t.Fatalf("%s: cpp is exempted but the harness names no exemption on its line; it must never skip the row in silence", caseName)
		}
		if got := string(byLang["rust"].want); got != "refused\n" {
			t.Fatalf("%s: the Rust expectation is %q", caseName, got)
		}
		// the reversed witness for the Rust leg: its reader storing the zero
		// as text again answers the base counters, and that must score red
		rustDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(rustDir, byLang["rust"].name), []byte(baseCounters+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		rr := &result{}
		scoreAnswer("rust", "json-hostile", rustDir, byLang["rust"], Instance{}, rr)
		if len(rr.failures) == 0 {
			t.Fatalf("%s: Rust answering %q scored green against %q; it must score red", caseName, baseCounters, byLang["rust"].want)
		}
		if got := string(byLang["go"].want); got != "refused\n" {
			t.Fatalf("%s: the Go expectation is %q", caseName, got)
		}
		if got := string(byLang["cpp"].want); got != baseCounters+"\n" {
			t.Fatalf("%s: the cpp expectation is %q", caseName, got)
		}
	}

	// a zero the read only SKIPS is not the event: the clean report counts the
	// unknown key and no port is exempt from it
	clean := findHostile(t, m, "str-zero-escape-unknown-key")
	if clean.Refused || clean.Verdict() != "1,0,0,0,0,false,read" {
		t.Fatalf("str-zero-escape-unknown-key: the standard verdict is %q; §16.3 stores nothing for a skipped value", clean.Verdict())
	}
	if len(clean.CarriedBy) != 0 || clean.Uncarried != "" {
		t.Fatalf("str-zero-escape-unknown-key: the row is exempted (%v, %q); a skipped zero is every port's clean read", clean.CarriedBy, clean.Uncarried)
	}
	for _, lang := range []string{"go", "cpp"} {
		if got := clean.VerdictFor(lang); got != "1,0,0,0,0,false,read" {
			t.Fatalf("str-zero-escape-unknown-key: %s owes %q, got %q", lang, "1,0,0,0,0,false,read", got)
		}
	}

	// the reversed witness: Go answering the base counters on the VALUE row is
	// red against the rule's expectation
	value := findHostile(t, m, "str-zero-escape-value")
	legWant, _ := hostileExpectations(m, "go")
	var valueWant expectation
	for _, e := range legWant {
		if e.name == value.Name {
			valueWant = e
			break
		}
	}
	if valueWant.name == "" {
		t.Fatalf("%s: the harness has no Go expectation", value.Name)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, valueWant.name), []byte(baseCounters+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &result{}
	scoreAnswer("go", "json-hostile", dir, valueWant, Instance{}, r)
	if len(r.failures) == 0 {
		t.Fatalf("%s: Go answering %q scored green against %q; it must score red", value.Name, baseCounters, valueWant.want)
	}
}

func findHostile(t *testing.T, m *Manifest, name string) *Hostile {
	t.Helper()
	for i := range m.Hostiles {
		if m.Hostiles[i].Name == name {
			return &m.Hostiles[i]
		}
	}
	t.Fatalf("the manifest names no hostile row %q", name)
	return nil
}
