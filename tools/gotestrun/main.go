// Command gotestrun runs `go test` and refuses a run that did not run a test.
//
//	go run ./tools/gotestrun [-each] [-red] [-dir DIR] [go test arguments...]
//	make test-run PKG=./internal/foo RUN='^TestBar$'
//
// WHY. `go test -run <pattern>` that matches no test prints "ok" and exits 0
// ("[no tests to run]"), so a typo in a pattern (-run Idcap for a test named
// TestIDCap) looks exactly like a pass. The same goes for a package with no
// test files, and for a run where a test skipped itself. A pass that executed
// nothing is not a pass, and a skipped test is a test that did not run.
//
// THE CONTRACT. gotestrun runs `go test -json` with the given arguments,
// writes go test's own text output through unchanged, and then exits non-zero
// when ANY of these holds, naming which:
//
//   - go test exited non-zero (gotestrun exits with the same status);
//   - no test ran at all (no "run" event for a test);
//   - with -each, any package named on the command line ran no passing test;
//   - any test skipped itself, for any reason. Each one is named on its own
//     line, "SKIPPED <package> <test>", and the run is red: there is no
//     opt-out. A test that is not to run is left out at the root (the one
//     driver's query, tools/schema-test), never skipped below it.
//
// THE MAKE RECIPES' go test. Every recipe of the make files runs its go test
// through this ($(GO_TEST) and $(GO_TEST_RED), Makefile): go test's plain
// output prints nothing for a skipped test, so a recipe that ran it bare
// could read a skip as a pass. -dir runs go test in DIR (the recipe's own
// directory; the wrapper builds gotestrun in the checkout's root). Under -red,
// gotestrun failing before a verdict is exit 0, NOT RED, since a control reads
// any non-zero exit as its sabotage caught. The text written is what go test -v
// prints, each package's output whole and in the order the packages started
// (go test -json interleaves packages that run at once; its plain output does
// not), but that a package's result line ("ok  <pkg>  0.1s") carries
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
// failed F across K package(s)", with ", skipped S" after P when S > 0.
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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
	// skippedTests names every test ("package test") that skipped, in the
	// order go test reported the skips.
	skippedTests []string
	// subRuns counts the subtests that ran under each top-level test
	// ("package test"); seedless names every fuzz target that passed having
	// run no seed input (each seed is a subtest), so tested nothing.
	subRuns  map[string]int
	seedless []string
	// readErr is why the stream could not be read to its end, or nil.
	readErr error
	// missing is the first output line saying a test lacked its toolchain or
	// pin (missingTool), or "".
	missing string
}

// missingTool is a test failing because what it runs is absent, not because
// what it checks is wrong: internal/slowtest.Missing's words, and the pinned
// interpreters' (pyrun, rubyrun, luarun). A negative control red for this
// reason caught no sabotage.
var missingTool = regexp.MustCompile(regexp.QuoteMeta(slowtest.MissingWords) + `|does not resolve: \S+ is absent`)

