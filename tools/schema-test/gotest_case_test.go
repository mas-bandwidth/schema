package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

func TestASubtestRowKeepsItsOwnVerdict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "p", "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.schema", "b.schema", ".c.schema"} {
		if err := os.WriteFile(filepath.Join(root, "p", "testdata", name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	row := func(full, test, sub, file string) testreg.Instance {
		in := `{"package":"./p","test":"` + test + `"`
		if sub != "" {
			in += `,"subtest":"` + sub + `"`
		}
		if file != "" {
			in += `,"files":["p/testdata/` + file + `"]`
		}
		in += `}`
		return testreg.Instance{Full: full, Lang: testreg.LangFree, Test: testreg.Test{
			Key: full, Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(in),
		}}
	}
	instances := []testreg.Instance{
		row("parent", "TestOther", "", ""),
		row("dot", "TestParent", ".c.schema", ".c.schema"),
		row("ok", "TestParent", "a.schema", "a.schema"),
		row("bad", "TestParent", "b.schema", "b.schema"),
		row("gone", "TestParent", "gone.schema", "gone.schema"),
	}
	var calls [][]string
	a := newGoTestAdapter(io.Discard, root, 1)
	a.goTest = func(_ context.Context, _ string, args []string, stdout, _ io.Writer) error {
		calls = append(calls, args)
		run := ""
		for i, arg := range args {
			if arg == "-run" {
				run = args[i+1]
			}
		}
		if run == "^(TestOther)$" {
			_, err := io.WriteString(stdout, `{"Action":"pass","Test":"TestOther","Elapsed":0.02}`+"\n")
			return err
		}
		events := "" +
			`{"Action":"output","Test":"TestParent","Output":"parent failed after the subtests\n"}` + "\n" +
			`{"Action":"fail","Test":"TestParent","Elapsed":0.50}` + "\n" +
			`{"Action":"output","Test":"TestParent/.c.schema","Output":"dot ok\n"}` + "\n" +
			`{"Action":"pass","Test":"TestParent/.c.schema","Elapsed":0}` + "\n" +
			`{"Action":"output","Test":"TestParent/a.schema","Output":"a ok\n"}` + "\n" +
			`{"Action":"pass","Test":"TestParent/a.schema","Elapsed":0.01}` + "\n" +
			`{"Action":"output","Test":"TestParent/b.schema","Output":"b.schema: want the answer\n"}` + "\n" +
			`{"Action":"fail","Test":"TestParent/b.schema","Elapsed":0.03}` + "\n" +
			`{"Action":"output","Test":"TestParent/skip.schema","Output":"skipped\n"}` + "\n" +
			`{"Action":"skip","Test":"TestParent/skip.schema"}` + "\n"
		_, err := io.WriteString(stdout, events)
		if err != nil {
			return err
		}
		return errors.New("exit status 1")
	}
	res, err := a.Run(context.Background(), testreg.LangFree, instances)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if got["parent"].Verdict != testreg.Pass || got["parent"].Seconds != 0.02 {
		t.Errorf("a row that names no subtest still takes the top-level verdict: %+v", got["parent"])
	}
	if got["dot"].Verdict != testreg.Pass || got["ok"].Verdict != testreg.Pass {
		t.Errorf("a passing subtest stays green when its parent fails: dot %+v ok %+v", got["dot"], got["ok"])
	}
	if got["ok"].Seconds != 0.01 || got["dot"].Seconds != goTestTick {
		t.Errorf("subtest seconds: ok %v dot %v", got["ok"].Seconds, got["dot"].Seconds)
	}
	if got["bad"].Verdict != testreg.Fail || !strings.Contains(got["bad"].Reason, "b.schema") || !strings.Contains(got["bad"].Reason, "want the answer") {
		t.Errorf("the failing subtest is red by its own name: %+v", got["bad"])
	}
	if got["gone"].Verdict != testreg.Fail || !strings.Contains(got["gone"].Reason, "p/testdata/gone.schema") {
		t.Errorf("a missing file fails that row before go test: %+v", got["gone"])
	}
	if len(calls) != 2 {
		t.Fatalf("go test calls: %d, want the parent row and one subtest call", len(calls))
	}
	var runs []string
	for _, args := range calls {
		for i, arg := range args {
			if arg == "-run" {
				runs = append(runs, args[i+1])
			}
		}
	}
	if runs[0] != "^(TestOther)$" {
		t.Errorf("a row that names no subtest still runs ^ (names) $: %q", runs[0])
	}
	wantRun := `^TestParent$/^(\.c\.schema|a\.schema|b\.schema)$`
	if runs[1] != wantRun {
		t.Errorf("subtest -run %q, want %q", runs[1], wantRun)
	}
}

func TestASubtestNameWithASlashIsRefused(t *testing.T) {
	t.Parallel()
	_, err := goTestInputsOf(testreg.Test{Inputs: []byte(`{"package":"./p","test":"TestA","subtest":"a/b"}`)})
	if err == nil || !strings.Contains(err.Error(), "subtest") {
		t.Fatalf("slash: %v", err)
	}
	if _, err := goTestInputsOf(testreg.Test{Inputs: []byte(`{"package":"./p","test":"TestA"}`)}); err != nil {
		t.Fatalf("a row that names no subtest: %v", err)
	}
}

func TestTwoRowsCannotNameOneSubtest(t *testing.T) {
	t.Parallel()
	row := func(full, sub string) goTestCase {
		return goTestCase{
			in:   goTestInputs{Package: "./p", Test: "TestA", Subtest: sub},
			inst: testreg.Instance{Full: full},
		}
	}
	a := newGoTestAdapter(io.Discard, t.TempDir(), 1)
	a.goTest = func(context.Context, string, []string, io.Writer, io.Writer) error {
		t.Error("go test ran for a duplicated subtest")
		return nil
	}
	_, err := a.runGoTestCases(context.Background(), []goTestCase{row("one", "a.schema"), row("two", "a.schema")})
	if err == nil || !strings.Contains(err.Error(), "both name TestA/a.schema") {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestSubtestCallsSplitOnTheShardBudget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := func(full, sub string, seconds float64) goTestCase {
		return goTestCase{
			in:   goTestInputs{Package: "./p", Test: "TestA", Subtest: sub},
			inst: testreg.Instance{Full: full, Test: testreg.Test{Seconds: seconds}},
		}
	}
	var runs []string
	a := newGoTestAdapter(io.Discard, root, 1)
	a.goTest = func(_ context.Context, _ string, args []string, stdout, _ io.Writer) error {
		for i, arg := range args {
			if arg == "-run" {
				runs = append(runs, args[i+1])
			}
		}
		_, err := io.WriteString(stdout, `{"Action":"pass","Test":"TestA/a","Elapsed":0.01}`+"\n"+`{"Action":"pass","Test":"TestA/b","Elapsed":0.01}`+"\n")
		return err
	}
	// The real budget is 60s. These weights are the split, not a raised cap:
	// 40 + 40 does not fit one call, so each subtest is its own go test.
	res, err := a.runGoTestCases(context.Background(), []goTestCase{row("a", "a", 40), row("b", "b", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Verdict != testreg.Pass || res[1].Verdict != testreg.Pass {
		t.Fatalf("results %+v", res)
	}
	if len(runs) != 2 || runs[0] != "^TestA$/^(a)$" || runs[1] != "^TestA$/^(b)$" {
		t.Fatalf("calls %q, want one subtest each", runs)
	}
}

// TestANonZeroExitWithEveryAskedSubtestPassingFailsThoseRows plants go test
// exiting non-zero while every asked subtest passed. Those rows fail, with
// go test's words. A sibling that itself failed is covered by
// TestASubtestRowKeepsItsOwnVerdict: it stays red and does not flip a pass.
func TestANonZeroExitWithEveryAskedSubtestPassingFailsThoseRows(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := func(full, sub string) testreg.Instance {
		in := `{"package":"./p","test":"TestParent","subtest":"` + sub + `"}`
		return testreg.Instance{Full: full, Lang: testreg.LangFree, Test: testreg.Test{
			Key: full, Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(in),
		}}
	}
	a := newGoTestAdapter(io.Discard, root, 1)
	a.goTest = func(_ context.Context, _ string, _ []string, stdout, stderr io.Writer) error {
		_, err := io.WriteString(stdout, `{"Action":"pass","Test":"TestParent/a.schema","Elapsed":0.02}`+"\n"+
			`{"Action":"pass","Test":"TestParent/b.schema","Elapsed":0.01}`+"\n")
		if err != nil {
			return err
		}
		_, err = io.WriteString(stderr, "the tree changed after the tests\n")
		if err != nil {
			return err
		}
		return errors.New("exit status 1")
	}
	res, err := a.Run(context.Background(), testreg.LangFree, []testreg.Instance{
		row("a", "a.schema"), row("b", "b.schema"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("results %+v", res)
	}
	for _, r := range res {
		if r.Verdict != testreg.Fail {
			t.Errorf("%s stayed green: %+v", r.Instance, r)
		}
		if !strings.Contains(r.Reason, "exit status 1") || !strings.Contains(r.Reason, "every test it ran passed") || !strings.Contains(r.Reason, "the tree changed after the tests") {
			t.Errorf("%s reason does not carry go test's words: %s", r.Instance, r.Reason)
		}
	}
}
