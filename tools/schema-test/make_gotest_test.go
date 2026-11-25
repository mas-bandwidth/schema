package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// makeGoTestRow is one make-gotest row: Go test test of ./x, run by make
// gate-check of make/x.mk.
func makeGoTestRow(t *testing.T, test string) testreg.Instance {
	t.Helper()
	in, err := json.Marshal(makeGoTestInputs{makeInputs: makeInputs{File: "make/x.mk", Target: "gate-check"}, Package: "./x", Test: test})
	if err != nil {
		t.Fatal(err)
	}
	return testreg.Instance{Full: "none.nightly.tooling.x." + test, Lang: testreg.LangFree,
		Test: testreg.Test{Key: "tooling.x." + test, Tier: "nightly", Scope: testreg.LangFree, Adapter: "make-gotest", Type: "harness-run", Inputs: in}}
}

// goTestEvents is a go test -json run of example.com/x: each test name with
// its last action (pass, fail, skip), and a subtest written as Test/sub.
func goTestEvents(tests ...[2]string) string {
	var b strings.Builder
	line := func(action, test, output string) {
		ev := map[string]string{"Action": action, "Package": "example.com/x"}
		if test != "" {
			ev["Test"] = test
		}
		if output != "" {
			ev["Output"] = output
		}
		raw, _ := json.Marshal(ev) //errdiscard: a map of strings always marshals
		b.Write(raw)
		b.WriteByte('\n')
	}
	line("start", "", "")
	for _, tc := range tests {
		line("run", tc[0], "")
		line("output", tc[0], "=== RUN   "+tc[0]+"\n")
		if tc[1] == "fail" {
			line("output", tc[0], "    x_test.go:9: the planted failure\n")
		}
		line(tc[1], tc[0], "")
	}
	line("pass", "", "")
	return b.String()
}

// fakeMakeGoTest is a make-gotest adapter over a temporary root (module
// example.com, package ./x), a make file whose gate-check recipe runs
// $(GO_TEST), and a make that writes events to the file the adapter names
// and exits with runErr: no make or go test runs.
func fakeMakeGoTest(t *testing.T, events string, runErr error) makeGoTestAdapter {
	t.Helper()
	return fakeMakeGoTestLog(t, events, "ok", runErr)
}

// fakeMakeGoTestLog is fakeMakeGoTest whose make prints log.
func fakeMakeGoTestLog(t *testing.T, events, log string, runErr error) makeGoTestAdapter {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// the root as the driver names it, relative (".", in a run): the events
	// file must reach make by an absolute path all the same
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, root)
	if err != nil {
		t.Fatal(err)
	}
	recipe := "\t$(GO_TEST) -count=1 ./x\n"
	return makeGoTestAdapter{root: rel, mk: makeAdapter{
		read: func(p string) ([]byte, error) {
			if p != "make/x.mk" {
				return nil, errors.New("no such file")
			}
			return []byte("gate-check:\n" + recipe), nil
		},
		plan: func(context.Context, []string) (string, error) {
			return fakeDatabase("GO_TEST = tools/gotestrun/gotest\n", "", "gate-check:\n"+recipe), errors.New("exit status 2")
		},
		exec: func(ctx context.Context, _ []string) (string, error) {
			p := testreg.EnvFrom(ctx)[GoTestJSONEnv]
			if !filepath.IsAbs(p) {
				return "", errors.New("the events file was not named by an absolute path: " + p)
			}
			if events != "" {
				if err := os.WriteFile(p, []byte(events), 0o644); err != nil {
					return "", err
				}
			}
			return log, runErr
		},
	}}
}

