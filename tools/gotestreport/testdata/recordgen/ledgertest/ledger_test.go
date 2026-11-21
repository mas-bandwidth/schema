// Package ledgertest is the test package whose real `go test -json` runs are
// the record corpus in ../../records (see ../main.go). Each test does what one
// case needs when RECORD_CASE names it, and passes otherwise.
package ledgertest

import (
	"os"
	"strings"
	"testing"
)

func is(name string) bool { return os.Getenv("RECORD_CASE") == name }

var repeatRuns int

// F-175: run with -count=2, it skips on its first run and passes on its
// second.
func TestRepeat(t *testing.T) {
	repeatRuns++
	if is("skip-then-pass") && repeatRuns == 1 {
		t.Skip("no cargo on PATH")
	}
}

// F-176: a cleanup registered before the skip logs after it.
func TestCleanup(t *testing.T) {
	switch {
	case is("cleanup-after-required-skip"):
		t.Cleanup(func() { t.Log("SCHEMA_SLOW: auxiliary log") })
		t.Skip("no cargo on PATH")
	case is("cleanup-after-slow-skip"):
		t.Cleanup(func() { t.Log("removed the scratch directory") })
		t.Skip("SCHEMA_SLOW: this test shells out to cargo; set SCHEMA_SLOW=1 to run it")
	}
}

// F-176: 70 KB of log, then the skip.
func TestLong(t *testing.T) {
	switch {
	case is("long-output-slow-skip"):
		t.Log(strings.Repeat("A", 70_000))
		t.Skip("SCHEMA_SLOW: trace collection finished")
	case is("long-output-required-skip"):
		t.Log(strings.Repeat("A", 70_000))
		t.Skip("no cargo on PATH")
	}
}

// F-177: an ordinary passing run.
func TestPasses(t *testing.T) {}
