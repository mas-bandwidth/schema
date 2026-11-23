// Command gotestreport reads the `go test -json` event streams a nightly row
// recorded (tools/pgroup's PGROUP_GO_TEST_JSON) and says, after the tests have
// all reported or the row was cut short, what every test package did.
//
//	go run ./tools/gotestreport [-budget 10m] [-slowest 15] <dir>...
//
// It prints, per package, its verdict and wall time; every failed test; every
// test that started and never reported (the row was killed while it ran); and
// the slowest tests, so the next split of a slow row is read off the log.
//
// It exits 1 when a test or a package started and never reported. A package
// whose test binary ran longer than its budget is NAMED (OVER BUDGET) and is
// not a failure: a nightly has no time limit (the owner, 2026-11-16 and
// 2026-11-21; the 2-minute per-commit limit is tools/pgroup's PGROUP_WALL, not
// this report's). The budget is the -timeout its go test command named, which
// pgroup's shim lifted and wrote as the record's first line (go test's own 10m
// default when the command named none; -budget for a record without that
// line; 0 is none). A test failure is the test step's verdict, not this
// report's, so it is listed and not counted twice. A directory with no record
// says so and is not an error: a row whose steps ran no `go test` has nothing
// to report. With -require-record it is red, and so is a record file that
// holds no go test event (empty, or the budget line alone): a recorder that
// started and recorded no run is never read as "no failures, no skips".
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type event struct {
	Action  string
	Package string
	Test    string
	Elapsed float64
	Budget  string
	Output  string
}

// key names a package (test "") or a test within ONE record: the same
// package run by two go test commands in a row (the Makefile runs ./compiler/
// twice, the conformance harness up to three times) is two keys, so a run cut
// short in one record is never covered by another record that finished.
type key struct {
	rec       int
	pkg, test string
}

type result struct {
	verdict  string // pass, fail, skip, or "" when its latest run never reported
	elapsed  time.Duration
	finished bool          // its latest start/run reported
	failed   bool          // some run of it failed, even when a later run passed
	budget   time.Duration // 0: none
	out      []byte        // a test's own output since its latest run, kept until it passes or fails (skips.go)
}

type report struct {
	packages map[key]*result
	tests    map[key]*result
	records  int
	// skips is every skip a test reported, in order: a later run of the same
	// test (-count=2) replaces its result and never its earlier skip
	skips []skipped
	// events counts each record's package events: a record with none (an
	// empty file, the recorder's budget line alone) recorded no run
	events []int
}

func newReport() *report {
	return &report{packages: map[key]*result{}, tests: map[key]*result{}}
}

