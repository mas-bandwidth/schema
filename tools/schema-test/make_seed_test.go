package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAFuzzRowTakesItsRunsSeed: a make row naming a seed variable runs with
// the run's seed (SeedEnv) set in it on make's command line, after its
// vars, and its verdict names the seed whether it passed or failed. A run
// with no seed, or one that is not a decimal number, fails the row naming
// the variable before make runs. A row that also fixes the seed in its vars,
// or names a variable make owns, is no make row.
func TestAFuzzRowTakesItsRunsSeed(t *testing.T) {
	t.Parallel()
	in := json.RawMessage(`{"file":"make/x.mk","target":"gate-check","vars":{"FUZZ_SECONDS":"600"},"seed":"FUZZ_SEED"}`)
	inst := testreg.Instance{Full: "s", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Inputs: in}}
	var ran []string
	var fail error
	a := makeAdapter{read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
		exec: func(_ context.Context, av []string) (string, error) {
			ran = append(ran, strings.Join(av, " "))
			return "", fail
		}, plan: emptyPlan}
	run := func(env map[string]string) testreg.Result {
		t.Helper()
		ctx := context.Background()
		if env != nil {
			ctx = testreg.WithEnv(ctx, env)
		}
		res, err := a.Run(ctx, "", []testreg.Instance{inst})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 1 {
			t.Fatalf("%d results, want 1", len(res))
		}
		return res[0]
	}
	if r := run(map[string]string{testreg.SeedEnv: "20261125"}); r.Verdict != testreg.Pass || r.Reason != "seed 20261125" {
		t.Errorf("a seeded pass: %+v, want a pass whose reason is %q", r, "seed 20261125")
	}
	if want := "make --no-print-directory gate-check FUZZ_SECONDS=600 FUZZ_SEED=20261125"; len(ran) != 1 || ran[0] != want {
		t.Fatalf("ran %q, want %q", ran, want)
	}
	fail = errors.New("exit status 2")
	if r := run(map[string]string{testreg.SeedEnv: "7"}); r.Verdict != testreg.Fail || !strings.HasPrefix(r.Reason, "seed 7: make gate-check") {
		t.Errorf("a seeded red: %+v, want a fail whose reason starts %q", r, "seed 7: make gate-check")
	}
	ran = nil
	for _, env := range []map[string]string{nil, {testreg.SeedEnv: ""}, {testreg.SeedEnv: "0x10"}, {testreg.SeedEnv: "-1"}} {
		if r := run(env); r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "FUZZ_SEED") || !strings.Contains(r.Reason, testreg.SeedEnv) {
			t.Errorf("seed env %v: %+v, want a fail naming FUZZ_SEED and %s", env, r, testreg.SeedEnv)
		}
	}
	if len(ran) != 0 {
		t.Errorf("a row without a seed ran make: %q", ran)
	}
	for in, ok := range map[string]bool{
		`{"file":"make/x.mk","target":"a","seed":"FUZZ_SEED"}`:                          true,
		`{"file":"make/x.mk","target":"a","seed":"FUZZ_SEED","vars":{"FUZZ_SEED":"1"}}`: false,
		`{"file":"make/x.mk","target":"a","seed":"fuzz_seed"}`:                          false,
		`{"file":"make/x.mk","target":"a","seed":"MAKEFLAGS"}`:                          false,
	} {
		if got := (makeAdapter{}).Answers(testreg.Test{Scope: testreg.LangFree, Inputs: json.RawMessage(in)}); got != ok {
			t.Errorf("%s: answered %v, want %v", in, got, ok)
		}
	}
}
