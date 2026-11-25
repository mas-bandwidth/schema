package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// cliRegistry is the CLI tests' registry, committed and only read: the tests
// write nothing anywhere.
const cliRegistry = "testdata/registry.jsonl"

// envFake passes every instance unless the run's environment carries BREAK,
// and then fails every one: a build a plant breaks.
type envFake struct {
	ignoreBreak bool
	why         string // the fail reason; empty is "broken"
}

func (envFake) Answers(t testreg.Test) bool { return t.PerLang() }

func (f envFake) Run(ctx context.Context, _ string, insts []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	for _, inst := range insts {
		v := testreg.Pass
		if testreg.EnvFrom(ctx)["BREAK"] != "" && !f.ignoreBreak {
			v = testreg.Fail
		}
		why := f.why
		if why == "" {
			why = "broken"
		}
		out = append(out, testreg.Result{Instance: inst.Full, Verdict: v, Reason: why})
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
		{[]string{"run", "--lang", "klingon"}, 1, `names what the registry does not have: language "klingon"`},
		{[]string{"run", "--group", "ready", "--lang", "klingon"}, 1, "--lang klingon is not one of the group's languages"},
		{[]string{"run", "--group", "ready", "--lang", "go"}, 0, "1 names"},
		{[]string{"run", "--group", "ready", "--tier", "functional"}, 2, "--group is a whole stored query"},
		{[]string{"langs", "go"}, 2, "langs takes no names"},
	} {
		code, out := cli(t, envFake{}, c.args...)
		if code != c.code || !strings.Contains(out, c.says) {
			t.Errorf("%v: exit %d, want %d saying %q:\n%s", c.args, code, c.code, c.says, out)
		}
	}
}

