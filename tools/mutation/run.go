package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The six things that can happen to a mutant.
const (
	outcomeKilled   = "killed"       // a test ran and went red: the tests noticed
	outcomeSurvived = "survived"     // every stage ran its tests and stayed green: nothing noticed
	outcomeBroken   = "build-broken" // a compiler refused the mutant, Go's or a target's: no test was asked
	outcomeTimeout  = "timeout"      // a stage, or the tester inside it, outran its bound: counted with the killed, named apart
	// outcomeUnproven is a mutant every stage passed while not testing it: a
	// test that passed on the unmutated tree was skipped, filtered out or gone
	// under the mutant, or the stage's output lost its proof that it tested.
	// It is no survivor and no kill: the coverage was not there to ask.
	outcomeUnproven = "unproven"
	// outcomeNotRun is a mutant every stage it was run through passed and
	// tested, while a stage of its scope that could still have killed it was
	// not run at this depth: a deep stage of a run without -deep, or a stage
	// -stages left out. It is no survivor: nothing was asked that could say so.
	outcomeNotRun = "not-run"
)

// Result is one mutant's outcome.
type Result struct {
	Mutant
	Scope   string `json:"scope"`
	Outcome string `json:"outcome"`
	// Stage is the stage that killed the mutant, or the last one it survived.
	Stage string `json:"stage"`
	// Stages are the stages the mutant was run through, in order.
	Stages  []string `json:"stages"`
	Seconds float64  `json:"seconds"`
	// Tail is the end of the killing stage's output.
	Tail string `json:"tail,omitempty"`
}

// Exec runs one shell command at dir under a time limit. It is an argument so
// the sweep's own tests run no process and read no clock.
type Exec func(dir, sh string, limit time.Duration) (out []byte, ok, timedOut bool, took time.Duration)

// sweepOptions are the choices of one run.
type sweepOptions struct {
	// Deep runs the stages that cost minutes each.
	Deep bool
	// Only, when set, names the stages to run, and overrides Deep.
	Only []string
	// BaselineLimit bounds a stage on the unmutated tree.
	BaselineLimit time.Duration
}

// buildFailed is a compiler refusing the mutant before any test of it ran:
// Go's own, and the native compilers the targets' stages run (C and C++ by
// gcc or clang and their linkers, C#, Dart, Java, Rust, Elixir, Lobster). A
// stage that dies here asked no test anything, so its verdict is build-broken
// and never a kill.
var buildFailed = regexp.MustCompile(`(?m)` + strings.Join([]string{
	`\[build failed\]`, `\[setup failed\]`, // go test
	// a path may hold spaces (`build/arithmetic example.c`), never a colon
	`^[^:\n]+:\d+:\d+: (?:fatal )?[Ee]rror: `,                                    // gcc, clang, dart: file:line:col: error:
	`^\s*(?:collect2|ld|clang|clang\+\+|gcc|g\+\+|cc|c\+\+): (?:fatal )?error: `, // the driver or the linker
	`^[^:\n]+\.java:\d+: error: `,                                                // javac
	`\berror CS\d{4}:`,                                                           // dotnet
	`^error\[E\d{4}\]: `, `^error: could not compile `,                           // rustc, cargo
	`\*\* \((?:CompileError|SyntaxError|TokenMissingError|MismatchedDelimiterError)\)`, // elixir
	`^[^:\n]+\.lobster\(\d+\): error: `,                                                // lobster
}, "|"))

// testerTimedOut is the tester inside a stage reporting that it gave up on a
// test at its own deadline: go test's -timeout, and the conformance harness's
// reply deadline. The stage then exits red before the sweep's own bound
// fires, and the verdict is a timeout, not an assertion that failed.
var testerTimedOut = regexp.MustCompile(`(?m)^panic: test timed out after |the driver stalled: no reply within `)

