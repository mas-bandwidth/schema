package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHostileIllFormedRowExemptsUnportedLegs pins the ill-formed-UTF-8 hostile
// row to docs/SPEC-TABLES.md §16.3's hold: the rule is held first in Go, so Go
// owes `refused` (as do C and Rust, which carry it), and C++ still owes the counters its port answers until it
// takes the rule. A C++ answer of `refused` and a Go answer of those counters
// each score red, because the exemption is a different expectation and never a
// skip.
func TestHostileIllFormedRowExemptsUnportedLegs(t *testing.T) {
	t.Parallel()

	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}

	const caseName = "str-illformed-at-bound"
	// the counters the row carried before the ruling: clamped, read
	const oldCounters = "0,0,0,1,0,false,read"

	var row *Hostile
	for i := range m.Hostiles {
		if m.Hostiles[i].Name == caseName {
			row = &m.Hostiles[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("the manifest names no hostile row %q", caseName)
	}

	want := map[string]string{
		"go":   "refused",
		"c":    "refused",
		"rust": "refused",
		"cpp":  oldCounters,
	}
	for lang, verdict := range want {
		if got := row.VerdictFor(lang); got != verdict {
			t.Fatalf("%s: %s expects %q, got %q", caseName, lang, verdict, got)
		}
	}

	// the harness's own expectations, not a second copy of the rule
	byLang := map[string]expectation{}
	exemptNamed := map[string]bool{}
	for lang := range want {
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
	if exemptNamed["go"] || exemptNamed["c"] || exemptNamed["rust"] {
		t.Fatalf("%s: Go, C and Rust carry the rule and must not be exempted", caseName)
	}
	if !exemptNamed["cpp"] {
		t.Fatalf("%s: C++ is exempted but the harness names no exemption on its line; it must never skip the row in silence", caseName)
	}

	dir := t.TempDir()

	// the reversed witness: each leg answering the OTHER leg's verdict is red
	reversed := map[string]string{
		"go":   oldCounters,
		"rust": oldCounters,
		"cpp":  "refused",
	}
	for lang, answer := range reversed {
		e, ok := byLang[lang]
		if !ok {
			t.Fatalf("%s: the harness has no expectation for %s", caseName, lang)
		}
		out := filepath.Join(dir, lang)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, e.name), []byte(answer+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{}
		scoreAnswer(lang, "json-hostile", out, e, Instance{}, r)
		if len(r.failures) == 0 {
			t.Fatalf("%s: %s answering %q scored green against %q; it must score red", caseName, lang, answer, e.want)
		}
	}

	// and each leg's own answer is green
	for lang, answer := range want {
		e, ok := byLang[lang]
		if !ok {
			t.Fatalf("%s: the harness has no expectation for %s", caseName, lang)
		}
		out := filepath.Join(dir, lang+"-right")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, e.name), []byte(answer+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{}
		scoreAnswer(lang, "json-hostile", out, e, Instance{}, r)
		if len(r.failures) != 0 {
			t.Fatalf("%s: %s answering %q scored red against %q: %v", caseName, lang, answer, e.want, r.failures)
		}
	}
}
