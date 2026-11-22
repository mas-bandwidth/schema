package slowtest

import (
	"os"
	"os/exec"
	"testing"
)

// A MISSING TOOLCHAIN IS A SKIP WITH A REASON, OR A FAILURE: NEVER A PASS.
//
// A test that looks for its toolchain and returns when it is not there
// prints PASS over a body that did not run. No skip ledger sees it, and
// SCHEMA_REQUIRE_CORPUS, the promise that a lane has what its tests need,
// cannot turn it red. internal/codegen/rust's
// TestAssembleLibExportsUnionModule did exactly that with cargo. Tool and
// Missing are the two ways a test says "what I need is not here":
// internal/ci (TestNoTestReturnsQuietlyWhenItsToolchainIsMissing) refuses the
// quiet return.

// Tool returns the path of a program the calling test needs. When it is not
// on PATH the test skips with the reason "no <name> on PATH", which the skip
// ledger (.github/skip-ledger.txt) reads; under SCHEMA_REQUIRE_CORPUS it
// fails instead.
func Tool(t testing.TB, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		Missing(t, "no "+name+" on PATH")
	}
	return path
}

// Missing ends the calling test because something it needs is absent: a
// failure under SCHEMA_REQUIRE_CORPUS, a skip with the reason otherwise.
func Missing(t testing.TB, reason string) {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set, so this lane is meant to have what the test needs: %s", reason)
	}
	t.Skip(reason)
}
