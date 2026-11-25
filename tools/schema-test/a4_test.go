package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

var updateA4 = flag.Bool("update-a4", false, "rewrite the registry's A4 rows from their corpora")

// a4RowLine is a row as the registry file holds it.
func a4RowLine(t testreg.Test) (string, error) {
	t.Line = 0
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return `{"rec":"test",` + string(b[1:]), nil
}

func rewriteA4Rows(path string, want []testreg.Test, have map[string]testreg.Test) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		var head struct {
			Rec string `json:"rec"`
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(line), &head); err == nil && head.Rec == "test" {
			if _, isA4 := have[head.Key]; isA4 {
				continue
			}
		}
		lines = append(lines, line)
	}
	for _, w := range want {
		if old, ok := have[w.Key]; ok {
			w.Seconds = old.Seconds
		}
		line, err := a4RowLine(w)
		if err != nil {
			return err
		}
		lines = append(lines, line)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// TestTheRegistryHoldsEveryA4Case holds the registry's A4 rows to their
// corpora field for field: a case with no row, a row with no case and a
// row that differs are each red by key. -update-a4 rewrites them.
func TestTheRegistryHoldsEveryA4Case(t *testing.T) {
	want, err := a4Rows("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the A4 corpora give no rows")
	}
	path := "../../" + testreg.DefaultPath
	reg, err := testreg.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]testreg.Test{}
	for _, row := range reg.Tests {
		if a4Families[row.Family] {
			have[row.Key] = row
		}
	}
	if *updateA4 {
		if err := rewriteA4Rows(path, want, have); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, w := range want {
		h, ok := have[w.Key]
		if !ok {
			t.Errorf("%s: the case %s has no registry row (go test ./tools/schema-test -run TestTheRegistryHoldsEveryA4Case -args -update-a4)", w.Key, w.Source)
			continue
		}
		delete(have, w.Key)
		w.Seconds, w.Line = h.Seconds, h.Line
		wl, err := a4RowLine(w)
		if err != nil {
			t.Fatal(err)
		}
		hl, err := a4RowLine(h)
		if err != nil {
			t.Fatal(err)
		}
		if wl != hl {
			t.Errorf("%s: the registry row differs from its corpus:\n want %s\n have %s", w.Key, wl, hl)
		}
	}
	for key := range have {
		t.Errorf("%s: a registry row with no A4 corpus case", key)
	}
}

// TestCsRowsVerdictsReadPerRow: conformancerows lines give each asked
// row its own verdict, and a row no line names gets none.
func TestCsRowsVerdictsReadPerRow(t *testing.T) {
	text := "PASS cs/C12\nFAIL cs/R21 — fails: Assertion failed.\nPASS cpp/C12\nconformance rows cs, shard 0/1: 2 of 2 row files ran, 1 passed, 1 failed\n"
	pass, reason := parseCsRowLines(text, "cs")
	if !pass["C12"] || pass["R21"] || !strings.Contains(reason["R21"], "Assertion failed") {
		t.Errorf("verdicts %v reasons %v", pass, reason)
	}
	if _, ok := pass["C8"]; ok {
		t.Error("C8 got a verdict no line gave it")
	}
	if _, ok := pass["CPP"]; ok {
		t.Error("another leg's row answered for cs")
	}
}

// TestTextformRunnersNameTheirCases: the Go and C++ runners select the
// case's own subtest, a whole-test language shares its test, and a
// language with no textform test for a case has no runner.
func TestTextformRunnersNameTheirCases(t *testing.T) {
	for _, tc := range []struct {
		lang string
		in   a4Inputs
		want textformRunner
		ok   bool
	}{
		{"go", a4Inputs{Case: "kinds"}, textformRunner{"./internal/codegen/gotable", "TestCommentedTextForm", ""}, true},
		{"go", a4Inputs{Case: "bound-array", Refusal: true}, textformRunner{"./internal/codegen/gotable", "TestCommentedTextForm", ""}, true},
		{"cpp", a4Inputs{Case: "layout"}, textformRunner{"./compiler", "TestCPPTextFormDocumentVectors", "layout"}, true},
		{"cpp", a4Inputs{Case: "duplicate-map", Refusal: true}, textformRunner{}, false},
		{"python", a4Inputs{Case: "kinds"}, textformRunner{"./internal/codegen/pythontable", "TestPythonHandtextCorpus", ""}, true},
		{"rust", a4Inputs{Case: "kinds"}, textformRunner{}, false},
	} {
		got, ok := textformRunnerFor(tc.lang, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("runner(%s, %+v) = %+v %v, want %+v %v", tc.lang, tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// A go test run that answered none of the asked names keeps go test's own
// error: the caller reads why it stopped through errors.Is or errors.As,
// and a run that exited 0 says so instead of printing a nil error.
func TestGoAnsweredNoneWrapsTheRunError(t *testing.T) {
	r := textformRunner{pkg: "./internal/x", test: "TestX"}
	runErr := errors.New("exit status 2")
	err := goAnsweredNone(r, 3, "^TestX$", runErr, []string{"line one", "line two"})
	if !errors.Is(err, runErr) {
		t.Errorf("want the error to wrap go test's own error, got %v", err)
	}
	for _, want := range []string{"go test ./internal/x TestX answered none of the 3 asked (^TestX$ matched no test)", "exit status 2", "line two"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in the error, got %q", want, err)
		}
	}
	err = goAnsweredNone(r, 3, "^TestX$", nil, nil)
	if !strings.Contains(err.Error(), "go test exited 0") || strings.Contains(err.Error(), "nil") {
		t.Errorf("a run that exited 0: want it said, and no nil error printed, got %q", err)
	}
}
