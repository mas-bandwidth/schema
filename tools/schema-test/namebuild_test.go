package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/tools/namebuild"
)

func nameBuildInstance(t *testing.T, lang, subtest string, in any) testreg.Instance {
	t.Helper()
	if in == nil {
		in = namebuild.Inputs{Package: namebuild.Package, Test: namebuild.TestPrefix + namebuild.LangPlaceholder, Subtest: subtest}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	rc := namebuild.RegistryCase(subtest)
	key := "codegen." + namebuild.Family + "." + rc
	return testreg.Instance{
		Full: lang + ".nightly." + key,
		Lang: lang,
		Test: testreg.Test{Key: key, Type: "codegen", Family: namebuild.Family, Case: rc, Tier: "nightly", Inputs: raw},
	}
}

// TestNameBuildPlanSelectsExactlyTheAsked: the -run pattern selects the
// asked subtests of the language's own test and nothing else, and inputs
// that name another package, another test or no subtest are refused.
func TestNameBuildPlanSelectsExactlyTheAsked(t *testing.T) {
	t.Parallel()
	insts := []testreg.Instance{nameBuildInstance(t, "rust", "b_Array", nil), nameBuildInstance(t, "rust", "b_Probe_", nil)}
	want, pattern, err := nameBuildPlan("rust", insts)
	if err != nil {
		t.Fatal(err)
	}
	if want["TestNameBuildability_rust/b_Array"] != insts[0].Full || want["TestNameBuildability_rust/b_Probe_"] != insts[1].Full || len(want) != 2 {
		t.Fatalf("plan %v", want)
	}
	top, sub, ok := strings.Cut(pattern, "/")
	if !ok {
		t.Fatalf("pattern %q has no subtest level", pattern)
	}
	topRE, subRE := regexp.MustCompile(top), regexp.MustCompile(sub)
	for name, match := range map[string]bool{"TestNameBuildability_rust": true, "TestNameBuildability_rusty": false, "TestNameBuildability_go": false} {
		if topRE.MatchString(name) != match {
			t.Errorf("pattern %q matches the test %s: %v, want %v", pattern, name, !match, match)
		}
	}
	for name, match := range map[string]bool{"b_Array": true, "b_Probe_": true, "b_Probe": false, "b_ArrayX": false, "ctl": false} {
		if subRE.MatchString(name) != match {
			t.Errorf("pattern %q matches the subtest %s: %v, want %v", pattern, name, !match, match)
		}
	}
	for what, in := range map[string]any{
		"another package": namebuild.Inputs{Package: "./tools/other", Test: namebuild.TestPrefix + namebuild.LangPlaceholder, Subtest: "ctl"},
		"another test":    namebuild.Inputs{Package: namebuild.Package, Test: "TestNameBuildability_rust", Subtest: "ctl"},
		"no subtest":      namebuild.Inputs{Package: namebuild.Package, Test: namebuild.TestPrefix + namebuild.LangPlaceholder},
		"an unknown key":  map[string]string{"package": namebuild.Package, "test": namebuild.TestPrefix + namebuild.LangPlaceholder, "subtest": "ctl", "seed": "1"},
	} {
		if _, _, err := nameBuildPlan("rust", []testreg.Instance{nameBuildInstance(t, "rust", "ctl", in)}); err == nil {
			t.Errorf("inputs with %s: no refusal", what)
		}
	}
	if _, _, err := nameBuildPlan("rust", nil); err == nil {
		t.Error("no instance: no refusal")
	}
}

// TestNameBuildResultsReadsTheEvents: a pass event passes its instance, a
// fail event fails it with the subtest's own output, a skip fails it, the
// parent test and unasked subtests answer nothing, and an asked subtest with
// no verdict is left unreported (the run then fails it as not reported).
func TestNameBuildResultsReadsTheEvents(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"TestNameBuildability_go/ctl":     "go.nightly.codegen.name-buildability.ctl",
		"TestNameBuildability_go/b_Array": "go.nightly.codegen.name-buildability.b-array",
		"TestNameBuildability_go/b_Map":   "go.nightly.codegen.name-buildability.b-map",
		"TestNameBuildability_go/b_Vec":   "go.nightly.codegen.name-buildability.b-vec",
	}
	events := strings.Join([]string{
		`{"Action":"run","Test":"TestNameBuildability_go"}`,
		`{"Action":"output","Test":"TestNameBuildability_go/b_Array","Output":"go build: undefined: Array\n"}`,
		`{"Action":"pass","Test":"TestNameBuildability_go/ctl"}`,
		`{"Action":"fail","Test":"TestNameBuildability_go/b_Array"}`,
		`{"Action":"skip","Test":"TestNameBuildability_go/b_Map"}`,
		`{"Action":"pass","Test":"TestNameBuildability_go/b_Other"}`,
		`{"Action":"fail","Test":"TestNameBuildability_go"}`,
		`{"Action":"output","Output":"name-buildability per-target summary\n"}`,
	}, "\n")
	res, err := nameBuildResults(strings.NewReader(events), want)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if len(res) != 3 {
		t.Fatalf("%d results, want 3 (b_Vec has no verdict): %v", len(res), res)
	}
	if r := got["go.nightly.codegen.name-buildability.ctl"]; r.Verdict != testreg.Pass {
		t.Errorf("ctl: %+v, want pass", r)
	}
	if r := got["go.nightly.codegen.name-buildability.b-array"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "undefined: Array") {
		t.Errorf("b_Array: %+v, want a fail naming its own output", r)
	}
	if r := got["go.nightly.codegen.name-buildability.b-map"]; r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "skipped") {
		t.Errorf("b_Map: %+v, want a skip read as a fail", r)
	}
	if _, err := nameBuildResults(strings.NewReader("make: not json\n"), want); err == nil {
		t.Error("events that are not go test -json: no error")
	}
}

