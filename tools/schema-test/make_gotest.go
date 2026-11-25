package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// makeGoTestAdapter runs the Go tests a make target's recipe runs, each as a
// registry row of its own: a row whose inputs are {"file": "make/x.mk",
// "target": "check-x", "package": "./internal/x", "test": "TestY"} is the
// one Go test TestY of ./internal/x that make check-x runs. A make row would
// give the whole target one verdict, so the Go tests inside it would have no
// names; this kind names each.
//
// The rows of one target are one call (testreg's unitOf keeps them in one
// shard): make runs the target once with GoTestJSONEnv naming a file under
// build/ (makeGoTestJSON), and every go test its recipes run through
// $(GO_TEST) (tools/gotestrun) appends its -json events there. Each row's
// verdict is read from that file by the exact package and test name:
//
//   - a pass is a pass; a failure fails with the test's own output;
//   - a test, or a subtest of it, that skips fails: there is no skip;
//   - a test the row claims that the file does not show run fails by name;
//   - a run of 0 tests (no file, or no test in it) fails every row;
//   - a test the file shows run that no row of the target names fails every
//     row of the call, naming it: a Go test the target runs has a row;
//   - go test's own timeout fails the running test's row naming the limit and
//     how long it ran (also when test2json closed it as failed), and every
//     row it never reached naming the test that was (goTestTimedOut);
//   - a test started more times than it finished (a package run twice, the
//     second cut short) fails: one run's pass is not the test's;
//   - a package that failed outside its tests (TestMain, a build), with none
//     of its tests failed or left running, fails its rows that passed,
//     with the package's own output;
//   - a skip make's log reports that no event names (a recipe's own words)
//     fails every row; a skip go test reported is the skipped row's alone;
//   - make exiting non-zero with no row failed fails every row with make's
//     words: the recipe failed outside the tests;
//   - make killed by the driver's deadline is an error naming the Go tests
//     the events show still running, which the driver reports as its
//     timeout.
//
// The events file is named to make by an absolute path, so a recipe that
// changes directory writes the same file.
//
// What a make row checks before it runs holds here too: the target is a rule
// its file writes, the tools and siblings it needs are there, and its
// recipes run no go test without -json. A target whose recipes run no
// $(GO_TEST) writes no file, and every row of it fails before make runs.
type makeGoTestAdapter struct {
	mk makeAdapter
	// root is the repository root: packages are relative to it, and the
	// events file is under its build/
	root string
	// reg is the registry the run loaded (withRunner): every row of a
	// target, so a test the target ran that no row names is found even when
	// only some of the target's rows were asked for. Nil counts the asked
	// rows alone.
	reg *testreg.Registry
}

// GoTestJSONEnv names the file $(GO_TEST) appends its go test -json events
// to (tools/gotestrun reads it); a negative control's $(GO_TEST_RED) writes
// none, since its red is the control's, not a test's verdict.
const GoTestJSONEnv = "SCHEMA_GOTEST_JSON"

type makeGoTestInputs struct {
	makeInputs
	Package string `json:"package"`
	Test    string `json:"test"`
}

func newMakeGoTestAdapter(root string) makeGoTestAdapter {
	return makeGoTestAdapter{mk: newMakeAdapter(), root: root}
}

func decodeMakeGoTestInputs(raw json.RawMessage) (makeGoTestInputs, error) {
	var in makeGoTestInputs
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return in, err
	}
	if !strings.HasPrefix(in.Package, "./") || in.Package != "./"+path.Clean(strings.TrimPrefix(in.Package, "./")) || !goTestName.MatchString(in.Test) {
		return in, fmt.Errorf("want a ./package and a Go test name, got %q %q", in.Package, in.Test)
	}
	if in.Seed != "" || len(in.Reads) > 0 {
		return in, errors.New("a make-gotest row takes no seed and no reads: its tests are the rows")
	}
	plain, err := json.Marshal(in.makeInputs)
	if err != nil {
		return in, err
	}
	if _, err := decodeMakeInputs(plain); err != nil {
		return in, err
	}
	return in, nil
}

