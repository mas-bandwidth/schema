// Command compilebudget holds every translation unit of a build to a budget
// of compile seconds.
//
//	compilebudget -budget SECONDS -dir DIR UNIT...
//
// Each UNIT's compile wrote DIR/UNIT.seconds (tools/compile-seconds.pl: one
// line, the CPU seconds the compiler took). compilebudget reads every one and
// exits non-zero, naming the unit and its seconds, when any unit took longer
// than SECONDS, and naming the file when a unit has no timing or one it cannot
// read: a unit that was never timed is not a unit within budget.
//
// WHY. The C++ conformance driver was one translation unit that compiled for
// 71 s, alone and after everything else, inside a CI job of two minutes
// (Makefile, CONFORMANCE_DRIVER_TUS). It is now several units compiled side
// by side, and the leg fits only while no one unit grows back toward the old
// one. This is the check that says so per pull request, on the produce job's
// own build (`make conformance-cpp-tu-budget`).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"strconv"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.DirFS, os.Stdout, os.Stderr))
}

// run is the command over open, which opens -dir (os.DirFS; a test hands in
// a file system of its own).
func run(args []string, open func(dir string) fs.FS, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("compilebudget", flag.ContinueOnError)
	flags.SetOutput(stderr)
	budget := flags.Float64("budget", 0, "the most CPU seconds one unit may take to compile")
	dir := flags.String("dir", "", "the directory holding UNIT.seconds for every unit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *dir == "" || flags.NArg() == 0 {
		say(stderr, "usage: compilebudget -budget SECONDS -dir DIR UNIT...\n")
		return 2
	}
	report, err := check(open(*dir), *dir, *budget, flags.Args())
	if !say(stdout, report) {
		return 1
	}
	if err != nil {
		say(stderr, "compilebudget: "+err.Error()+"\n")
		return 1
	}
	return 0
}

// say writes s to w and reports whether all of it was written. A caller
// that cannot write its report exits non-zero: an unread pass is no pass.
func say(w io.Writer, s string) bool {
	_, err := io.WriteString(w, s)
	return err == nil
}

// check reads UNIT.seconds for every unit from fsys (named dir in messages)
// and holds each to budget. The report lists every unit read, in the order
// given; the error names every unit past the budget and every timing that
// is missing or unreadable.
func check(fsys fs.FS, dir string, budget float64, units []string) (string, error) {
	if budget <= 0 {
		return "", fmt.Errorf("-budget %g: a budget must be more than zero seconds", budget)
	}
	if len(units) == 0 {
		return "", errors.New("no unit named: there is nothing to hold to the budget")
	}
	var report strings.Builder
	var problems []string
	seen := map[string]bool{}
	for _, unit := range units {
		if seen[unit] {
			problems = append(problems, fmt.Sprintf("%s is named twice", unit))
			continue
		}
		seen[unit] = true
		name := unit + ".seconds"
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s/%s: no timing for unit %s (%v): every unit's compile must be timed", dir, name, unit, err))
			continue
		}
		text := strings.TrimSpace(string(data))
		seconds, err := strconv.ParseFloat(text, 64)
		// NaN and Inf parse, and NaN compares false against any budget
		if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			problems = append(problems, fmt.Sprintf("%s/%s: %q is not a number of seconds", dir, name, text))
			continue
		}
		verdict := "ok"
		if seconds > budget {
			verdict = "OVER"
			problems = append(problems, fmt.Sprintf("unit %s took %.2f s to compile, past the budget of %g s: cut it into smaller units", unit, seconds, budget))
		}
		fmt.Fprintf(&report, "%-24s %7.2f s  %s\n", unit, seconds, verdict)
	}
	if len(problems) > 0 {
		return report.String(), errors.New(strings.Join(problems, "\n"))
	}
	fmt.Fprintf(&report, "compilebudget: %d units, every one within %g s\n", len(units), budget)
	return report.String(), nil
}
