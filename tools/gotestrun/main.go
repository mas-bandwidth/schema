// Command gotestrun runs `go test` and refuses a run that did not run a test.
//
//	go run ./tools/gotestrun [-each] [-red] [-dir DIR] [go test arguments...]
//	make test-run PKG=./internal/foo RUN='^TestBar$'
//
// WHY. `go test -run <pattern>` that matches no test prints "ok" and exits 0
// ("[no tests to run]"), so a typo in a pattern (-run Idcap for a test named
// TestIDCap) looks exactly like a pass. The same goes for a package with no
// test files, and for a run whose every test skipped itself (SCHEMA_SLOW
// unset). A pass that executed nothing is not a pass.
//
// THE CONTRACT. gotestrun runs `go test -json` with the given arguments,
// writes go test's own text output through unchanged, and then exits non-zero
// when ANY of these holds, naming which:
//
//   - go test exited non-zero (gotestrun exits with the same status);
//   - no test ran at all (no "run" event for a test);
//   - every test that ran was skipped (nothing passed);
//   - with -each, any package named on the command line ran no passing test;
//   - any test skipped itself because SCHEMA_SLOW is unset. Each one is named
//     on its own line, "SKIPPED-SLOW <package> <test>", and the run is red
//     unless the caller sets SKIP_SLOW_OK=1: a local "pass" over a selection
//     whose slow tests never ran is not a pass of those tests (2026-11-22).
//   - any other test skipped itself: nothing is ever skipped, so a skip is
//     never read as a pass (each is named on its own line, "SKIPPED
//     <package> <test>").
//
// THE MAKE RECIPES' go test. Every recipe of the make files runs its go test
// through this (20 20 12 61 79 80 81 702 706 708 707 705 703 709 33 98 100 204 250 395 398 399 400 704 701GO_TEST) and 20 20 12 61 79 80 81 702 706 708 707 705 703 709 33 98 100 204 250 395 398 399 400 704 701GO_TEST_RED), Makefile): go test's plain
// output prints nothing for a skipped test, so a recipe that ran it bare
// could read a skip as a pass. -dir runs go test in DIR (the recipe's own
// directory, which go run -C left). The text written is what go test -v
// prints, but that a package's result line ("ok  <pkg>  0.1s") carries
// " (go test -json, read by gotestrun)", so a log of go test's bare plain
// output still reads as one (tools/schema-test's make adapter refuses it).
//
// -v is dropped (-json writes what -v would); with -list the verdict is go
// test's exit status, since a list runs no test.
//
// -red is for a negative control's go test, the run the control expects to
// go red: it exits 1 (red) only when a test reported a failure and no test
// skipped. A run in which nothing failed, a test skipped, or no test ran (a
// build that did not compile, a -run that matched nothing) exits 0, so the
// control that wanted red fails loudly instead of reading the skip, or the
// broken build, as its sabotage caught.
//
// -each is for `./...` or several packages when every one of them is expected
// to hold a test; without it the count is over the whole run, which is what a
// -run pattern aimed at one package needs.
//
// Every run is uncached (-count=1 unless the caller names a -count): a cached
// pass is not a run of the test.
//
// A trailing summary line is always printed: "gotestrun: ran N, passed P,
// skipped S, failed F across K package(s)".
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// event is one line of `go test -json`.
type event struct {
	Action  string
	Package string
	Test    string
	Output  string
}

// tally is what a stream of events came to.
type tally struct {
	ran, passed, skipped, failed int
	// perPackage counts the tests that passed, by package, for -each; a
	// package seen in any event is a key.
	perPackage map[string]int
	// slowSkipped names every test ("package test") that skipped with a
	// SCHEMA_SLOW reason, in the order go test reported the skips.
	slowSkipped []string
	// skippedNames names every test ("package test") that skipped.
	skippedNames []string
}