// Answers is true for a language-free row whose inputs read and whose Go
// test its package's source declares: a row whose test was renamed or
// removed is MISSING, by name.
func (a makeGoTestAdapter) Answers(t testreg.Test) bool {
	if t.Scope != testreg.LangFree {
		return false
	}
	in, err := decodeMakeGoTestInputs(t.Inputs)
	if err != nil {
		return false
	}
	return newGoTestAdapter(nil, a.root, 1).declared(in.Package)[in.Test]
}

// Serial: the target builds in the checkout (testreg.Serial).
func (makeGoTestAdapter) Serial() bool { return true }

// makeGoTestJSON is the file a target's go test events go to, under build/.
func makeGoTestJSON(target string) string {
	return path.Join("build", "registry-gotest", strings.NewReplacer("/", "_", "%", "_").Replace(target)+".jsonl")
}

// goTestWriter is a recipe line that runs go test through $(GO_TEST), the
// writer of the events file (not $(GO_TEST_RED), a control's).
var goTestWriter = regexp.MustCompile(`\$[({]GO_TEST[)}]|go run \./tools/gotestrun(\s|$)`)

// goTestKey is a Go test by its package's import path and its name.
type goTestKey struct{ pkg, test string }

func (a makeGoTestAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	type target struct{ file, name string }
	var order []target
	rows := map[target][]testreg.Instance{}
	ins := map[string]makeGoTestInputs{}
	for _, inst := range instances {
		in, err := decodeMakeGoTestInputs(inst.Test.Inputs)
		if err != nil {
			return nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		ins[inst.Full] = in
		k := target{in.File, in.Target}
		if rows[k] == nil {
			order = append(order, k)
		}
		rows[k] = append(rows[k], inst)
	}
	var out []testreg.Result
	for _, k := range order {
		res, err := a.runTarget(ctx, k.file, k.name, rows[k], ins)
		out = append(out, res...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// failAll is one reason for every row of a target.
func failAll(rows []testreg.Instance, why string) []testreg.Result {
	out := make([]testreg.Result, len(rows))
	for i, r := range rows {
		out[i] = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: why}
	}
	return out
}

func (a makeGoTestAdapter) runTarget(ctx context.Context, file, name string, rows []testreg.Instance, ins map[string]makeGoTestInputs) ([]testreg.Result, error) {
	first := ins[rows[0].Full]
	for _, r := range rows[1:] {
		in := ins[r.Full]
		if !slices.Equal(in.Tools, first.Tools) || !slices.Equal(in.Siblings, first.Siblings) || !maps.Equal(in.Vars, first.Vars) {
			return failAll(rows, fmt.Sprintf("the rows of make %s disagree on its vars, tools or siblings (%s and %s): one target runs one way", name, rows[0].Full, r.Full)), nil
		}
	}
	data, err := a.mk.read(file)
	if err != nil {
		return nil, fmt.Errorf("%s: make file: %w", rows[0].Full, err)
	}
	rules, err := makeRules(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rows[0].Full, err)
	}
	if !rules[name] {
		return failAll(rows, fmt.Sprintf("%s writes no rule %q: the target these rows run does not exist", file, name)), nil
	}
	if why := a.mk.needs(first.makeInputs); why != "" {
		return failAll(rows, why), nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	why, err := a.mk.vet(ctx, first.makeInputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rows[0].Full, err)
	}
	if why != "" {
		return failAll(rows, why), nil
	}
	db, err := a.mk.database(ctx)
	if err != nil {
		return failAll(rows, fmt.Sprintf("make %s cannot be read before it runs: %v", name, err)), nil
	}
	// the make vet above refused a go test outside $(GO_TEST), so every Go
	// test the target runs writes its events (a negative control's
	// $(GO_TEST_RED) writes none: its red is the control's, which make's
	// exit carries)
	if _, text := db.reach(name); !goTestWriter.MatchString(strings.Join(text, "\n")) {
		return failAll(rows, fmt.Sprintf("make %s runs no go test through $(GO_TEST), so it writes no go test -json events for these rows: a make-gotest row's target runs its Go tests through $(GO_TEST)", name)), nil
	}

	// absolute: a recipe that changes directory hands the same name to
	// $(GO_TEST), which opens it from wherever the recipe runs
	events, err := filepath.Abs(filepath.Join(a.root, filepath.FromSlash(makeGoTestJSON(name))))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(events), 0o755); err != nil {
		return nil, err
	}
	// a file a run before this one left would read as this run's tests
	if err := os.Remove(events); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	argv := []string{"make", "--no-print-directory"}
	if a.mk.jobs > 0 {
		argv = append(argv, fmt.Sprintf("-j%d", a.mk.jobs))
	}
	argv = append(argv, name)
	for _, k := range slices.Sorted(maps.Keys(first.Vars)) {
		argv = append(argv, k+"="+first.Vars[k])
	}
	log, runErr := a.mk.exec(testreg.WithEnv(ctx, map[string]string{GoTestJSONEnv: events}), argv)
	raw, err := os.ReadFile(events)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if ctx.Err() != nil {
		// the driver's deadline killed make: name the Go tests it was
		// running, from the events written so far (the driver names the
		// call, its limit and how long it ran)
		running := "no Go test of it had started"
		switch open, err := openGoTests(raw); {
		case err != nil:
			running = "its events file " + err.Error()
		case len(open) > 0:
			running = "the Go tests running were " + strings.Join(open, ", ")
		}
		return nil, fmt.Errorf("make %s, killed while %s: %w\n%s", name, running, ctx.Err(), tail(log, 40))
	}
	all := rows
	if a.reg != nil {
		all = nil
		for _, t := range a.reg.Tests {
			in, err := decodeMakeGoTestInputs(t.Inputs)
			if t.Adapter == "make-gotest" && err == nil && in.File == file && in.Target == name {
				all = append(all, testreg.Instance{Full: testreg.LangFree + "." + t.Tier + "." + t.Key, Test: t})
			}
		}
	}
	return a.verdicts(name, rows, all, raw, log, runErr)
}

// verdicts reads the events a target's go tests wrote (raw) into one verdict
// per asked row. all is every row of the target, asked or not: a test the
// events show run that none of them names fails the call.
func (a makeGoTestAdapter) verdicts(target string, rows, all []testreg.Instance, raw []byte, log string, runErr error) ([]testreg.Result, error) {
	lines := strings.Split(log, "\n")
	if plain := plainGoTest(lines); plain != "" {
		return failAll(rows, fmt.Sprintf("make %s: its log holds go test's plain output, which prints nothing for a skipped test, so a skip would read as a pass: %q", target, plain)), nil
	}
	keyOf := map[string]goTestKey{} // a row's full name -> its Go test
	named := map[goTestKey]bool{}
	for _, r := range all {
		in, err := decodeMakeGoTestInputs(r.Test.Inputs)
		if err != nil {
			continue
		}
		imp, err := a.importPath(in.Package)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Full, err)
		}
		k := goTestKey{imp, in.Test}
		keyOf[r.Full] = k
		named[k] = true
	}
	for _, r := range rows {
		if _, ok := keyOf[r.Full]; ok {
			continue
		}
		in, _ := decodeMakeGoTestInputs(r.Test.Inputs)
		imp, err := a.importPath(in.Package)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Full, err)
		}
		keyOf[r.Full] = goTestKey{imp, in.Test}
		named[keyOf[r.Full]] = true
	}

	ev, err := readGoTestEvents(raw)
	if err != nil {
		return failAll(rows, fmt.Sprintf("make %s: %s %v", target, makeGoTestJSON(target), err)), nil
	}
	// a skip the log reports that no event names (a recipe's own words,
	// not go test's) fails every row; a skip go test reported is the
	// skipped row's own red, by its name, below
	if err := testreg.RefuseSkips(withoutGoTestSkips(lines, ev)); err != nil {
		return failAll(rows, fmt.Sprintf("make %s: %v", target, err)), nil
	}
	if ev.ran == 0 {
		why := fmt.Sprintf("make %s ran 0 Go tests: its $(GO_TEST) wrote no test to %s", target, makeGoTestJSON(target))
		if runErr != nil {
			why += fmt.Sprintf("; make: %v\n%s", runErr, tail(log, 30))
		}
		return failAll(rows, why), nil
	}
	var unnamed []string
	for k, s := range ev.tests {
		if s.runs > 0 && !named[k] {
			unnamed = append(unnamed, k.pkg+" "+k.test)
		}
	}
	if len(unnamed) > 0 {
		slices.Sort(unnamed)
		return failAll(rows, fmt.Sprintf("make %s ran Go tests no registry row of it names: %s: every Go test a target runs is a row", target, strings.Join(unnamed, ", "))), nil
	}

	out := make([]testreg.Result, 0, len(rows))
	anyFail := false
	for _, r := range rows {
		k := keyOf[r.Full]
		s := ev.tests[k]
		var res testreg.Result
		open := s != nil && s.runs > s.closes
		to, timedOut := goTestTimedOut(strings.Join(ev.byPkg[k.pkg], "\n"))
		switch {
		case timedOut && (s == nil || open || s.failed && to.wasRunning(k.test)):
			// go test's own -timeout fired in this test's package before the
			// test finished: the limit and the test running, never "not run"
			res = to.verdicts("in make "+target, map[string]string{k.test: r.Full}, nil)[0]
		case s == nil || s.runs == 0:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("make %s never ran %s %s: the row claims it and %s does not show it run", target, k.pkg, k.test, makeGoTestJSON(target))}
		case len(s.skipped) > 0:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("%s skipped (%s), and nothing is ever skipped: %s", k.test, strings.Join(s.skipped, ", "), tail(strings.Join(s.output, "\n"), 5))}
		case s.failed:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: tail(strings.Join(s.output, "\n"), 20)}
		case open:
			// run more often than it finished (a package run twice, the
			// second cut short): a pass of one run is not the test's
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("make %s started %s %d times and it finished %d: a run that never finished is no pass", target, k.test, s.runs, s.closes)}
		case ev.pkgFailed[k.pkg] && !ev.pkgTestFailed[k.pkg] && !timedOut:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("make %s: package %s failed outside its tests (its TestMain, a build, or after the tests) though %s passed:\n%s", target, k.pkg, k.test, tail(strings.Join(ev.pkgOutput[k.pkg], "\n"), 20))}
		case s.passed:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Pass}
		default:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("make %s started %s and it never finished", target, k.test)}
		}
		anyFail = anyFail || res.Verdict == testreg.Fail
		out = append(out, res)
	}
	if runErr != nil && !anyFail {
		return failAll(rows, fmt.Sprintf("make %s exited %v though every Go test of its rows passed: the recipe failed outside them:\n%s", target, runErr, tail(log, 30))), nil
	}
	return out, nil
}

