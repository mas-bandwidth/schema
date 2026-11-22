// Command gotestrun runs `go test` and refuses a run that did not run a test.
//
//	go run ./tools/gotestrun [-each] [go test arguments...]
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
//   - with -each, any package named on the command line ran no passing test.
//
// -each is for `./...` or several packages when every one of them is expected
// to hold a test; without it the count is over the whole run, which is what a
// -run pattern aimed at one package needs.
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
}

// read consumes a `go test -json` stream. Text that is not an event (a
// toolchain message before the first package) is passed to text unchanged.
// Output events are written to text as go test would have printed them.
func read(in io.Reader, text io.Writer) tally {
	t := tally{perPackage: map[string]int{}}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil || e.Action == "" {
			_, _ = text.Write(append(bytes.Clone(line), '\n'))
			continue
		}
		if e.Package != "" {
			if _, ok := t.perPackage[e.Package]; !ok {
				t.perPackage[e.Package] = 0
			}
		}
		if e.Action == "output" {
			_, _ = io.WriteString(text, e.Output)
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
		case "fail":
			t.failed++
		}
	}
	return t
}

// verdict decides a finished run from go test's exit status and the tally. It
// returns the exit status and, when that is non-zero for a reason gotestrun
// owns, the reason. A pure function: no clock, no process.
func verdict(exit int, t tally, each bool) (int, string) {
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
	return 0, ""
}

func summary(t tally) string {
	return fmt.Sprintf("gotestrun: ran %d, passed %d, skipped %d, failed %d across %d package(s)",
		t.ran, t.passed, t.skipped, t.failed, len(t.perPackage))
}

func run(args []string, stdout, stderr io.Writer) int {
	each, control := false, false
	for len(args) > 0 && (args[0] == "-each" || args[0] == "-control") {
		if args[0] == "-each" {
			each = true
		} else {
			control = true
		}
		args = args[1:]
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: gotestrun [-each] [-control] <go test arguments, e.g. -run '^TestX$' ./pkg>")
		return 2
	}
	for _, a := range args {
		if a == "-json" || a == "-list" || strings.HasPrefix(a, "-list=") || a == "-c" || strings.HasPrefix(a, "-exec") || a == "-n" {
			_, _ = fmt.Fprintf(stderr, "gotestrun: %s is not for this wrapper (it already runs go test -json)\n", a)
			return 2
		}
	}
	cmd := exec.Command("go", append([]string{"test", "-json"}, args...)...)
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
	code, why := verdict(exit, t, each)
	_, _ = fmt.Fprintln(stderr, summary(t))
	if why != "" {
		_, _ = fmt.Fprintf(stderr, "gotestrun: REFUSED: %s\n", why)
	}
	return controlCode(code, exit, control)
}

// controlCode is the exit status of a -control run. A negative control plants
// a defect and expects go test to go RED, in the shape `if go test ...; then
// echo NEGATIVE CONTROL FAILED; exit 1; fi`: it reads exit 0 as "the planted
// defect stayed green". A refusal of this wrapper (no test ran, or all
// skipped: go test itself exited 0) is not the planted defect going red, so
// under -control it exits 0 too, and the control's own `then` branch fires
// with the REFUSED line in its log, exactly as it did on a bare go test.
// Without this, a mistyped control pattern exits 1 and reads as "caught": a
// silent pass. A real failure (go test exited non-zero: a test went red, or
// the build broke) keeps its status, which is what the control wants.
func controlCode(code, goTestExit int, control bool) int {
	if control && goTestExit == 0 {
		return 0
	}
	return code
}