// slowReason is the reason the skip ledger names "slow" (^SCHEMA_SLOW: ), as
// go test prints a t.Skip message: "    file_test.go:16: SCHEMA_SLOW: ...".
var slowReason = regexp.MustCompile(`(?m)^\s*\S+\.go:[0-9]+: SCHEMA_SLOW: `)

// read consumes a `go test -json` stream. Text that is not an event (a
// toolchain message before the first package) is passed to text unchanged.
// Output events are written to text as go test would have printed them.
func read(in io.Reader, text io.Writer) tally {
	t := tally{perPackage: map[string]int{}}
	slow := map[string]bool{} // "package test" -> printed a SCHEMA_SLOW reason
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil || e.Action == "" {
			_, _ = text.Write(append(bytes.Clone(line), '\n')) // errdiscard: best-effort mirror of go test output; tally decides the verdict
			continue
		}
		if e.Package != "" {
			if _, ok := t.perPackage[e.Package]; !ok {
				t.perPackage[e.Package] = 0
			}
		}
		if e.Action == "output" {
			_, _ = io.WriteString(text, markResult(e)) // errdiscard: best-effort mirror of go test output; tally decides the verdict
			if e.Test != "" && slowReason.MatchString(e.Output) {
				slow[e.Package+" "+e.Test] = true
			}
			continue
		}
		if e.Test == "" {
			continue // a package-level verdict is not a test
		}
		switch e.Action {
		case "run":
			t.ran++
		case "pass":
			t.passed++
			t.perPackage[e.Package]++
		case "skip":
			t.skipped++
			t.skippedNames = append(t.skippedNames, e.Package+" "+e.Test)
			if slow[e.Package+" "+e.Test] {
				t.slowSkipped = append(t.slowSkipped, e.Package+" "+e.Test)
			}
		case "fail":
			t.failed++
		}
	}
	return t
}

// verdict decides a finished run from go test's exit status and the tally. It
// returns the exit status and, when that is non-zero for a reason gotestrun
// owns, the reason. A pure function: no clock, no process.
func verdict(exit int, t tally, each, skipSlowOK bool) (int, string) {
	if exit != 0 {
		return exit, fmt.Sprintf("go test exited %d", exit)
	}
	if t.ran == 0 {
		return 1, "go test exited 0 and ran NO test: the -run pattern (or the package) matched nothing, so this is not a pass"
	}
	if t.passed == 0 {
		return 1, fmt.Sprintf("go test exited 0 but no test passed: all %d that ran skipped", t.skipped)
	}
	if each {
		var empty []string
		for p, n := range t.perPackage {
			if n == 0 {
				empty = append(empty, p)
			}
		}
		sort.Strings(empty)
		if len(empty) > 0 {
			return 1, "-each: no test passed in " + strings.Join(empty, ", ")
		}
	}
	if len(t.slowSkipped) > 0 && !skipSlowOK {
		return 1, fmt.Sprintf("SKIPPED-SLOW: %d test(s) of this selection skipped because SCHEMA_SLOW is unset, so they were not run: "+
			"set SCHEMA_SLOW=1 to run them, or SKIP_SLOW_OK=1 to accept the skip by name", len(t.slowSkipped))
	}
	if other := t.skipped - len(t.slowSkipped); other > 0 {
		return 1, fmt.Sprintf("SKIPPED: %d test(s) skipped themselves, and nothing is ever skipped: a skip is no pass", other)
	}
	return 0, ""
}

// redVerdict decides a run a negative control expects to go red: red (1)
// only when a test reported a failure and none skipped; otherwise green (0),
// with why it is no red, so the control fails.
func redVerdict(exit int, t tally) (int, string) {
	switch {
	case t.skipped > 0:
		return 0, fmt.Sprintf("NOT RED: %d test(s) skipped, so the run proved nothing", t.skipped)
	case t.ran == 0:
		return 0, fmt.Sprintf("NOT RED: no test ran (go test exited %d): a build that failed or a -run that matched nothing is no red a test gave", exit)
	case t.failed == 0:
		return 0, fmt.Sprintf("NOT RED: no test failed (go test exited %d)", exit)
	}
	return 1, ""
}