// TestAPlantPassesOnlyWhenItsTargetGoesRed: the plant's env breaks the
// fake's build, so its target fails and the plant passes; a fake that
// ignores the break leaves the target green, and the plant fails; a target
// red for a reason other than the plant's is a failed plant.
func TestAPlantPassesOnlyWhenItsTargetGoesRed(t *testing.T) {
	t.Parallel()
	if code, out := cli(t, envFake{}, "run", "none.functional.plant.fam.one-red"); code != 0 {
		t.Errorf("the plant failed against a build it breaks:\n%s", out)
	}
	code, out := cli(t, envFake{ignoreBreak: true}, "run", "none.functional.plant.fam.one-red")
	if code != 1 || !strings.Contains(out, "not red") {
		t.Errorf("the plant passed against a target that stayed green (exit %d):\n%s", code, out)
	}
	// the plant names its reason (testdata: broken): red for another cause
	// is not the planted bug
	code, out = cli(t, envFake{why: "some other failure"}, "run", "none.functional.plant.fam.one-red")
	if code != 1 || !strings.Contains(out, "not for the planted reason") {
		t.Errorf("the plant passed against a target red for another reason (exit %d):\n%s", code, out)
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

// faultFake is an adapter with one fault: it crashes, hangs until its
// context ends, reports a result nobody asked for, leaves an asked instance
// out, or answers no test at all.
type faultFake struct{ fault string }

func (f faultFake) Answers(t testreg.Test) bool { return t.PerLang() && f.fault != "no-instance" }

func (f faultFake) Run(ctx context.Context, _ string, insts []testreg.Instance) ([]testreg.Result, error) {
	switch f.fault {
	case "crash":
		return nil, errors.New("exit status 2")
	case "hang":
		<-ctx.Done()
		return nil, ctx.Err()
	case "omit":
		return nil, nil
	}
	var out []testreg.Result
	for _, inst := range insts {
		out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
	}
	if f.fault == "extra" {
		out = append(out, testreg.Result{Instance: "go.functional.roundtrip.fam.stray", Verdict: testreg.Pass})
	}
	return out, nil
}

// TestTheCommandExitsNonZeroOnEveryAdapterFault runs the command line
// against an adapter that crashes, times out, reports a stray result, omits
// one, or has no instance: each is exit 1 with its reason and the counts line.
func TestTheCommandExitsNonZeroOnEveryAdapterFault(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		fault string
		args  []string
		says  string
	}{
		{"crash", nil, "adapter fake crashed: exit status 2"},
		{"hang", []string{"--timeout", "1ms"}, "adapter fake timed out"},
		{"extra", nil, `reported "go.functional.roundtrip.fam.stray", which it was not asked for`},
		{"omit", nil, "expected 1 instances to run, 0 ran"},
		{"no-instance", nil, "adapter fake has no instance of roundtrip.fam.one"},
	} {
		args := append([]string{"run", "go.functional.roundtrip.fam.one"}, c.args...)
		code, out := cli(t, faultFake{c.fault}, args...)
		if code != 1 || !strings.Contains(out, c.says) || !strings.Contains(out, "schema-test FAIL: expected ") {
			t.Errorf("%s: exit %d, want 1 saying %q and the counts line:\n%s", c.fault, code, c.says, out)
		}
	}
}

// TestARunThatSelectsNothingStillPrintsItsCounts: an unknown name, a query
// matching 0 tests, an unknown group and an empty shard fail before any
// instance runs, and still print the counts line, every count 0.
func TestARunThatSelectsNothingStillPrintsItsCounts(t *testing.T) {
	t.Parallel()
	const counts = "schema-test FAIL: expected 0, ran 0, passed 0, failed 0, missing 0, unrepresentable 0"
	for _, args := range [][]string{
		{"run", "go.functional.roundtrip.fam.two"},
		{"run", "go.nightly.*"},
		{"run", "--group", "nope"},
		{"run", "--lang", "go", "--shard", "2/2"},
	} {
		code, out := cli(t, envFake{}, args...)
		if code != 1 || !strings.Contains(out, counts) {
			t.Errorf("%v: exit %d, want 1 with %q:\n%s", args, code, counts, out)
		}
	}
}

// TestLangsIsTheRegistrysList: the CI matrix's language list comes from the
// registry, with "none" for the language-free tests; --ready keeps the
// languages whose adapter has landed.
func TestLangsIsTheRegistrysList(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"langs"}, `["go","c","none"]`},
		{[]string{"langs", "--ready"}, `["go","none"]`},
	} {
		code, out := cli(t, envFake{}, c.args...)
		if code != 0 || strings.TrimSpace(out) != c.want {
			t.Errorf("%v: exit %d, %q; want %s", c.args, code, out, c.want)
		}
	}
}

// TestEveryCorpusFamilyHasItsHarness: the corpus adapter answers every
// per-language family a harness exists for, and a family with none is
// MISSING (it does not answer it), never routed to a harness that would run
// something else.
func TestEveryCorpusFamilyHasItsHarness(t *testing.T) {
	t.Parallel()
	a := &harnessAdapter{}
	for fam, want := range map[string]string{
		"align-arrays":          "align-arrays",
		"conformance-wire":      "conformance",
		"conformance-json-read": "conformance",
		"name-buildability":     "name-buildability",
		"no-such-family":        "",
	} {
		h, ok := harnessFor(testreg.Test{Family: fam})
		if (want != "") != ok || h.Name != want {
			t.Errorf("harnessFor(%s) = %q %v, want %q", fam, h.Name, ok, want)
		}
		if got := a.Answers(testreg.Test{Family: fam}); got != (want != "") {
			t.Errorf("Answers(%s) = %v", fam, got)
		}
	}
	if a.Answers(testreg.Test{Family: "conformance-wire", Scope: testreg.LangFree}) {
		t.Error("the corpus adapter answers a language-free test")
	}
	// the toolchains install once per process, before any language runs
	if got := nameBuild.Build("lua"); len(got) != 1 || strings.Join(got[0], " ") != "make name-buildability-deps" {
		t.Errorf("name-buildability build for lua: %q", got)
	}
	if got := conformance.Build("lua"); len(got) != 1 || strings.Join(got[0], " ") != "make conformance-leg-lua" {
		t.Errorf("conformance build for lua: %q", got)
	}
	// an instance routed here with no harness is an error naming it, before
	// anything builds or runs
	inst := testreg.Instance{Full: "go.functional.codegen.no-such-family.x", Lang: "go", Test: testreg.Test{Family: "no-such-family"}}
	if _, err := a.Run(context.Background(), "go", []testreg.Instance{inst}); err == nil || !strings.Contains(err.Error(), "no harness for family no-such-family") {
		t.Errorf("an instance with no harness: %v", err)
	}
}

