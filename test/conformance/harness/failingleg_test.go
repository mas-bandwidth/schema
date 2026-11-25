// A LEG THAT FAILS IS RED. There is no list of legs a run leaves out: a leg
// the registry discovers is run, and one whose driver fails turns the run red
// by name. Which legs a run selects is decided at the root (--only), never by
// a skip list below it.
package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

// loudLeg is what the failing leg's driver says on stderr when it is run. The
// harness folds a failing driver's stderr into the matrix, so this string
// appearing in the output is proof the leg was reached.
const loudLeg = "the %s driver was RUN"

// withLoudLeg plants a DISCOVERED registry: the reference leg answering this
// corpus, and one port whose driver registers the wire surface and then exits
// 1 on it, saying so.
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

// TestFailingLegIsRed: the loud leg is reached and the run is red, naming it.
func TestFailingLegIsRed(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok := c.runHarness(t, c.withLoudLeg(t, "zz"))
	if ok {
		t.Errorf("the zz driver exited 1 on the wire surface and the harness stayed green:\n%s", out)
	}
	mustSay(t, out, []string{fmt.Sprintf(loudLeg, "zz"), "FAIL"}, []string{footer})
}