// failed is the verdict of a stage that exited red: a compiler that refused
// the mutant, a tester that hit its own deadline, or a test that failed.
func failed(out []byte) string {
	switch {
	case buildFailed.Match(out):
		return outcomeBroken
	case testerTimedOut.Match(out):
		return outcomeTimeout
	}
	return outcomeKilled
}

// passedTest is one test a `go test -v` passed, by name; packageLine is the
// line go test ends each package's output with, naming the package.
var (
	passedTest  = regexp.MustCompile(`^\s*--- PASS: (\S+)`)
	packageLine = regexp.MustCompile(`^(?:ok|FAIL|\?)\s+(\S+)`)
)

// passedTests counts the tests a `go test -v` output passed, each named by
// its package and its name: two packages each with a TestValue are two
// tests, and one of them that stops running is not stood in for by the
// other. go test prints a package's output whole and then the line naming
// the package, so each name belongs to the package line that follows it.
func passedTests(out []byte) map[string]int {
	got := map[string]int{}
	var pending []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if m := passedTest.FindStringSubmatch(line); m != nil {
			pending = append(pending, m[1])
			continue
		}
		if m := packageLine.FindStringSubmatch(line); m != nil {
			for _, name := range pending {
				got[m[1]+" "+name]++
			}
			pending = nil
		}
	}
	for _, name := range pending { // no package line followed them
		got[name]++
	}
	return got
}

