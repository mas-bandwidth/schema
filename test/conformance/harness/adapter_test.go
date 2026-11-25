package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

func corpusReq(instance, lang, surface, name string) testreg.Request {
	in, _ := json.Marshal(corpusInputs{Surface: surface, Case: name, Driver: corpusDriver})
	return testreg.Request{Instance: instance, Lang: lang, Inputs: in}
}

// TestTheCorpusAdapterGivesEachCaseItsVerdict holds the adapter's one job:
// each requested case's verdict is its verdict in the leg's run, and every
// way a case can fail to be the corpus's answer is a fail that names it.
func TestTheCorpusAdapterGivesEachCaseItsVerdict(t *testing.T) {
	wire := &result{cases: map[string]string{
		"ok": outcomePass, "named": outcomeNamed, "gone": outcomeAbsent,
		"wrong": outcomeFail, "only": outcomeUncredited, "claimed": outcomeAbsent,
	}, failures: []string{"claimed: CLAIMED and answered ABSENT"}}
	off := &result{absent: true, cases: map[string]string{"x": outcomeAbsent}}
	run := legRun{results: map[string]*result{"wire": wire, "block": off}}
	for _, c := range []struct {
		surface, name string
		pass          bool
		reason        string
	}{
		{"wire", "ok", true, ""},
		{"wire", "named", true, ""},
		{"wire", "gone", false, "MISSING: the leg does not answer this case"},
		{"wire", "wrong", false, "not the corpus's"},
		{"wire", "only", false, "REFUSAL-ONLY"},
		{"wire", "claimed", false, "CLAIMED and answered ABSENT"},
		{"block", "x", false, "does not answer the block surface"},
		{"wire", "nosuch", false, "no case \"nosuch\" on the wire surface"},
		{"nosurface", "ok", false, "no surface \"nosurface\""},
	} {
		res := adapterResults("go", []testreg.Request{corpusReq("i", "go", c.surface, c.name)}, run)
		if len(res) != 1 {
			t.Fatalf("%s/%s: %d results for one request", c.surface, c.name, len(res))
		}
		got := res[0]
		if (got.Verdict == testreg.Pass) != c.pass || !strings.Contains(got.Reason, c.reason) {
			t.Errorf("%s/%s: got %s %q, want pass=%v and a reason with %q", c.surface, c.name, got.Verdict, got.Reason, c.pass, c.reason)
		}
	}
}

// TestASurfaceFailureThatNamesNoCaseFailsEveryCaseOfIt: a stray answer or a
// surface with no rows names no case, and it must not be lost.
func TestASurfaceFailureThatNamesNoCaseFailsEveryCaseOfIt(t *testing.T) {
	wire := &result{cases: map[string]string{"ok": outcomePass},
		failures: []string{"stray: the driver answered a row the corpus did not send on the wire surface"}}
	res := adapterResults("go", []testreg.Request{corpusReq("i", "go", "wire", "ok")}, legRun{results: map[string]*result{"wire": wire}})
	if res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "the wire surface failed: stray") {
		t.Errorf("got %s %q, want the case failed naming the surface's failure", res[0].Verdict, res[0].Reason)
	}
	res = adapterResults("go", []testreg.Request{corpusReq("i", "go", "wire", "ok")},
		legRun{results: map[string]*result{"wire": {cases: map[string]string{"ok": outcomePass}}}, legFailures: []string{"answered no row"}})
	if res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "answered no row") {
		t.Errorf("a leg failure: got %s %q, want the case failed naming it", res[0].Verdict, res[0].Reason)
	}
}

// TestTheCorpusAdapterRefusesARequestItCannotRead: inputs that are not a
// corpus row, and a request for another language, fail by name.
func TestTheCorpusAdapterRefusesARequestItCannotRead(t *testing.T) {
	run := legRun{results: map[string]*result{"wire": {cases: map[string]string{"ok": outcomePass}}}}
	bad := testreg.Request{Instance: "i", Lang: "go", Inputs: json.RawMessage(`{"surface":"wire","case":"ok","extra":1}`)}
	other := corpusReq("j", "lua", "wire", "ok")
	res := adapterResults("go", []testreg.Request{bad, other}, run)
	if res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "inputs") {
		t.Errorf("bad inputs: got %s %q", res[0].Verdict, res[0].Reason)
	}
	if res[1].Verdict != testreg.Fail || !strings.Contains(res[1].Reason, "lua reached the go leg") {
		t.Errorf("another language: got %s %q", res[1].Verdict, res[1].Reason)
	}
}

