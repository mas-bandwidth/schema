package main

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// makeInst is one make row's instance.
func makeInst(t *testing.T, name, file, target string) testreg.Instance {
	t.Helper()
	in, err := json.Marshal(makeInputs{File: file, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	return testreg.Instance{Full: name, Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Adapter: "make", Type: "harness-run", Inputs: in}}
}

// fakeMake is a make adapter over a make file held in memory and a make
// that answers from a table: no unit test reads the tree or runs make.
func fakeMake(file string, rules string, runs map[string]struct {
	log string
	err error
}, ran *[]string) makeAdapter {
	return makeAdapter{
		read: func(p string) ([]byte, error) {
			if p != file {
				return nil, errors.New("no such file")
			}
			return []byte(rules), nil
		},
		exec: func(_ context.Context, argv []string) (string, error) {
			target := argv[len(argv)-1]
			*ran = append(*ran, target)
			r := runs[target]
			return r.log, r.err
		},
	}
}

const fakeRules = `# a make file
VAR := x
gate-check: dep
	@echo checking
gate-skips gate-red: dep
	@echo two rules
`

// TestAMakeRowWhoseTargetIsGoneFails: a row whose target its file no longer
// writes fails by name and runs nothing (it is a test that does not exist).
func TestAMakeRowWhoseTargetIsGoneFails(t *testing.T) {
	t.Parallel()
	var ran []string
	a := fakeMake("make/x.mk", fakeRules, nil, &ran)
	res, err := a.Run(context.Background(), "", []testreg.Instance{makeInst(t, "gone", "make/x.mk", "gate-renamed"), makeInst(t, "var", "make/x.mk", "VAR")})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "writes no rule") {
			t.Errorf("%s: %+v, want a fail naming the missing rule", r.Instance, r)
		}
	}
	if len(res) != 2 || len(ran) != 0 {
		t.Errorf("results %+v, make ran %v: a missing target runs nothing", res, ran)
	}
}

// TestAMakeTargetPassesOnlyOnExitZeroWithNoSkip: exit 0 passes; a non-zero
// exit fails with its log; exit 0 with a skip in the log fails.
func TestAMakeTargetPassesOnlyOnExitZeroWithNoSkip(t *testing.T) {
	t.Parallel()
	var ran []string
	a := fakeMake("make/x.mk", fakeRules, map[string]struct {
		log string
		err error
	}{
		"gate-check": {log: "checking\nok\n"},
		"gate-red":   {log: "FAIL: row 3\n", err: errors.New("exit status 2")},
		"gate-skips": {log: "lua not found, skipping the lua rows\n"},
	}, &ran)
	res, err := a.Run(context.Background(), "", []testreg.Instance{
		makeInst(t, "check", "make/x.mk", "gate-check"),
		makeInst(t, "red", "make/x.mk", "gate-red"),
		makeInst(t, "skips", "make/x.mk", "gate-skips"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if got["check"].Verdict != testreg.Pass {
		t.Errorf("exit 0: %+v", got["check"])
	}
	if got["red"].Verdict != testreg.Fail || !strings.Contains(got["red"].Reason, "FAIL: row 3") {
		t.Errorf("a non-zero exit: %+v", got["red"])
	}
	if got["skips"].Verdict != testreg.Fail || !strings.Contains(got["skips"].Reason, "skip") {
		t.Errorf("a skip in the log: %+v", got["skips"])
	}
	if strings.Join(ran, " ") != "gate-check gate-red gate-skips" {
		t.Errorf("make ran %v, want each target once, in order", ran)
	}
}

// TestMakeInputsNameOneTargetOfAMakeFile: inputs that are not one target of
// the Makefile or a .mk file are not a make row (Answers is false, and Run
// refuses them as a crash, never as a pass).
func TestMakeInputsNameOneTargetOfAMakeFile(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		`{"file":"make/x.mk","target":"a b"}`,
		`{"file":"make/x.mk","target":"-k"}`,
		`{"file":"make/x.mk","target":"X=1"}`,
		`{"file":"scripts/x.sh","target":"a"}`,
		`{"file":"make/x.mk"}`,
		`{"file":"make/x.mk","target":"a","env":{}}`,
	} {
		test := testreg.Test{Scope: testreg.LangFree, Inputs: json.RawMessage(in)}
		if (makeAdapter{}).Answers(test) {
			t.Errorf("%s answered as a make row", in)
		}
		if _, err := (makeAdapter{}).Run(context.Background(), "", []testreg.Instance{{Full: "x", Test: test}}); err == nil {
			t.Errorf("%s ran", in)
		}
	}
}

// TestAMakeRowRunsWithTheCoresAndAControlWithout: a make row runs make -j by
// the cores, as the workflows ran it; a negative control runs with no -j, as
// the controls job ran it.
func TestAMakeRowRunsWithTheCoresAndAControlWithout(t *testing.T) {
	t.Parallel()
	if newMakeAdapter().jobs != runtime.NumCPU() || newPlantAdapter(nil).make.jobs != 0 {
		t.Fatalf("make row jobs %d, control jobs %d", newMakeAdapter().jobs, newPlantAdapter(nil).make.jobs)
	}
	for jobs, want := range map[int]string{4: "make --no-print-directory -j4 gate-check", 0: "make --no-print-directory gate-check"} {
		var argv []string
		a := makeAdapter{jobs: jobs, read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
			exec: func(_ context.Context, av []string) (string, error) { argv = av; return "", nil }}
		if _, err := a.Run(context.Background(), "", []testreg.Instance{makeInst(t, "c", "make/x.mk", "gate-check")}); err != nil {
			t.Fatal(err)
		}
		if strings.Join(argv, " ") != want {
			t.Errorf("jobs %d ran %q, want %q", jobs, strings.Join(argv, " "), want)
		}
	}
}
