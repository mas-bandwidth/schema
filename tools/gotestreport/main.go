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
// It exits 1 when a test or a package started and never reported, or when a
// package's test binary ran longer than its budget. The budget is the
// -timeout its go test command named, which pgroup's shim lifted and wrote as
// the record's first line (go test's own 10m default when the command named
// none; -budget for a record without that line; 0 is none). That is the
// detection the nightly's old limits gave: a hang or a blow-up still turns the
// row red, but only AFTER every test has run and reported, never by cutting
// the run short. A test failure is the test step's verdict, not this
// report's, so it is listed and not counted twice. A directory with no record
// says so and is not an error: a row whose steps ran no `go test` has nothing
// to report.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	case "output":
		// a test's own lines are where a skip says why (skips.go); a line is
		// kept only for a test that is running, and only up to maxSkipOutput
		if res := r.tests[k]; ev.Test != "" && res != nil && !res.finished && len(res.out) < maxSkipOutput {
			res.out = append(res.out, ev.Output...)
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
		if ev.Action != "skip" {
			res.out = nil // only a skip's reason is read
		}
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
			fmt.Fprintf(&b, "  OVER BUDGET     %s ran %s, past the %s its go test named: every test reported first; split the row or find what grew\n",
				r.name(k, multi), res.elapsed.Round(time.Second), res.budget)
			red = true
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
	budget := flag.Duration("budget", 10*time.Minute, "the package budget for a record that names none (0: no budget)")
	slowest := flag.Int("slowest", 15, "how many of the slowest tests to list")
	skips := flag.String("skips", "", "the skip ledger (.github/skip-ledger.txt): every skip is read against it, and a skip with no reason, a reason no line names, or a reason this lane satisfies is red")
	provides := flag.String("provides", "", "with -skips: the ledger classes this lane satisfies, space-separated (a lane with elixir installed provides tool:elixir)")
	requireRecord := flag.Bool("require-record", false, "a row with no go test record is red (a lane whose step always runs go test)")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: gotestreport [-budget 10m] [-slowest 15] [-skips <ledger> [-provides \"<class>...\"]] [-require-record] <dir>...")
		os.Exit(2)
	}
	var led *ledger
	if *skips != "" {
		f, err := os.Open(*skips)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gotestreport: %v\n", err)
			os.Exit(2)
		}
		led, err = readLedger(f, *skips, strings.Fields(*provides))
		_ = f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "gotestreport: %v\n", err)
			os.Exit(2)
		}
	} else if *provides != "" {
		fmt.Fprintln(os.Stderr, "gotestreport: -provides needs -skips")
		os.Exit(2)
	}
	r := newReport()
	files := 0
	for _, dir := range flag.Args() {
		if dir == "" {
			// an unset PGROUP_GO_TEST_JSON: no record could have been written,
			// and "no go test ran" would read green
			fmt.Fprintln(os.Stderr, "gotestreport: an empty directory was named (is PGROUP_GO_TEST_JSON set?)")
			os.Exit(2)
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		sort.Strings(matches)
		for _, m := range matches {
			f, err := os.Open(m)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gotestreport: %v\n", err)
				os.Exit(2)
			}
			err = r.add(f, *budget)
			_ = f.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "gotestreport: %s: %v\n", m, err)
				os.Exit(2)
			}
			files++
		}
	}
	if files == 0 {
		fmt.Printf("go test report: no go test ran on this row (%s holds no record)\n", strings.Join(flag.Args(), " "))
		if *requireRecord {
			// the step this report follows always runs go test: no record is
			// a recorder that did not record, and nothing was read
			fmt.Println("  NO RECORD       this lane's go test left no record, so no test and no skip was read")
			os.Exit(1)
		}
		return
	}
	f := r.findings(*slowest)
	if led != nil {
		sf := r.skipFindings(led)
		f.text += sf.text
		f.red = f.red || sf.red
	}
	fmt.Print(f.text)
	if s := os.Getenv("GITHUB_STEP_SUMMARY"); s != "" {
		if out, err := os.OpenFile(s, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = fmt.Fprintf(out, "```\n%s```\n", f.text)
			_ = out.Close()
		}
	}
	if f.red {
		os.Exit(1)
	}
}
