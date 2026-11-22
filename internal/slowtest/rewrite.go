package slowtest

import (
	"os"
	"testing"
)

// A REWRITE RUN COMPARES NOTHING. A golden test that reads NAME=1 as "write
// the golden from this run" writes it and passes: whatever the code produced
// is the new truth. Exported in a shell or set in a workflow, the variable
// turns every such test into a green over no comparison. Rewriting is the one
// way a test reads such a variable, so the run is loud wherever it happens:
//
//   - under CI (CI is set) it fails the test: no lane may bless its own output;
//   - a value other than 1 fails the test: a typo is never a quiet compare
//     or a quiet rewrite;
//   - a local rewrite says in the test log that it compared nothing.
//
// internal/ci (TestGoldenRewritesGoThroughRewriting) refuses a test that reads
// a *_UPDATE* or *_WRITE* variable any other way.
func Rewriting(t testing.TB, name string) bool {
	t.Helper()
	v, set := os.LookupEnv(name)
	if !set || v == "" {
		return false
	}
	if v != "1" {
		t.Fatalf("%s=%q: set it to 1 to rewrite the golden, or unset it to compare", name, v)
		return false
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s=1 under CI: a rewrite run compares nothing and passes, so no lane may set it; unset it", name)
		return false
	}
	t.Logf("%s=1: this run rewrote the golden and compared nothing; run again without it to compare", name)
	return true
}