// lost is the tests that passed on the unmutated tree and did not pass under
// the mutant, sorted: each was skipped, filtered out or never reached.
func lost(base, now map[string]int) []string {
	var out []string
	for name, n := range base {
		if now[name] < n {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// sweep runs the mutants of one scope through its stages in the scratch tree.
//
// Every stage is first run on the unmutated tree and must pass and show its
// proof: a stage that is red before any mutation, or that passes without
// testing, would make every verdict below it meaningless, so the sweep
// refuses to start instead. The same stages are run again after the last
// mutant, and a red there voids the run: the tree did not come back.
//
// The sweep fails if it applied no mutant or ran no stage. A mutation driver
// that did nothing reports no survivors, which reads exactly like success.
func sweep(scope Scope, tree string, muts []Mutant, opt sweepOptions, run Exec, log io.Writer, emit func(Result)) error {
	stages, err := scope.stagesFor(opt)
	if err != nil {
		return err
	}
	notRun := scope.notRun(stages)
	if len(muts) == 0 {
		return fmt.Errorf("scope %s: no mutant to apply: a sweep that mutates nothing proves nothing", scope.Name)
	}
	limits := make([]time.Duration, len(stages))
	proofs := make([]*regexp.Regexp, len(stages))
	passed := make([]map[string]int, len(stages))
	for i, st := range stages {
		took, tests, err := baseline(scope, st, tree, opt.BaselineLimit, run)
		if err != nil {
			return err
		}
		proofs[i], passed[i] = regexp.MustCompile(st.Proof), tests
		limits[i] = 3*took + 2*time.Minute
		_, _ = fmt.Fprintf(log, "baseline %s/%s green in %.0fs\n", scope.Name, st.Name, took.Seconds())
	}
	build := "go build " + strings.Join(scope.goPackages(), " ")
	if out, ok, _, _ := run(tree, build, opt.BaselineLimit); !ok {
		return fmt.Errorf("scope %s: %q fails on the unmutated tree:\n%s", scope.Name, build, tail(out))
	}

	applied, stagesRun := 0, 0
	for _, m := range muts {
		path := filepath.Join(tree, filepath.FromSlash(m.File))
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mutated, err := apply(original, m)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, mutated, info.Mode().Perm()); err != nil {
			return err
		}
		applied++
		res := Result{Mutant: m, Scope: scope.Name, Outcome: outcomeSurvived}
		var seconds float64
		if strings.HasSuffix(m.File, ".go") {
			out, ok, _, took := run(tree, build, opt.BaselineLimit)
			seconds += took.Seconds()
			if !ok {
				res.Outcome, res.Stage, res.Tail = outcomeBroken, "build", tail(out)
			}
		}
		if res.Outcome == outcomeSurvived {
			// a green stage counts only if it tested the mutant: it must show its
			// proof again, and pass every test it passed on the unmutated tree. A
			// stage that did not is remembered, and a later stage may still kill
			// the mutant.
			var unproven *Result
			for i, st := range stages {
				out, ok, timedOut, took := run(tree, st.Sh, limits[i])
				stagesRun++
				seconds += took.Seconds()
				res.Stage = st.Name
				res.Stages = append(res.Stages, st.Name)
				if timedOut {
					res.Outcome, res.Tail = outcomeTimeout, tail(out)
					break
				}
				if !ok {
					res.Outcome, res.Tail = failed(out), tail(out)
					break
				}
				if unproven != nil {
					continue
				}
				if !proofs[i].Match(out) {
					unproven = &Result{Stage: st.Name, Tail: fmt.Sprintf("the stage passed and its output does not match %q: it did not test the mutant\n%s", st.Proof, tail(out))}
				} else if st.Counted {
					if missing := lost(passed[i], passedTests(out)); len(missing) > 0 {
						unproven = &Result{Stage: st.Name, Tail: fmt.Sprintf("%d tests passed on the unmutated tree and did not pass under the mutant (skipped, filtered out or not reached): %s\n%s", len(missing), clip(strings.Join(missing, " ")), tail(out))}
					}
				}
			}
			switch {
			case res.Outcome == outcomeSurvived && unproven != nil:
				res.Outcome, res.Stage, res.Tail = outcomeUnproven, unproven.Stage, unproven.Tail
			case res.Outcome == outcomeSurvived && len(notRun) > 0:
				// a survivor is a mutant every stage of its scope was asked about
				res.Outcome, res.Tail = outcomeNotRun, fmt.Sprintf("not run at this depth: every stage it was run through passed and tested it, and %s, which could still kill it, did not run; run it with -deep and without -stages for a verdict", strings.Join(notRun, ", "))
			}
		}
		res.Seconds = seconds
		if err := os.WriteFile(path, original, info.Mode().Perm()); err != nil {
			return fmt.Errorf("mutant %s could not be reverted in %s, the scratch tree is spoiled: %w", m.ID, m.File, err)
		}
		back, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(back, original) {
			return fmt.Errorf("mutant %s was not reverted in %s, the scratch tree is spoiled", m.ID, m.File)
		}
		_, _ = fmt.Fprintf(log, "%-12s %s %s:%d %s %q -> %q (%s, %.0fs)\n", res.Outcome, m.ID, m.File, m.Line, m.Op, clip(m.Old), clip(m.New), res.Stage, seconds)
		emit(res)
	}
	if applied == 0 {
		return fmt.Errorf("scope %s: applied no mutation", scope.Name)
	}
	if stagesRun == 0 {
		return fmt.Errorf("scope %s: %d mutants applied and no test stage ran: every one failed to build, so no test was asked anything", scope.Name, applied)
	}
	for i, st := range stages {
		_, tests, err := baseline(scope, st, tree, opt.BaselineLimit, run)
		if err == nil && st.Counted {
			if missing := lost(passed[i], tests); len(missing) > 0 {
				err = fmt.Errorf("scope %s stage %s: %d tests that passed before the first mutant did not pass after the last: %s", scope.Name, st.Name, len(missing), clip(strings.Join(missing, " ")))
			}
		}
		if err != nil {
			return fmt.Errorf("after the last mutant was reverted, and so every verdict of this run is void: %w", err)
		}
	}
	return nil
}

// baseline runs a stage on the unmutated tree, and holds it to green and to
// its proof. A counted stage must also have passed at least one test by name,
// and the tests it passed are returned: a `go test` that filtered out or
// skipped every test still prints `ok` for the package.
func baseline(scope Scope, st Stage, tree string, limit time.Duration, run Exec) (time.Duration, map[string]int, error) {
	proof, err := regexp.Compile(st.Proof)
	if err != nil || st.Proof == "" {
		return 0, nil, fmt.Errorf("scope %s stage %s: a stage needs a proof that it tested, and %q is not one", scope.Name, st.Name, st.Proof)
	}
	if proofMetByCommand(proof, st.Sh) {
		return 0, nil, fmt.Errorf("scope %s stage %s: its proof %q is met by its own command line %q, or by make saying a target it names had nothing to do: a stage that ran nothing prints that, so it proves nothing", scope.Name, st.Name, st.Proof, st.Sh)
	}
	out, ok, timedOut, took := run(tree, st.Sh, limit)
	tests := passedTests(out)
	switch {
	case timedOut:
		return 0, nil, fmt.Errorf("scope %s stage %s: the unmutated tree did not finish %q within %s:\n%s", scope.Name, st.Name, st.Sh, limit, tail(out))
	case !ok:
		return 0, nil, fmt.Errorf("scope %s stage %s: the unmutated tree is red under %q, so the stage can tell a mutant nothing:\n%s", scope.Name, st.Name, st.Sh, tail(out))
	case !proof.Match(out):
		return 0, nil, fmt.Errorf("scope %s stage %s: %q passed and its output does not match %q: it tested nothing:\n%s", scope.Name, st.Name, st.Sh, st.Proof, tail(out))
	case st.Counted && len(tests) == 0:
		return 0, nil, fmt.Errorf("scope %s stage %s: %q passed and ran no test: no `--- PASS` line, so every test it names was filtered out or skipped, or it is not run with -v:\n%s", scope.Name, st.Name, st.Sh, tail(out))
	}
	return took, tests, nil
}

// proofMetByCommand reports whether a stage's proof is met by text a stage
// that tested nothing still prints: its own command line, which make and a
// shell echo, or make's word that a target the command names is up to date
// or had nothing to be done.
func proofMetByCommand(proof *regexp.Regexp, sh string) bool {
	if proof.MatchString(sh) {
		return true
	}
	for _, w := range strings.Fields(sh) {
		if proof.MatchString("make: Nothing to be done for '"+w+"'.") || proof.MatchString("make: '"+w+"' is up to date.") {
			return true
		}
	}
	return false
}

// stagesFor is the scope's stages under the options, and an error when that
// is none, or when a named stage does not exist.
func (s *Scope) stagesFor(opt sweepOptions) ([]Stage, error) {
	var out []Stage
	if len(opt.Only) > 0 {
		for _, name := range opt.Only {
			found := false
			for _, st := range s.Stages {
				if st.Name == name {
					out = append(out, st)
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("scope %s has no stage %q", s.Name, name)
			}
		}
		return out, nil
	}
	for _, st := range s.Stages {
		if !st.Deep || opt.Deep {
			out = append(out, st)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("scope " + s.Name + " has no stage to run")
	}
	return out, nil
}

// notRun names the scope's stages a run of these stages leaves out, in the
// scope's order.
func (s *Scope) notRun(stages []Stage) []string {
	ran := map[string]bool{}
	for _, st := range stages {
		ran[st.Name] = true
	}
	var out []string
	for _, st := range s.Stages {
		if !ran[st.Name] {
			out = append(out, st.Name)
		}
	}
	return out
}

func (s *Scope) goPackages() []string {
	var out []string
	for _, d := range s.Go {
		out = append(out, "./"+d+"/...")
	}
	return out
}

// tail is the end of a command's output, enough to see why it failed.
func tail(out []byte) string {
	const keep = 3000
	if len(out) > keep {
		out = out[len(out)-keep:]
		if i := bytes.IndexByte(out, '\n'); i >= 0 {
			out = out[i+1:]
		}
	}
	return string(out)
}

func clip(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
