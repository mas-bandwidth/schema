package testselect

import (
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// plant writes a repo root with one package p and the two registry files.
func plant(t *testing.T, ledger, registry string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "p")
	files := map[string]string{
		"p/p.go":                       "package p\n",
		"p/a_test.go":                  "package p\n\nimport \"testing\"\n\nfunc TestMain(m *testing.M) {}\nfunc TestA(t *testing.T) {}\nfunc TestB(t *testing.T) {}\nfunc Testlower(t *testing.T) {}\nfunc helper(t *testing.T) {}\nfunc FuzzC(f *testing.F) {}\nfunc ExampleD() {}\n",
		"p/x_test.go":                  "package p_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		"p/w_test.go":                  "//go:build windows\n\npackage p\n\nimport \"testing\"\n\nfunc TestWindowsOnly(t *testing.T) {}\n",
		"test/registry/unmoved.tsv":    ledger,
		"test/registry/registry.jsonl": registry,
	}
	for f, body := range files {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, dir
}

func linux() build.Context {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH = "linux", "amd64"
	return ctx
}

// TestNamesAreWhatGoTestRuns: Test, Fuzz and Example functions of both test
// packages, under the build constraints; never TestMain, a lower-case
// Testlower or a helper.
func TestNamesAreWhatGoTestRuns(t *testing.T) {
	t.Parallel()
	_, dir := plant(t, "", "")
	got, err := Names(linux(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "ExampleD FuzzC TestA TestB TestX"; strings.Join(got, " ") != want {
		t.Errorf("names: %q, want %q", strings.Join(got, " "), want)
	}
}

// TestSelectIsTheComplementLessNightly: the pattern names what -not does not
// match, leaves out a test whose tier line or registry row says nightly, and
// keeps it when all is set.
func TestSelectIsTheComplementLessNightly(t *testing.T) {
	t.Parallel()
	root, dir := plant(t,
		"tier\tp:TestB\t1\tL3\tnightly: a long campaign\ntier\tp:TestA\t1\tL3\tfunctional: needs cc\n",
		`{"rec":"test","key":"k","tier":"nightly","adapter":"gotest","inputs":{"package":"./p","test":"FuzzC"}}`+"\n")
	got, err := Select(linux(), root, []string{dir}, nil, regexp.MustCompile(`^Example`), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "^(TestA|TestX)$"; got != want {
		t.Errorf("select: %q, want %q", got, want)
	}
	got, err = Select(linux(), root, []string{dir}, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "^(ExampleD|FuzzC|TestA|TestB|TestX)$"; got != want {
		t.Errorf("select all: %q, want %q", got, want)
	}
	// a row that ran -run X -skip Y runs the tests X matches less those Y does
	got, err = Select(linux(), root, []string{dir}, regexp.MustCompile(`^Test`), regexp.MustCompile(`^TestA$`), true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "^(TestB|TestX)$"; got != want {
		t.Errorf("select run less not: %q, want %q", got, want)
	}
}

// TestAPerLanguageNightlyRowLeavesOutEachLanguagesTest: a nightly row of
// any adapter, one per language, whose test is named Test{lang}, leaves out
// TestA for the listed language A, and nothing for the language-free none.
// A row with a scope is not per-language and leaves out nothing.
func TestAPerLanguageNightlyRowLeavesOutEachLanguagesTest(t *testing.T) {
	t.Parallel()
	root, dir := plant(t, "",
		`{"rec":"lang","name":"none","adapter":"gotest"}`+"\n"+
			`{"rec":"lang","name":"A","adapter":"corpus"}`+"\n"+
			`{"rec":"test","key":"k","tier":"nightly","adapter":"corpus","inputs":{"package":"./p","test":"Test{lang}","subtest":"c"}}`+"\n"+
			`{"rec":"test","key":"s","tier":"nightly","scope":"none","adapter":"corpus","inputs":{"package":"./p","test":"Test{lang}X"}}`+"\n")
	got, err := Select(linux(), root, []string{dir}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "^(ExampleD|FuzzC|TestB|TestX)$"; got != want {
		t.Errorf("select: %q, want %q", got, want)
	}
}

// TestAnEmptySelectionFails: a row that would select nothing is an error by
// name, never a pattern that runs nothing.
func TestAnEmptySelectionFails(t *testing.T) {
	t.Parallel()
	root, dir := plant(t, "", "")
	if got, err := Select(linux(), root, []string{dir}, nil, regexp.MustCompile(`.`), false); err == nil || !strings.Contains(err.Error(), "names no test") {
		t.Errorf("an empty selection: %q, %v", got, err)
	}
}

// TestANameNightlyInOnePackageOnlyFails: the pattern is names only and go
// test applies it to every package, so a name nightly in one package of the
// selection and kept in another cannot be said; Select refuses it.
func TestANameNightlyInOnePackageOnlyFails(t *testing.T) {
	t.Parallel()
	root, dir := plant(t, "tier\tp:TestB\t1\tL3\tnightly: a long campaign\n", "")
	q := filepath.Join(root, "q")
	if err := os.MkdirAll(q, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(q, "b_test.go"), []byte("package q\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Select(linux(), root, []string{dir, q}, nil, nil, false); err == nil || !strings.Contains(err.Error(), "TestB is nightly in p") {
		t.Errorf("a name nightly in p and kept in q: %q, %v", got, err)
	}
	// apart, each is said
	if got, err := Select(linux(), root, []string{q}, nil, nil, false); err != nil || got != "^(TestB)$" {
		t.Errorf("q alone: %q, %v", got, err)
	}
}
