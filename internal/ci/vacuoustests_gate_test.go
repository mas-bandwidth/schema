package ci

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/vacuous"
)

// TestEveryGoTestCanFail is the vacuous-test gate (2026-11-21): a test that
// exists has to be able to fail, and has to check what its name promises.
//
// internal/vacuous parses every _test.go file of the repository (parse only:
// nothing is compiled and no test runs) and refuses the classes it can decide
// mechanically: a test or subtest with no reachable call that can fail it,
// assertions only inside a loop over a filesystem or registry read with
// nothing checking it was non-empty, an error from this tree or from a child
// process dropped, and a test named as a negative control that never fails
// when the operation is accepted. Its own negative controls, one fixture per
// gated class, are internal/vacuous's TestGateRefusesEachGatedClass and
// TestGateRefusesAnEmptyTree.
//
// The gate proves it ran: Run answers 1 when it parsed no test file or saw no
// test function, and this test also requires both counts to be non-zero.
func TestEveryGoTestCanFail(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status, c := vacuous.Run(repoRoot(t), false, &stdout, &stderr)
	if c.TestFiles == 0 || c.TestFuncs == 0 {
		t.Fatalf("the vacuous-test gate analyzed %d test files and %d test functions: a gate that looked at nothing is not a pass\n%s", c.TestFiles, c.TestFuncs, stderr.String())
	}
	if status != 0 {
		t.Fatalf("the vacuous-test gate refused (status %d); each line is a Go test that can pass while checking nothing (classes in internal/vacuous/vacuous.go; list every class with `go run ./tools/vacuoustests -all`):\n%s%s", status, stdout.String(), stderr.String())
	}
	t.Logf("vacuous-test gate: %d test files, %d test functions, 0 refused findings", c.TestFiles, c.TestFuncs)
}
