package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixtureHeader is the head of every fixture test file. The blank uses keep
// each fixture a file gofmt and the parser accept whatever it imports.
const fixtureHeader = `package p

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"fix/lib"
)

var _ = bytes.Equal
var _ = errors.New
var _ = os.ReadDir
var _ = exec.Command
var _ = filepath.Glob
var _ = reflect.DeepEqual
var _ = runtime.GOOS
var _ = strings.Contains
var _ = lib.Parse

func mustParse(t *testing.T, s string) int {
	t.Helper()
	n, err := lib.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func quiet(t *testing.T, s string) int {
	return len(s)
}

func checkEach(t *testing.T, name string) {
	if name == "" {
		t.Error("empty name")
	}
}
`

const fixtureLib = `package lib

import "testing"

func Parse(s string) (int, error) { return len(s), nil }

func Refuse(s string) error { return nil }

func Targets() []string { return []string{"go"} }

type Compiler struct{}

func (Compiler) Targets() []string { return []string{"go"} }

func Require(t *testing.T, ok bool) {
	if !ok {
		t.Fatal("not ok")
	}
}

func Log(t *testing.T) { t.Log("nothing") }
`

// analyzeFixture writes one fixture module into a temp dir and returns its
// findings as "class func" strings.
func analyzeFixture(t *testing.T, body string) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, text string) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fix\n\ngo 1.26\n")
	write("lib/lib.go", fixtureLib)
	write("p/p_test.go", fixtureHeader+body)
	tr, err := loadTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range analyze(tr) {
		out = append(out, f.Class+" "+f.Func)
	}
	if tr.testFiles != 1 {
		t.Fatalf("the fixture has one test file and the analyzer saw %d", tr.testFiles)
	}
	return out
}

