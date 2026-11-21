// Command vacuoustests finds Go tests that run and pass while checking
// nothing.
//
// Glenn, 2026-11-21: "We simply have to actually be able to trust that our
// tests actually check for things, don't do nothing, and that we are actually
// running the tests we think we are."
//
// It parses every _test.go file of the tree (go/ast only: nothing is compiled
// and no test is run, so it costs seconds) and reports seven classes:
//
//	1  a Test, Fuzz, Benchmark or t.Run body with no reachable call that can
//	   fail it, followed through every helper this tree declares
//	2  assertions that run only inside a loop over a collection computed at
//	   run time, with nothing checking that the collection is non-empty
//	3  assertions behind a guard that may never open (if err == nil with no
//	   else, if len(x) > 0, a GOOS or environment test, an early return)
//	4  an error returned to a test and dropped
//	5  a comparison that cannot fail (a value against itself, or two values
//	   produced by the same call)
//	6  a subtest registered from a goroutine, or a parent that reads what its
//	   parallel subtests have not yet written
//	7  a test named as a negative control that never looks at a failure
//
// Classes 1, 2 (when the collection is a filesystem or registry read), 4 (when
// the error comes from this tree or from a child process) and 7 are decided
// mechanically and are the GATE: any such finding exits 1. The others are
// listed by -all for a reader to judge.
//
// The gate itself proves it ran: it exits 1 if it parsed no test file or saw
// no test function, and it prints both counts.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
)

func main() {
	root := flag.String("root", ".", "the tree to analyze")
	all := flag.Bool("all", false, "list every class, not only the gated ones")
	fix := flag.Bool("fix", false, "write the missing non-empty check before every class 2 loop that can take one")
	flag.Parse()
	if *fix {
		t, err := loadTree(*root)
		if err != nil {
			say(os.Stderr, "vacuoustests: %v\n", err)
			os.Exit(2)
		}
		n, err := applyFixes(*root, analyze(t))
		say(os.Stderr, "vacuoustests: wrote %d non-empty checks\n", n)
		if err != nil {
			say(os.Stderr, "vacuoustests: %v\n", err)
			os.Exit(2)
		}
		return
	}
	os.Exit(runMain(*root, *all, os.Stdout, os.Stderr))
}

// say prints to a stream whose write error has nowhere to be reported.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// runMain is main with its inputs as arguments. It returns the exit status.
func runMain(root string, all bool, stdout, stderr io.Writer) int {
	t, err := loadTree(root)
	if err != nil {
		say(stderr, "vacuoustests: %v\n", err)
		return 2
	}
	findings := analyze(t)
	counts := map[string]int{}
	gated := 0
	for _, f := range findings {
		counts[f.Class]++
		if f.Gated {
			gated++
		}
		if f.Gated || all {
			say(stdout, "%s\n", f)
		}
	}
	if all {
		var classes []string
		for c := range counts {
			classes = append(classes, c)
		}
		sort.Strings(classes)
		for _, c := range classes {
			say(stderr, "vacuoustests: class %s: %d\n", c, counts[c])
		}
	}
	say(stderr, "vacuoustests: %d test files, %d test functions, %d refused findings\n", t.testFiles, t.testFuncs, gated)
	switch {
	case t.testFiles == 0:
		say(stderr, "vacuoustests: no _test.go file was analyzed: a gate that looked at nothing is not a pass\n")
		return 1
	case t.testFuncs == 0:
		say(stderr, "vacuoustests: no test function was analyzed: a gate that looked at nothing is not a pass\n")
		return 1
	case gated > 0:
		return 1
	}
	return 0
}
