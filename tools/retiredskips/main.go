// Command retiredskips runs one leg's fixed-form gate and holds every
// retirement it prints to the shared ledger of retired native tests.
//
//	retiredskips -leg c -- ./build/schema_test_c_fixedform
//
// docs/FIXED-FORM-ALGORITHM.md §5.6 retired the run-time walk of a stranger's
// layout, and each leg's fixed-form program keeps the retired checks in the
// tree, SKIPPED BY NAME with a printed line (§5.7 step 5, §5.9 #23). A skip is
// a test that did not run, so a printed one is only allowed when the ledger
// names it: test/conformance/fixed-retired.json, one entry per (leg, exact
// line) with the tests that carry its coverage now. The same file is the
// nightly readiness gate's ledger (test/conformance/nightly-readiness reads
// it and also requires those replacement tests to pass); this command is the
// gate-side half, so the fixed-form gate itself, at every tier it runs, is
// red on:
//
//   - a printed skip or retirement line the ledger does not name for the leg
//     (an unledgered skip);
//   - a ledger line for the leg that the run did not print (a retirement that
//     went quiet, or a check that came back and still holds its line);
//   - the gate's own failure (its exit status is passed through).
//
// The program's output is copied through unchanged.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
)

func main() {
	leg := flag.String("leg", "", "the leg whose ledger entries the run must print exactly")
	ledgerPath := flag.String("ledger", "test/conformance/fixed-retired.json", "the shared ledger of retired native tests")
	flag.Parse()
	argv := flag.Args()
	if *leg == "" || len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: retiredskips -leg LEG [-ledger FILE] -- command [args...]")
		os.Exit(2)
	}
	data, err := os.ReadFile(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "retiredskips: %v\n", err)
		os.Exit(2)
	}
	ledger, err := parseLedger(data, *leg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "retiredskips: %s: %v\n", *ledgerPath, err)
		os.Exit(2)
	}
	printed, status, err := runThrough(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "retiredskips: %v\n", err)
		os.Exit(2)
	}
	// The set is judged on a red run too, so both failures are said; the
	// program's own status wins the exit code.
	cerr := check(printed, ledger, *leg, *ledgerPath)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "retiredskips: %v\n", cerr)
	} else {
		fmt.Printf("retiredskips %s: %d retired checks printed, each named in %s\n", *leg, len(printed), *ledgerPath)
	}
	if status != 0 {
		fmt.Fprintf(os.Stderr, "retiredskips: %s exited %d\n", argv[0], status)
		os.Exit(status)
	}
	if cerr != nil {
		os.Exit(1)
	}
}

// parseLedger returns the ledger lines of one leg. The file is a JSON array
// of [leg, exact line, [[package, test], ...]] entries.
func parseLedger(data []byte, leg string) (map[string]bool, error) {
	var entries [][]json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	held := map[string]bool{}
	for i, e := range entries {
		if len(e) != 3 {
			return nil, fmt.Errorf("entry %d: want [leg, line, needs]", i)
		}
		var l, line string
		if err := json.Unmarshal(e[0], &l); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		if err := json.Unmarshal(e[1], &line); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		if l != leg {
			continue
		}
		if held[line] {
			return nil, fmt.Errorf("entry %d: %s line listed twice: %s", i, leg, line)
		}
		held[line] = true
	}
	return held, nil
}

// prefixRe and markerRe are the nightly readiness gate's own reading of a
// native skip (native_retirement_errors): a leading file:line prefix and
// whitespace come off, and a line that starts with SKIP, SKIPPED or RETIRED
// is a skip.
var (
	prefixRe = regexp.MustCompile(`^\s*(?:[^:\s]+:\d+:\s*)?`)
	markerRe = regexp.MustCompile(`^(?:SKIP(?:PED)?|RETIRED)(?:[: ]|$)`)
)

// skipLine returns the line as the ledger names it, and whether it is a skip.
func skipLine(raw string) (string, bool) {
	line := strings.TrimRight(prefixRe.ReplaceAllString(raw, ""), " \t\r")
	return line, markerRe.MatchString(line)
}

// check holds the printed skip lines against the leg's ledger as a set.
func check(printed []string, ledger map[string]bool, leg, ledgerPath string) error {
	seen := map[string]bool{}
	var unledgered, quiet []string
	for _, p := range printed {
		if seen[p] {
			continue
		}
		seen[p] = true
		if !ledger[p] {
			unledgered = append(unledgered, p)
		}
	}
	for l := range ledger {
		if !seen[l] {
			quiet = append(quiet, l)
		}
	}
	sort.Strings(unledgered)
	sort.Strings(quiet)
	var msg []string
	if len(unledgered) > 0 {
		msg = append(msg, fmt.Sprintf("%d skips printed that %s does not name for %s (a skip is a test that did not run):\n  %s",
			len(unledgered), ledgerPath, leg, strings.Join(unledgered, "\n  ")))
	}
	if len(quiet) > 0 {
		msg = append(msg, fmt.Sprintf("%d %s retirements in %s were not printed by this run (remove the line with the check that came back, or restore the printed skip):\n  %s",
			len(quiet), leg, ledgerPath, strings.Join(quiet, "\n  ")))
	}
	if len(msg) > 0 {
		return errors.New(strings.Join(msg, "\n"))
	}
	return nil
}

// runThrough runs the command with its output copied through, and returns
// the skip lines it printed and its exit status.
func runThrough(argv []string) ([]string, int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, 0, err
	}
	if err := cmd.Start(); err != nil {
		return nil, 0, err
	}
	var mu sync.Mutex
	var printed []string
	var echoErr error
	var wg sync.WaitGroup
	// A line that cannot be copied through is an error of the run, but the
	// scan goes on draining the pipe so the gate is never left blocked.
	scan := func(r io.Reader, w io.Writer) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for sc.Scan() {
			_, werr := fmt.Fprintln(w, sc.Text())
			line, ok := skipLine(sc.Text())
			mu.Lock()
			if werr != nil && echoErr == nil {
				echoErr = werr
			}
			if ok {
				printed = append(printed, line)
			}
			mu.Unlock()
		}
	}
	wg.Add(2)
	go scan(stdout, os.Stdout)
	go scan(stderr, os.Stderr)
	wg.Wait()
	err = cmd.Wait()
	if echoErr != nil {
		return nil, 0, fmt.Errorf("copying the gate's output through: %w", echoErr)
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		if code := exit.ExitCode(); code > 0 {
			return printed, code, nil
		}
		return printed, 128 + 1, nil
	}
	if err != nil {
		return nil, 0, err
	}
	return printed, 0, nil
}