// read consumes a `go test -json` stream. Text that is not an event (a
// toolchain message before the first package) is passed to text unchanged.
// Output events are written to text as go test would have printed them, each
// package's output whole and ending in its result line, in the order the
// packages started: go test -json interleaves the events of packages that run
// at once, where its plain output does not, and a reader of the log (the make
// adapter's go_tests check, tools/schema-test) credits a --- PASS line to the
// package whose result line follows it. The first unfinished package's
// output streams; a later one's is held until every package before it ends.
func read(in io.Reader, text io.Writer) tally {
	t := tally{perPackage: map[string]int{}, subRuns: map[string]int{}}
	out := packageOrder{text: text, held: map[string]*bytes.Buffer{}, done: map[string]bool{}}
	defer out.flush()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil || e.Action == "" {
			_, _ = text.Write(append(bytes.Clone(line), '\n')) // errdiscard: best-effort mirror of go test output; tally decides the verdict
			continue
		}
		if e.Action == "build-output" {
			// a build's own text (the compiler's errors) comes as
			// build-output with an ImportPath and no Package: it is the
			// named cause of the package's [build failed], so it is written
			out.write("", e.Output)
			continue
		}
		if e.Package != "" {
			if _, ok := t.perPackage[e.Package]; !ok {
				t.perPackage[e.Package] = 0
			}
			out.see(e.Package)
		}
		if e.Action == "output" {
			if t.missing == "" && missingTool.MatchString(e.Output) {
				t.missing = strings.TrimSpace(e.Output)
			}
			out.write(e.Package, markResult(e))
			continue
		}
		if e.Test == "" {
			if e.Action == "pass" || e.Action == "fail" || e.Action == "skip" {
				out.end(e.Package)
			}
			continue // a package-level verdict is not a test
		}
		top, _, isSub := strings.Cut(e.Test, "/")
		switch e.Action {
		case "run":
			t.ran++
			if isSub {
				t.subRuns[e.Package+" "+top]++
			}
		case "pass":
			t.passed++
			t.perPackage[e.Package]++
			if !isSub && strings.HasPrefix(top, "Fuzz") && t.subRuns[e.Package+" "+top] == 0 {
				t.seedless = append(t.seedless, e.Package+" "+top)
			}
		case "skip":
			t.skipped++
			t.skippedTests = append(t.skippedTests, e.Package+" "+e.Test)
		case "fail":
			t.failed++
		}
	}
	t.readErr = sc.Err()
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
	if t.skipped > 0 {
		return 1, fmt.Sprintf("SKIPPED: %d test(s) of this selection skipped themselves, so they did not run; a skip is a failure, and a test that is not to run is left out at the root", t.skipped)
	}
	if len(t.seedless) > 0 {
		return 1, fmt.Sprintf("NO SEED: fuzz target(s) ran no seed input (no f.Add and no testdata/fuzz corpus), so they tested nothing: %s", strings.Join(t.seedless, ", "))
	}
	if t.passed == 0 {
		return 1, "go test exited 0 but no test passed"
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

// redVerdict decides a run a negative control expects to go red: red (1)
// only when a test reported a failure, none skipped and none lacked its
// toolchain; otherwise green (0), with why it is no red, so the control fails.
func redVerdict(exit int, t tally) (int, string) {
	switch {
	case t.skipped > 0:
		return 0, fmt.Sprintf("NOT RED: %d test(s) skipped, so the run proved nothing", t.skipped)
	case t.missing != "":
		return 0, fmt.Sprintf("NOT RED: a test lacked what it runs, so its red is no sabotage caught: %s", t.missing)
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

// summary is the run's last line. A count of skips is in it only when some
// test skipped: the harnesses that read this log (testreg.RefuseSkips) refuse
// any line that reports a skip, and "skipped 0" reports none, so it is not
// written; "skipped 3" is, and is refused.
func summary(t tally) string {
	skipped := ""
	if t.skipped > 0 {
		skipped = fmt.Sprintf(", skipped %d", t.skipped)
	}
	return fmt.Sprintf("gotestrun: ran %d, passed %d%s, failed %d across %d package(s)",
		t.ran, t.passed, skipped, t.failed, len(t.perPackage))
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
	// broken is gotestrun failing before a verdict. Under -red it is exit 0,
	// NOT RED: a control reads any non-zero exit as its sabotage caught, so a
	// wrapper that could not run must never look like one.
	broken := func(format string, a ...any) int {
		msg := fmt.Sprintf(format, a...)
		if red {
			_, _ = fmt.Fprintf(stderr, "gotestrun: NOT RED: gotestrun could not run, so nothing was proved: %s\n", msg)
			return 0
		}
		_, _ = fmt.Fprintf(stderr, "gotestrun: %s\n", msg)
		return 2
	}
	if len(args) == 0 {
		return broken("usage: gotestrun [-each] [-red] [-dir DIR] <go test arguments, e.g. -run '^TestX$' ./pkg>")
	}
	// -v is what -json writes already; -list lists and runs nothing, so its
	// verdict is go test's own exit status
	list := false
	kept := args[:0:0]
	for _, a := range args {
		if a == "-json" || a == "-c" || strings.HasPrefix(a, "-exec") || a == "-n" {
			return broken("%s is not for this wrapper (it already runs go test -json)", a)
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
		return broken("%v", err)
	}
	if err := cmd.Start(); err != nil {
		return broken("%v", err)
	}
	t := read(pipe, stdout)
	if t.readErr != nil {
		// what go test still writes is drained, so it is not left blocked on
		// a pipe nobody reads
		_, _ = io.Copy(io.Discard, pipe) // errdiscard: the run is already broken; this only lets go test exit
		_ = cmd.Wait()                   // errdiscard: as above
		return broken("reading go test -json: %v", t.readErr)
	}
	exit := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return broken("%v", err)
		}
		exit = ee.ExitCode()
		if exit < 0 {
			exit = 1
		}
	}
	code, why := verdict(exit, t, each)
	if red {
		code, why = redVerdict(exit, t)
	}
	if list {
		code, why = exit, ""
		if exit != 0 {
			why = fmt.Sprintf("go test -list exited %d", exit)
		}
	}
	for _, n := range t.skippedTests {
		_, _ = fmt.Fprintf(stderr, "SKIPPED %s\n", n)
	}
	_, _ = fmt.Fprintln(stderr, summary(t))
	if why != "" {
		_, _ = fmt.Fprintf(stderr, "gotestrun: REFUSED: %s\n", why)
	}
	return code
}

// packageOrder writes each package's output whole, in the order the packages
// started: the first unfinished package's output goes straight to text, and
// a later package's is held until every package started before it has ended.
type packageOrder struct {
	text  io.Writer
	order []string                 // packages, in the order first seen
	at    map[string]int           // each package's place in order
	head  int                      // order[head] is the package streaming now
	held  map[string]*bytes.Buffer // output of packages behind the head
	done  map[string]bool          // packages whose verdict event came
}

// see records a package the first time an event names it.
func (o *packageOrder) see(pkg string) {
	if _, ok := o.held[pkg]; ok {
		return
	}
	if o.at == nil {
		o.at = map[string]int{}
	}
	o.at[pkg] = len(o.order)
	o.held[pkg] = &bytes.Buffer{}
	o.order = append(o.order, pkg)
}

// write writes one package's output, or holds it behind the head. Output with
// no package, of the head, or of a package before the head (one already
// written out) goes straight through; a package behind the head that ended
// keeps its place.
func (o *packageOrder) write(pkg, s string) {
	if pkg != "" {
		o.see(pkg)
	}
	if pkg == "" || o.at[pkg] <= o.head {
		_, _ = io.WriteString(o.text, s) // errdiscard: best-effort mirror of go test output; tally decides the verdict
		return
	}
	o.held[pkg].WriteString(s)
}

// end marks a package finished; when it is the head, the packages behind it
// are written out in order until one that has not finished, which streams.
func (o *packageOrder) end(pkg string) {
	o.done[pkg] = true
	for o.head < len(o.order) && o.done[o.order[o.head]] {
		o.head++
		if o.head < len(o.order) {
			o.release(o.order[o.head])
		}
	}
}

// release writes a package's held output.
func (o *packageOrder) release(pkg string) {
	_, _ = o.text.Write(o.held[pkg].Bytes()) // errdiscard: best-effort mirror of go test output; tally decides the verdict
	o.held[pkg].Reset()
}

// flush writes whatever is still held, in order, when the stream ends.
func (o *packageOrder) flush() {
	for ; o.head < len(o.order); o.head++ {
		o.release(o.order[o.head])
	}
}
