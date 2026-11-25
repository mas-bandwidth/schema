package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// scopedRun runs the fake corpus scoped to cases (runOptions.cases), the way
// the corpus adapter runs it (main.go), and hands back the port leg's run as
// the adapter reads it: its surface results and its leg failures.
func (c fakeCorpus) scopedRun(t *testing.T, drivers string, cases map[caseKey]bool) (legRun, string) {
	t.Helper()
	var got legRun
	opts := runOptions{strictRows: true, cases: cases, capture: func(results map[string]map[string]*result, legFailures map[string][]string) {
		got = legRun{results: results["port"], legFailures: legFailures["port"]}
	}}
	work := filepath.Join(t.TempDir(), "work")
	var out bytes.Buffer
	if _, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers, work, "", opts); err != nil {
		t.Fatalf("a scoped run failed whole: %v\n%s", err, out.String())
	}
	if got.results == nil {
		t.Fatalf("the scoped run scored no surface of the port leg\n%s", out.String())
	}
	return got, work
}

// TestAScopedRunRunsOnlyTheAskedCases drives runWith the way the corpus
// adapter does (scope.go): asked one wire case, the leg runs the wire surface
// alone, is handed the scoped manifest, and the case gets its verdict and
// the surface's seconds; the surfaces nothing asked are not run at all.
func TestAScopedRunRunsOnlyTheAskedCases(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.substituted(t, "port", fakeLeg{})
	run, work := c.scopedRun(t, drivers, map[caseKey]bool{{"wire", "alpha"}: true})
	got := run.results
	var ran []string
	for s := range got {
		ran = append(ran, s)
	}
	slices.Sort(ran)
	if strings.Join(ran, ",") != "wire" {
		t.Errorf("the scoped run ran surfaces %v, want the wire surface alone", ran)
	}
	if v := got["wire"].cases["alpha"]; v != "pass" {
		t.Errorf("wire/alpha is %q in the scoped run, want pass", v)
	}
	if got["wire"].seconds <= 0 {
		t.Error("the wire surface carries no seconds: the adapter could not weigh its cases")
	}
	if _, err := os.Stat(filepath.Join(work, "manifest-port-scope.txt")); err != nil {
		t.Errorf("the leg was not handed the scoped manifest: %v", err)
	}
}

// TestAScopedRunOfAnAbsentSurfaceAnswersEachCase: a call whose asked cases
// all sit on a surface the leg leaves out of `list` is no refused run; each
// case gets its own result line, a failure saying the leg answered none of
// what it was sent (answeredNothing). The whole-run refusal (emptySelection)
// would have left every request without a result line.
func TestAScopedRunOfAnAbsentSurfaceAnswersEachCase(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.substituted(t, "port", fakeLeg{unlisted: "wire"})
	run, _ := c.scopedRun(t, drivers, map[caseKey]bool{{"wire", "alpha"}: true})
	r := run.results["wire"]
	if r == nil || !r.absent || r.cases["alpha"] != "absent" {
		t.Fatalf("wire in the scoped run: %+v, want the surface absent and alpha absent", r)
	}
	res := adapterResults("port", []testreg.Request{corpusReq("i", "port", "wire", "alpha")}, run)
	if len(res) != 1 || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "answered none") {
		t.Errorf("the adapter's answer: %+v, want a failure saying the leg answered none of its rows", res)
	}
}

// TestAScopedRunOfStaleCasesAnswersEachCase: a call whose every asked case is
// one the corpus does not have (a stale registry row) is no refused run
// either; each request fails, naming the stale row.
func TestAScopedRunOfStaleCasesAnswersEachCase(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.substituted(t, "port", fakeLeg{})
	run, _ := c.scopedRun(t, drivers, map[caseKey]bool{{"wire", "gone"}: true})
	res := adapterResults("port", []testreg.Request{corpusReq("i", "port", "wire", "gone")}, run)
	if len(res) != 1 || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "stale") {
		t.Errorf("the adapter's answer: %+v, want a failure naming the stale row", res)
	}
}

// TestHostileSavesAreReadOnlyWhenTheBatteryCanRun: the battery's saves are
// read by a run that selects the json-hostile surface, and by a scoped run
// only when a case of it was asked (hostileRuns).
func TestHostileSavesAreReadOnlyWhenTheBatteryCanRun(t *testing.T) {
	for _, c := range []struct {
		name string
		opts runOptions
		want bool
	}{
		{"every surface", runOptions{}, true},
		{"--surfaces json-hostile", runOptions{only: map[string]bool{"json-hostile": true}}, true},
		{"--surfaces wire", runOptions{only: map[string]bool{"wire": true}}, false},
		{"scoped to a hostile case", runOptions{cases: map[caseKey]bool{{"json-hostile", "h"}: true}}, true},
		{"scoped to a wire case", runOptions{cases: map[caseKey]bool{{"wire", "w"}: true}}, false},
	} {
		if got := hostileRuns(c.opts); got != c.want {
			t.Errorf("%s: hostileRuns %v, want %v", c.name, got, c.want)
		}
	}
}
