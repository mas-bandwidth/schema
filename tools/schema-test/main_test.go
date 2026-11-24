package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// cliRegistry is the CLI tests' registry, committed and only read: the tests
// write nothing anywhere.
const cliRegistry = "testdata/registry.jsonl"

// envFake passes every instance unless the run's environment carries BREAK,
// and then fails every one: a build a plant breaks.
type envFake struct{ ignoreBreak bool }

func (envFake) Answers(t testreg.Test) bool { return t.PerLang() }

func (f envFake) Run(ctx context.Context, _ string, insts []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	for _, inst := range insts {
		v := testreg.Pass
		if testreg.EnvFrom(ctx)["BREAK"] != "" && !f.ignoreBreak {
			v = testreg.Fail
		}
		out = append(out, testreg.Result{Instance: inst.Full, Verdict: v, Reason: "broken"})
	}
	return out, nil
}

func cli(t *testing.T, a testreg.Adapter, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), append(args, "--registry", cliRegistry), &out, &errOut, map[string]testreg.Adapter{"fake": a})
	return code, out.String() + errOut.String()
}

// TestTheCommandExitsNonZeroOnEveryFailRule runs the command line itself:
// each fail rule is a non-zero exit with its reason, and a green run is 0.
func TestTheCommandExitsNonZeroOnEveryFailRule(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args []string
		code int
		says string
	}{
		{[]string{"run", "go.functional.roundtrip.fam.one"}, 0, "schema-test PASS: expected 1, ran 1, passed 1"},
		{[]string{"run", "--group", "ready"}, 0, "1 names"},
		{[]string{"run", "go.functional.roundtrip.fam.two"}, 1, "unknown test"},
		{[]string{"run", "go.nightly.*"}, 1, "matches 0 tests"},
		{[]string{"run", "--lang", "c"}, 1, "MISSING c.functional.roundtrip.fam.one: language c has no adapter"},
		{[]string{"run", "--group", "nope"}, 1, `group "nope" is not in the registry`},
		{[]string{"run", "--lang", "go", "--shard", "2/2"}, 1, "shard 2/2 of 1 names matches 0 tests"},
		{[]string{"run"}, 2, "name what to run"},
		{[]string{"run", "c.functional.roundtrip.fam.one", "go.functional.roundtrip.fam.one", "--ready"}, 1, "the filters exclude the full names c.functional.roundtrip.fam.one"},
		{[]string{"list", "c.functional.roundtrip.fam.one", "--lang", "go"}, 1, "the filters exclude the full names c.functional.roundtrip.fam.one"},
		{[]string{"list", "*"}, 0, "3 names"},
	} {
		code, out := cli(t, envFake{}, c.args...)
		if code != c.code || !strings.Contains(out, c.says) {
			t.Errorf("%v: exit %d, want %d saying %q:\n%s", c.args, code, c.code, c.says, out)
		}
	}
}

// TestAPlantPassesOnlyWhenItsTargetGoesRed: the plant's env breaks the
// fake's build, so its target fails and the plant passes; a fake that
// ignores the break leaves the target green, and the plant fails.
func TestAPlantPassesOnlyWhenItsTargetGoesRed(t *testing.T) {
	t.Parallel()
	if code, out := cli(t, envFake{}, "run", "none.functional.plant.fam.one-red"); code != 0 {
		t.Errorf("the plant failed against a build it breaks:\n%s", out)
	}
	code, out := cli(t, envFake{ignoreBreak: true}, "run", "none.functional.plant.fam.one-red")
	if code != 1 || !strings.Contains(out, "not red") {
		t.Errorf("the plant passed against a target that stayed green (exit %d):\n%s", code, out)
	}
}

// TestGoTestEventsGiveOneVerdictEach: pass, fail and skip events; a skip is
// a fail, and a test that never reports is left for the runner to fail.
func TestGoTestEventsGiveOneVerdictEach(t *testing.T) {
	t.Parallel()
	events := `{"Action":"run","Test":"TestA"}
{"Action":"pass","Test":"TestA"}
{"Action":"output","Test":"TestB","Output":"boom\n"}
{"Action":"fail","Test":"TestB"}
{"Action":"output","Test":"TestC","Output":"needs lua\n"}
{"Action":"skip","Test":"TestC"}
{"Action":"pass","Test":"TestA/sub"}
{"Action":"pass"}
`
	asked := map[string]string{"TestA": "a", "TestB": "b", "TestC": "c", "TestD": "d"}
	res, err := goTestResults(strings.NewReader(events), asked)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if len(res) != 3 || got["a"].Verdict != testreg.Pass || got["b"].Verdict != testreg.Fail ||
		got["c"].Verdict != testreg.Fail || !strings.Contains(got["c"].Reason, "skipped") {
		t.Fatalf("results %+v", res)
	}
	if _, ok := got["d"]; ok {
		t.Fatal("a test that never reported has a verdict")
	}
}
