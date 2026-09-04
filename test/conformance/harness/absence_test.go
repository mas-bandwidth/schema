// THE REFERENCE LEG MAY NOT ANSWER ABSENT, at every grain the harness can meet
// an absence (test/conformance/README.md).
//
// A whole SURFACE goes absent two ways — left out of `list`, or exit code 2 —
// and a single CASE a third, `<case>.absent`. All three are the corpus losing
// its own expectation when they come from the reference leg, and an ordinary
// missing feature when they come from a port. These tests drive `run` with
// fake drivers over a one-instance corpus, so every path is gated without a
// language leg: the reference goes red on each grain and the matrix cannot
// print the success footer, and the same three answers from a port stay green
// and stay visible.
package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// footer is what `report` prints when nothing failed, and the thing that must
// never appear beside a reference absence.
const footer = "tables conformance: every registered surface passes"

// fakeLeg is how one fake driver goes absent. The zero value answers every
// surface the corpus asks about and registers all thirteen.
type fakeLeg struct {
	unlisted string // a surface left out of `list` altogether
	exitTwo  string // a surface the driver exits 2 on
	caseGone bool   // the wire surface answers `<case>.absent` for its one case
}

// fakeCorpus is a one-instance conformance corpus: enough for the wire, json-read
// and json-write surfaces to carry an expectation, and nothing else, so a test
// never builds a fixture generator or a language leg.
type fakeCorpus struct {
	manifest string
	jsonDir  string
	reports  string
	wire     string
	json     string
	m        *Manifest
}

func newFakeCorpus(t *testing.T) fakeCorpus {
	t.Helper()
	dir := t.TempDir()
	c := fakeCorpus{
		manifest: filepath.Join(dir, "MANIFEST.txt"),
		jsonDir:  filepath.Join(dir, "json"),
		reports:  filepath.Join(dir, "reports.txt"),
		wire:     filepath.Join(dir, "alpha.bin"),
	}
	c.json = filepath.Join(c.jsonDir, "alpha.json")
	writeFile(t, c.wire, "\x01\x02\x03\x04")
	writeFile(t, c.json, "{\"alpha\":1}\n")
	writeFile(t, c.manifest, "unit u tables/examples\ninstance alpha u Root "+c.wire+"\n")
	writeFile(t, c.reports, "")
	m, err := ReadManifest(c.manifest, c.jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	c.m = m
	return c
}

// driverScript writes one fake driver: a command that answers this corpus and
// goes absent exactly where the case under test says it does.
func (c fakeCorpus) driverScript(t *testing.T, path string, leg fakeLeg) {
	t.Helper()
	var list strings.Builder
	for _, s := range surfaces {
		if s == leg.unlisted {
			continue
		}
		fmt.Fprintf(&list, "\techo %s\n", s)
	}
	answer := fmt.Sprintf("cp %q \"$out/alpha\"", c.wire)
	if leg.caseGone {
		answer = `: > "$out/alpha.absent"`
	}
	exitTwo := leg.exitTwo
	if exitTwo == "" {
		exitTwo = "\x00" // a name no surface has, so the arm never fires
	}
	writeExec(t, path, fmt.Sprintf(`#!/bin/sh
surface="$2"
out="$3"
if [ "$surface" = list ]; then
%s	exit 0
fi
if [ "$surface" = %q ]; then
	exit 2
fi
case "$surface" in
wire|json-read) %s ;;
json-write) cp %q "$out/alpha.json" ;;
esac
exit 0
`, list.String(), exitTwo, answer, c.json))
}

// committed plants a DISCOVERED registry — one <lang>/driver per leg, which is
// the committed registry's own shape and the only one the reference rule
// reaches.
func (c fakeCorpus) committed(t *testing.T, legs map[string]fakeLeg) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "registry")
	for lang, leg := range legs {
		c.driverScript(t, filepath.Join(dir, lang, "driver"), leg)
	}
	return dir
}

// substituted plants a SUBSTITUTED registry — a file of "<lang> <command>"
// lines, which is one leg of a port rather than the matrix, so its first line
// is not the reference.
func (c fakeCorpus) substituted(t *testing.T, lang string, leg fakeLeg) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "driver")
	c.driverScript(t, script, leg)
	path := filepath.Join(dir, "drivers.txt")
	writeFile(t, path, lang+" "+script+"\n")
	return path
}

