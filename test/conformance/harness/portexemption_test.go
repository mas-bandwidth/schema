package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every leg owes refusal for ill-formed UTF-8 (SPEC-TABLES §16.3), including
// a newly added leg. The opposite verdict must score red without exemptions.
func TestHostileIllFormedRowRequiresEveryLegToRefuse(t *testing.T) {
	t.Parallel()

	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}

	const caseName = "str-illformed-at-bound"

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
		"go":       "refused",
		"c":        "refused",
		"rust":     "refused",
		"cpp":      "refused",
		"unported": "refused",
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
	for lang := range want {
		if exemptNamed[lang] {
			t.Errorf("%s: %s must refuse without an exemption", caseName, lang)
		}
	}

	dir := t.TempDir()

	// A read verdict and value are wrong for every leg, even a new one.
	for lang := range want {
		answer := "read\n{}"
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
