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
func plantedBuilder(target string, err error) *builder {
	return &builder{log: io.Discard, done: map[string]error{strings.Join(needsStep(target), "\x00"): err}}
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