// runHarness runs the gate over this corpus and hands back what it printed.
func (c fakeCorpus) runHarness(t *testing.T, drivers string) (string, bool) {
	t.Helper()
	var out bytes.Buffer
	ok, err := run(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers,
		filepath.Join(t.TempDir(), "work"), "")
	if err != nil {
		t.Fatalf("harness run: %v\n%s", err, out.String())
	}
	return out.String(), ok
}

func needShell(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
}

// want holds the printed matrix to what it must and must not say.
func mustSay(t *testing.T, out string, has []string, hasNot []string) {
	t.Helper()
	for _, s := range has {
		if !strings.Contains(out, s) {
			t.Errorf("the matrix does not say %q:\n%s", s, out)
		}
	}
	for _, s := range hasNot {
		if strings.Contains(out, s) {
			t.Errorf("the matrix says %q and must not:\n%s", s, out)
		}
	}
}

// TestReferenceSurfaceMissingFromList is one half of the coarse grain: a
// reference leg that never registers a surface leaves the corpus's
// expectations for it comparing against nothing, so the surface is red and the
// success footer may not print beside its cell.
func TestReferenceSurfaceMissingFromList(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok := c.runHarness(t, c.committed(t, map[string]fakeLeg{
		referenceLang: {unlisted: "json-write"},
	}))
	if ok {
		t.Errorf("the reference leg left json-write out of `list` and the harness stayed green:\n%s", out)
	}
	mustSay(t, out,
		[]string{
			"the REFERENCE leg is ABSENT on the whole json-write surface (`list` does not name it)",
			"FAIL absent",
			"pass 1/1", // the surfaces it did register still pass, so this localises
		},
		[]string{footer})
	if n := strings.Count(out, "  cpp / "); n != 1 {
		t.Errorf("one surface went absent and the harness reported %d failures:\n%s", n, out)
	}
}

// TestReferenceSurfaceExitsTwo is the other half: the same absence said the
// other way the contract allows it to be said.
func TestReferenceSurfaceExitsTwo(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok := c.runHarness(t, c.committed(t, map[string]fakeLeg{
		referenceLang: {exitTwo: "json-read"},
	}))
	if ok {
		t.Errorf("the reference leg exited 2 on json-read and the harness stayed green:\n%s", out)
	}
	mustSay(t, out,
		[]string{
			"the REFERENCE leg is ABSENT on the whole json-read surface (the driver exited 2)",
			"FAIL absent",
			"pass 1/1",
		},
		[]string{footer})
	if n := strings.Count(out, "  cpp / "); n != 1 {
		t.Errorf("one surface went absent and the harness reported %d failures:\n%s", n, out)
	}
}

// TestReferenceCaseAbsent is the fine grain, gated beside the two coarse ones
// so that no grain of the rule can regress on its own.
func TestReferenceCaseAbsent(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok := c.runHarness(t, c.committed(t, map[string]fakeLeg{
		referenceLang: {caseGone: true},
	}))
	if ok {
		t.Errorf("the reference leg answered a case ABSENT and the harness stayed green:\n%s", out)
	}
	mustSay(t, out,
		[]string{"the REFERENCE leg answered ABSENT for 1 case(s)"},
		[]string{footer})
}

// TestPortAbsenceStaysAllowed is the other direction, and the half a one-sided
// rule would break: absent is not failure for a port. The same three answers
// from a leg that is not the reference stay green and stay PRINTED, because
// the matrix is the completion tracker.
func TestPortAbsenceStaysAllowed(t *testing.T) {
	needShell(t)
	absences := fakeLeg{unlisted: "json-write", exitTwo: "json-read", caseGone: true}

	t.Run("a second leg of the committed registry", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok := c.runHarness(t, c.committed(t, map[string]fakeLeg{
			referenceLang: {},
			"zz":          absences,
		}))
		if !ok {
			t.Errorf("a PORT's absences turned the harness red:\n%s", out)
		}
		mustSay(t, out,
			[]string{
				footer,
				"absent",       // the surfaces zz does not implement, named where they can be seen
				"pass 0/0 +1a", // and the case it said it cannot answer
			},
			[]string{"REFERENCE leg", "FAIL"})
	})

	// A run handed a SUBSTITUTED registry is one leg of a port and not the
	// matrix, so its first line is not the reference however it goes absent.
	t.Run("the first leg of a substituted registry", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok := c.runHarness(t, c.substituted(t, referenceLang, absences))
		if !ok {
			t.Errorf("absences under a SUBSTITUTED registry turned the harness red:\n%s", out)
		}
		mustSay(t, out, []string{footer, "absent"}, []string{"REFERENCE leg", "FAIL"})
	})
}