// runMakeGoTest runs the rows of the named tests and returns their results
// by test name.
func runMakeGoTest(t *testing.T, a makeGoTestAdapter, tests ...string) map[string]testreg.Result {
	t.Helper()
	var rows []testreg.Instance
	for _, test := range tests {
		rows = append(rows, makeGoTestRow(t, test))
	}
	res, err := a.Run(context.Background(), testreg.LangFree, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(tests) {
		t.Fatalf("want one result per row, got %+v", res)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[strings.TrimPrefix(r.Instance, "none.nightly.tooling.x.")] = r
	}
	return got
}

// TestAMakeGoTestRowIsReadByItsExactName: each row's verdict is its own Go
// test's, read by package and name from the events the target's $(GO_TEST)
// wrote. Plants, each red by the row's name: a test the row claims that the
// events do not show run, a test that skipped (itself or a subtest), a test
// that failed, a run of 0 tests, a test the target ran that no row names,
// and make failing outside every test.
func TestAMakeGoTestRowIsReadByItsExactName(t *testing.T) {
	t.Parallel()
	t.Run("pass", func(t *testing.T) {
		t.Parallel()
		got := runMakeGoTest(t, fakeMakeGoTest(t, goTestEvents([2]string{"TestA", "pass"}, [2]string{"TestB", "pass"}), nil), "TestA", "TestB")
		for name, r := range got {
			if r.Verdict != testreg.Pass {
				t.Errorf("%s: %+v, want a pass", name, r)
			}
		}
	})
	cases := []struct {
		name   string
		events string
		runErr error
		rows   []string
		red    string // the row that goes red
		want   []string
	}{
		{"claimed but not run", goTestEvents([2]string{"TestA", "pass"}), nil, []string{"TestA", "TestGone"}, "TestGone",
			[]string{"never ran example.com/x TestGone"}},
		{"skipped", goTestEvents([2]string{"TestA", "pass"}, [2]string{"TestB", "skip"}), nil, []string{"TestA", "TestB"}, "TestB",
			[]string{"TestB skipped", "nothing is ever skipped"}},
		{"a subtest skipped", goTestEvents([2]string{"TestA", "pass"}, [2]string{"TestB/inner", "skip"}, [2]string{"TestB", "pass"}), nil, []string{"TestA", "TestB"}, "TestB",
			[]string{"TestB skipped (inner)"}},
		{"failed", goTestEvents([2]string{"TestA", "pass"}, [2]string{"TestB", "fail"}), errors.New("exit status 2"), []string{"TestA", "TestB"}, "TestB",
			[]string{"the planted failure"}},
		{"0 tests", goTestEvents(), nil, []string{"TestA"}, "TestA",
			[]string{"ran 0 Go tests"}},
		{"no events file", "", nil, []string{"TestA"}, "TestA",
			[]string{"ran 0 Go tests"}},
		{"a test no row names", goTestEvents([2]string{"TestA", "pass"}, [2]string{"TestStray", "pass"}), nil, []string{"TestA"}, "TestA",
			[]string{"no registry row of it names: example.com/x TestStray"}},
		{"make failed outside the tests", goTestEvents([2]string{"TestA", "pass"}), errors.New("exit status 2"), []string{"TestA"}, "TestA",
			[]string{"the recipe failed outside them"}},
		{"not an event", "FAIL example.com/x\n", nil, []string{"TestA"}, "TestA",
			[]string{"no go test -json event"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runMakeGoTest(t, fakeMakeGoTest(t, tc.events, tc.runErr), tc.rows...)
			r := got[tc.red]
			if r.Verdict != testreg.Fail {
				t.Fatalf("%s: %+v, want a fail", tc.red, r)
			}
			for _, w := range tc.want {
				if !strings.Contains(r.Reason, w) {
					t.Errorf("%s: reason %q, want %q in it", tc.red, r.Reason, w)
				}
			}
			if tc.red != "TestA" && got["TestA"].Verdict != testreg.Pass {
				t.Errorf("TestA, which passed, is %+v: one row's red is its own", got["TestA"])
			}
		})
	}
}

// TestAMakeGoTestTimeoutIsNamed: go test's own -timeout firing inside a
// make-gotest target fails the running test's row naming the limit and the
// test, never "not run" (the replay is go test's own output).
func TestAMakeGoTestTimeoutIsNamed(t *testing.T) {
	t.Parallel()
	got := runMakeGoTest(t, fakeMakeGoTest(t, goTestTimeoutReplay, errors.New("exit status 2")), "TestQuick", "TestBlocks")
	if got["TestQuick"].Verdict != testreg.Pass {
		t.Errorf("TestQuick passed before the limit: %+v", got["TestQuick"])
	}
	b := got["TestBlocks"]
	for _, w := range []string{"TIMED OUT", "-timeout of 1s", "TestBlocks was running"} {
		if b.Verdict != testreg.Fail || !strings.Contains(b.Reason, w) {
			t.Errorf("TestBlocks: %+v, want %q in its reason", b, w)
		}
	}
}

// TestAMakeGoTestTargetWithoutGoTestIsRefused: a target whose recipes run
// no $(GO_TEST) writes no events, so its rows fail before make runs.
func TestAMakeGoTestTargetWithoutGoTestIsRefused(t *testing.T) {
	t.Parallel()
	a := fakeMakeGoTest(t, goTestEvents([2]string{"TestA", "pass"}), nil)
	ran := false
	a.mk.plan = func(context.Context, []string) (string, error) {
		return fakeDatabase("", "", "gate-check:\n\t./bin/check\n"), errors.New("exit status 2")
	}
	exec := a.mk.exec
	a.mk.exec = func(ctx context.Context, argv []string) (string, error) { ran = true; return exec(ctx, argv) }
	got := runMakeGoTest(t, a, "TestA")
	if r := got["TestA"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "runs no go test through $(GO_TEST)") || ran {
		t.Errorf("TestA: %+v (make ran: %v), want a fail before make runs", r, ran)
	}
}

// TestAMakeGoTestReadsWhatTheEventsSayNotTheLogAlone: a skip go test
// reported is the skipped row's red by name, and the other rows keep their
// verdicts though gotestrun's copy of the skip is in make's log; a skip the
// log reports that no event names fails every row. A test started twice and
// finished once is no pass, and neither is a test whose package failed
// outside its tests. The plants: the log's skip refused before the events
// are read (TestA goes red), runs counted as booleans (TestA passes on its
// first run), and package-level fail events passed over.
func TestAMakeGoTestReadsWhatTheEventsSayNotTheLogAlone(t *testing.T) {
	t.Parallel()
	ev := func(lines ...string) string { return strings.Join(lines, "\n") + "\n" }
	cases := []struct {
		name, events, log string
		runErr            error
		rows              []string
		red               string
		green             []string
		want              string
	}{
		{"a go test skip in the log", goTestEvents([2]string{"TestA", "pass"}, [2]string{"TestB", "skip"}), "=== RUN   TestB\n--- SKIP: TestB (0.00s)\n", errors.New("exit status 1"),
			[]string{"TestA", "TestB"}, "TestB", []string{"TestA"}, "TestB skipped"},
		{"a skip no event names", goTestEvents([2]string{"TestA", "pass"}), "SKIP: the recipe skipped its check\n", nil,
			[]string{"TestA"}, "TestA", nil, "reported a skip"},
		{"started twice, finished once", ev(`{"Action":"run","Package":"example.com/x","Test":"TestA"}`, `{"Action":"pass","Package":"example.com/x","Test":"TestA"}`,
			`{"Action":"run","Package":"example.com/x","Test":"TestA"}`, `{"Action":"output","Package":"example.com/x","Output":"FAIL\texample.com/x\n"}`),
			"ok", errors.New("exit status 2"), []string{"TestA"}, "TestA", nil, "started TestA 2 times and it finished 1"},
		{"the package failed outside its tests", ev(`{"Action":"run","Package":"example.com/x","Test":"TestA"}`, `{"Action":"pass","Package":"example.com/x","Test":"TestA"}`,
			`{"Action":"output","Package":"example.com/x","Output":"TestMain: leaked a goroutine\n"}`, `{"Action":"fail","Package":"example.com/x"}`),
			"ok", nil, []string{"TestA"}, "TestA", nil, "TestMain: leaked a goroutine"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := runMakeGoTest(t, fakeMakeGoTestLog(t, c.events, c.log, c.runErr), c.rows...)
			if r := got[c.red]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, c.want) {
				t.Errorf("%s: %+v, want a fail saying %q", c.red, r, c.want)
			}
			for _, g := range c.green {
				if got[g].Verdict != testreg.Pass {
					t.Errorf("%s, which passed, is %+v: another row's skip is not its red", g, got[g])
				}
			}
		})
	}
}

