package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

var updateJSONTest = flag.Bool("update-jsontest", false, "rewrite the registry's JSONTestSuite rows from the corpus")

// jsontestRowLine is a row as the registry file holds it.
func jsontestRowLine(t testreg.Test) (string, error) {
	t.Line = 0
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return `{"rec":"test",` + string(b[1:]), nil
}

// TestTheRegistryHoldsEveryJSONTestSuiteCase holds the registry's
// JSONTestSuite rows to the corpus field for field: a case with no
// row, a row with no case and a row that differs are each red by key.
// -update-jsontest rewrites them (keeping each row's measured seconds).
func TestTheRegistryHoldsEveryJSONTestSuiteCase(t *testing.T) {
	want, err := jsontestRows("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the JSONTestSuite corpus gives no rows")
	}
	path := "../../" + testreg.DefaultPath
	reg, err := testreg.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]testreg.Test{}
	for _, row := range reg.Tests {
		if row.Family == jsontestFamily {
			have[row.Key] = row
		}
	}
	if *updateJSONTest {
		if err := rewriteJSONTestRows(path, want, have); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, w := range want {
		h, ok := have[w.Key]
		if !ok {
			t.Errorf("%s: the case %s has no registry row (go test ./tools/schema-test -run TestTheRegistryHoldsEveryJSONTestSuiteCase -args -update-jsontest)", w.Key, w.Source)
			continue
		}
		delete(have, w.Key)
		w.Seconds, w.Line = h.Seconds, h.Line
		wl, err := jsontestRowLine(w)
		if err != nil {
			t.Fatal(err)
		}
		hl, err := jsontestRowLine(h)
		if err != nil {
			t.Fatal(err)
		}
		if wl != hl {
			t.Errorf("%s:%d: the row is not the corpus's:\n have %s\n want %s", path, h.Line, hl, wl)
		}
	}
	for key, h := range have {
		t.Errorf("%s:%d: %s is a JSONTestSuite row the corpus has no case for", path, h.Line, key)
	}
}

