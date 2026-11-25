package main

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

func subtestInst(full, test, sub string) testreg.Instance {
	in := `{"package":"./p","test":"` + test + `","subtest":"` + sub + `"}`
	return testreg.Instance{Full: full, Test: testreg.Test{Scope: testreg.LangFree, Adapter: "subtest", Inputs: []byte(in)}}
}

func TestSubtestPatternSelectsEachAskedSubtest(t *testing.T) {
	t.Parallel()
	want, pattern, err := subtestPlan([]testreg.Instance{
		subtestInst("none.unit.a.b.one", "TestWasRenameCorpus", "release-variant/bare.schema"),
		subtestInst("none.unit.a.b.two", "TestFmt", "fmt --migrate/invalid second file"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want["TestWasRenameCorpus/release-variant/bare.schema"] != "none.unit.a.b.one" {
		t.Errorf("want map %v", want)
	}
	if want["TestFmt/fmt_--migrate/invalid_second_file"] != "none.unit.a.b.two" {
		t.Errorf("want map %v", want)
	}
	const got = "^TestWasRenameCorpus$/^release-variant$/^bare\\.schema$|^TestFmt$/^fmt_--migrate$/^invalid_second_file$"
	if pattern != got {
		t.Errorf("pattern\n%s\nwant\n%s", pattern, got)
	}
	if _, _, err := subtestPlan([]testreg.Instance{
		subtestInst("none.unit.a.b.one", "TestA", "x"),
		subtestInst("none.unit.a.b.two", "TestA", "x"),
	}); err == nil || !strings.Contains(err.Error(), "both name") {
		t.Errorf("a duplicated subtest: %v", err)
	}
}

func TestSubtestResultsMapTheAskedSubtest(t *testing.T) {
	t.Parallel()
	events := strings.Join([]string{
		`{"Action":"output","Test":"TestA","Output":"parent\n"}`,
		`{"Action":"output","Test":"TestA/one","Output":"--- FAIL: TestA/one\n"}`,
		`{"Action":"fail","Test":"TestA/one","Elapsed":0.02}`,
		`{"Action":"pass","Test":"TestA/two","Elapsed":0.03}`,
		`{"Action":"output","Test":"TestA/three","Output":"skip me\n"}`,
		`{"Action":"skip","Test":"TestA/three"}`,
		`{"Action":"pass","Test":"TestA/other","Elapsed":1}`,
		`{"Action":"pass","Test":"TestA","Elapsed":0.04}`,
	}, "\n")
	want := map[string]string{"TestA/one": "inst.one", "TestA/two": "inst.two", "TestA/three": "inst.three"}
	res, other, err := subtestResults(strings.NewReader(events), want)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if got["inst.one"].Verdict != testreg.Fail || !strings.Contains(got["inst.one"].Reason, "TestA/one") {
		t.Errorf("one: %+v", got["inst.one"])
	}
	if got["inst.two"].Verdict != testreg.Pass || got["inst.two"].Seconds != 0.03 {
		t.Errorf("two: %+v", got["inst.two"])
	}
	if got["inst.three"].Verdict != testreg.Fail || !strings.Contains(got["inst.three"].Reason, "skipped") {
		t.Errorf("three: %+v", got["inst.three"])
	}
	if _, ok := got["TestA"]; ok {
		t.Errorf("the parent was reported")
	}
	if len(other) == 0 || !strings.Contains(strings.Join(other, "\n"), "parent") {
		t.Errorf("parent output was not kept aside: %v", other)
	}
}
