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
//     how long it ran, and every row it never reached naming the test that
//     was (goTestTimedOut);
//   - make exiting non-zero with no row failed fails every row with make's
//     words: the recipe failed outside the tests.
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
	if _, text := db.reach(name); !goTestWriter.MatchString(strings.Join(text, "\n")) {
		return failAll(rows, fmt.Sprintf("make %s runs no go test through $(GO_TEST), so it writes no go test -json events for these rows: a make-gotest row's target runs its Go tests through $(GO_TEST)", name)), nil
	}

	events := filepath.Join(a.root, filepath.FromSlash(makeGoTestJSON(name)))
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
	if ctx.Err() != nil {
		return nil, fmt.Errorf("make %s: %w\n%s", name, ctx.Err(), tail(log, 40))
	}
	raw, err := os.ReadFile(events)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
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
	if err := testreg.RefuseSkips(lines); err != nil {
		return failAll(rows, fmt.Sprintf("make %s: %v", target, err)), nil
	}
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

	type event struct {
		Action, Package, Test, Output string
	}
	type seen struct {
		ran, passed, failed bool
		skipped             []string
		output              []string
	}
	tests := map[goTestKey]*seen{}
	byPkg := map[string][]string{} // a package's events' text, for go test's own timeout
	ran := 0
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil || ev.Action == "" {
			return failAll(rows, fmt.Sprintf("make %s: %s holds a line that is no go test -json event: %q", target, makeGoTestJSON(target), sc.Text())), nil
		}
		byPkg[ev.Package] = append(byPkg[ev.Package], sc.Text())
		if ev.Test == "" {
			continue
		}
		top, sub, isSub := strings.Cut(ev.Test, "/")
		k := goTestKey{ev.Package, top}
		s := tests[k]
		if s == nil {
			s = &seen{}
			tests[k] = s
		}
		switch ev.Action {
		case "run":
			if !isSub {
				s.ran = true
				ran++
			}
		case "output":
			s.output = append(s.output, strings.TrimRight(ev.Output, "\n"))
		case "skip":
			if isSub {
				s.skipped = append(s.skipped, sub)
			} else {
				s.skipped = append(s.skipped, "(the test itself)")
			}
		case "pass":
			if !isSub {
				s.passed = true
			}
		case "fail":
			if !isSub {
				s.failed = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return failAll(rows, fmt.Sprintf("make %s: reading %s: %v", target, makeGoTestJSON(target), err)), nil
	}
	if ran == 0 {
		why := fmt.Sprintf("make %s ran 0 Go tests: its $(GO_TEST) wrote no test to %s", target, makeGoTestJSON(target))
		if runErr != nil {
			why += fmt.Sprintf("; make: %v\n%s", runErr, tail(log, 30))
		}
		return failAll(rows, why), nil
	}
	var unnamed []string
	for k, s := range tests {
		if s.ran && !named[k] {
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
		s := tests[k]
		var res testreg.Result
		to, timedOut := goTestTimedOut(strings.Join(byPkg[k.pkg], "\n"))
		switch {
		case timedOut && (s == nil || !s.passed):
			// go test's own -timeout fired in this test's package before the
			// test finished: the limit and the test running, never "not run"
			res = to.verdicts("in make "+target, map[string]string{k.test: r.Full}, nil)[0]
		case s == nil || !s.ran:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("make %s never ran %s %s: the row claims it and %s does not show it run", target, k.pkg, k.test, makeGoTestJSON(target))}
		case len(s.skipped) > 0:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("%s skipped (%s), and nothing is ever skipped: %s", k.test, strings.Join(s.skipped, ", "), tail(strings.Join(s.output, "\n"), 5))}
		case s.failed:
			res = testreg.Result{Instance: r.Full, Verdict: testreg.Fail, Reason: tail(strings.Join(s.output, "\n"), 20)}
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
