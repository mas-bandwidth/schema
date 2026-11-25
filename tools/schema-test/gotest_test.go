package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestASubtestThatSkipsFailsItsParent: go test reports the parent of a
// skipped subtest as passed; the driver has no skipped verdict, so the
// parent fails and names the subtest. A subtest's failure output joins the
// parent's reason, and the parent's seconds are go test's own.
func TestASubtestThatSkipsFailsItsParent(t *testing.T) {
	t.Parallel()
	events := `{"Action":"run","Test":"TestA"}
{"Action":"run","Test":"TestA/needs_cc"}
{"Action":"output","Test":"TestA/needs_cc","Output":"no cc on PATH\n"}
{"Action":"skip","Test":"TestA/needs_cc"}
{"Action":"pass","Test":"TestA","Elapsed":0.5}
{"Action":"run","Test":"TestB"}
{"Action":"output","Test":"TestB/row","Output":"row 3: got 4\n"}
{"Action":"fail","Test":"TestB/row"}
{"Action":"fail","Test":"TestB","Elapsed":1.25}
{"Action":"pass","Test":"TestC","Elapsed":2}
`
	res, err := goTestResults(strings.NewReader(events), map[string]string{"TestA": "a", "TestB": "b", "TestC": "c"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if a := got["a"]; a.Verdict != testreg.Fail || !strings.Contains(a.Reason, "subtests skipped") || !strings.Contains(a.Reason, "needs_cc") {
		t.Errorf("a parent whose subtest skipped: %+v", a)
	}
	if b := got["b"]; b.Verdict != testreg.Fail || !strings.Contains(b.Reason, "row 3: got 4") || b.Seconds != 1.25 {
		t.Errorf("a parent whose subtest failed: %+v", b)
	}
	if c := got["c"]; c.Verdict != testreg.Pass || c.Seconds != 2 {
		t.Errorf("a passing test: %+v", c)
	}
}

// TestARowWhoseGoTestIsGoneIsMissing: the gotest adapter answers a row only
// when its package's source declares the test, so a deleted or renamed test
// still registered is MISSING by name, never run and never passed.
func TestARowWhoseGoTestIsGoneIsMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package pkg\n\nimport \"testing\"\n\nfunc TestHere(t *testing.T) {}\n\nfunc FuzzHere(f *testing.F) {}\n"
	if err := os.WriteFile(filepath.Join(root, "pkg", "a_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newGoTestAdapter(nil, root, 1)
	row := func(test string) testreg.Test {
		return testreg.Test{Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(`{"package":"./pkg","test":"` + test + `"}`)}
	}
	for test, want := range map[string]bool{"TestHere": true, "FuzzHere": true, "TestGone": false, "TestHer": false} {
		if got := a.Answers(row(test)); got != want {
			t.Errorf("Answers(%s) = %v, want %v", test, got, want)
		}
	}
	if a.Answers(testreg.Test{Inputs: []byte(`{"package":"./pkg","test":"TestHere"}`)}) {
		t.Error("a per-language row is answered by the gotest adapter")
	}
	if a.Answers(testreg.Test{Scope: testreg.LangFree, Inputs: []byte(`{"package":"pkg","test":"TestHere"}`)}) {
		t.Error("a row whose package is not ./ relative is answered")
	}
}

// TestAPackageInAModuleOfItsOwnRunsFromThatModule: go test runs a package
// from the nearest directory holding a go.mod, the root's or its own.
func TestAPackageInAModuleOfItsOwnRunsFromThatModule(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, f := range []string{"go.mod", "a/b/go.mod", "a/b/c/x_test.go", "d/y_test.go"} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newGoTestAdapter(nil, root, 1)
	for pkg, want := range map[string][2]string{
		"./a/b/c": {filepath.Join(root, "a/b"), "./c"},
		"./a/b":   {filepath.Join(root, "a/b"), "./."},
		"./d":     {root, "./d"},
	} {
		dir, rel, err := a.moduleOf(pkg)
		if err != nil || dir != want[0] || rel != want[1] {
			t.Errorf("moduleOf(%s) = %s %s %v, want %s %s", pkg, dir, rel, err, want[0], want[1])
		}
	}
	if _, _, err := newGoTestAdapter(nil, t.TempDir(), 1).moduleOf("./x"); err == nil {
		t.Error("a package in no module was given one")
	}
}

// TestRowsAreNamedFromTheirGoTests: a moved Go test's family, case and type
// come from its package and name.
func TestRowsAreNamedFromTheirGoTests(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"TestEveryDriverTestIsRegistered": "every-driver-test-is-registered",
		"TestCPPJSONArrayRoot":            "cppjson-array-root",
		"TestIssue710PacketStringBounds":  "issue710-packet-string-bounds",
		"Test_snake_Case":                 "snake-case",
		"FuzzReadText":                    "fuzz-read-text",
		"Test":                            "test",
	} {
		if got := caseOf(name); got != want {
			t.Errorf("caseOf(%s) = %s, want %s", name, got, want)
		}
	}
	for dir, want := range map[string]string{"internal/ci/hostilecorpus": "ci-hostilecorpus", "cmd/schema": "cmd-schema", "tools/realpacket-gen": "realpacket-gen", "compiler": "compiler"} {
		if got := familyOf(dir); got != want {
			t.Errorf("familyOf(%s) = %s, want %s", dir, got, want)
		}
	}
	for _, c := range [][3]string{
		{"internal/fuzz", "FuzzPipeline", "fuzz"}, {"internal/ci", "TestX", "tooling"}, {"tools/roadmap", "TestX", "tooling"},
		{"bench/paired", "TestX", "bench"}, {"bench/tools", "TestX", "tooling"}, {"compiler", "TestX", "compiler"}, {"internal/check", "TestX", "compiler"},
	} {
		if got := typeOf(c[0], c[1]); got != c[2] {
			t.Errorf("typeOf(%s, %s) = %s, want %s", c[0], c[1], got, c[2])
		}
	}
}

// TestSecondsAreReadFromARunsLog: schema-test seconds records the seconds a
// log shows for each passing instance, keeps every other byte of the
// registry, and refuses a log with no such line and a name that is no
// instance.
func TestSecondsAreReadFromARunsLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry.jsonl")
	data, err := os.ReadFile(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reg, data, 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("good.log", "2026-11-24T00:00:00Z PASS go.functional.roundtrip.fam.one (1.234s)\nFAIL go.functional.roundtrip.fam.one (9.00s): broken\n")
	var out strings.Builder
	if err := runErr(t.Context(), []string{"seconds", "--registry", reg, good}, &out, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"case":"one",`) || !strings.Contains(string(after), `,"seconds":1.24}`) || len(after) != len(data)+len(`,"seconds":1.24`) {
		t.Fatalf("the registry after seconds:\n%s", after)
	}
	for log, says := range map[string]string{
		write("none.log", "schema-test PASS: expected 1\n"):                     "shows no passing test",
		write("stranger.log", "PASS go.functional.roundtrip.fam.two (1.00s)\n"): "unknown test",
	} {
		if err := runErr(t.Context(), []string{"seconds", "--registry", reg, log}, &out, nil); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: %v, want %q", log, err, says)
		}
	}
}