// TestTheAddendaVerbChecksTheCommittedPages runs `schema-test addenda
// --check` itself: against the repository it is 0, and against a tree with
// no pages it exits 1 naming the missing page, writing nothing.
func TestTheAddendaVerbChecksTheCommittedPages(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		root string
		code int
		says string
	}{
		{"../..", 0, "are what ../../test/registry/registry.jsonl generates"},
		{"testdata", 1, "docs/SPEC-TESTS.md is missing"},
	} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), []string{"addenda", "--check", "--registry", "../../" + testreg.DefaultPath, "--root", c.root}, &out, &errOut, nil)
		if all := out.String() + errOut.String(); code != c.code || !strings.Contains(all, c.says) {
			t.Errorf("addenda --check --root %s: exit %d, want %d saying %q:\n%s", c.root, code, c.code, c.says, all)
		}
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"addenda", "stray"}, &out, &errOut, nil); code != 2 {
		t.Errorf("addenda with a name: exit %d, want 2:\n%s", code, errOut.String())
	}
}

// TestTheAddendaVerbWritesEveryPageAndRemovesTheStale drives the write mode
// through an in-memory tree: every generated page is written, a stale page
// is removed, and a write or remove that fails stops it naming the page.
func TestTheAddendaVerbWritesEveryPageAndRemovesTheStale(t *testing.T) {
	t.Parallel()
	files := []testreg.File{{Path: "docs/SPEC-TESTS.md", Data: []byte("a\n")}, {Path: "docs/SPEC-TESTS-UNIT.md", Data: []byte("b\n")}}
	tree := map[string]string{"docs/SPEC-TESTS-OLD.md": "old\n"}
	write := func(p string, d []byte) error { tree[p] = string(d); return nil }
	remove := func(p string) error {
		if _, ok := tree[p]; !ok {
			return errors.New("no such page")
		}
		delete(tree, p)
		return nil
	}
	var out bytes.Buffer
	if err := writeAddenda(&out, files, []string{"docs/SPEC-TESTS-OLD.md"}, write, remove); err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 || tree["docs/SPEC-TESTS.md"] != "a\n" || tree["docs/SPEC-TESTS-UNIT.md"] != "b\n" {
		t.Errorf("tree after writing: %q", tree)
	}
	if want := "removed docs/SPEC-TESTS-OLD.md\nwrote docs/SPEC-TESTS.md\nwrote docs/SPEC-TESTS-UNIT.md\n"; out.String() != want {
		t.Errorf("said %q, want %q", out.String(), want)
	}
	failWrite := func(p string, _ []byte) error { return errors.New("disk full") }
	if err := writeAddenda(&out, files, nil, failWrite, remove); err == nil || !strings.Contains(err.Error(), "writing docs/SPEC-TESTS.md: disk full") {
		t.Errorf("a failed write: %v", err)
	}
	if err := writeAddenda(&out, files, []string{"docs/SPEC-TESTS-GONE.md"}, write, remove); err == nil || !strings.Contains(err.Error(), "removing docs/SPEC-TESTS-GONE.md: no such page") {
		t.Errorf("a failed remove: %v", err)
	}
}