// goTestSeen is what the events say of one top-level Go test.
type goTestSeen struct {
	runs, closes   int // its runs, and the runs a pass, fail or skip ended
	passed, failed bool
	skipped        []string
	output         []string
}

// goTestRead is a target's events file, read.
type goTestRead struct {
	tests     map[goTestKey]*goTestSeen
	byPkg     map[string][]string // a package's event lines, for go test's own timeout
	pkgFailed map[string]bool     // a package-level fail event (no test named)
	// pkgTestFailed: a test of the package failed or never finished, which
	// go test's package-level fail reports too, so that fail is not one
	// outside the tests
	pkgTestFailed map[string]bool
	// skipLines: the lines go test and gotestrun print of the skips the
	// events name (the skipped tests' output, "--- SKIP: TestB", "SKIPPED
	// <package> <test>"), each its row's red by name
	skipLines map[string]bool
	skips     int                 // skip events, of tests and subtests
	pkgOutput map[string][]string // a package's output that names no test
	ran       int                 // top-level test runs
}

// readGoTestEvents reads go test -json events; a line that is not one is
// an error, never a line passed over.
func readGoTestEvents(raw []byte) (goTestRead, error) {
	ev := goTestRead{pkgTestFailed: map[string]bool{}, skipLines: map[string]bool{}, tests: map[goTestKey]*goTestSeen{}, byPkg: map[string][]string{}, pkgFailed: map[string]bool{}, pkgOutput: map[string][]string{}}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e struct{ Action, Package, Test, Output string }
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Action == "" {
			return ev, fmt.Errorf("holds a line that is no go test -json event: %q", sc.Text())
		}
		ev.byPkg[e.Package] = append(ev.byPkg[e.Package], sc.Text())
		if e.Test == "" {
			switch e.Action {
			case "fail":
				ev.pkgFailed[e.Package] = true
			case "output":
				ev.pkgOutput[e.Package] = append(ev.pkgOutput[e.Package], strings.TrimRight(e.Output, "\n"))
			}
			continue
		}
		top, sub, isSub := strings.Cut(e.Test, "/")
		k := goTestKey{e.Package, top}
		s := ev.tests[k]
		if s == nil {
			s = &goTestSeen{}
			ev.tests[k] = s
		}
		switch e.Action {
		case "run":
			if !isSub {
				s.runs++
				ev.ran++
			}
		case "output":
			s.output = append(s.output, strings.TrimRight(e.Output, "\n"))
		case "skip":
			ev.skips++
			ev.skipLines["--- SKIP: "+e.Test] = true
			ev.skipLines["SKIPPED "+e.Package+" "+e.Test] = true
			if isSub {
				s.skipped = append(s.skipped, sub)
			} else {
				s.skipped = append(s.skipped, "(the test itself)")
				s.closes++
			}
		case "pass":
			if !isSub {
				s.passed = true
				s.closes++
			}
		case "fail":
			if !isSub {
				s.failed = true
				s.closes++
			}
		}
	}
	if err := sc.Err(); err != nil {
		return ev, fmt.Errorf("could not be read: %w", err)
	}
	for k, s := range ev.tests {
		if s.failed || s.runs > s.closes {
			ev.pkgTestFailed[k.pkg] = true
		}
		if len(s.skipped) > 0 {
			for _, o := range s.output {
				if o = strings.TrimSpace(o); o != "" {
					ev.skipLines[o] = true
				}
			}
		}
	}
	return ev, nil
}

