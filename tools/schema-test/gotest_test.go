package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestASubtestThatSkipsFailsItsParent: go test reports the parent of a
// skipped subtest as passed; the driver has no skipped verdict, so the
// parent fails and names the subtest. A subtest's failure output joins the
// parent's reason, and the parent's seconds are go test's own.
func TestASubtestThatSkipsFailsItsParent(t *testing.T) {
	t.Parallel()
	events := `{"Action":"run","Test":"TestA"}
{"Action":"run","Test":"TestA/needs_cc"}
{"Action":"output","Test":"TestA/needs_cc","Output":"no cc on PATH\n"}
{"Action":"skip","Test":"TestA/needs_cc"}
{"Action":"pass","Test":"TestA","Elapsed":0.5}
{"Action":"run","Test":"TestB"}
{"Action":"output","Test":"TestB/row","Output":"row 3: got 4\n"}
{"Action":"fail","Test":"TestB/row"}
{"Action":"fail","Test":"TestB","Elapsed":1.25}
{"Action":"pass","Test":"TestC","Elapsed":2}
{"Action":"pass","Test":"TestD","Elapsed":0}
`
	res, err := goTestResults(strings.NewReader(events), map[string]string{"TestA": "a", "TestB": "b", "TestC": "c", "TestD": "d"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if a := got["a"]; a.Verdict != testreg.Fail || !strings.Contains(a.Reason, "subtests skipped") || !strings.Contains(a.Reason, "needs_cc") {
		t.Errorf("a parent whose subtest skipped: %+v", a)
	}
	if b := got["b"]; b.Verdict != testreg.Fail || !strings.Contains(b.Reason, "row 3: got 4") || b.Seconds != 1.25 {
		t.Errorf("a parent whose subtest failed: %+v", b)
	}
	if c := got["c"]; c.Verdict != testreg.Pass || c.Seconds != 2 {
		t.Errorf("a passing test: %+v", c)
	}
	// a pass go test timed at 0.00 s was measured: it carries one tick
	if d := got["d"]; d.Verdict != testreg.Pass || d.Seconds != goTestTick {
		t.Errorf("a pass timed at 0.00 s: %+v", d)
	}
}

// TestAGoTestsSecondsCountItsParallelSubtests: a test weighs what it costs
// run alone: its own Elapsed plus every parallel subtest's under it, at any
// depth. go test -json gives TestGate an Elapsed of its body alone (0.01 s)
// though its parallel subtests a (29.99 s) and b (30 s, holding a parallel
// b/x of 5 s and a serial b/y already in b's 30 s) ran after it: it weighs
// 65 s. A serial subtest's parallel children are in its parent's Elapsed
// already (TestWrap/g holds them; TestWrap weighs its 10 s, not 50). A paused top-level test weighs its Elapsed, never its wait (TestPaused:
// 5 s). A failing parent weighs the same way (TestRed: 7 s). The wall time
// between events counts nothing: TestPaused's events are 105 s apart.
// Planted: with Elapsed alone, TestGate weighs 0.01 s and TestRed 0.
func TestAGoTestsSecondsCountItsParallelSubtests(t *testing.T) {
	t.Parallel()
	events := `{"Time":"2026-11-25T10:00:00Z","Action":"run","Test":"TestGate"}
{"Time":"2026-11-25T10:00:00Z","Action":"run","Test":"TestPaused"}
{"Time":"2026-11-25T10:00:00Z","Action":"pause","Test":"TestPaused"}
{"Action":"run","Test":"TestGate/a"}
{"Action":"pause","Test":"TestGate/a"}
{"Action":"run","Test":"TestGate/b"}
{"Action":"pause","Test":"TestGate/b"}
{"Action":"cont","Test":"TestGate/a"}
{"Action":"cont","Test":"TestGate/b"}
{"Action":"run","Test":"TestGate/b/x"}
{"Action":"pause","Test":"TestGate/b/x"}
{"Action":"run","Test":"TestGate/b/y"}
{"Action":"pass","Test":"TestGate/b/y","Elapsed":3}
{"Action":"cont","Test":"TestGate/b/x"}
{"Action":"pass","Test":"TestGate/b/x","Elapsed":5}
{"Action":"pass","Test":"TestGate/a","Elapsed":29.99}
{"Action":"pass","Test":"TestGate/b","Elapsed":30}
{"Action":"pass","Test":"TestGate","Elapsed":0.01}
{"Time":"2026-11-25T10:01:40Z","Action":"cont","Test":"TestPaused"}
{"Time":"2026-11-25T10:01:45Z","Action":"pass","Test":"TestPaused","Elapsed":5}
{"Action":"run","Test":"TestWrap"}
{"Action":"run","Test":"TestWrap/g"}
{"Action":"run","Test":"TestWrap/g/1"}
{"Action":"pause","Test":"TestWrap/g/1"}
{"Action":"run","Test":"TestWrap/g/2"}
{"Action":"pause","Test":"TestWrap/g/2"}
{"Action":"cont","Test":"TestWrap/g/1"}
{"Action":"cont","Test":"TestWrap/g/2"}
{"Action":"pass","Test":"TestWrap/g/1","Elapsed":10}
{"Action":"pass","Test":"TestWrap/g/2","Elapsed":10}
{"Action":"pass","Test":"TestWrap/g","Elapsed":0}
{"Action":"pass","Test":"TestWrap","Elapsed":10}
{"Action":"run","Test":"TestRed"}
{"Action":"run","Test":"TestRed/a"}
{"Action":"pause","Test":"TestRed/a"}
{"Action":"cont","Test":"TestRed/a"}
{"Action":"fail","Test":"TestRed/a","Elapsed":7}
{"Action":"fail","Test":"TestRed","Elapsed":0}
`
	res, err := goTestResults(strings.NewReader(events), map[string]string{"TestGate": "gate", "TestPaused": "paused", "TestRed": "red", "TestWrap": "wrap"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	near := func(a, b float64) bool { return a-b < 1e-6 && b-a < 1e-6 }
	if g := got["gate"]; g.Verdict != testreg.Pass || !near(g.Seconds, 65) {
		t.Errorf("a parent of parallel subtests weighs %v s, want 65 s: %+v", g.Seconds, g)
	}
	if p := got["paused"]; p.Verdict != testreg.Pass || !near(p.Seconds, 5) {
		t.Errorf("a paused parallel test weighs %v s, want its Elapsed, 5 s: %+v", p.Seconds, p)
	}
	if w := got["wrap"]; w.Verdict != testreg.Pass || !near(w.Seconds, 10) {
		t.Errorf("a parent of a serial subtest holding parallel ones weighs %v s, want its own 10 s: %+v", w.Seconds, w)
	}
	if r := got["red"]; r.Verdict != testreg.Fail || !near(r.Seconds, 7) {
		t.Errorf("a failing parent of a parallel subtest weighs %v s, want 7 s: %+v", r.Seconds, r)
	}
}

// TestARowWhoseGoTestIsGoneIsMissing: the gotest adapter answers a row only
// when its package's source declares the test, so a deleted or renamed test
// still registered is MISSING by name, never run and never passed. A test
// written inside a fixture string is not declared.
func TestARowWhoseGoTestIsGoneIsMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package pkg\n\nimport \"testing\"\n\nfunc TestHere(t *testing.T) {}\n\nfunc FuzzHere(f *testing.F) {}\n\nvar fixture = `\nfunc TestInAString(t *testing.T) {}\n`\n"
	if err := os.WriteFile(filepath.Join(root, "pkg", "a_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newGoTestAdapter(nil, root, 1)
	row := func(test string) testreg.Test {
		return testreg.Test{Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(`{"package":"./pkg","test":"` + test + `"}`)}
	}
	for test, want := range map[string]bool{"TestHere": true, "FuzzHere": true, "TestGone": false, "TestHer": false, "TestInAString": false} {
		if got := a.Answers(row(test)); got != want {
			t.Errorf("Answers(%s) = %v, want %v", test, got, want)
		}
	}
	if a.Answers(testreg.Test{Inputs: []byte(`{"package":"./pkg","test":"TestHere"}`)}) {
		t.Error("a per-language row is answered by the gotest adapter")
	}
	if a.Answers(testreg.Test{Scope: testreg.LangFree, Inputs: []byte(`{"package":"pkg","test":"TestHere"}`)}) {
		t.Error("a row whose package is not ./ relative is answered")
	}
}

// TestAPackageInAModuleOfItsOwnRunsFromThatModule: go test runs a package
// from the nearest directory holding a go.mod, the root's or its own.
func TestAPackageInAModuleOfItsOwnRunsFromThatModule(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, f := range []string{"go.mod", "a/b/go.mod", "a/b/c/x_test.go", "d/y_test.go"} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newGoTestAdapter(nil, root, 1)
	for pkg, want := range map[string][2]string{
		"./a/b/c": {filepath.Join(root, "a/b"), "./c"},
		"./a/b":   {filepath.Join(root, "a/b"), "./."},
		"./d":     {root, "./d"},
	} {
		dir, rel, err := a.moduleOf(pkg)
		if err != nil || dir != want[0] || rel != want[1] {
			t.Errorf("moduleOf(%s) = %s %s %v, want %s %s", pkg, dir, rel, err, want[0], want[1])
		}
	}
	if _, _, err := newGoTestAdapter(nil, t.TempDir(), 1).moduleOf("./x"); err == nil {
		t.Error("a package in no module was given one")
	}
}

// TestAGoTestFamilyNeverTakesTheCorpusPrefix: a family beginning
// conformance- is a conformance corpus row's, so a Go test package under
// test/conformance takes its family from the rest of its path, and the
// registry's Go test rows hold none of the corpus's families.
func TestAGoTestFamilyNeverTakesTheCorpusPrefix(t *testing.T) {
	t.Parallel()
	for dir, want := range map[string]string{"test/conformance/harness": "harness", "test/conformance/packet": "packet", "test/conformance/go/rows": "go-rows"} {
		if got := familyOf(dir); got != want {
			t.Errorf("familyOf(%s) = %s, want %s", dir, got, want)
		}
	}
	reg, err := testreg.Load(filepath.Join("..", "..", testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range reg.Tests {
		if row.Adapter == "gotest" && strings.HasPrefix(row.Family, corpusFamilyPrefix) {
			t.Errorf("%s: a Go test row in the corpus's family %s", row.Key, row.Family)
		}
	}
}

// TestRowsAreNamedFromTheirGoTests: a moved Go test's family, case and type
// come from its package and name.
func TestRowsAreNamedFromTheirGoTests(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"TestEveryDriverTestIsRegistered": "every-driver-test-is-registered",
		"TestCPPJSONArrayRoot":            "cppjson-array-root",
		"TestIssue710PacketStringBounds":  "issue710-packet-string-bounds",
		"Test_snake_Case":                 "snake-case",
		"FuzzReadText":                    "fuzz-read-text",
		"Test":                            "test",
	} {
		if got := caseOf(name); got != want {
			t.Errorf("caseOf(%s) = %s, want %s", name, got, want)
		}
	}
	for dir, want := range map[string]string{"internal/ci/hostilecorpus": "ci-hostilecorpus", "cmd/schema": "cmd-schema", "tools/realpacket-gen": "realpacket-gen", "compiler": "compiler"} {
		if got := familyOf(dir); got != want {
			t.Errorf("familyOf(%s) = %s, want %s", dir, got, want)
		}
	}
	for _, c := range [][3]string{
		{"internal/fuzz", "FuzzPipeline", "fuzz"}, {"internal/ci", "TestX", "tooling"}, {"tools/roadmap", "TestX", "tooling"},
		{"bench/paired", "TestX", "bench"}, {"bench/tools", "TestX", "tooling"}, {"compiler", "TestX", "compiler"}, {"internal/check", "TestX", "compiler"},
	} {
		if got := typeOf(c[0], c[1]); got != c[2] {
			t.Errorf("typeOf(%s, %s) = %s, want %s", c[0], c[1], got, c[2])
		}
	}
}

// TestSecondsAreReadFromARunsLog: schema-test seconds records the seconds a
// log shows for each passing instance, keeps every other byte of the
// registry, and refuses a log with no such line and a name that is no
// instance.
func TestSecondsAreReadFromARunsLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry.jsonl")
	data, err := os.ReadFile(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reg, data, 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("good.log", "2026-11-24T00:00:00Z PASS go.functional.roundtrip.fam.one (1.234s)\nFAIL go.functional.roundtrip.fam.one (9.00s): broken\n")
	var out strings.Builder
	if err := runErr(t.Context(), []string{"seconds", "--registry", reg, good}, &out, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"case":"one",`) || !strings.Contains(string(after), `,"seconds":1.24}`) || len(after) != len(data)+len(`,"seconds":1.24`) {
		t.Fatalf("the registry after seconds:\n%s", after)
	}
	for log, says := range map[string]string{
		write("none.log", "schema-test PASS: expected 1\n"):                     "shows no passing test",
		write("stranger.log", "PASS go.functional.roundtrip.fam.two (1.00s)\n"): "unknown test",
	} {
		if err := runErr(t.Context(), []string{"seconds", "--registry", reg, log}, &out, nil); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: %v, want %q", log, err, says)
		}
	}
}

// TestGoTestRunsToTheDriversDeadline: go test's -timeout is what is left of
// the adapter call's deadline, so go test's own ten-minute default never cuts
// a package short of the driver's --timeout; with no deadline it is 0 (none).
// The plant is go test's default: a 25-minute deadline that yields 10m is red.
func TestGoTestRunsToTheDriversDeadline(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	if got := goTestTimeout(context.Background(), now); got != "0" {
		t.Errorf("no deadline: -timeout %s, want 0", got)
	}
	ctx, cancel := context.WithDeadline(context.Background(), now.Add(25*time.Minute))
	defer cancel()
	if got := goTestTimeout(ctx, now); got != "24m50s" {
		t.Errorf("a 25-minute deadline: -timeout %s, want 24m50s, its margin before the driver's kill (go test's default is 10m0s)", got)
	}
	if got := goTestTimeout(ctx, now.Add(25*time.Minute+time.Hour)); got != "1s" {
		t.Errorf("a passed deadline: -timeout %s, want 1s (0 would mean none)", got)
	}
}

// TestAGoTestThatExitsNonZeroFailsItsRows: a go test whose tests all passed
// and that still exits non-zero (a TestMain's check after m.Run) fails every
// row with go test's words; one with a failing test keeps its verdicts; one
// that reported nothing is the call's error; a clean exit keeps the passes.
// The plant is packageVerdict ignoring the exit: the rows would pass.
func TestAGoTestThatExitsNonZeroFailsItsRows(t *testing.T) {
	t.Parallel()
	pass := []testreg.Result{{Instance: "a", Verdict: testreg.Pass, Seconds: 1}, {Instance: "b", Verdict: testreg.Pass}}
	exit1 := errors.New("exit status 1")
	res, err := packageVerdict("./x", pass, exit1, "FAIL: the tree changed under the tests\n", "")
	if err != nil || len(res) != 2 {
		t.Fatalf("passing tests, exit 1: %v %v", res, err)
	}
	for _, r := range res {
		if r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "the tree changed under the tests") || !strings.Contains(r.Reason, "every test it ran passed") {
			t.Errorf("row %s of a run that exited 1: %+v", r.Instance, r)
		}
	}
	mixed := []testreg.Result{{Instance: "a", Verdict: testreg.Pass}, {Instance: "b", Verdict: testreg.Fail, Reason: "own"}}
	if res, err := packageVerdict("./x", mixed, exit1, "", ""); err != nil || res[0].Verdict != testreg.Pass || res[1].Reason != "own" {
		t.Errorf("a failing test's run keeps its verdicts: %+v %v", res, err)
	}
	if _, err := packageVerdict("./x", nil, exit1, "", "cannot build"); err == nil || !strings.Contains(err.Error(), "cannot build") {
		t.Errorf("a run that reported nothing: %v", err)
	}
	if res, err := packageVerdict("./x", pass, nil, "", ""); err != nil || res[0].Verdict != testreg.Pass {
		t.Errorf("a clean run: %+v %v", res, err)
	}
}

// TestAPackagesRowsAreCutIntoCallsByWeight: the adapter cuts one package's
// rows into go test calls of at most its call budget of measured seconds,
// in order, so no call outweighs its shard; a row heavier than the budget
// is a call alone. The
// plant is a budget that lets everything into one call: the heavy package
// is one call again, over budget, and the check names it.
func TestAPackagesRowsAreCutIntoCallsByWeight(t *testing.T) {
	t.Parallel()
	inst := func(name string, s float64) testreg.Instance {
		return testreg.Instance{Full: name, Test: testreg.Test{Seconds: s}}
	}
	rows := []testreg.Instance{inst("a", 10), inst("b", 10), inst("c", 10), inst("d", 40), inst("e", 0)}
	cut := func(budget float64) []goTestCall {
		var calls []goTestCall
		for _, r := range rows {
			calls = addToCall(calls, "Test"+r.Full, r, budget)
		}
		return calls
	}
	over := func(calls []goTestCall, budget float64) []string {
		var bad []string
		for i, c := range calls {
			if c.seconds > budget && len(c.tests) > 1 {
				bad = append(bad, fmt.Sprintf("call %d weighs %.0f s", i, c.seconds))
			}
		}
		return bad
	}
	calls := cut(20)
	var shape []int
	for _, c := range calls {
		shape = append(shape, len(c.tests))
	}
	if fmt.Sprint(shape) != "[2 1 1 1]" || len(over(calls, 20)) > 0 {
		t.Errorf("rows of 10,10,10,40,unmeasured at a 20 s budget cut into %v: %v", shape, over(calls, 20))
	}
	if a := newGoTestAdapter(nil, ".", 4); a.callBudget() != testreg.ShardBudget {
		t.Errorf("a call budget of %v, not one shard's", a.callBudget())
	}
	if bad := over(cut(1000), 20); len(bad) == 0 {
		t.Error("plant: one call holding every row was not named over the 20 s budget")
	}
}

// TestAGoTestRowWhoseDataIsGoneFails: a row's "files" are the data its test
// reads; a file the tree does not hold fails that row by name, before any go
// test runs, and a row whose files are all there is not refused. Planted:
// with the check off, the row would run its test instead.
func TestAGoTestRowWhoseDataIsGoneFails(t *testing.T) {
	t.Parallel()
	root := noNeedsRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "testdata", "here.tsv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if why := missingFiles(root, []string{"testdata/here.tsv"}); why != "" {
		t.Fatalf("a file that is there: %s", why)
	}
	if why := missingFiles(root, []string{"testdata", "testdata/here.tsv", "testdata/gone.tsv"}); !strings.Contains(why, "testdata, testdata/gone.tsv") || strings.Contains(why, "here.tsv") {
		t.Fatalf("a directory and a missing file: %q", why)
	}
	inst := testreg.Instance{Full: "none.unit.tooling.x.gone", Lang: testreg.LangFree, Test: testreg.Test{
		Key: "tooling.x.gone", Scope: testreg.LangFree, Adapter: "gotest",
		Inputs: []byte(`{"package":"./x","test":"TestGone","files":["testdata/gone.tsv"]}`)}}
	res, err := newGoTestAdapter(nil, root, 1).Run(context.Background(), testreg.LangFree, []testreg.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Instance != inst.Full || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "testdata/gone.tsv") {
		t.Fatalf("results %+v", res)
	}
}

// TestAGoTestRowsMakeTargetIsBuiltFirst: a row naming a make target in its
// inputs has that target made once in Prepare, before any call runs, however
// many rows name it; a row naming none makes nothing; a target that does not
// make fails Prepare naming the row and the target; and a make input that is
// not one target is refused as the row's inputs.
func TestAGoTestRowsMakeTargetIsBuiltFirst(t *testing.T) {
	t.Parallel()
	row := func(full, inputs string) testreg.Instance {
		return testreg.Instance{Full: full, Test: testreg.Test{Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(inputs)}}
	}
	rows := []testreg.Instance{
		row("a", `{"package":"./p","test":"TestA","make":"build/tree/.stamp"}`),
		row("b", `{"package":"./p","test":"TestB","make":"build/tree/.stamp"}`),
		row("c", `{"package":"./p","test":"TestC"}`),
	}
	var made [][]string
	a := newGoTestAdapterBuilding(nil, noNeedsRoot(t), 1, func(_ context.Context, argv []string) error {
		made = append(made, argv)
		return nil
	})
	if err := a.Prepare(context.Background(), "", rows); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(made) != "[[make build/tree/.stamp]]" {
		t.Fatalf("made %v, want the one target once", made)
	}

	var tried [][]string
	broken := newGoTestAdapterBuilding(nil, noNeedsRoot(t), 1, func(_ context.Context, argv []string) error {
		tried = append(tried, argv)
		return errors.New("build step failed")
	})
	err := broken.Prepare(context.Background(), "", rows)
	if err == nil || !strings.Contains(err.Error(), "a: build step failed") || fmt.Sprint(tried) != "[[make build/tree/.stamp]]" {
		t.Fatalf("a target that does not make: got %v, want the row named", err)
	}

	none := newGoTestAdapter(nil, noNeedsRoot(t), 1)
	if err := none.Prepare(context.Background(), "", rows[2:]); err != nil {
		t.Fatalf("a row naming no target: %v", err)
	}
	if err := none.Prepare(context.Background(), "", rows[:1]); err == nil || !strings.Contains(err.Error(), "make build/tree/.stamp") {
		t.Fatalf("no builder: got %v, want the target named", err)
	}

	if _, err := goTestInputsOf(row("d", `{"package":"./p","test":"TestD","make":"a b; rm"}`).Test); err == nil {
		t.Fatal("a make input that is not one target was accepted")
	}
	if _, err := goTestInputsOf(row("e", `{"package":"./p","test":"TestE","subtest":"a/b"}`).Test); err == nil {
		t.Fatal("a subtest name with a slash was accepted")
	}
}

// TestASubtestRowTakesThatSubtestsVerdict: a row that names a subtest gets
// that subtest's pass or fail, and a parent event does not flip a subtest
// that passed. A row that does not name a subtest still runs as the top-level
// test. The plant is go test exiting non-zero after the subtest passed.
func TestASubtestRowTakesThatSubtestsVerdict(t *testing.T) {
	t.Parallel()
	events := strings.Join([]string{
		`{"Action":"output","Test":"TestP/ok","Output":"=== RUN   TestP/ok\n"}`,
		`{"Action":"pass","Test":"TestP/ok","Elapsed":0.01}`,
		`{"Action":"output","Test":"TestP/bad","Output":"--- FAIL: TestP/bad\n"}`,
		`{"Action":"output","Test":"TestP/bad","Output":"    x_test.go:1: bad case\n"}`,
		`{"Action":"fail","Test":"TestP/bad","Elapsed":0.02}`,
		`{"Action":"output","Test":"TestP/skip","Output":"--- SKIP: TestP/skip\n"}`,
		`{"Action":"skip","Test":"TestP/skip"}`,
		`{"Action":"output","Test":"TestP","Output":"--- FAIL: TestP\n"}`,
		`{"Action":"fail","Test":"TestP","Elapsed":0.03}`,
		`{"Action":"pass","Test":"TestP/other","Elapsed":1}`,
		"",
	}, "\n")
	want := map[string]string{"TestP/ok": "ok", "TestP/bad": "bad", "TestP/skip": "skip"}
	res, other, err := goTestSubtestResults(strings.NewReader(events), want)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) == 0 || !strings.Contains(strings.Join(other, "\n"), "FAIL: TestP") {
		t.Fatalf("parent output was not kept aside: %q", other)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if len(got) != 3 {
		t.Fatalf("results %+v, want the three asked subtests and not the other", res)
	}
	if got["ok"].Verdict != testreg.Pass || got["ok"].Seconds != 0.01 {
		t.Fatalf("a passing subtest beside a failing parent: %+v", got["ok"])
	}
	if got["bad"].Verdict != testreg.Fail || !strings.Contains(got["bad"].Reason, "TestP/bad") || !strings.Contains(got["bad"].Reason, "bad case") {
		t.Fatalf("a failing subtest: %+v", got["bad"])
	}
	if got["skip"].Verdict != testreg.Fail || !strings.Contains(got["skip"].Reason, "skipped") {
		t.Fatalf("a skipped subtest: %+v", got["skip"])
	}

	root := noNeedsRoot(t)
	for name, src := range map[string]string{
		"go.mod":      "module x\n\ngo 1.25\n",
		"p/a_test.go": "package p\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) {}\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newGoTestAdapter(nil, root, 1)
	var runs []string
	a.goTest = func(_ context.Context, dir string, args []string, stdout, _ io.Writer) error {
		runs = append(runs, strings.Join(args, " "))
		m, held := goTestPackageLocks.Load(filepath.Join(dir, args[len(args)-1]))
		if !held {
			t.Errorf("go test %v runs with no lock", args)
		} else if mu := m.(*sync.Mutex); mu.TryLock() {
			mu.Unlock()
			t.Errorf("go test %v runs without holding its package lock", args)
		}
		run := strings.Join(args, " ")
		if strings.Contains(run, "$/^(") {
			if _, err := io.WriteString(stdout, strings.Join([]string{
				`{"Action":"pass","Test":"TestP/ok","Elapsed":0}`,
				`{"Action":"output","Test":"TestP/bad","Output":"--- FAIL: TestP/bad\n"}`,
				`{"Action":"fail","Test":"TestP/bad","Elapsed":0.02}`,
				`{"Action":"output","Test":"TestP","Output":"parent check failed\n"}`,
				`{"Action":"fail","Test":"TestP"}`,
				"",
			}, "\n")); err != nil {
				return err
			}
			return errors.New("exit status 1")
		}
		_, err := io.WriteString(stdout, `{"Action":"pass","Test":"TestP","Elapsed":0.04}`+"\n")
		return err
	}
	row := func(full, inputs string) testreg.Instance {
		return testreg.Instance{Full: full, Lang: testreg.LangFree, Test: testreg.Test{
			Key: full, Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(inputs)}}
	}
	res, err = a.Run(context.Background(), testreg.LangFree, []testreg.Instance{
		row("parent", `{"package":"./p","test":"TestP"}`),
		row("ok", `{"package":"./p","test":"TestP","subtest":"ok"}`),
		row("bad", `{"package":"./p","test":"TestP","subtest":"bad"}`),
		row("spaced", `{"package":"./p","test":"TestP","subtest":"control: slot names"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]testreg.Result{}
	for _, r := range res {
		by[r.Instance] = r
	}
	if by["parent"].Verdict != testreg.Pass {
		t.Fatalf("the top-level row: %+v", by["parent"])
	}
	if by["ok"].Verdict != testreg.Pass || by["ok"].Seconds != goTestTick {
		t.Fatalf("a subtest that passed while its parent failed: %+v", by["ok"])
	}
	if by["bad"].Verdict != testreg.Fail || !strings.Contains(by["bad"].Reason, "TestP/bad") {
		t.Fatalf("the failing subtest: %+v", by["bad"])
	}
	if _, ok := by["spaced"]; ok {
		t.Fatalf("an asked subtest with no event was reported: %+v", by["spaced"])
	}
	if len(runs) != 2 {
		t.Fatalf("go test runs:\n%s", strings.Join(runs, "\n"))
	}
	plain, sub := runs[0], runs[1]
	if !strings.Contains(plain, "-run ^(TestP)$ ./p") {
		t.Fatalf("top-level -run: %s", plain)
	}
	if !strings.Contains(sub, "-run ^TestP$/^(bad|control: slot names|ok)$ ./p") {
		t.Fatalf("subtest -run: %s", sub)
	}
}
