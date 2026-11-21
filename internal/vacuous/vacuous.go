// Package vacuous finds Go tests that run and pass while checking nothing.
//
// The rule (2026-11-21): a test that exists has to be able to fail, and has to
// check what its name promises.
//
// It parses every _test.go file of a tree (go/ast only: nothing is compiled
// and no test is run, so it costs seconds) and reports seven classes:
//
//	1  a Test, Fuzz, Benchmark or t.Run body with no reachable call that can
//	   fail it, followed through every helper the tree declares
//	2  assertions that run only inside a loop over a collection computed at
//	   run time, with nothing checking that the collection is non-empty
//	3  assertions behind a guard that may never open (if err == nil with no
//	   else, if len(x) > 0, a GOOS or environment test, an early return)
//	4  an error returned to a test and dropped
//	5  a comparison that cannot fail (a value against itself, or two values
//	   produced by the same call)
//	6  a subtest registered from a goroutine, or a parent that reads what its
//	   parallel subtests have not yet written
//	7  a test named as a negative control that never fails on acceptance
//
// THE GATE. Classes 1, 2 (when the collection is a filesystem listing or a
// registry read), 4 (when the error comes from this tree or from a child
// process) and 7 are decided mechanically: any such finding makes Run answer
// 1. internal/ci runs it over the repository on every pull request. The other
// classes need a reader, and are listed by `go run ./tools/vacuoustests -all`.
//
// The gate proves it ran: Run answers 1 if it parsed no test file or saw no
// test function, and it prints both counts.
package vacuous

import (
	"fmt"
	"io"
	"sort"
)

// say prints to a stream whose write error has nowhere to be reported.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// Counts is what one run looked at.
type Counts struct {
	TestFiles int // _test.go files parsed
	TestFuncs int // Test, Fuzz and Benchmark functions analyzed
	Refused   int // findings of the gated classes
}

// Run analyzes the tree at root, prints the refused findings (every finding
// when all is set) to stdout and the counts to stderr, and returns the exit
// status: 0 clean, 1 refused or nothing analyzed, 2 the tree could not be read.
func Run(root string, all bool, stdout, stderr io.Writer) (int, Counts) {
	t, err := loadTree(root)
	if err != nil {
		say(stderr, "vacuoustests: %v\n", err)
		return 2, Counts{}
	}
	findings := analyze(t)
	perClass := map[string]int{}
	c := Counts{TestFiles: t.testFiles, TestFuncs: t.testFuncs}
	for _, f := range findings {
		perClass[f.Class]++
		if f.Gated {
			c.Refused++
		}
		if f.Gated || all {
			say(stdout, "%s\n", f)
		}
	}
	if all {
		var classes []string
		for class := range perClass {
			classes = append(classes, class)
		}
		sort.Strings(classes)
		for _, class := range classes {
			say(stderr, "vacuoustests: class %s: %d\n", class, perClass[class])
		}
	}
	say(stderr, "vacuoustests: %d test files, %d test functions, %d refused findings\n", c.TestFiles, c.TestFuncs, c.Refused)
	switch {
	case c.TestFiles == 0:
		say(stderr, "vacuoustests: no _test.go file was analyzed: a gate that looked at nothing is not a pass\n")
		return 1, c
	case c.TestFuncs == 0:
		say(stderr, "vacuoustests: no test function was analyzed: a gate that looked at nothing is not a pass\n")
		return 1, c
	case c.Refused > 0:
		return 1, c
	}
	return 0, c
}