// withoutGoTestSkips is make's log without what go test and gotestrun print
// of the skips the events name: a skipped test's own output, its "--- SKIP:
// TestB" line, gotestrun's "SKIPPED <package> <test>" line, and, when the
// events hold a skip, gotestrun's own summary and refusal ("gotestrun: ran
// 2, passed 1, skipped 1, ..."), which count those skips. Each such skip is
// its row's red, by name. Any other skip the log reports is the recipe's
// own, and fails every row.
func withoutGoTestSkips(lines []string, ev goTestRead) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if ev.skips > 0 && strings.HasPrefix(t, "gotestrun: ") {
			continue
		}
		if ev.skipLines[t] {
			continue
		}
		if rest, ok := strings.CutPrefix(t, "--- SKIP: "); ok {
			if name, _, _ := strings.Cut(rest, " "); ev.skipLines["--- SKIP: "+name] {
				continue
			}
		}
		out = append(out, l)
	}
	return out
}

// openGoTests are the Go tests the events show started and not finished,
// as "<package> <test>": what was running when make was killed.
func openGoTests(raw []byte) ([]string, error) {
	ev, err := readGoTestEvents(raw)
	if err != nil {
		return nil, err
	}
	var open []string
	for k, s := range ev.tests {
		if s.runs > s.closes {
			open = append(open, k.pkg+" "+k.test)
		}
	}
	slices.Sort(open)
	return open, nil
}

// importPath is a ./package's import path, as go test -json names it: the
// module path of the nearest go.mod at or above it, and the rest.
func (a makeGoTestAdapter) importPath(pkg string) (string, error) {
	dir, rel, err := newGoTestAdapter(nil, a.root, 1).moduleOf(pkg)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			mod = strings.Trim(strings.TrimSpace(mod), `"`)
			if rel = strings.TrimPrefix(rel, "./"); rel == "." || rel == "" {
				return mod, nil
			}
			return mod + "/" + rel, nil
		}
	}
	return "", fmt.Errorf("%s/go.mod names no module", dir)
}
