package main

// THE GO TEST SHIM: NIGHTLY RUNS HAVE NO TEST TIMEOUT.
//
// The owner's rule, 2026-11-16: "Nightly runs don't have a timeout... they can
// run as long as they need." A nightly row used to die at go test's ten-minute
// default (or a recipe's own -timeout 600s) with no per-test verdict, which hid
// nine red tests in internal/codegen/gotable for days. Hundreds of make recipes
// and test helpers spell their own -timeout, and the same recipes serve a
// workstation and the two-minute fast lane, so the nightly lifts the limit at
// the one place every nightly step already passes through: this command.
//
// When PGROUP_GO_TEST_TIMEOUT is set (the nightly workflows set it to 0),
// pgroup puts a directory holding a `go` that is pgroup itself first on the
// step's PATH. Invoked as `go`, pgroup is the shim: `go test` has every
// -timeout it was given removed and `-timeout=<value>` put in their place, and
// every other go command runs untouched. The real toolchain is the `go` the
// step had before (PGROUP_REAL_GO). A `go test` a test starts is rewritten
// the same way, because it finds the shim on the PATH it inherits.
//
// When PGROUP_GO_TEST_JSON names a directory (with the timeout lifted or,
// in the fast lane, with every go test keeping the -timeout it named), the
// step's outermost `go test` records its `go test -json` event stream there,
// one file per invocation, written line by line, so a row cut short still shows which
// tests reported and which one was running (tools/gotestreport reads them).
// Each record starts with the -timeout the command named (go test's 10m
// default when it named none): the report holds every package to that
// budget AFTER it reported, which is the detection the lifted limit gave.
// The log keeps its text: an invocation that did not ask for -json gets the
// events' own output printed, which is what `go test -v` prints.
//
// PGROUP_GO_TEST_SUMMARY is the same record for jobs whose make recipes grep
// go test's own text (certify's `make test`, where -v-shaped output would
// change what a `--- SKIP` or `--- PASS` grep sees): the output passes
// through byte for byte, and only each package's result line (`ok`/`FAIL`,
// its wall time) is recorded. That is enough for the budget, not for naming
// the test a cut row was running.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	envGoTestTimeout = "PGROUP_GO_TEST_TIMEOUT"
	envGoTestJSON    = "PGROUP_GO_TEST_JSON"
	envGoTestSummary = "PGROUP_GO_TEST_SUMMARY"
	envRealGo        = "PGROUP_REAL_GO"
	envGoTestNested  = "PGROUP_GO_TEST_NESTED"
)

// isGoShim reports whether this process was started as the `go` shim.
func isGoShim(argv0 string) bool {
	base := filepath.Base(argv0)
	return base == "go" || base == "go.exe"
}

// goTestTimeout validates the PGROUP_GO_TEST_TIMEOUT value: "" (no shim), or
// a duration go test accepts, of which 0 means none.
func goTestTimeout(v string) (string, error) {
	if v == "" || v == "0" {
		return v, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return "", fmt.Errorf("%s=%q is not a duration go test accepts (0 is no timeout)", envGoTestTimeout, v)
	}
	return v, nil
}

// testArgs finds the `test` subcommand in a go command line, past the one
// global flag that may precede it (-C dir). It returns the index of `test`,
// or -1 when the command is not `go test`.
func testArgs(args []string) int {
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-C" || a == "--C":
			i += 2
			continue
		case strings.HasPrefix(a, "-C=") || strings.HasPrefix(a, "--C="):
			i++
			continue
		}
		break
	}
	if i < len(args) && args[i] == "test" {
		return i
	}
	return -1
}

// isTimeoutFlag reports whether a is a -timeout flag go test passes on, and
// whether its value is the next argument.
func isTimeoutFlag(a string) (match, takesNext bool) {
	name := strings.TrimLeft(a, "-")
	if len(a)-len(name) < 1 || len(a)-len(name) > 2 {
		return false, false
	}
	for _, n := range []string{"timeout", "test.timeout"} {
		if name == n {
			return true, true
		}
		if strings.HasPrefix(name, n+"=") {
			return true, false
		}
	}
	return false, false
}

// leadingC is how many arguments right after `test` are its -C dir flag,
// which go requires to be the first flag on the command line (0, 1 or 2).
func leadingC(args []string, at int) int {
	if at+1 >= len(args) {
		return 0
	}
	switch a := args[at+1]; {
	case a == "-C" || a == "--C":
		return 2
	case strings.HasPrefix(a, "-C=") || strings.HasPrefix(a, "--C="):
		return 1
	}
	return 0
}

