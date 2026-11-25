package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// adapterTree is a paired tla/ with one exhaustive config and one reversed
// witness.
func adapterTree() map[string]string {
	return map[string]string{
		"M.tla":         "---- MODULE M ----\n====\n",
		"MCM.cfg":       cfgPositive,
		"MCMBroken.cfg": cfgCardHops,
		"RUNS.tsv": header +
			rowLine("MCM.cfg", "M", "exhaustive", "-", "68", "0") +
			rowLine("MCMBroken.cfg", "M", "counterexample", "ImplementationsAgree", "2", "12"),
	}
}

func request(t *testing.T, key, inputs string) string {
	t.Helper()
	return `{"instance":"none.functional.` + key + `","lang":"none","key":"` + key + `","type":"model","family":"m","case":"c","inputs":` + inputs + `,"expect":"e"}` + "\n"
}

// verdicts runs the harness and returns each instance's result.
func verdicts(t *testing.T, dir string, tlc checker, tools, in string) map[string]testreg.Result {
	t.Helper()
	var out strings.Builder
	if err := runAdapter(dir, tlc, tools, 2, strings.NewReader(in), &out); err != nil {
		t.Fatalf("the harness crashed: %v", err)
	}
	results, log, err := testreg.ParseResults(strings.NewReader(out.String()))
	if err != nil || len(log) > 0 {
		t.Fatalf("output %q: err=%v log=%q", out.String(), err, log)
	}
	got := map[string]testreg.Result{}
	for _, r := range results {
		got[r.Instance] = r
	}
	return got
}

// TestTheAdapterJudgesEachRequestedConfig: each request is one config run
// and judged against its row; a run that is not its row is a fail verdict
// naming the config and why.
func TestTheAdapterJudgesEachRequestedConfig(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, adapterTree())
	in := request(t, "model.m.holds", `{"config":"MCM.cfg"}`) + request(t, "model.m.broken", `{"config":"MCMBroken.cfg","workers":2}`)
	got := verdicts(t, dir, scripted{"MCM.cfg": {0, logExhaustive}, "MCMBroken.cfg": {12, logImplementationsAgree}}, "", in)
	if len(got) != 2 || got["none.functional.model.m.holds"].Verdict != testreg.Pass || got["none.functional.model.m.broken"].Verdict != testreg.Pass {
		t.Fatalf("the recorded runs: %v", got)
	}
	// the reviewer's first case: the witness violates another invariant
	got = verdicts(t, dir, scripted{"MCM.cfg": {0, logExhaustive}, "MCMBroken.cfg": {12, logNoFieldLost}}, "", in)
	bad := got["none.functional.model.m.broken"]
	if got["none.functional.model.m.holds"].Verdict != testreg.Pass || bad.Verdict != testreg.Fail || !strings.Contains(bad.Reason, "MCMBroken.cfg") || !strings.Contains(bad.Reason, "violated NoFieldLost") {
		t.Fatalf("another invariant's violation: %v", got)
	}
}

// TestTheAdapterFailsEveryInstanceNamingTheRunnerWithoutTLC: a runner with
// no TLC jar or no java fails every instance it was asked for, naming the
// runner; nothing is skipped and nothing passes.
func TestTheAdapterFailsEveryInstanceNamingTheRunnerWithoutTLC(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, adapterTree())
	jar := filepath.Join(t.TempDir(), "tla2tools.jar")
	problem := toolProblem(jar, "java")
	if !strings.Contains(problem, "no TLC jar at "+jar) || !strings.Contains(problem, "runner") {
		t.Fatalf("a missing jar is reported as %q", problem)
	}
	if err := os.WriteFile(jar, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if p := toolProblem(jar, "no-such-java-for-schema-test"); !strings.Contains(p, "no no-such-java-for-schema-test on PATH of") {
		t.Fatalf("a missing java is reported as %q", p)
	}
	in := request(t, "model.m.holds", `{"config":"MCM.cfg"}`) + request(t, "plant.m.controls", `{"controls":true}`)
	got := verdicts(t, dir, refuseToRun{}, problem, in)
	if len(got) != 2 {
		t.Fatalf("got %d verdicts for 2 requests", len(got))
	}
	for name, r := range got {
		if r.Verdict != testreg.Fail || r.Reason != problem {
			t.Fatalf("%s without TLC: %v", name, r)
		}
	}
}