// TestRunNameBuildReadsWhatTheRunWrote runs the runner against a stand-in
// make in a temporary root. Events the run wrote answer their instances even
// when the run exits non-zero (a failing subtest does that). A run that wrote
// no events is an error saying so, and so is one that answered nothing. The
// work folder is removed every time.
func TestRunNameBuildReadsWhatTheRunWrote(t *testing.T) {
	t.Parallel()
	insts := []testreg.Instance{nameBuildInstance(t, "go", "ctl", nil), nameBuildInstance(t, "go", "b_Array", nil)}
	for _, tc := range []struct {
		name, script, says string
		verdicts           map[string]string
	}{
		{
			name: "events-answer",
			script: `printf '%s\n' '{"Action":"pass","Test":"TestNameBuildability_go/ctl"}' '{"Action":"fail","Test":"TestNameBuildability_go/b_Array"}' >"$NAME_BUILDABILITY_JSON"
exit 1
`,
			verdicts: map[string]string{insts[0].Full: testreg.Pass, insts[1].Full: testreg.Fail},
		},
		{name: "no-events", script: "echo 'stand-in: broke before the tests' >&2\nexit 2\n", says: "wrote no events"},
		{name: "no-answers", script: ": >\"$NAME_BUILDABILITY_JSON\"\nexit 2\n", says: "make name-buildability-run: exit status 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			script := filepath.Join(dir, "make")
			if err := os.WriteFile(script, []byte("#!/bin/sh\n"+tc.script), 0o755); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(dir, "work")
			var log bytes.Buffer
			res, err := runNameBuildWith(context.Background(), []string{script}, root, "go", insts, &log)
			if tc.says != "" {
				if err == nil || !strings.Contains(err.Error(), tc.says) {
					t.Errorf("err %v, want one saying %q", err, tc.says)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, r := range res {
				got[r.Instance] = r.Verdict
			}
			if len(got) != len(tc.verdicts) {
				t.Errorf("results %v, want %v", got, tc.verdicts)
			}
			for inst, v := range tc.verdicts {
				if got[inst] != v {
					t.Errorf("%s: %v, want %v", inst, got[inst], v)
				}
			}
			left, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(left) != 0 {
				t.Errorf("the work folder was left behind: %v", left)
			}
		})
	}
}
