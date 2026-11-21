package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHostileIllFormedRowHasNoPortExemption pins the ill-formed-UTF-8 hostile
// row to docs/SPEC-TABLES.md §16.3's rule. Under layer 3's one read every port,
// including a synthetic unported leg, owes the same verdict: the old
// `uncarried` counters are retired, not re-pinned, and no leg may be excused
// from the row. Answering the retired counters scores red.
func TestHostileIllFormedRowHasNoPortExemption(t *testing.T) {
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

	langs := []string{"go", "c", "rust", "cpp", "unported"}
	for _, lang := range langs {
		if got := row.VerdictFor(lang); got != "refused" {
			t.Fatalf("%s: %s expects refused, got %q", caseName, lang, got)
		}
	}

	// the harness's own expectations, not a second copy of the rule
	byLang := map[string]expectation{}
	for _, lang := range langs {
		legWant, exemptions := hostileExpectations(m, lang)
		for _, e := range legWant {
			if e.name == caseName {
				byLang[lang] = e
			}
		}
		for _, line := range exemptions {
			if line != "" {
				t.Fatalf("%s: %s still carries a retired exemption: %s", caseName, lang, line)
			}
		}
		if got := string(byLang[lang].want); got != "refused\n" {
			t.Fatalf("%s: %s owes %q, got %q", caseName, lang, "refused\n", got)
		}
	}

	dir := t.TempDir()

	// the retired counters on any leg score red
	for _, lang := range langs {
		e, ok := byLang[lang]
		if !ok {
			t.Fatalf("%s: the harness has no expectation for %s", caseName, lang)
		}
		out := filepath.Join(dir, lang+"-old")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, e.name), []byte(oldCounters+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{}
		scoreAnswer(lang, "json-hostile", out, e, Instance{}, r)
		if len(r.failures) == 0 {
			t.Fatalf("%s: %s answering the retired counters scored green against %q; it must score red", caseName, lang, e.want)
		}
	}

	// and each leg's own answer is green
	for _, lang := range langs {
		e := byLang[lang]
		out := filepath.Join(dir, lang+"-right")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, e.name), []byte("refused\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{}
		scoreAnswer(lang, "json-hostile", out, e, Instance{}, r)
		if len(r.failures) != 0 {
			t.Fatalf("%s: %s answering refused scored red against %q: %v", caseName, lang, e.want, r.failures)
		}
	}
}
