package ci

import (
	"bytes"
	"os/exec"
	"sync"
	"testing"
)

// The Python CI planner lists the tests of four packages, which compiles their
// test binaries; one run is the cost of a single check. Every planner check in
// this package reads that one plan, exactly as the workflow does, so the
// package stays inside the policy job's two-minute bound.
var pythonCIPlanRun struct {
	once sync.Once
	out  []byte
	err  error
}

// pythonCIPlan returns the planner's combined output, running it once per
// test process; a failed run fails every caller with the planner's output.
func pythonCIPlan(t *testing.T) []byte {
	t.Helper()
	pythonCIPlanRun.once.Do(func() {
		cmd := exec.Command("sh", "tools/python-ci-rows.sh")
		cmd.Dir = "../.."
		pythonCIPlanRun.out, pythonCIPlanRun.err = cmd.CombinedOutput()
	})
	if pythonCIPlanRun.err != nil {
		t.Fatalf("python CI plan: %v\n%s", pythonCIPlanRun.err, pythonCIPlanRun.out)
	}
	return bytes.Clone(pythonCIPlanRun.out)
}
