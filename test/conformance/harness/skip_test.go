// A NAMED SKIP REACHES THE MATRIX, and the reference leg cannot be one
// (issue #599, `skipSet` and the driver loop in run.go).
//
// `make test SCHEMA_SKIP_LEGS=<lang>` becomes `--skip <lang>` on the harness,
// and the whole point of naming a skip on purpose is that the run says which
// legs it did not measure rather than passing over them in silence. These
// tests read the EFFECT and not only the line: the skipped leg's driver would
// turn the run red if it were reached, so a skip that failed to skip cannot
// pass here, and a skip line printed for a leg that ran anyway cannot either.
//
// The corpus, the fake drivers and the run helpers are absence_test.go's
// (`fakeCorpus`), which is the one place in this package that drives `run`
// without a language leg.
package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// loudLeg is what the driver of a skipped leg says on stderr when it is run.
// The harness folds a failing driver's stderr into the matrix, so this string
// appearing in the output is proof the leg was reached.
const loudLeg = "the %s driver was RUN"

// withLoudLeg plants a DISCOVERED registry: the reference leg answering this
// corpus, and one port whose driver registers the wire surface and then exits
// 1 on it, saying so. A run that reaches that leg is RED and names it; a run
// that skips it by name is green.
func (c fakeCorpus) withLoudLeg(t *testing.T, lang string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "registry")
	c.driverScript(t, filepath.Join(dir, referenceLang, "driver"), fakeLeg{})
	writeExec(t, filepath.Join(dir, lang, "driver"), fmt.Sprintf(`#!/bin/sh
if [ "$2" = list ]; then
	echo wire
	exit 0
fi
echo %q >&2
exit 1
`, fmt.Sprintf(loudLeg, lang)))
	return dir
}

// TestUnskippedLegRuns is the control for the two below: without --skip, the
// loud leg is reached and the run is red. A skip test whose leg would have
// been green either way measures nothing.
func TestUnskippedLegRuns(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok := c.runHarness(t, c.withLoudLeg(t, "zz"))
	if ok {
		t.Errorf("the zz driver exited 1 on the wire surface and the harness stayed green:\n%s", out)
	}
	mustSay(t, out, []string{fmt.Sprintf(loudLeg, "zz"), "FAIL"}, []string{footer})
}

// TestNamedSkipIsPrintedAndTheLegIsNotRun is the rule itself: the leg is left
// out, the run is green without it, and the line that says so names the leg
// and where the name came from.
func TestNamedSkipIsPrintedAndTheLegIsNotRun(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok, err := c.runHarnessSkipping(t, c.withLoudLeg(t, "zz"), "zz")
	if err != nil {
		t.Fatalf("--skip zz was refused: %v\n%s", err, out)
	}
	if !ok {
		t.Errorf("--skip zz named the only failing leg and the harness stayed red:\n%s", out)
	}
	mustSay(t, out,
		[]string{
			"conformance SKIPS the zz leg: --skip names it (SCHEMA_SKIP_LEGS)",
			footer,
		},
		[]string{
			fmt.Sprintf(loudLeg, "zz"), // it was named, not merely tolerated
			"FAIL",
		})
	// and it is gone from the matrix's columns, not printed as an empty one:
	// a column of blanks reads like a leg that answered nothing.
	header := matrixHeader(t, out)
	if strings.Contains(header, "zz") {
		t.Errorf("the skipped leg still has a matrix column:\n%s", header)
	}
	if !strings.Contains(header, referenceLang) {
		t.Errorf("the reference leg lost its matrix column:\n%s", header)
	}
}

// TestSkipIsMatchedByName holds the list to entry-by-entry reading: surrounding
// space and empty entries are not names, and a name no leg carries skips
// nothing rather than everything.
func TestSkipIsMatchedByName(t *testing.T) {
	needShell(t)

	t.Run("space and empty entries are not names", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok, err := c.runHarnessSkipping(t, c.withLoudLeg(t, "zz"), " , zz , ")
		if err != nil {
			t.Fatalf("--skip ' , zz , ' was refused: %v\n%s", err, out)
		}
		if !ok {
			t.Errorf("--skip named zz around blanks and the harness stayed red:\n%s", out)
		}
		mustSay(t, out,
			[]string{"conformance SKIPS the zz leg: --skip names it (SCHEMA_SKIP_LEGS)", footer},
			[]string{fmt.Sprintf(loudLeg, "zz")})
	})

	t.Run("a name no leg carries skips nothing", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok, err := c.runHarnessSkipping(t, c.withLoudLeg(t, "zz"), "yy")
		if err != nil {
			t.Fatalf("--skip yy was refused: %v\n%s", err, out)
		}
		if ok {
			t.Errorf("--skip named yy and the zz leg was passed over anyway:\n%s", out)
		}
		mustSay(t, out, []string{fmt.Sprintf(loudLeg, "zz")}, []string{"conformance SKIPS", footer})
	})
}

// TestSkipRefusesTheReferenceLeg is the half a skip list without a floor would
// lose: every other leg compares against the reference, so skipping it leaves
// the matrix comparing against nothing while still printing a verdict. It is
// refused before any driver runs.
func TestSkipRefusesTheReferenceLeg(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok, err := c.runHarnessSkipping(t, c.withLoudLeg(t, "zz"), "zz,"+referenceLang)
	if err == nil {
		t.Fatalf("--skip named the reference leg %s and the harness ran anyway (ok=%v):\n%s",
			referenceLang, ok, out)
	}
	for _, want := range []string{"--skip names the reference leg", referenceLang, "compares against"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	if out != "" {
		t.Errorf("the reference skip was refused and the harness printed anyway:\n%s", out)
	}
}

// matrixHeader is the matrix's column line, the one place the run says which
// legs it measured.
func matrixHeader(t *testing.T, out string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "surface ") {
			return line
		}
	}
	t.Fatalf("the run printed no matrix header:\n%s", out)
	return ""
}