// parseBudget reads a go test -timeout value; 0 or unreadable is no budget.
func parseBudget(s string) time.Duration {
	if s == "" || s == "0" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// add folds one recorded stream into the report. budget holds its packages
// until a budget line says otherwise. A line that is not an event (a cut
// last line, a stray print) is skipped.
func (r *report) add(rd io.Reader, budget time.Duration) error {
	r.records++
	r.events = append(r.events, 0)
	br := bufio.NewReader(rd)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var ev event
			if json.Unmarshal(line, &ev) == nil {
				switch {
				case ev.Action == "budget":
					budget = parseBudget(ev.Budget)
				case ev.Action != "" && ev.Package != "":
					r.event(ev, budget)
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

func (r *report) event(ev event, budget time.Duration) {
	k := key{r.records, ev.Package, ev.Test}
	switch ev.Action {
	case "start", "run", "pass", "fail", "skip":
		r.events[len(r.events)-1]++
	case "output":
		// a test's own lines are where a skip says why (skips.go); a line is
		// kept only for a test that is running, and only its last
		// maxSkipOutput bytes: t.Skip prints its reason at the END of what
		// the test logged, so a test that logs 70 KB and then skips keeps
		// the reason, not the head of its log
		if res := r.tests[k]; ev.Test != "" && res != nil && !res.finished {
			res.out = append(res.out, ev.Output...)
			if len(res.out) > 2*maxSkipOutput {
				res.out = append([]byte(nil), tail(res.out)...)
			}
		}
		return
	default:
		return // pause, cont, bench: no verdict in them
	}
	m := r.tests
	if ev.Test == "" {
		m = r.packages
	}
	res := m[k]
	if res == nil {
		res = &result{}
		m[k] = res
	}
	if ev.Test == "" {
		res.budget = budget
	}
	switch ev.Action {
	case "start", "run":
		// A new run (-count>1, or a package started again in this record)
		// owes its own verdict: what an earlier run reported does not stand
		// for it.
		res.verdict, res.finished, res.elapsed, res.out = "", false, 0, nil
	case "pass", "fail", "skip":
		res.verdict = ev.Action
		res.finished = true
		res.elapsed = time.Duration(ev.Elapsed * float64(time.Second))
		if ev.Action == "fail" {
			res.failed = true
		}
		if ev.Action == "skip" && ev.Test != "" {
			r.skips = append(r.skips, skipped{k, tail(res.out)})
		}
		res.out = nil // a skip's output is kept with the skip; nothing else is read
	}
}

func sortedKeys(m map[key]*result) []key {
	out := make([]key, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.pkg != b.pkg {
			return a.pkg < b.pkg
		}
		if a.test != b.test {
			return a.test < b.test
		}
		return a.rec < b.rec
	})
	return out
}

// name is a package or test as the report prints it, with its record when
// the row ran that package from more than one go test command.
func (r *report) name(k key, multi map[string]bool) string {
	s := k.pkg
	if k.test != "" {
		s += " " + k.test
	}
	if multi[k.pkg] {
		s += fmt.Sprintf(" [go test #%d]", k.rec)
	}
	return s
}

// findings is what the report says and whether the row is red because of it.
type findings struct {
	text string
	red  bool
}

func (r *report) findings(slowest int) findings {
	var b strings.Builder
	red := false

	pkgs := sortedKeys(r.packages)
	tests := sortedKeys(r.tests)
	recs := map[string]map[int]bool{}
	for _, k := range pkgs {
		if recs[k.pkg] == nil {
			recs[k.pkg] = map[int]bool{}
		}
		recs[k.pkg][k.rec] = true
	}
	multi := map[string]bool{}
	for p, rs := range recs {
		multi[p] = len(rs) > 1
	}

	var pass, fail, skip, open int
	for _, k := range tests {
		res := r.tests[k]
		switch {
		case !res.finished:
			open++
		case res.failed:
			fail++
		case res.verdict == "pass":
			pass++
		case res.verdict == "skip":
			skip++
		}
	}
	fmt.Fprintf(&b, "go test report: %d package run(s), %d test(s): %d passed, %d failed, %d skipped, %d never reported\n",
		len(pkgs), len(tests), pass, fail, skip, open)

	for _, k := range pkgs {
		res := r.packages[k]
		if !res.finished {
			fmt.Fprintf(&b, "  NEVER FINISHED  %s: its test binary started and the row ended before it reported\n", r.name(k, multi))
			red = true
			continue
		}
		fmt.Fprintf(&b, "  %-14s  %s (%s)\n", strings.ToUpper(res.verdict), r.name(k, multi), res.elapsed.Round(time.Second))
	}
	for _, k := range tests {
		res := r.tests[k]
		if res.failed {
			fmt.Fprintf(&b, "  FAIL            %s (%s)\n", r.name(k, multi), res.elapsed.Round(time.Millisecond))
		}
		if !res.finished {
			fmt.Fprintf(&b, "  NEVER REPORTED  %s: it was running when the row ended\n", r.name(k, multi))
			red = true
		}
	}
	for _, k := range pkgs {
		if res := r.packages[k]; res.finished && res.budget > 0 && res.elapsed > res.budget {
			// named, never red: the nightly has no time limit (TestThePackageBudget)
			fmt.Fprintf(&b, "  OVER BUDGET     %s ran %s, past the %s its go test named: every test reported first; not a failure (a nightly has no time limit), find what grew\n",
				r.name(k, multi), res.elapsed.Round(time.Second), res.budget)
		}
	}

	if slowest > 0 {
		done := make([]key, 0, len(tests))
		for _, k := range tests {
			if r.tests[k].finished && !strings.Contains(k.test, "/") {
				done = append(done, k)
			}
		}
		sort.SliceStable(done, func(i, j int) bool { return r.tests[done[i]].elapsed > r.tests[done[j]].elapsed })
		if len(done) > slowest {
			done = done[:slowest]
		}
		if len(done) > 0 {
			fmt.Fprintf(&b, "slowest tests:\n")
			for _, k := range done {
				fmt.Fprintf(&b, "  %10s  %s\n", r.tests[k].elapsed.Round(time.Millisecond), r.name(k, multi))
			}
		}
	}
	return findings{text: b.String(), red: red}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv("GITHUB_STEP_SUMMARY")))
}

// recordFiles is every *.jsonl file in dir, sorted. A directory that does not
// exist holds no record, as Glob answered before; one that cannot be read is
// an error.
func recordFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// appendSummary appends text to the step summary file, which must exist.
func appendSummary(path, text string) error {
	out, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(out, text); err != nil {
		_ = out.Close() // errdiscard: the write's error is the one returned
		return err
	}
	return out.Close()
}

// run is the command: its exit status (0 green, 1 red, 2 a usage or read
// error) from its arguments, what it prints, and the step summary file it
// appends the report to ("" for none). The record corpus (records_test.go)
// runs it as CI does.
func run(args []string, stdout, stderr io.Writer, summary string) int {
	fs := flag.NewFlagSet("gotestreport", flag.ContinueOnError)
	fs.SetOutput(stderr)
	budget := fs.Duration("budget", 10*time.Minute, "the package budget for a record that names none (0: no budget)")
	slowest := fs.Int("slowest", 15, "how many of the slowest tests to list")
	skips := fs.String("skips", "", "the skip ledger (.github/skip-ledger.txt): every skip is read against it, and a skip with no reason, a reason no line names, or a reason this lane satisfies is red")
	provides := fs.String("provides", "", "with -skips: the ledger classes this lane satisfies, space-separated (a lane with elixir installed provides tool:elixir)")
	requireRecord := fs.Bool("require-record", false, "a row with no go test record is red, and so is a record file that holds no go test event (a lane whose step always runs go test)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: gotestreport [-budget 10m] [-slowest 15] [-skips <ledger> [-provides \"<class>...\"]] [-require-record] <dir>...")
		return 2
	}
	var led *ledger
	if *skips != "" {
		f, err := os.Open(*skips)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "gotestreport: %v\n", err)
			return 2
		}
		led, err = readLedger(f, *skips, strings.Fields(*provides))
		_ = f.Close()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "gotestreport: %v\n", err)
			return 2
		}
	} else if *provides != "" {
		_, _ = fmt.Fprintln(stderr, "gotestreport: -provides needs -skips")
		return 2
	}
	r := newReport()
	files := 0
	for _, dir := range fs.Args() {
		if dir == "" {
			// an unset PGROUP_GO_TEST_JSON: no record could have been written,
			// and "no go test ran" would read green
			_, _ = fmt.Fprintln(stderr, "gotestreport: an empty directory was named (is PGROUP_GO_TEST_JSON set?)")
			return 2
		}
		// the directory is listed, never globbed: Glob reads a "[" in its
		// name as a pattern, matches another directory or nothing, and
		// drops the error, and nothing read is "no go test ran"
		matches, err := recordFiles(dir)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "gotestreport: %v\n", err)
			return 2
		}
		for _, m := range matches {
			f, err := os.Open(m)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "gotestreport: %v\n", err)
				return 2
			}
			err = r.add(f, *budget)
			_ = f.Close()
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "gotestreport: %s: %v\n", m, err)
				return 2
			}
			files++
		}
	}
	if files == 0 {
		_, _ = fmt.Fprintf(stdout, "go test report: no go test ran on this row (%s holds no record)\n", strings.Join(fs.Args(), " "))
		if *requireRecord {
			// the step this report follows always runs go test: no record is
			// a recorder that did not record, and nothing was read
			_, _ = fmt.Fprintln(stdout, "  NO RECORD       this lane's go test left no record, so no test and no skip was read")
			return 1
		}
		return 0
	}
	f := r.findings(*slowest)
	if empty := r.emptyRecords(); *requireRecord && len(empty) > 0 {
		// a file is not a record: an empty file, or the recorder's budget
		// line alone, is a recorder that was cut before go test reported
		// one package, and nothing in it can be read as "no skips"
		f.text += fmt.Sprintf("  NO RECORD       %d of %d record file(s) hold no go test event (record #%v): the recorder started and recorded no run\n", len(empty), r.records, empty)
		f.red = true
	}
	if led != nil {
		sf := r.skipFindings(led)
		f.text += sf.text
		f.red = f.red || sf.red
	}
	_, _ = fmt.Fprint(stdout, f.text)
	if summary != "" {
		// a summary the step was told to write and did not is a report
		// nobody sees: say so, and the step is red
		if err := appendSummary(summary, "```\n"+f.text+"```\n"); err != nil {
			_, _ = fmt.Fprintf(stderr, "gotestreport: the step summary %s: %v\n", summary, err)
			return 2
		}
	}
	if f.red {
		return 1
	}
	return 0
}

// tail is the last maxSkipOutput bytes of a test's output, from the start of
// a line: a line cut in the middle is dropped, so its end is never read as an
// entry of its own.
func tail(out []byte) []byte {
	if len(out) <= maxSkipOutput {
		return out
	}
	cut := len(out) - maxSkipOutput
	if out[cut-1] == '\n' {
		return out[cut:]
	}
	if i := bytes.IndexByte(out[cut:], '\n'); i >= 0 {
		return out[cut+i+1:]
	}
	return out[cut:]
}

// emptyRecords are the records (1-based, in the order read) that hold no
// package or test event.
func (r *report) emptyRecords() []int {
	var out []int
	for i, n := range r.events {
		if n == 0 {
			out = append(out, i+1)
		}
	}
	return out
}