// TestTheAdapterRefusesAnUnpairedTreeAndAnUnrecordedConfig: a tree whose
// ledger does not pair records no run, and a config with no row has none;
// both fail without starting TLC.
func TestTheAdapterRefusesAnUnpairedTreeAndAnUnrecordedConfig(t *testing.T) {
	t.Parallel()
	files := adapterTree()
	files["MCMNew.cfg"] = cfgPositive
	got := verdicts(t, writeTree(t, files), refuseToRun{}, "", request(t, "model.m.holds", `{"config":"MCM.cfg"}`))
	if r := got["none.functional.model.m.holds"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "tla/MCMNew.cfg has no RUNS.tsv row") {
		t.Fatalf("an unpaired tree: %v", r)
	}
	got = verdicts(t, writeTree(t, adapterTree()), refuseToRun{}, "", request(t, "model.m.gone", `{"config":"MCGone.cfg"}`))
	if r := got["none.functional.model.m.gone"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "MCGone.cfg has no RUNS.tsv row") {
		t.Fatalf("a config with no row: %v", r)
	}
}

// TestTheAdapterRefusesARequestItCannotRead: inputs it cannot read, a
// config that is a path, and no request at all crash the call by name.
func TestTheAdapterRefusesARequestItCannotRead(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, adapterTree())
	for _, c := range []struct{ in, want string }{
		{request(t, "model.m.x", `{"config":"MCM.cfg","extra":1}`), "unknown field"},
		{request(t, "model.m.x", `{"config":"../MCM.cfg"}`), "a config file name under tla/"},
		{request(t, "model.m.x", `{"config":"MCM.cfg","controls":true}`), "names no config"},
		{"", "no requests"},
	} {
		var out strings.Builder
		err := runAdapter(dir, refuseToRun{}, "", 1, strings.NewReader(c.in), &out)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err=%v, want one holding %q", c.in, err, c.want)
		}
	}
}

// readsConfig answers from the config's text in the directory TLC is run
// in: an exhaustive run, unless the text holds the planted line, which it
// reports as the violation TLC would find.
type readsConfig struct{ planted string }

func (r readsConfig) check(dir, config, _ string) (int, string, error) {
	src, err := os.ReadFile(filepath.Join(dir, config))
	if err != nil {
		return 0, "", err
	}
	if strings.Contains(string(src), r.planted) {
		return 12, logNoFieldLost, nil
	}
	return 0, logExhaustive, nil
}

// TestThePlantHookBreaksTheRequestedConfig: SCHEMA_TEST_TLC_PLANT_APPEND
// reaches TLC in the config it runs, in a scratch copy (tla/ itself is not
// written), and the run is red; without it the same run is green.
func TestThePlantHookBreaksTheRequestedConfig(t *testing.T) {
	dir := writeTree(t, adapterTree())
	in := request(t, "model.m.holds", `{"config":"MCM.cfg"}`)
	tlc := readsConfig{planted: "CONSTANT EditDropsUnknown = TRUE"}
	if r := verdicts(t, dir, tlc, "", in)["none.functional.model.m.holds"]; r.Verdict != testreg.Pass {
		t.Fatalf("unplanted: %v", r)
	}
	t.Setenv(PlantAppendEnv, tlc.planted)
	r := verdicts(t, dir, tlc, "", in)["none.functional.model.m.holds"]
	if r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "exit 12, want 0") {
		t.Fatalf("planted: %v", r)
	}
	src, err := os.ReadFile(filepath.Join(dir, "MCM.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != cfgPositive {
		t.Fatalf("the plant wrote tla/ itself: %q", src)
	}
}
