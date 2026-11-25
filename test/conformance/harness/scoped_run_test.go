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
// the corpus adapter runs it, and hands back the port leg's surface results.
func (c fakeCorpus) scopedRun(t *testing.T, drivers string, cases map[caseKey]bool) (map[string]*result, string) {
	t.Helper()
	var got map[string]*result
	opts := runOptions{strictRows: true, cases: cases, capture: func(results map[string]map[string]*result, _ map[string][]string) {
		got = results["port"]
	}}
	work := filepath.Join(t.TempDir(), "work")
	var out bytes.Buffer
	if _, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers, work, "", opts); err != nil {
		t.Fatalf("a scoped run failed whole: %v\n%s", err, out.String())
	}
	if got == nil {
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
	got, work := c.scopedRun(t, drivers, map[caseKey]bool{{"wire", "alpha"}: true})
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
// case is answered, absent, and the adapter fails it MISSING by name
// (adapter.go). The whole-run refusal (emptySelection) would have left every
// request without a result line.
func TestAScopedRunOfAnAbsentSurfaceAnswersEachCase(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.substituted(t, "port", fakeLeg{unlisted: "wire"})
	got, _ := c.scopedRun(t, drivers, map[caseKey]bool{{"wire", "alpha"}: true})
	r := got["wire"]
	if r == nil || !r.absent || r.cases["alpha"] != "absent" {
		t.Fatalf("wire in the scoped run: %+v, want the surface absent and alpha absent", r)
	}
	res := adapterResults("port", []testreg.Request{
		corpusReq("i", "port", "wire", "alpha")}, legRun{results: got})
	if len(res) != 1 || !strings.Contains(res[0].Reason, "MISSING") {
		t.Errorf("the adapter's answer: %+v, want a MISSING failure", res)
	}
}
