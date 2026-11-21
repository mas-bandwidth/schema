// THE DECLARED ABSENCES (declared.go): a port's ABSENT answer fails unless its
// committed capability list declares it. These tests drive the rule directly
// over recorded outcomes, with no driver, no shell and no corpus, so the fast
// lane holds it; absence_test.go drives the same rule end to end through
// `run` with fake drivers.
package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// outcomes builds a result whose cases came to the given outcomes, as
// scoreAnswer would have recorded them.
func outcomes(cases map[string]string) (*result, []expectation) {
	r := &result{total: len(cases)}
	var want []expectation
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		want = append(want, expectation{name: name})
		r.mark(name, cases[name])
		switch cases[name] {
		case outcomePass:
			r.pass++
		case outcomeAbsent:
			r.missing++
		}
	}
	return r, want
}

func parseDeclared(t *testing.T, text string) declaredSet {
	t.Helper()
	path := filepath.Join(t.TempDir(), declaredAbsenceFile)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := readDeclaredAbsences(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestUndeclaredCaseAbsenceFails is the reversed witness: before the rule, a
// port's `<case>.absent` was counted (`+1a`) and the leg stayed green whatever
// the reason — the JavaScript messagedemo/ToolMessage row, 2026-11-21, was a
// missing driver entry read as a missing feature. Now an absence nothing
// declares is a failure that names the case and the list.
func TestUndeclaredCaseAbsenceFails(t *testing.T) {
	r, want := outcomes(map[string]string{"alpha": outcomePass, "tool_msg": outcomeAbsent})
	enforceDeclared("js", "message", declaredSet{}, want, r)
	if len(r.failures) != 1 {
		t.Fatalf("one undeclared absence, %d failures: %q", len(r.failures), r.failures)
	}
	if !strings.Contains(r.failures[0], "tool_msg: answered ABSENT") || !strings.Contains(r.failures[0], "js/absent.txt") {
		t.Errorf("the failure does not name the case and the leg's list: %q", r.failures[0])
	}
}

// TestDeclaredCaseAbsenceStaysGreen: the same absence, declared with its
// citation, is the missing feature it says it is.
func TestDeclaredCaseAbsenceStaysGreen(t *testing.T) {
	for name, text := range map[string]string{
		"one case":      "message tool_msg docs/SPEC-TABLES.md§3.3 not carried\n",
		"whole surface": "message * docs/SPEC-TABLES.md§3.3\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, want := outcomes(map[string]string{"alpha": outcomePass, "tool_msg": outcomeAbsent})
			enforceDeclared("js", "message", parseDeclared(t, text), want, r)
			if len(r.failures) != 0 {
				t.Errorf("a declared absence failed: %q", r.failures)
			}
		})
	}
}

// TestDeclaredButAnsweredFails: a line the leg no longer needs cannot stay
// behind a fix, or the list would hide the next regression on that row.
func TestDeclaredButAnsweredFails(t *testing.T) {
	t.Run("one case", func(t *testing.T) {
		r, want := outcomes(map[string]string{"tool_msg": outcomePass})
		enforceDeclared("js", "message", parseDeclared(t, "message tool_msg docs/SPEC-TABLES.md\n"), want, r)
		if len(r.failures) != 1 || !strings.Contains(r.failures[0], "but the leg answered it") {
			t.Errorf("a declared case the leg answered did not fail: %q", r.failures)
		}
	})
	t.Run("whole surface", func(t *testing.T) {
		r, want := outcomes(map[string]string{"alpha": outcomePass, "tool_msg": outcomePass})
		enforceDeclared("js", "message", parseDeclared(t, "message * docs/SPEC-TABLES.md\n"), want, r)
		if len(r.failures) != 1 || !strings.Contains(r.failures[0], "answered every case") {
			t.Errorf("a declared surface the leg answered whole did not fail: %q", r.failures)
		}
	})
}

