// Command retiredskips runs one leg's fixed-form gate and fails it on any
// skip the run prints.
//
//	retiredskips -leg c -- ./build/schema_test_c_fixedform
//
// docs/FIXED-FORM-ALGORITHM.md §5.6 retired the run-time walk of a stranger's
// layout, and a check it retired LEAVES each leg's fixed-form program
// (§5.7 step 5, §5.9 #23: a retired test leaves the suite). A skip is a
// test that did not run, and nothing excuses one: there is no ledger. The
// gate, at every tier it runs, is red on:
//
//   - any printed skip or retirement line (a SKIP, SKIPPED or RETIRED line);
//   - the gate's own failure (its exit status is passed through).
//
// A retired check leaves the leg's program, with its coverage carried by the
// tests that replace it; it is never printed and passed over.
//
// The program's output is copied through unchanged.
package main

import (
	"bufio"
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
	leg := flag.String("leg", "", "the leg whose fixed-form gate runs")
	flag.Parse()
	argv := flag.Args()
	if *leg == "" || len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: retiredskips -leg LEG -- command [args...]")
		os.Exit(2)
	}
	printed, status, err := runThrough(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "retiredskips: %v\n", err)
		os.Exit(2)
	}
	// The skips are judged on a red run too, so both failures are said; the
	// program's own status wins the exit code.
	cerr := check(printed, *leg)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "retiredskips: %v\n", cerr)
	} else {
		fmt.Printf("retiredskips %s: no skip printed\n", *leg)
	}
	if status != 0 {
		fmt.Fprintf(os.Stderr, "retiredskips: %s exited %d\n", argv[0], status)
		os.Exit(status)
	}
	if cerr != nil {
		os.Exit(1)
	}
}

// prefixRe and markerRe are the nightly readiness gate's own reading of a
// native skip (native_retirement_errors): a leading file:line prefix and
// whitespace come off, and a line that starts with SKIP, SKIPPED or RETIRED
// is a skip.
var (
	prefixRe = regexp.MustCompile(`^\s*(?:[^:\s]+:\d+:\s*)?`)
	markerRe = regexp.MustCompile(`^(?:SKIP(?:PED)?|RETIRED)(?:[: ]|$)`)
)

// skipLine returns the line as printed, less its prefix, and whether it is a
// skip.
func skipLine(raw string) (string, bool) {
	line := strings.TrimRight(prefixRe.ReplaceAllString(raw, ""), " \t\r")
	return line, markerRe.MatchString(line)
}

// check fails on every distinct skip line the run printed.
func check(printed []string, leg string) error {
	seen := map[string]bool{}
	var skips []string
	for _, p := range printed {
		if seen[p] {
			continue
		}
		seen[p] = true
		skips = append(skips, p)
	}
	if len(skips) == 0 {
		return nil
	}
	sort.Strings(skips)
	return fmt.Errorf("the %s fixed-form gate printed %d skips (a skip is a test that did not run, and none is excused):\n  %s",
		leg, len(skips), strings.Join(skips, "\n  "))
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
	var wg sync.WaitGroup
	scan := func(r io.Reader, w io.Writer) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for sc.Scan() {
			fmt.Fprintln(w, sc.Text())
			if line, ok := skipLine(sc.Text()); ok {
				mu.Lock()
				printed = append(printed, line)
				mu.Unlock()
			}
		}
	}
	wg.Add(2)
	go scan(stdout, os.Stdout)
	go scan(stderr, os.Stderr)
	wg.Wait()
	err = cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
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
