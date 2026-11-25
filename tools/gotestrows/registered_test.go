package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestRegisteredTestsLeaveTheSelection: a package whose every test is a
// registry row leaves go-test-touched's selection, a package with tests no row
// runs keeps one row running only those, and a package with no rows stays as
// it was. A test written in a fixture string, a method, TestMain and a
// lower-case Testhelper are not tests.
func TestRegisteredTestsLeaveTheSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"all/a_test.go":  "package all\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n\nfunc FuzzTwo(f *testing.F) {}\n\nfunc TestMain(m *testing.M) {}\n\nfunc Testhelper(t *testing.T) {}\n\nvar fixture = `\nfunc TestInAString(t *testing.T) {}\n`\n",
		"some/s_test.go": "package some\n\nimport \"testing\"\n\nfunc TestDone(t *testing.T) {}\n\nfunc TestNotYet(t *testing.T) {}\n\ntype r struct{}\n\nfunc (r) TestMethod(t *testing.T) {}\n",
		"none/n_test.go": "package none\n\nimport \"testing\"\n\nfunc TestAlone(t *testing.T) {}\n",
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := map[string]map[string]bool{
		"./all":  {"TestOne": true, "FuzzTwo": true},
		"./some": {"TestDone": true},
	}
	kept, left, err := unregistered(root, []string{"./all", "./some", "./none"}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"./some", "./none"}; !reflect.DeepEqual(kept, want) {
		t.Errorf("kept %v, want %v", kept, want)
	}
	if want := map[string][]string{"./some": {"TestNotYet"}}; !reflect.DeepEqual(left, want) {
		t.Errorf("left %v, want %v", left, want)
	}
}

// TestTheRegistryRowsReadTheirPackages: registered reads every gotest row of
// the repository's registry, and every one names a ./package and a test. A
// nightly row is one: the nightly lane runs its test, and go-test-touched,
// a per-change row, never runs a nightly test.
func TestTheRegistryRowsReadTheirPackages(t *testing.T) {
	t.Parallel()
	reg, err := registered(filepath.Join("..", "..", "test", "registry", "registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg["./compiler"]) == 0 {
		t.Fatalf("the registry has no gotest row for ./compiler: %d packages read", len(reg))
	}
	dir := t.TempDir()
	const head = `{"rec":"lang","name":"go","adapter":"go","write":"refuse"}
{"rec":"tier","name":"unit","what":"u"}
{"rec":"tier","name":"functional","what":"f"}
{"rec":"tier","name":"nightly","what":"n"}
{"rec":"type","name":"tooling","what":"t"}
{"rec":"group","name":"pr","what":"p","query":{"ready":true,"tiers":["unit","functional"]}}
`
	row := func(tier, test string) string {
		return `{"rec":"test","key":"tooling.a.` + strings.ToLower(test[4:]) + `","type":"tooling","family":"a","case":"` + strings.ToLower(test[4:]) + `","tier":"` + tier + `","scope":"none","adapter":"gotest","checks":"c","inputs":{"package":"./a","test":"` + test + `"},"expect":"e","source":"a/a_test.go:1"}` + "\n"
	}
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(path, []byte(head+row("unit", "TestUnit")+row("nightly", "TestNight")), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := registered(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got["./a"]["TestUnit"] || !got["./a"]["TestNight"] {
		t.Fatalf("registered %v: want TestUnit (unit) and TestNight (nightly, which the nightly lane runs)", got["./a"])
	}
}

// TestAPackageLeftOnlyWithForeignTestsLeavesTheSelection: a package whose
// tests without a row are each built only for another platform has nothing
// left for go-test-touched to run, so it leaves the selection rather than
// becoming a row that selects nothing; a package with a test left keeps only
// that one. (A nightly row counts as a row: TestTheRegistryRowsReadTheirPackages.)
func TestAPackageLeftOnlyWithForeignTestsLeavesTheSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	foreign := "plan9"
	if runtime.GOOS == "plan9" {
		foreign = "windows"
	}
	files := map[string]string{
		"a/a_test.go":       "package a\n\nimport \"testing\"\n\nfunc TestUnit(t *testing.T) {}\n\nfunc TestNight(t *testing.T) {}\n",
		"a/other_test.go":   "//go:build " + foreign + "\n\npackage a\n\nimport \"testing\"\n\nfunc TestElsewhere(t *testing.T) {}\n",
		"b/b_test.go":       "package b\n\nimport \"testing\"\n\nfunc TestUnit(t *testing.T) {}\n\nfunc TestLeft(t *testing.T) {}\n",
		"b/b_other_test.go": "//go:build " + foreign + "\n\npackage b\n\nimport \"testing\"\n\nfunc TestElsewhere(t *testing.T) {}\n",
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := map[string]map[string]bool{
		"./a": {"TestUnit": true, "TestNight": true},
		"./b": {"TestUnit": true},
	}
	kept, left, err := unregistered(root, []string{"./a", "./b"}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"./b"}; !reflect.DeepEqual(kept, want) {
		t.Errorf("kept %v, want %v", kept, want)
	}
	if want := map[string][]string{"./b": {"TestLeft"}}; !reflect.DeepEqual(left, want) {
		t.Errorf("left %v, want %v", left, want)
	}
}