// ruleCases pairs, for every rule, a fixture the rule must report (the
// NEGATIVE CONTROL: a vacuous test) with the honest spelling of the same test,
// which it must leave alone.
var ruleCases = []struct {
	name string
	body string
	want []string // findings, as "class func"; nil for the honest spelling
}{
	// ---- class 1 ----
	{"1 empty test", `func TestEmpty(t *testing.T) {}`, []string{"1a TestEmpty"}},
	{"1 only logs", `func TestLogs(t *testing.T) { n, _ := lib.Parse("x"); t.Log(n) }`, []string{"1a TestLogs", "4p TestLogs"}},
	{"1 helper that cannot fail", `func TestQuiet(t *testing.T) { quiet(t, "x"); lib.Log(t) }`, []string{"1a TestQuiet"}},
	{"1 fuzz that cannot fail", `func FuzzNothing(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) { _ = len(b) }) }`, []string{"1a FuzzNothing"}},
	{"1 benchmark is listed", `func BenchmarkNothing(b *testing.B) { for b.Loop() { quiet(nil, "x") } }`, []string{"1b BenchmarkNothing"}},
	{"1 subtest that cannot fail", `func TestSub(t *testing.T) {
	if mustParse(t, "x") != 1 { t.Fatal("no") }
	t.Run("vacuous", func(t *testing.T) { t.Log("nothing") })
}`, []string{"1s TestSub"}},
	{"1 honest: direct failure", `func TestDirect(t *testing.T) { if 1+1 != 2 { t.Fatal("no") } }`, nil},
	{"1 honest: local helper", `func TestHelper(t *testing.T) { mustParse(t, "x") }`, nil},
	{"1 honest: helper in another package", `func TestRequire(t *testing.T) { lib.Require(t, true) }`, nil},
	{"1 honest: closure bound to a name", `func TestClosure(t *testing.T) {
	check := func(n int) { if n != 1 { t.Error("no") } }
	t.Run("a", func(t *testing.T) { check(1) })
}`, nil},
	{"1 honest: helper passed as a subtest", `func TestPassed(t *testing.T) { t.Run("a", subtest) }
func subtest(t *testing.T) { t.Error("always") }`, nil},
	{"1 honest: table of checks", `func TestTable(t *testing.T) {
	for _, tc := range []struct{ check func(*testing.T) }{{func(t *testing.T) { t.Error("x") }}} {
		t.Run("a", func(t *testing.T) { tc.check(t) })
	}
}`, nil},

	// ---- class 2 ----
	{"2 directory listing", `func TestDir(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil { t.Fatal(err) }
	for _, e := range entries {
		if e.Name() == "" { t.Error("empty name") }
	}
}`, []string{"2 TestDir"}},
	{"2 glob with the error dropped", `func TestGlob(t *testing.T) {
	paths, _ := filepath.Glob("*.golden")
	for _, p := range paths { checkEach(t, p) }
}`, []string{"2 TestGlob", "4s TestGlob"}},
	{"2 walk", `func TestWalk(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil { return err }
		if strings.HasSuffix(path, ".x") { t.Errorf("%s", path) }
		return nil
	})
	if err != nil { t.Fatal(err) }
}`, []string{"2 TestWalk"}},
	{"2 registry", `func TestRegistry(t *testing.T) {
	var c lib.Compiler
	for _, target := range c.Targets() {
		if target == "" { t.Error("empty target") }
	}
}`, []string{"2 TestRegistry"}},
	{"2 filtered slice", `func TestFiltered(t *testing.T) {
	var kept []string
	for _, s := range []string{"a", "b"} {
		if s > "b" { kept = append(kept, s) }
	}
	for _, s := range kept { checkEach(t, s) }
}`, []string{"2 TestFiltered"}},
	{"2 honest: count asserted", `func TestDirCounted(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil { t.Fatal(err) }
	if len(entries) == 0 { t.Fatal("no entries") }
	for _, e := range entries {
		if e.Name() == "" { t.Error("empty name") }
	}
}`, nil},
	{"2 honest: counted in the loop, checked after", `func TestDirTally(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil { t.Fatal(err) }
	seen := 0
	for _, e := range entries {
		seen++
		if e.Name() == "" { t.Error("empty name") }
	}
	if seen != 3 { t.Fatalf("saw %d", seen) }
}`, nil},
	{"2 honest: static table", `func TestStatic(t *testing.T) {
	for _, s := range []string{"a", "b"} { checkEach(t, s) }
}`, nil},
	{"2 honest: a list of problems, where empty is the pass", `func TestProblems(t *testing.T) {
	for _, p := range lib.Targets() { t.Errorf("problem: %s", p) }
}`, nil},
	{"2 honest: walk counted", `func TestWalkCounted(t *testing.T) {
	n := 0
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil { return err }
		n++
		if strings.HasSuffix(path, ".x") { t.Errorf("%s", path) }
		return nil
	})
	if err != nil || n == 0 { t.Fatal(err, n) }
}`, nil},

	// ---- class 3 ----
	{"3 err == nil with no else", `func TestGuard(t *testing.T) {
	n, err := lib.Parse("x")
	if err == nil {
		if n != 1 { t.Errorf("n = %d", n) }
	}
}`, []string{"3g TestGuard"}},
	{"3 len guard", `func TestLenGuard(t *testing.T) {
	xs := lib.Targets()
	if len(xs) > 0 {
		if xs[0] != "go" { t.Error("first") }
	}
}`, []string{"3g TestLenGuard"}},
	{"3 platform guard", `func TestPlatform(t *testing.T) {
	if runtime.GOOS == "plan9" { checkEach(t, "x") }
}`, []string{"3g TestPlatform"}},
	{"3 environment guard", `func TestEnv(t *testing.T) {
	if os.Getenv("RUN_IT") == "1" { checkEach(t, "x") }
}`, []string{"3g TestEnv"}},
	{"3 early return", `func TestEarly(t *testing.T) {
	if os.Getenv("RUN_IT") == "" { return }
	checkEach(t, "x")
}`, []string{"3r TestEarly"}},
	{"3 honest: else fails", `func TestGuardElse(t *testing.T) {
	n, err := lib.Parse("x")
	if err == nil {
		if n != 1 { t.Errorf("n = %d", n) }
	} else {
		t.Fatal(err)
	}
}`, nil},
	{"3 honest: the opposite case already failed", `func TestGuardAfter(t *testing.T) {
	_, err := lib.Parse("x")
	if err == nil { t.Fatal("want an error") }
	if err != nil {
		if !strings.Contains(err.Error(), "x") { t.Error("message") }
	}
}`, nil},
	{"3 honest: the block is the assertion", `func TestWantErr(t *testing.T) {
	_, err := lib.Parse("x")
	if err == nil { t.Fatal("want an error") }
}`, nil},

	// ---- class 4 ----
	{"4 product error to blank", `func TestDrop(t *testing.T) {
	n, _ := lib.Parse("x")
	if n != 1 { t.Fatal(n) }
}`, []string{"4p TestDrop"}},
	{"4 product error not looked at", `func TestBare(t *testing.T) {
	lib.Refuse("x")
	checkEach(t, "x")
}`, []string{"4p TestBare"}},
	{"4 child process result dropped", `func TestExec(t *testing.T) {
	out, _ := exec.Command("true").CombinedOutput()
	if len(out) != 0 { t.Fatal(out) }
}`, []string{"4x TestExec"}},
	{"4 child process run unchecked", `func TestExecRun(t *testing.T) {
	cmd := exec.Command("true")
	cmd.Run()
	checkEach(t, "x")
}`, []string{"4x TestExecRun"}},
	{"4 standard library is listed", `func TestStd(t *testing.T) {
	data, _ := os.ReadFile("x")
	if len(data) != 0 { t.Fatal("data") }
}`, []string{"4s TestStd"}},
	{"4 honest: checked", `func TestKept(t *testing.T) {
	n, err := lib.Parse("x")
	if err != nil || n != 1 { t.Fatal(n, err) }
	if err := lib.Refuse("x"); err != nil { t.Fatal(err) }
	if out, err := exec.Command("true").CombinedOutput(); err != nil { t.Fatal(err, out) }
}`, nil},

	// ---- class 5 ----
	{"5 value against itself", `func TestSelf(t *testing.T) {
	got := mustParse(t, "x")
	if got != got { t.Fatal("no") }
	if !reflect.DeepEqual(got, got) { t.Fatal("no") }
}`, []string{"5s TestSelf", "5s TestSelf"}},
	{"5 want from the same call as got", `func TestSameCall(t *testing.T) {
	got := mustParse(t, "x")
	want := mustParse(t, "x")
	if got != want { t.Fatal("no") }
}`, []string{"5c TestSameCall"}},
	{"5 honest: pinned", `func TestPinned(t *testing.T) {
	if got := mustParse(t, "x"); got != 1 { t.Fatal(got) }
}`, nil},

	// ---- class 6 ----
	{"6 subtest from a goroutine", `func TestGo(t *testing.T) {
	done := make(chan bool)
	go func() {
		t.Run("late", func(t *testing.T) { t.Error("x") })
		close(done)
	}()
	<-done
	checkEach(t, "x")
}`, []string{"6g TestGo"}},
	{"6 parent reads what parallel subtests write later", `func TestParallelRead(t *testing.T) {
	ran := 0
	for _, name := range []string{"a", "b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ran++
			checkEach(t, name)
		})
	}
	if ran != 2 { t.Fatalf("ran %d", ran) }
}`, []string{"6p TestParallelRead"}},
	{"6 honest: serial subtests", `func TestSerialRead(t *testing.T) {
	ran := 0
	for _, name := range []string{"a", "b"} {
		t.Run(name, func(t *testing.T) {
			ran++
			checkEach(t, name)
		})
	}
	if ran != 2 { t.Fatalf("ran %d", ran) }
}`, nil},

	// ---- class 7 ----
	{"7 a control that never sees the failure", `func TestGateRefusesBadInput(t *testing.T) {
	if mustParse(t, "x9") != 2 { t.Fatal("not two") }
}`, []string{"7 TestGateRefusesBadInput"}},
	{"7 a red test that only checks success", `func TestScanGoesRed(t *testing.T) {
	if got := mustParse(t, "x"); got != 1 { t.Fatal(got) }
}`, []string{"7 TestScanGoesRed"}},
	{"7 honest: the error is looked at", `func TestGateRefusesEmpty(t *testing.T) {
	if err := lib.Refuse(""); err == nil { t.Fatal("accepted") }
}`, nil},
	{"7 honest: the diagnostic is matched", `func TestNegativeControl(t *testing.T) {
	out, err := exec.Command("false").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "refused") { t.Fatal(string(out)) }
}`, nil},
	{"7 honest: not a negative word", `func TestReduceShared(t *testing.T) {
	if got := mustParse(t, "x"); got != 1 { t.Fatal(got) }
}`, nil},
}

