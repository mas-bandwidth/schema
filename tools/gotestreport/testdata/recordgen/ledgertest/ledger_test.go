// Package ledgertest is the test package whose real `go test -json` runs are
// the record corpus in ../../records (see ../main.go). Each test does what one
// case needs when RECORD_CASE names it, and passes otherwise.
package ledgertest

import (
	"os"
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

// F-177: an ordinary passing run.
func TestPasses(t *testing.T) {}