// resultLine is a package's result line in go test's plain output.
var resultLine = regexp.MustCompile(`^(ok|FAIL|\?)\s+\S+\s+(\d+(\.\d+)?s|\(cached\)|\[no test files\])`)

// markResult is an output event's text, a package result line marked as
// read from -json.
func markResult(e event) string {
	if e.Test != "" || !resultLine.MatchString(e.Output) {
		return e.Output
	}
	return strings.TrimSuffix(e.Output, "\n") + " (go test -json, read by gotestrun)\n"
}

func summary(t tally) string {
	return fmt.Sprintf("gotestrun: ran %d, passed %d, skipped %d, failed %d across %d package(s)",
		t.ran, t.passed, t.skipped, t.failed, len(t.perPackage))
}

func run(args []string, stdout, stderr io.Writer) int {
	each, red, dir := false, false, ""
	for len(args) > 0 {
		switch {
		case args[0] == "-each":
			each, args = true, args[1:]
			continue
		case args[0] == "-red":
			red, args = true, args[1:]
			continue
		case args[0] == "-dir" && len(args) > 1:
			dir, args = args[1], args[2:]
			continue
		}
		break
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: gotestrun [-each] [-red] [-dir DIR] <go test arguments, e.g. -run '^TestX$' ./pkg>")
		return 2
	}
	// -v is what -json writes already; -list lists and runs nothing, so its
	// verdict is go test's own exit status
	list := false
	kept := args[:0:0]
	for _, a := range args {
		if a == "-json" || a == "-c" || strings.HasPrefix(a, "-exec") || a == "-n" {
			_, _ = fmt.Fprintf(stderr, "gotestrun: %s is not for this wrapper (it already runs go test -json)\n", a)
			return 2
		}
		if a == "-v" {
			continue
		}
		if a == "-list" || strings.HasPrefix(a, "-list=") {
			list = true
		}
		kept = append(kept, a)
	}
	args = kept
	// a cached pass is not a run: a test that hands its input to a subprocess
	// is not keyed on that input, so without -count=1 a stale "ok (cached)"
	// stands after the input changed. A -count the caller named is kept.
	if !slices.ContainsFunc(args, func(a string) bool {
		n, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		return strings.HasPrefix(a, "-") && n == "count"
	}) {
		args = append([]string{"-count=1"}, args...)
	}
	cmd := exec.Command("go", append([]string{"test", "-json"}, args...)...)
	cmd.Dir = dir
	cmd.Stderr = stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "gotestrun: %v\n", err)
		return 2
	}
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(stderr, "gotestrun: %v\n", err)
		return 2
	}
	t := read(pipe, stdout)
	exit := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			_, _ = fmt.Fprintf(stderr, "gotestrun: %v\n", err)
			return 2
		}
		exit = ee.ExitCode()
		if exit < 0 {
			exit = 1
		}
	}
	code, why := verdict(exit, t, each, os.Getenv("SKIP_SLOW_OK") == "1")
	if red {
		code, why = redVerdict(exit, t)
	}
	if list {
		code, why = exit, ""
		if exit != 0 {
			why = fmt.Sprintf("go test -list exited %d", exit)
		}
	}
	for _, n := range t.skippedNames {
		_, _ = fmt.Fprintf(stderr, "SKIPPED %s\n", n)
	}
	for _, n := range t.slowSkipped {
		_, _ = fmt.Fprintf(stderr, "SKIPPED-SLOW %s\n", n)
	}
	_, _ = fmt.Fprintln(stderr, summary(t))
	if why != "" {
		_, _ = fmt.Fprintf(stderr, "gotestrun: REFUSED: %s\n", why)
	}
	return code
}