// isBinaryTimeoutFlag reports whether a, after -args, is the test binary's
// own -test.timeout (which go test's -timeout becomes, and which a later one
// overrides), and whether its value is the next argument. A bare -timeout
// after -args is a flag of the test's own and is not this.
func isBinaryTimeoutFlag(a string) (match, takesNext bool) {
	if m, next := isTimeoutFlag(a); m && strings.Contains(a, "test.timeout") {
		return true, next
	}
	return false, false
}

// rewriteGoTest returns the go command line with every -timeout of a `go
// test` removed and -timeout=<timeout> put right after `test` (after its -C
// dir, which must stay first). After -args the arguments belong to the test
// binary and are copied as they are, except -test.timeout, which would
// override the lifted one there and is removed too. A command that is not
// `go test` comes back unchanged.
func rewriteGoTest(args []string, timeout string) []string {
	at := testArgs(args)
	if at < 0 {
		return args
	}
	c := leadingC(args, at)
	if at+1+c > len(args) {
		return args // a -C with no dir: go reports it
	}
	out := append([]string{}, args[:at+1+c]...)
	out = append(out, "-timeout="+timeout)
	binary := false
	for i := at + 1 + c; i < len(args); i++ {
		a := args[i]
		if !binary && (a == "-args" || a == "--args") {
			binary = true
			out = append(out, a)
			continue
		}
		m, next := isTimeoutFlag(a)
		if binary {
			m, next = isBinaryTimeoutFlag(a)
		}
		if m {
			if next {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// namedTimeout is the -timeout a go test command line asked for (the last
// one, as go test reads it), or "10m", go test's default, when it named
// none. It is what the shim lifted, so the report can hold the package to it
// after the fact.
func namedTimeout(args []string) string {
	at := testArgs(args)
	if at < 0 {
		return ""
	}
	named := "10m"
	binary := false
	for i := at + 1 + leadingC(args, at); i < len(args); i++ {
		a := args[i]
		if !binary && (a == "-args" || a == "--args") {
			binary = true
			continue
		}
		m, next := isTimeoutFlag(a)
		if binary {
			m, next = isBinaryTimeoutFlag(a)
		}
		if m {
			if next {
				if i+1 < len(args) {
					named = args[i+1]
				}
				i++
			} else {
				_, v, _ := strings.Cut(a, "=")
				named = v
			}
		}
	}
	return named
}

// budgetLine is the first line of a record: the -timeout the invocation
// named, which tools/gotestreport holds each package to once it reported.
func budgetLine(named string) []byte {
	b, _ := json.Marshal(struct{ Action, Budget string }{"budget", named}) // errdiscard: two strings always marshal
	return append(b, '\n')
}

// testFlag reports whether the go test command line (before -args) carries
// one of the named boolean flags.
func testFlag(args []string, names ...string) bool {
	at := testArgs(args)
	if at < 0 {
		return false
	}
	from := min(at+1+leadingC(args, at), len(args))
	for _, a := range args[from:] {
		if a == "-args" || a == "--args" {
			return false
		}
		name := strings.TrimLeft(a, "-")
		name = strings.TrimSuffix(name, "=true")
		if slices.Contains(names, name) {
			return true
		}
	}
	return false
}

// withJSON inserts -json after `test` (after its -C dir, which must stay
// first) when the command line lacks it.
func withJSON(args []string) []string {
	if testFlag(args, "json") {
		return args
	}
	at := testArgs(args)
	c := leadingC(args, at)
	if at+1+c > len(args) {
		return args
	}
	out := append([]string{}, args[:at+1+c]...)
	out = append(out, "-json")
	return append(out, args[at+1+c:]...)
}

// renderEvents copies a go test -json stream from r to the record, line by
// line, and to stdout: as it came when the caller asked for -json, otherwise as
// the text the events carry (what `go test -v` prints).
func renderEvents(r io.Reader, record io.Writer, stdout io.Writer, raw bool) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if record != nil {
				if _, werr := record.Write(line); werr != nil {
					record = nil // a full disk loses the record, never the run
				}
			}
			if raw {
				_, _ = stdout.Write(line)
			} else {
				var ev struct {
					Action string
					Output *string
				}
				trimmed := bytes.TrimSpace(line)
				if json.Unmarshal(trimmed, &ev) == nil && ev.Action != "" {
					if ev.Output != nil && (ev.Action == "output" || ev.Action == "build-output") {
						_, _ = io.WriteString(stdout, *ev.Output)
					}
				} else {
					_, _ = stdout.Write(line)
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// realGo is the toolchain the shim stands in front of: PGROUP_REAL_GO, or,
// when a test's environment dropped it, the first `go` on PATH that is not
// this program.
func realGo() (string, error) {
	if p := os.Getenv(envRealGo); p != "" {
		return p, nil
	}
	// without its own path the shim cannot step over itself on PATH, and
	// would run itself in a loop
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("the go shim cannot find its own executable: %w", err)
	}
	selfReal, err := filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("the go shim cannot resolve its own executable %s: %w", self, err)
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		cand := filepath.Join(dir, name)
		st, err := os.Stat(cand)
		if err != nil || st.IsDir() {
			continue
		}
		if r, err := filepath.EvalSymlinks(cand); err == nil && r == selfReal {
			continue
		}
		return cand, nil
	}
	return "", errors.New("no go toolchain on PATH behind the pgroup go shim")
}

// goShim runs the real go with the rewritten command line and returns its
// exit status.
func goShim(args []string) int {
	real, err := realGo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pgroup go shim: %v\n", err)
		return 2
	}
	timeout := os.Getenv(envGoTestTimeout)
	if _, err := goTestTimeout(timeout); err != nil {
		fmt.Fprintf(os.Stderr, "pgroup go shim: %v\n", err)
		return 2
	}
	isTest := testArgs(args) >= 0
	named := namedTimeout(args)
	if timeout != "" {
		args = rewriteGoTest(args, timeout)
	}
	dir, summary := os.Getenv(envGoTestJSON), false
	if dir == "" {
		dir, summary = os.Getenv(envGoTestSummary), true
	}
	if !isTest || dir == "" || os.Getenv(envGoTestNested) != "" || testFlag(args, "c", "h", "help") {
		env := os.Environ()
		if isTest {
			env = append(env, envGoTestNested+"=1")
		}
		return execGo(real, args, env)
	}
	return recordGoTest(real, args, dir, named, summary)
}

// summaryLine is a package's result line in go test's text output: its
// verdict and its test binary's wall time.
var summaryLine = regexp.MustCompile(`^(ok|FAIL)\s+(\S+)\s+([0-9]+(?:\.[0-9]+)?)s\b`)

// summarizeLines copies go test's text output from r to stdout byte for byte and
// records, for each package result line, a pass or fail event carrying the
// package's wall time: what tools/gotestreport needs to hold the package to
// its named budget. A caller that asked for -json gets its events recorded
// as they are.
func summarizeLines(r io.Reader, record io.Writer, stdout io.Writer, raw bool) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			_, _ = stdout.Write(line)
			if record != nil {
				var rec []byte
				if raw {
					rec = line
				} else if m := summaryLine.FindSubmatch(bytes.TrimRight(line, "\r\n")); m != nil {
					secs, _ := strconv.ParseFloat(string(m[3]), 64) // errdiscard: summaryLine matched a decimal number
					action := "pass"
					if string(m[1]) == "FAIL" {
						action = "fail"
					}
					rec, _ = json.Marshal(struct { // errdiscard: two strings and a finite float always marshal
						Action, Package string
						Elapsed         float64
					}{action, string(m[2]), secs})
					rec = append(rec, '\n')
				}
				if rec != nil {
					if _, werr := record.Write(rec); werr != nil {
						record = nil // a full disk loses the record, never the run
					}
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// recordGoTest runs the outermost go test, records under dir (after a first
// line naming the -timeout the command asked for) and prints its output as
// the caller asked for it. In full mode the run is switched to -json and
// every event is recorded; in summary mode the output is left as it is and
// only each package's verdict and wall time are recorded.
func recordGoTest(real string, args []string, dir, named string, summary bool) int {
	raw := testFlag(args, "json")
	if !summary {
		args = withJSON(args)
	}
	// A RECORD THAT COULD NOT BE WRITTEN WHOLE NEVER STOPS THE RUN, AND NEVER
	// PASSES: the tests run to the end, and then the shim's status is red
	// naming the record, because tools/gotestreport would read a record cut
	// short (a full disk) as every skip and failure there was.
	var record *os.File
	lost := lostRecord{path: dir}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		name := fmt.Sprintf("%d-%d.jsonl", time.Now().UnixNano(), os.Getpid())
		lost.path = filepath.Join(dir, name)
		record, err = os.OpenFile(lost.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			lost.err = err
		} else {
			lost.w = record
			_, _ = lost.Write(budgetLine(named)) // errdiscard: lostRecord keeps the error and reports it below
		}
	} else {
		lost.err = err
	}
	if lost.err != nil {
		fmt.Fprintf(os.Stderr, "pgroup go shim: cannot record go test events: %v\n", lost.err)
	}
	cmd := exec.Command(real, args...)
	cmd.Env = append(os.Environ(), envGoTestNested+"=1")
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pgroup go shim: %v\n", err)
		return 2
	}
	stop := holdSignals()
	defer stop()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "pgroup go shim: %v\n", err)
		return 127
	}
	var w io.Writer
	if record != nil {
		w = &lost
	}
	var copyErr error
	if summary {
		copyErr = summarizeLines(out, w, os.Stdout, raw)
	} else {
		copyErr = renderEvents(out, w, os.Stdout, raw)
	}
	status := shimStatus(cmd, cmd.Wait())
	if record != nil {
		if err := record.Close(); err != nil && lost.err == nil {
			lost.err = err
		}
	}
	if copyErr != nil && lost.err == nil {
		// go test's output stopped being read part-way: the record and the
		// log both end there
		lost.err = fmt.Errorf("reading go test's output: %w", copyErr)
	}
	if lost.err != nil {
		fmt.Fprintf(os.Stderr, "pgroup go shim: the go test record %s is not whole, so nothing may be read from it as the run: %v\n", lost.path, lost.err)
		if status == 0 {
			return 2
		}
	}
	return status
}

// lostRecord writes the record until a write fails, then drops the rest and
// keeps the first error, so a full disk never interrupts the tests and the
// shim still answers red at the end.
type lostRecord struct {
	w    io.Writer
	path string
	err  error
}

func (l *lostRecord) Write(p []byte) (int, error) {
	if l.err == nil && l.w != nil {
		if _, err := l.w.Write(p); err != nil {
			l.err = err
		}
	}
	return len(p), nil
}

// shimWanted reports whether the step needs the go shim on its PATH: to lift
// the test timeout (PGROUP_GO_TEST_TIMEOUT), or to record what its go test
// ran (PGROUP_GO_TEST_JSON, PGROUP_GO_TEST_SUMMARY). The record does not need the
// lift: a fast lane keeps the -timeout its go test named and is still read by
// tools/gotestreport, skip by skip.
func shimWanted(timeout, jsonDir, summaryDir string) bool {
	return timeout != "" || jsonDir != "" || summaryDir != ""
}

// prepareGoShim puts the shim first on this process's PATH (so the command
// pgroup runs inherits it) when PGROUP_GO_TEST_TIMEOUT, PGROUP_GO_TEST_JSON
// or PGROUP_GO_TEST_SUMMARY is set. It returns the cleanup to run when the
// command is done.
func prepareGoShim() (func(), error) {
	timeout, err := goTestTimeout(os.Getenv(envGoTestTimeout))
	if err != nil {
		return func() {}, err
	}
	if !shimWanted(timeout, os.Getenv(envGoTestJSON), os.Getenv(envGoTestSummary)) {
		return func() {}, nil
	}
	if os.Getenv(envRealGo) == "" {
		goPath, err := exec.LookPath("go")
		if err != nil {
			return func() {}, fmt.Errorf("the go test shim was asked for (%s, %s or %s) but no go is on PATH: %w", envGoTestTimeout, envGoTestJSON, envGoTestSummary, err)
		}
		if goPath, err = filepath.Abs(goPath); err != nil {
			return func() {}, err
		}
		if err := os.Setenv(envRealGo, goPath); err != nil {
			return func() {}, err
		}
	}
	self, err := os.Executable()
	if err != nil {
		return func() {}, err
	}
	dir, err := os.MkdirTemp("", "pgroup-go-")
	if err != nil {
		return func() {}, err
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	if err := linkOrCopy(self, filepath.Join(dir, name)); err != nil {
		return func() {}, errors.Join(err, os.RemoveAll(dir))
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return func() {}, errors.Join(err, os.RemoveAll(dir))
	}
	return func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "pgroup: the go shim's directory %s was not removed: %v\n", dir, err)
		}
	}, nil
}

func linkOrCopy(src, dst string) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
