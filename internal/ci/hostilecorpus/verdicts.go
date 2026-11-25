package hostilecorpus

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// THE PER-CASE VERDICTS: what the registry reads from one run of a port's
// hostile corpus (docs/TEST-REGISTRY.md, "The hostile corpus"). Each case of
// the manifest is one registry test per language, and its verdict is read
// from the run's own lines, the ledger's protocol:
//
//	FAIL <case name> <why>        the port disagreed with the manifest
//	hostile-ledger ran <case>     the port's reader gave the case a verdict
//	hostile-ledger end            the block is done
//
// A case passes when its block names it ran and no FAIL line names it. A
// case named by no line has no verdict, and fails as such. A row id
// `<case>@<row>` (a carrier read as several rows) is its case's. Lines after
// a run's last `hostile-ledger end` are not the corpus run's: a make target
// runs its own controls after it (the carry controls, the ledger's negative
// control), and a FAIL those print names their own scratch corpus.
//
// A runner written in Go as a test writes the same lines to the file
// VerdictsEnv names (WriteVerdicts), since go test's own output is not the
// recipe's to shape.

// VerdictsEnv names the file a Go-test runner writes its per-case lines to.
// tools/schema-test sets it for each run; a run outside the driver writes
// them under the test's own temporary directory.
const VerdictsEnv = "SCHEMA_HOSTILE_VERDICTS"

const (
	verdictRan  = "hostile-ledger ran "
	verdictEnd  = "hostile-ledger end"
	verdictFail = "FAIL "
)

// VerdictsPath is the file a Go-test runner writes to: VerdictsEnv's, or
// one in dir (the test's temporary directory) when the driver did not set it.
func VerdictsPath(dir string) string {
	return cmp.Or(os.Getenv(VerdictsEnv), filepath.Join(dir, "hostile-verdicts.log"))
}

// WriteVerdicts appends one run's block to path: a FAIL line for each failed
// case (its reason on one line), a ran line for each case the reader
// answered, and the end line.
func WriteVerdicts(path string, ran []string, failed map[string]string) (err error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	for _, name := range slices.Sorted(maps.Keys(failed)) {
		why := strings.Join(strings.Fields(failed[name]), " ")
		if _, err := fmt.Fprintf(w, "%s%s %s\n", verdictFail, name, why); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(slices.Values(ran)) {
		if _, err := fmt.Fprintf(w, "%s%s\n", verdictRan, name); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, verdictEnd); err != nil {
		return err
	}
	return w.Flush()
}

// Verdicts is what one run's lines say about each case.
type Verdicts struct {
	Ran    map[string]bool   // the cases some block names as run
	Failed map[string]string // the cases a FAIL line names, with its line
	Blocks int               // how many blocks ended
	// Other is every line inside a block that is neither a ran nor a FAIL
	// line naming a case: a FIX-CARD or STALE line (a carried finding is an
	// exemption, and there are none: docs/rulings.json no-skips), a FAIL line
	// that names no case (a ledger that did not close), a ran line naming a
	// case the manifest does not register. The driver fails the run on any.
	// Other lines are the runner's log and are not kept.
	Other []string
}

// ReadVerdicts reads a run's lines for the cases named in cases: only the
// lines up to the last `hostile-ledger end` count.
func ReadVerdicts(r io.Reader, cases []string) (Verdicts, error) {
	known := map[string]bool{}
	for _, c := range cases {
		known[c] = true
	}
	var lines []string
	last := -1
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == verdictEnd {
			last = len(lines)
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return Verdicts{}, err
	}
	v := Verdicts{Ran: map[string]bool{}, Failed: map[string]string{}}
	for _, line := range lines[:last+1] {
		switch {
		case line == verdictEnd:
			v.Blocks++
		case strings.HasPrefix(line, verdictRan):
			name := caseOf(strings.TrimPrefix(line, verdictRan))
			if !known[name] {
				v.Other = append(v.Other, line)
				continue
			}
			v.Ran[name] = true
		case strings.HasPrefix(line, verdictFail):
			rest := strings.TrimPrefix(line, verdictFail)
			field, _, _ := strings.Cut(rest, " ")
			name := caseOf(strings.TrimSuffix(field, ":"))
			if !known[name] {
				v.Other = append(v.Other, line)
				continue
			}
			if _, seen := v.Failed[name]; !seen {
				v.Failed[name] = line
			}
		case strings.HasPrefix(line, "FIX-CARD") || strings.HasPrefix(line, "STALE "):
			v.Other = append(v.Other, line)
		}
	}
	return v, nil
}

// caseOf is a row id's case: `<case>@<row>` is its case's row.
func caseOf(id string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(id), "@")
	return name
}