// TestTheCorpusAdapterAnswersEveryRequestOnce runs adapterMain end to end
// over a fake leg run: one result line per request, in order, and a call
// with no requests, or a leg that cannot run, is an error.
func TestTheCorpusAdapterAnswersEveryRequestOnce(t *testing.T) {
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	for _, q := range []testreg.Request{corpusReq("a", "go", "wire", "ok"), corpusReq("b", "go", "wire", "bad")} {
		if err := enc.Encode(q); err != nil {
			t.Fatal(err)
		}
	}
	runner := func(string) (legRun, error) {
		return legRun{results: map[string]*result{"wire": {cases: map[string]string{"ok": outcomePass, "bad": outcomeFail}}}}, nil
	}
	var out bytes.Buffer
	if err := adapterMain(&in, &out, &bytes.Buffer{}, nil, "go", runner); err != nil {
		t.Fatal(err)
	}
	results, log, err := testreg.ParseResults(&out)
	if err != nil || len(log) != 0 {
		t.Fatalf("output: %v, log %q", err, log)
	}
	if len(results) != 2 || results[0].Instance != "a" || results[0].Verdict != testreg.Pass || results[1].Instance != "b" || results[1].Verdict != testreg.Fail {
		t.Errorf("results %+v, want a pass then b fail", results)
	}
	if err := adapterMain(strings.NewReader(""), &out, &bytes.Buffer{}, nil, "go", runner); err == nil {
		t.Error("a call with no request ran")
	}
	broken := func(string) (legRun, error) { return legRun{}, errors.New("no driver") }
	in.Reset()
	if err := enc.Encode(corpusReq("a", "go", "wire", "ok")); err != nil {
		t.Fatal(err)
	}
	if err := adapterMain(&in, &out, &bytes.Buffer{}, nil, "go", broken); err == nil {
		t.Error("a leg that could not run gave results")
	}
}

// TestCaseSlugsAreRegistryCases: a case file name becomes a registry case,
// and two names of one surface that come to one slug are refused.
func TestCaseSlugsAreRegistryCases(t *testing.T) {
	for in, want := range map[string]string{"root_full": "root-full", "Album": "album", "a.json": "a-json", "x__y-": "x-y", "root_full-be": "root-full-be"} {
		if got := caseSlug(in); got != want {
			t.Errorf("caseSlug(%q) = %q, want %q", in, got, want)
		}
	}
	m := &Manifest{Blocks: []Block{{Name: "a_b"}, {Name: "a-b"}}}
	if _, err := corpusRows(m, nil, ""); err == nil || !strings.Contains(err.Error(), "are both the registry case") {
		t.Errorf("two cases with one slug: %v", err)
	}
}

// TestTheRegistryHoldsEveryConformanceCase: test/registry/registry.jsonl
// holds exactly the corpus's rows, field for field (adapter.go). A case added
// to the corpus with no row, a row whose case is gone, and a row that names
// other files, another type or another surface are each red, by key. Its owed
// lines for the corpus are exactly the harness's retain-refusal ledger: a
// ledger line with no owed line, or an owed line the ledger does not hold,
// is red by language and case.
func TestTheRegistryHoldsEveryConformanceCase(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := readReports(defaultReports)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := corpusRows(m, reports, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		want[r.Key] = string(b)
	}
	got := registryCorpusLines(t, filepath.FromSlash(testreg.DefaultPath))
	for _, problem := range compareCorpusRows(want, got) {
		t.Error(problem)
	}
	if len(rows) == 0 {
		t.Fatal("the corpus has no rows")
	}
}

// registryCorpusLines is every conformance row of the registry file, by key.
func registryCorpusLines(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var head struct {
			Rec    string `json:"rec"`
			Key    string `json:"key"`
			Family string `json:"family"`
		}
		if json.Unmarshal(sc.Bytes(), &head) != nil || head.Rec != "test" || !strings.HasPrefix(head.Family, corpusFamilyPrefix) {
			continue
		}
		got[head.Key] = sc.Text()
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

// compareCorpusRows names every disagreement between the corpus's rows and
// the registry's, by key.
func compareCorpusRows(want, got map[string]string) []string {
	var out []string
	for k, w := range want {
		g, ok := got[k]
		switch {
		case !ok:
			out = append(out, k+": a corpus case with no registry row: run `build/conformance-harness-vm registry-rows` and put its line in test/registry/registry.jsonl")
		case g != w:
			out = append(out, k+": the registry row is not the corpus's:\n  registry "+g+"\n  corpus   "+w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			out = append(out, k+": a registry row whose case the corpus does not have")
		}
	}
	return out
}

// TestTheRegistryCheckGoesRedOnEachDrift plants each drift the check exists
// for and requires it named.
func TestTheRegistryCheckGoesRedOnEachDrift(t *testing.T) {
	want := map[string]string{"a": `{"k":"a"}`, "b": `{"k":"b"}`}
	for name, c := range map[string]struct {
		got  map[string]string
		says string
	}{
		"a case with no row": {map[string]string{"a": `{"k":"a"}`}, "b: a corpus case with no registry row"},
		"a row with no case": {map[string]string{"a": `{"k":"a"}`, "b": `{"k":"b"}`, "c": `{}`}, "c: a registry row whose case"},
		"a row that differs": {map[string]string{"a": `{"k":"a"}`, "b": `{"k":"B"}`}, "b: the registry row is not the corpus's"},
	} {
		problems := compareCorpusRows(want, c.got)
		if len(problems) != 1 || !strings.Contains(problems[0], c.says) {
			t.Errorf("%s: got %q, want one problem saying %q", name, problems, c.says)
		}
	}
	if p := compareCorpusRows(want, map[string]string{"a": `{"k":"a"}`, "b": `{"k":"b"}`}); len(p) != 0 {
		t.Errorf("an exact registry: %q", p)
	}
}