// rewriteJSONTestRows drops every JSONTestSuite row from the registry
// file and appends want, each keeping the seconds its old row measured.
func rewriteJSONTestRows(path string, want []testreg.Test, have map[string]testreg.Test) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	for line := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		var head struct{ Rec, Family string }
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			return err
		}
		if head.Rec == "test" && head.Family == jsontestFamily {
			continue
		}
		out.WriteString(line + "\n")
	}
	for _, w := range want {
		w.Seconds = have[w.Key].Seconds
		l, err := jsontestRowLine(w)
		if err != nil {
			return err
		}
		out.WriteString(l + "\n")
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// TestJSONTestCaseKeysAreDistinct: two file names that meet at one
// registry case are refused, naming both.
func TestJSONTestCaseKeysAreDistinct(t *testing.T) {
	_, err := jsontestKeys([]string{"Y_A.json", "y_a.json"})
	if err == nil || !strings.Contains(err.Error(), "Y_A.json and y_a.json are one registry case") {
		t.Fatalf("two names of one case key: got %v", err)
	}
}

// jsontestInstance is an instance of a JSONTestSuite row for lang.
func jsontestInstance(t *testing.T, lang, file, typ string) testreg.Instance {
	t.Helper()
	in, err := json.Marshal(jsontestInputs{Case: file, Files: []string{jsontestCorpus + "/" + file}})
	if err != nil {
		t.Fatal(err)
	}
	key := typ + "." + jsontestFamily + "." + jsontestCaseKey(file)
	return testreg.Instance{
		Full: lang + ".nightly." + key,
		Lang: lang,
		Test: testreg.Test{Key: key, Type: typ, Family: jsontestFamily, Case: jsontestCaseKey(file), Tier: "nightly", Inputs: in},
	}
}

func verdictsByInstance(res []testreg.Result) map[string]testreg.Result {
	out := map[string]testreg.Result{}
	for _, r := range res {
		out[r.Instance] = r
	}
	return out
}

// TestJSONTestHarnessGivesEachCaseItsVerdict: a runner that names one
// case as disagreeing turns exactly that instance red.
func TestJSONTestHarnessGivesEachCaseItsVerdict(t *testing.T) {
	good := jsontestInstance(t, "go", "y_one.json", "decode")
	bad := jsontestInstance(t, "go", "n_two.json", "load-refusal")
	out := "=== RUN TestFake\n" +
		"    fake_test.go:1: mismatch: n_two.json: want refuse, got accept\n" +
		"--- FAIL: TestFake (0.01s)\n"
	res, err := jsontestVerdicts("go", []testreg.Instance{good, bad}, "TestFake", out, errors.New("exit status 1"), "")
	if err != nil {
		t.Fatal(err)
	}
	by := verdictsByInstance(res)
	if len(res) != 2 || by[good.Full].Verdict != testreg.Pass || by[bad.Full].Verdict != testreg.Fail {
		t.Fatalf("verdicts %v, want %s pass and %s fail", res, good.Full, bad.Full)
	}
	if !strings.Contains(by[bad.Full].Reason, "n_two.json") {
		t.Fatalf("the red reason %q names no case", by[bad.Full].Reason)
	}
}

// TestJSONTestHarnessFailsEveryCaseWhenItsRunnerGivesNoVerdict: a run
// that never reports its test fails every asked case with what it said.
func TestJSONTestHarnessFailsEveryCaseWhenItsRunnerGivesNoVerdict(t *testing.T) {
	a := jsontestInstance(t, "go", "y_one.json", "decode")
	b := jsontestInstance(t, "go", "n_two.json", "load-refusal")
	res, err := jsontestVerdicts("go", []testreg.Instance{a, b}, "TestFake", "the build broke\n", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("gave %d verdicts for 2 asked", len(res))
	}
	for _, r := range res {
		if r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "gave the cases no verdict") {
			t.Fatalf("verdict %v, want a by-name failure", r)
		}
	}
}

// TestJSONTestPlantsTurnExactlyOneCaseRed: a flip and a drop each turn
// only their named case red, for their own reason.
func TestJSONTestPlantsTurnExactlyOneCaseRed(t *testing.T) {
	a := jsontestInstance(t, "go", "y_one.json", "decode")
	b := jsontestInstance(t, "go", "n_two.json", "load-refusal")
	out := "--- PASS: TestFake (0.01s)\n"
	for _, plant := range []string{"flip:y_one.json", "absent:y_one.json"} {
		res, err := jsontestVerdicts("go", []testreg.Instance{a, b}, "TestFake", out, nil, plant)
		if err != nil {
			t.Fatal(err)
		}
		by := verdictsByInstance(res)
		if by[a.Full].Verdict != testreg.Fail || by[b.Full].Verdict != testreg.Pass {
			t.Fatalf("plant %s: verdicts %v, want only %s red", plant, res, a.Full)
		}
	}
	res, err := jsontestVerdicts("go", []testreg.Instance{a, b}, "TestFake", out, nil, "absent:y_one.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verdictsByInstance(res)[a.Full].Reason, "MISSING") {
		t.Fatalf("the absent plant's reason %q is no MISSING", verdictsByInstance(res)[a.Full].Reason)
	}
}

// TestJSONTestNoRunnerLanguageFailsByName: a language with no runner
// fails every asked case by name, and reports each one.
func TestJSONTestNoRunnerLanguageFailsByName(t *testing.T) {
	a := jsontestInstance(t, "rust", "y_one.json", "decode")
	b := jsontestInstance(t, "rust", "n_two.json", "load-refusal")
	res, err := runJSONTest(context.Background(), "rust", []testreg.Instance{a, b}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("gave %d verdicts for 2 asked", len(res))
	}
	for _, r := range res {
		if r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "no JSONTestSuite runner for rust") {
			t.Fatalf("verdict %v, want a by-name failure", r)
		}
	}
}
