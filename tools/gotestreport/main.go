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
}

type key struct{ pkg, test string }

type result struct {
	verdict  string // pass, fail, skip, or "" when it never reported
	elapsed  time.Duration
	finished bool
	budget   time.Duration // 0: none
}

type report struct {
	packages map[string]*result
	tests    map[key]*result
}

func newReport() *report {
	return &report{packages: map[string]*result{}, tests: map[key]*result{}}
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
	switch ev.Action {
	case "start", "run", "pass", "fail", "skip":
	default:
		return // output, pause, cont, bench: no verdict in them
	}
	var res *result
	if ev.Test == "" {
		res = r.packages[ev.Package]
		if res == nil {
			res = &result{}
			r.packages[ev.Package] = res
		}
		res.budget = budget
	} else {
		k := key{ev.Package, ev.Test}
		res = r.tests[k]
		if res == nil {
			res = &result{}
			r.tests[k] = res
		}
	}
	switch ev.Action {
	case "pass", "fail", "skip":
		res.verdict = ev.Action
		res.finished = true
		res.elapsed = time.Duration(ev.Elapsed * float64(time.Second))
	}
}

// findings is what the report says and whether the row is red because of it.
type findings struct {
	text string
	red  bool
}

func (r *report) findings(slowest int) findings {
	var b strings.Builder
	red := false

	pkgs := make([]string, 0, len(r.packages))
	for p := range r.packages {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	tests := make([]key, 0, len(r.tests))
	for k := range r.tests {
		tests = append(tests, k)
	}
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].pkg != tests[j].pkg {
			return tests[i].pkg < tests[j].pkg
		}
		return tests[i].test < tests[j].test
	})

	var pass, fail, skip, open int
	for _, k := range tests {
		switch r.tests[k].verdict {
		case "pass":
			pass++
		case "fail":
			fail++
		case "skip":
			skip++
		default:
			open++
		}
	}
	fmt.Fprintf(&b, "go test report: %d package(s), %d test(s): %d passed, %d failed, %d skipped, %d never reported\n",
		len(pkgs), len(tests), pass, fail, skip, open)

	for _, p := range pkgs {
		res := r.packages[p]
		if !res.finished {
			fmt.Fprintf(&b, "  NEVER FINISHED  %s: its test binary started and the row ended before it reported\n", p)
			red = true
			continue
		}
		fmt.Fprintf(&b, "  %-14s  %s (%s)\n", strings.ToUpper(res.verdict), p, res.elapsed.Round(time.Second))
	}
	for _, k := range tests {
		res := r.tests[k]
		switch {
		case res.verdict == "fail":
			fmt.Fprintf(&b, "  FAIL            %s %s (%s)\n", k.pkg, k.test, res.elapsed.Round(time.Millisecond))
		case !res.finished:
			fmt.Fprintf(&b, "  NEVER REPORTED  %s %s: it was running when the row ended\n", k.pkg, k.test)
			red = true
		}
	}
	for _, p := range pkgs {
		if res := r.packages[p]; res.finished && res.budget > 0 && res.elapsed > res.budget {
			fmt.Fprintf(&b, "  OVER BUDGET     %s ran %s, past the %s its go test named: every test reported first; split the row or find what grew\n",
				p, res.elapsed.Round(time.Second), res.budget)
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
				fmt.Fprintf(&b, "  %10s  %s %s\n", r.tests[k].elapsed.Round(time.Millisecond), k.pkg, k.test)
			}
		}
	}
	return findings{text: b.String(), red: red}
}

func main() {
	budget := flag.Duration("budget", 10*time.Minute, "the package budget for a record that names none (0: no budget)")
	slowest := flag.Int("slowest", 15, "how many of the slowest tests to list")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: gotestreport [-budget 10m] [-slowest 15] <dir>...")
		os.Exit(2)
	}
	r := newReport()
	files := 0
	for _, dir := range flag.Args() {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		sort.Strings(matches)
		for _, m := range matches {
			f, err := os.Open(m)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gotestreport: %v\n", err)
				os.Exit(2)
			}
			err = r.add(f, *budget)
			f.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "gotestreport: %s: %v\n", m, err)
				os.Exit(2)
			}
			files++
		}
	}
	if files == 0 {
		fmt.Printf("go test report: no go test ran on this row (%s holds no record)\n", strings.Join(flag.Args(), " "))
		return
	}
	f := r.findings(*slowest)
	fmt.Print(f.text)
	if s := os.Getenv("GITHUB_STEP_SUMMARY"); s != "" {
		if out, err := os.OpenFile(s, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(out, "```\n%s```\n", f.text)
			out.Close()
		}
	}
	if f.red {
		os.Exit(1)
	}
}