// TestWholeSurfaceAbsenceNeedsADeclaration: a surface left out of `list` (or
// exit 2) is declared by `*` or by every case it asks; anything less is not.
func TestWholeSurfaceAbsenceNeedsADeclaration(t *testing.T) {
	want := []expectation{{name: "a"}, {name: "b"}}
	for _, c := range []struct {
		text string
		ok   bool
	}{
		{"", false},
		{"cook a docs/SPEC-TABLES.md\n", false},
		{"cook a docs/SPEC-TABLES.md\ncook b docs/SPEC-TABLES.md\n", true},
		{"cook * docs/SPEC-TABLES.md\n", true},
		{"block * docs/SPEC-TABLES.md\n", false},
	} {
		if got := surfaceDeclared(parseDeclared(t, c.text), "cook", want); got != c.ok {
			t.Errorf("%q: surfaceDeclared = %v, want %v", c.text, got, c.ok)
		}
	}
	if !strings.Contains(undeclaredSurface("dart", "cook", "the driver exited 2"), "dart/absent.txt does not declare it") {
		t.Error("the whole-surface failure does not name the leg's list")
	}
}

// TestStaleDeclarationNamesNoCase: a line naming a case the corpus does not
// have on that surface is a typo or a removed row, and fails.
func TestStaleDeclarationNamesNoCase(t *testing.T) {
	d := parseDeclared(t, "message gone_row docs/SPEC-TABLES.md\nmessage tool_msg docs/SPEC-TABLES.md\n")
	stale := staleDeclarations("js", "message", d, []expectation{{name: "tool_msg"}})
	if len(stale) != 1 || !strings.Contains(stale[0], "gone_row") {
		t.Errorf("the line naming no case was not reported alone: %q", stale)
	}
	if got := staleDeclarations("js", "message", parseDeclared(t, "message * docs/x.md\n"), nil); len(got) != 0 {
		t.Errorf("a `*` line is never stale by name: %q", got)
	}
}

// TestDeclaredAbsenceGrammar holds the list's grammar: a surface, a case or
// `*`, a citation; nothing declared twice; `*` alone on its surface.
func TestDeclaredAbsenceGrammar(t *testing.T) {
	for name, text := range map[string]string{
		"no citation":       "message tool_msg\n",
		"not a surface":     "messages tool_msg docs/SPEC-TABLES.md\n",
		"not a citation":    "message tool_msg because\\it\\said\n",
		"declared twice":    "message a docs/x.md\nmessage a docs/y.md\n",
		"star beside cases": "message * docs/x.md\nmessage a docs/y.md\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), declaredAbsenceFile)
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := readDeclaredAbsences(path); err == nil {
				t.Errorf("%q parsed", text)
			}
		})
	}
	if d, err := readDeclaredAbsences(filepath.Join(t.TempDir(), "missing.txt")); err != nil || len(d) != 0 {
		t.Errorf("a missing list is not an empty one: %v %v", d, err)
	}
}

// TestCommittedDeclaredAbsences holds every committed list: it parses, every
// citation names a file in the repository, and the reference leg has none.
func TestCommittedDeclaredAbsences(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	lists, err := filepath.Glob(filepath.Join(root, defaultDriversDir, "*", declaredAbsenceFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range lists {
		lang := filepath.Base(filepath.Dir(path))
		if lang == referenceLang {
			t.Errorf("%s: the reference leg may answer no absence, so it declares none", path)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "driver")); err != nil {
			t.Errorf("%s: no driver beside it, so no leg reads it", path)
		}
		d, err := readDeclaredAbsences(path)
		if err != nil {
			t.Error(err)
			continue
		}
		for surface, cases := range d {
			for name, a := range cases {
				file := a.citation
				if i := strings.IndexAny(file, "§#"); i >= 0 {
					file = file[:i]
				}
				if _, err := os.Stat(filepath.Join(root, file)); err != nil {
					t.Errorf("%s:%d (%s %s): the citation %s names no file in the repository", path, a.line, surface, name, a.citation)
				}
			}
		}
	}
}
