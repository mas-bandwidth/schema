package slowtest

import (
	"os/exec"
	"testing"
)

// A MISSING TOOLCHAIN IS A FAILURE THAT NAMES IT: NEVER A SKIP, NEVER A PASS.
//
// A test that needs a program is tiered functional (or nightly), so the
// driver runs it only where its runner carries the toolchain. There, a
// program that is not found is a fault of the runner, and the test fails
// saying which program. A test that returned quietly, or skipped, would print
// a verdict over a body that did not run.

// Tool returns the path of a program the calling test needs, and fails the
// test naming it when it is not on PATH.
func Tool(t testing.TB, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		Missing(t, "no "+name+" on PATH")
	}
	return path
}

// MissingWords start every failure Missing writes. tools/gotestrun reads them:
// a negative control's test red for this reason caught no sabotage.
const MissingWords = "missing what this test needs"

// Missing fails the calling test because something it needs is absent.
func Missing(t testing.TB, reason string) {
	t.Helper()
	t.Fatalf("%s (its tier is functional or nightly, so its runner must carry it): %s", MissingWords, reason)
}