// TestRules runs every rule against its negative control and against the
// honest spelling. A rule that stops reporting its control fails here by name.
func TestRules(t *testing.T) {
	classes := map[string]int{}
	for _, tc := range ruleCases {
		t.Run(tc.name, func(t *testing.T) {
			got := analyzeFixture(t, tc.body)
			want := slices.Clone(tc.want)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("findings = %q, want %q", got, want)
			}
		})
		for _, w := range tc.want {
			classes[w[:1]]++
		}
	}
	// the table itself is counted: every class has a control that is reported
	for _, class := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		if classes[class] == 0 {
			t.Errorf("class %s has no negative control in ruleCases", class)
		}
	}
}

// TestGateRefusesEachGatedClass is the gate's own negative control: one
// fixture per gated class, and the exit status is 1 with the finding printed.
func TestGateRefusesEachGatedClass(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range ruleCases {
		if len(tc.want) == 0 {
			continue
		}
		class := strings.Fields(tc.want[0])[0]
		gated := map[string]bool{"1a": true, "1s": true, "2": true, "4p": true, "4x": true, "7": true}[class]
		dir := t.TempDir()
		for rel, text := range map[string]string{"go.mod": "module fix\n\ngo 1.26\n", "lib/lib.go": fixtureLib, "p/p_test.go": fixtureHeader + tc.body} {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		status := runMain(dir, false, &stdout, &stderr)
		refusedHere := strings.Contains(stdout.String(), class+"\t")
		if gated && tc.name != "2 filtered slice" {
			seen[class] = true
			if status != 1 || !refusedHere {
				t.Errorf("%s: the gate answered %d and printed %q; want 1 and a class %s line", tc.name, status, stdout.String(), class)
			}
		}
		if !gated && refusedHere {
			t.Errorf("%s: class %s is listed, not gated, and the gate printed it: %q", tc.name, class, stdout.String())
		}
	}
	for _, class := range []string{"1a", "1s", "2", "4p", "4x", "7"} {
		if !seen[class] {
			t.Errorf("no fixture exercised the gate on class %s", class)
		}
	}
}

// TestGateRefusesAnEmptyTree: a gate that looked at nothing is not a pass.
func TestGateRefusesAnEmptyTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if status := runMain(dir, false, &stdout, &stderr); status != 1 || !strings.Contains(stderr.String(), "no _test.go file was analyzed") {
		t.Fatalf("an empty tree: status %d, stderr %q; want 1 and the no-file line", status, stderr.String())
	}
	// a test file with no test function in it
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\n\nfunc helper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if status := runMain(dir, false, &stdout, &stderr); status != 1 || !strings.Contains(stderr.String(), "no test function was analyzed") {
		t.Fatalf("a tree with no test function: status %d, stderr %q; want 1 and the no-function line", status, stderr.String())
	}
	// and the honest tree passes, so the two refusals above are not the only answer
	if err := os.WriteFile(filepath.Join(dir, "y_test.go"), []byte("package x\n\nimport \"testing\"\n\nfunc TestY(t *testing.T) { if 1 != 1 { t.Fatal() } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if status := runMain(dir, false, &stdout, &stderr); status != 0 || !strings.Contains(stderr.String(), "2 test files, 1 test functions, 0 refused findings") {
		t.Fatalf("an honest tree: status %d, stderr %q; want 0 and the counts", status, stderr.String())
	}
}