// TestAMakeGoTestTargetRunningGoTestAnotherWayIsRefused: a target whose
// recipes run a go test other than through $(GO_TEST) (a bare go test,
// -json or not, or a variable that runs one) would run tests no events file
// names, so its rows fail before make runs: the make vet the make adapter
// applies refuses it. The plant is the vet's go test check left out: make
// runs, and those tests are never held to a row.
func TestAMakeGoTestTargetRunningGoTestAnotherWayIsRefused(t *testing.T) {
	t.Parallel()
	for _, other := range []string{"\tgo test -json ./y\n", "\t$(RUN_Y)\n"} {
		a := fakeMakeGoTest(t, goTestEvents([2]string{"TestA", "pass"}), nil)
		ran := false
		a.mk.plan = func(context.Context, []string) (string, error) {
			return fakeDatabase("GO_TEST = tools/gotestrun/gotest\nRUN_Y = go test -json ./y\n", "", "gate-check:\n\t$(GO_TEST) ./x\n"+other), errors.New("exit status 2")
		}
		exec := a.mk.exec
		a.mk.exec = func(ctx context.Context, argv []string) (string, error) { ran = true; return exec(ctx, argv) }
		got := runMakeGoTest(t, a, "TestA")
		if r := got["TestA"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "$(GO_TEST)") || !strings.Contains(r.Reason, "go test") || ran {
			t.Errorf("recipe line %q: TestA %+v (make ran: %v), want a fail before make runs", other, r, ran)
		}
	}
}

// TestAKilledMakeGoTestNamesTheTestsRunning: when the driver's deadline
// kills make, the call's error names the Go tests the events show started
// and unfinished, which the driver reports as its timeout. The context is
// cancelled before make returns, so no real time passes. The plant is the
// error without the events read: it names no test.
func TestAKilledMakeGoTestNamesTheTestsRunning(t *testing.T) {
	t.Parallel()
	a := fakeMakeGoTest(t, goTestTimeoutReplay, errors.New("signal: killed"))
	ctx, cancel := context.WithCancel(context.Background())
	exec := a.mk.exec
	a.mk.exec = func(c context.Context, argv []string) (string, error) {
		log, err := exec(c, argv)
		cancel() // the driver's deadline, reached while make ran
		return log, err
	}
	_, err := a.Run(ctx, testreg.LangFree, []testreg.Instance{makeGoTestRow(t, "TestQuick"), makeGoTestRow(t, "TestBlocks")})
	if err == nil || !strings.Contains(err.Error(), "the Go tests running were example.com/x TestBlocks") {
		t.Errorf("a killed make-gotest call: %v, want the running test named", err)
	}
}
