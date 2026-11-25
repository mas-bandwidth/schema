package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// needsRoot is a module with one package of two tests, TestReads reading the
// build "corpus" (test/registry/go-needs.txt) and TestFree reading nothing.
func needsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":            "module x\n\ngo 1.25\n",
		"g/a_test.go":       "package g\n\nimport \"testing\"\n\nfunc TestReads(t *testing.T) {}\n\nfunc TestFree(t *testing.T) {}\n",
		testreg.GoNeedsPath: "needs ./g TestReads corpus\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// plantedBuilder is a builder whose step for target already ran and answered
// err: run returns it at once and execs nothing.
func plantedBuilder(target string, err error) func(context.Context, []string) error {
	return (&builder{log: io.Discard, done: map[string]error{strings.Join(needsStep(target), "\x00"): err}}).run
}

// stubGoTest answers go test for every test the call names as passed, and
// records the -run of each call.
func stubGoTest(a *goTestAdapter) *[]string {
	var mu sync.Mutex
	var runs []string
	a.goTest = func(_ context.Context, _ string, args []string, stdout, _ io.Writer) error {
		var run string
		for i, s := range args {
			if s == "-run" && i+1 < len(args) {
				run = args[i+1]
			}
		}
		mu.Lock()
		runs = append(runs, run)
		mu.Unlock()
		for _, name := range []string{"TestReads", "TestFree"} {
			if strings.Contains(run, name) {
				if _, err := io.WriteString(stdout, `{"Action":"run","Test":"`+name+`"}`+"\n"+`{"Action":"pass","Test":"`+name+`","Elapsed":0}`+"\n"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return &runs
}

func verdicts(res []testreg.Result) map[string]testreg.Result {
	m := map[string]testreg.Result{}
	for _, r := range res {
		m[r.Instance] = r
	}
	return m
}

// TestAGoTestsDeclaredBuildFailsOnlyTheRowsThatReadIt: the gotest adapter
// builds what a row's Go test reads (go-needs.txt) through its builder, in
// Prepare and before the call; a build that failed fails exactly the rows
// that read it, by name and with the build's failure, and their go test does
// not run, while the other rows run and pass. Prepare does not fail the group
// for it. A needs table that cannot be read is Prepare's error.
func TestAGoTestsDeclaredBuildFailsOnlyTheRowsThatReadIt(t *testing.T) {
	t.Parallel()
	root := needsRoot(t)
	planted := errors.New("build step \"make --no-print-directory corpus\": exit status 2\nmake: *** no rule to make corpus")
	a := newGoTestAdapterBuilding(io.Discard, root, 1, plantedBuilder("corpus", planted))
	runs := stubGoTest(a)
	row := func(test string) testreg.Instance {
		return testreg.Instance{Full: "none.unit.g." + test, Test: testreg.Test{Seconds: 10, Inputs: []byte(`{"package":"./g","test":"` + test + `"}`)}}
	}
	insts := []testreg.Instance{row("TestReads"), row("TestFree")}
	if err := a.Prepare(context.Background(), "", insts); err != nil {
		t.Fatalf("Prepare failed the group for one row's build: %v", err)
	}
	res, err := a.Run(context.Background(), "", insts)
	if err != nil {
		t.Fatal(err)
	}
	got := verdicts(res)
	if r := got["none.unit.g.TestReads"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "(corpus) failed") || !strings.Contains(r.Reason, "no rule to make corpus") {
		t.Fatalf("the row reading the failed build: %+v, want a fail naming the build and its reason", r)
	}
	if r := got["none.unit.g.TestFree"]; r.Verdict != testreg.Pass {
		t.Fatalf("the row reading nothing: %+v, want it run and passed", r)
	}
	for _, run := range *runs {
		if strings.Contains(run, "TestReads") {
			t.Fatalf("go test ran TestReads (-run %s) with the build it reads failed", run)
		}
	}

	// a built need lets its reader run
	ok := newGoTestAdapterBuilding(io.Discard, root, 1, plantedBuilder("corpus", nil))
	stubGoTest(ok)
	res, err = ok.Run(context.Background(), "", insts)
	if err != nil {
		t.Fatal(err)
	}
	for name, r := range verdicts(res) {
		if r.Verdict != testreg.Pass {
			t.Fatalf("%s: %+v with its build made, want pass", name, r)
		}
	}

	// an unreadable needs table is the group's error
	if err := os.WriteFile(filepath.Join(root, testreg.GoNeedsPath), []byte("needs g TestReads\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := newGoTestAdapterBuilding(io.Discard, root, 1, plantedBuilder("corpus", nil))
	if err := bad.Prepare(context.Background(), "", insts); err == nil {
		t.Fatal("Prepare passed a malformed needs table")
	}
}

// TestAUnionRowsDeclaredBuildFailsOnlyTheRowsThatReadIt: the same for the
// union harness: a union row whose language's Go test reads a failed build
// fails by name and its go test does not run; the others run.
func TestAUnionRowsDeclaredBuildFailsOnlyTheRowsThatReadIt(t *testing.T) {
	t.Parallel()
	root := needsRoot(t)
	a := newGoTestAdapterBuilding(io.Discard, root, 1, plantedBuilder("corpus", errors.New("build step: exit status 2")))
	runs := stubGoTest(a)
	row := func(key, test string) testreg.Instance {
		return testreg.Instance{Full: "moo.unit.roundtrip." + key, Lang: "moo", Test: testreg.Test{Key: "roundtrip." + key, Family: "fam", Seconds: 10, Inputs: []byte(`{"tests":{"moo":{"package":"./g","test":"` + test + `"}}}`)}}
	}
	res, err := a.runUnion(context.Background(), "moo", []testreg.Instance{row("reads", "TestReads"), row("free", "TestFree")})
	if err != nil {
		t.Fatal(err)
	}
	got := verdicts(res)
	if r := got["moo.unit.roundtrip.reads"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "(corpus) failed") {
		t.Fatalf("the union row reading the failed build: %+v, want a fail naming the build", r)
	}
	if r := got["moo.unit.roundtrip.free"]; r.Verdict != testreg.Pass {
		t.Fatalf("the union row reading nothing: %+v, want it run and passed", r)
	}
	for _, run := range *runs {
		if strings.Contains(run, "TestReads") {
			t.Fatalf("go test ran TestReads (-run %s) with the build it reads failed", run)
		}
	}
}

// writeNoNeeds gives a test root an empty needs table: a root without one is
// refused (the table is read, never assumed empty).
func writeNoNeeds(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, testreg.GoNeedsPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// noNeedsRoot is a temporary test root holding an empty needs table.
func noNeedsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeNoNeeds(t, root)
	return root
}

// TestAPrepareCutShortByItsDeadlineSaysSo: a needs build that the Prepare
// deadline ended is the deadline's: Prepare returns the context's error, so
// the run names the timeout, never the killed build's own failure.
func TestAPrepareCutShortByItsDeadlineSaysSo(t *testing.T) {
	t.Parallel()
	root := needsRoot(t)
	a := newGoTestAdapterBuilding(io.Discard, root, 1, plantedBuilder("corpus", errors.New("build step: signal: killed")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inst := testreg.Instance{Full: "none.unit.g.TestReads", Test: testreg.Test{Seconds: 10, Inputs: []byte(`{"package":"./g","test":"TestReads"}`)}}
	if err := a.Prepare(ctx, "", []testreg.Instance{inst}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Prepare under an ended deadline: %v, want the context's error", err)
	}
}

// TestABuildItsDeadlineCutShortIsRunAgain: a build step its deadline ended
// is not recorded as a failed build: the next call runs it again and takes
// its answer. Run and the union harness return the deadline's error, never
// the killed build's as a row's failure. Planted: with every answer
// recorded, the second call answers "signal: killed" without running.
func TestABuildItsDeadlineCutShortIsRunAgain(t *testing.T) {
	t.Parallel()
	ran := 0
	var cancel context.CancelFunc
	b := &builder{log: io.Discard, done: map[string]error{}, step: func(ctx context.Context, _ []string) ([]byte, error) {
		ran++
		if cancel != nil {
			cancel()
			return nil, errors.New("signal: killed")
		}
		return nil, nil
	}}
	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	if err := b.run(ctx, needsStep("corpus")); err == nil {
		t.Fatal("the killed step answered nil")
	}
	cancel = nil
	if err := b.run(context.Background(), needsStep("corpus")); err != nil || ran != 2 {
		t.Fatalf("after a step its deadline killed: %v, ran %d times, want it run again and passed", err, ran)
	}
	if err := b.run(context.Background(), needsStep("corpus")); err != nil || ran != 2 {
		t.Fatalf("a step that passed: %v, ran %d times, want it answered from the record", err, ran)
	}

	root := needsRoot(t)
	inst := testreg.Instance{Full: "none.unit.g.TestReads", Test: testreg.Test{Seconds: 10, Inputs: []byte(`{"package":"./g","test":"TestReads"}`)}}
	union := testreg.Instance{Full: "moo.unit.roundtrip.reads", Lang: "moo", Test: testreg.Test{Key: "roundtrip.reads", Family: "fam", Seconds: 10, Inputs: []byte(`{"tests":{"moo":{"package":"./g","test":"TestReads"}}}`)}}
	for _, tc := range []struct {
		name string
		run  func(*goTestAdapter, context.Context) ([]testreg.Result, error)
	}{
		{"Run", func(a *goTestAdapter, ctx context.Context) ([]testreg.Result, error) {
			return a.Run(ctx, "", []testreg.Instance{inst})
		}},
		{"runUnion", func(a *goTestAdapter, ctx context.Context) ([]testreg.Result, error) {
			return a.runUnion(ctx, "moo", []testreg.Instance{union})
		}},
	} {
		ctx, cut := context.WithCancel(context.Background())
		a := newGoTestAdapterBuilding(io.Discard, root, 1, func(context.Context, []string) error {
			cut()
			return errors.New("build step: signal: killed")
		})
		runs := stubGoTest(a)
		res, err := tc.run(a, ctx)
		if !errors.Is(err, context.Canceled) || len(*runs) != 0 {
			t.Fatalf("%s with its build killed by the deadline: %v %+v (go test ran %d times), want the context's error", tc.name, err, res, len(*runs))
		}
	}
}

// TestTheUnionHarnessPrepareBuildsWhatItsRowsRead: the corpus adapter's
// Prepare builds, through its builder, what a union row's Go test reads, and
// a deadline that ends the build is Prepare's error. Planted: without the
// union step in harnessAdapter.Prepare, nothing is built.
func TestTheUnionHarnessPrepareBuildsWhatItsRowsRead(t *testing.T) {
	t.Chdir(needsRoot(t)) // the harness reads the needs table at the checkout root
	var built []string
	b := &builder{log: io.Discard, done: map[string]error{}, step: func(_ context.Context, argv []string) ([]byte, error) {
		built = append(built, strings.Join(argv, " "))
		return nil, nil
	}}
	union := testreg.Instance{Full: "moo.unit.roundtrip.reads", Lang: "moo", Test: testreg.Test{Key: "roundtrip.reads", Family: "fam", Seconds: 10, Inputs: []byte(`{"tests":{"moo":{"package":"./g","test":"TestReads"}}}`)}}
	h := &harnessAdapter{build: b, log: io.Discard}
	if err := h.Prepare(context.Background(), "moo", []testreg.Instance{union}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(built, ";") != "make corpus" {
		t.Fatalf("Prepare built %q, want the union row's need, make corpus", built)
	}

	ctx, cut := context.WithCancel(context.Background())
	killed := &harnessAdapter{log: io.Discard, build: &builder{log: io.Discard, done: map[string]error{}, step: func(context.Context, []string) ([]byte, error) {
		cut()
		return nil, errors.New("signal: killed")
	}}}
	if err := killed.Prepare(ctx, "moo", []testreg.Instance{union}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a union Prepare its deadline cut short: %v, want the context's error", err)
	}
}

// TestThePrepareStepsAreTheTargetsInTheOrderTheyAreDeclared: the gotest
// adapter's prepare steps are each build its rows name or their Go tests
// read, once each, in the order the needs table declares them, each named by
// its target, so the runner gives each its own limit. Planted: sorted, the
// step that builds the others (b-all) would come first.
func TestThePrepareStepsAreTheTargetsInTheOrderTheyAreDeclared(t *testing.T) {
	t.Parallel()
	root := needsRoot(t)
	if err := os.WriteFile(filepath.Join(root, testreg.GoNeedsPath), []byte("needs ./g TestReads z-first\nneeds ./g TestReads m-second\nneeds ./g .* z-first\nneeds ./g TestReads b-all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ran []string
	a := newGoTestAdapterBuilding(io.Discard, root, 1, func(_ context.Context, argv []string) error {
		ran = append(ran, argv[len(argv)-1])
		return nil
	})
	row := func(test string) testreg.Instance {
		return testreg.Instance{Full: "none.unit.g." + test, Test: testreg.Test{Seconds: 10, Inputs: []byte(`{"package":"./g","test":"` + test + `","make":"made-by-the-row"}`)}}
	}
	var names []string
	for _, s := range a.PrepareSteps("", []testreg.Instance{row("TestReads"), row("TestFree")}) {
		names = append(names, s.Name)
		if err := s.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	want := "made-by-the-row,z-first,m-second,b-all"
	if strings.Join(names, ",") != want || strings.Join(ran, ",") != want {
		t.Fatalf("steps %v, built %v; want %s", names, ran, want)
	}
}
