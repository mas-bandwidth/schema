package namebuild

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// THE OWED LEDGER (testdata/name-buildability/owed.txt). A builds row is a
// name every target must build; a (leg, row) that does not yet is written
// down here with its true cause, one line each:
//
//	<leg> <row> owed: <cause>
//
// The ledger is an EXACT SET. The nightly judges every job against it
// (Judge): a job that fails and is ledgered is counted owed, a job that fails
// and is not ledgered fails the run, and a ledgered job that passes fails the
// run too (a stale line: the fix landed, so the line goes). So a line can
// neither hide a new failure on another job nor outlive its fix. The fast
// lane holds the ledger's form (ReadOwed): each leg is a target, each row a
// builds row of cases.tsv, each cause written out, no pair twice.

// OwedFile is the ledger's name in the corpus directory.
const OwedFile = "owed.txt"

// Owed is one ledger line.
type Owed struct {
	Job
	Cause string
	Line  int
}

// ReadOwed reads dir/owed.txt against the corpus's rows. A missing file is
// an empty ledger; any malformed line is an error naming it.
func ReadOwed(dir string, rows []Row) (map[Job]Owed, error) {
	out := map[Job]Owed{}
	f, err := os.Open(filepath.Join(dir, OwedFile))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: a close error loses nothing
	builds := map[string]bool{}
	for _, r := range rows {
		if r.Verdict == Builds {
			builds[r.Case] = true
		}
	}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		head, cause, ok := strings.Cut(line, " owed: ")
		cause = strings.TrimSpace(cause)
		f := strings.Fields(head)
		if !ok || len(f) != 2 || cause == "" {
			return nil, fmt.Errorf("%s:%d: want `<leg> <row> owed: <cause>`, got %q", OwedFile, n, line)
		}
		j := Job{Target: f[0], Case: f[1]}
		if !slices.Contains(Targets, j.Target) {
			return nil, fmt.Errorf("%s:%d: %q is no target (%s)", OwedFile, n, j.Target, strings.Join(Targets, " "))
		}
		if !builds[j.Case] {
			return nil, fmt.Errorf("%s:%d: %q is no builds row of cases.tsv", OwedFile, n, j.Case)
		}
		if prev, dup := out[j]; dup {
			return nil, fmt.Errorf("%s:%d: %s %s is owed already at line %d", OwedFile, n, j.Target, j.Case, prev.Line)
		}
		out[j] = Owed{Job: j, Cause: cause, Line: n}
	}
	return out, sc.Err()
}

// Verdicts Judge returns.
const (
	Pass    = "pass"
	OwedRun = "owed"
	Fail    = "fail"
)

// Judge is the nightly's verdict on one job's build error (nil: it built and
// loaded) against the ledger: Pass, OwedRun (failed, ledgered), or Fail with
// the reason (failed and not ledgered, or ledgered and passed).
func Judge(j Job, err error, owed map[Job]Owed) (string, error) {
	o, ledgered := owed[j]
	switch {
	case err == nil && !ledgered:
		return Pass, nil
	case err == nil:
		return Fail, fmt.Errorf("STALE OWED: %s %s builds and loads, so %s line %d (%s) is stale; delete it", j.Target, j.Case, OwedFile, o.Line, o.Cause)
	case ledgered:
		return OwedRun, nil
	}
	return Fail, err
}
