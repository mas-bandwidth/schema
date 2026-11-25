package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAUnionLanguageWithNoGoTestIsRed: a union row's instance for a language
// whose inputs name no Go test runs and fails, naming the languages that do
// answer the case; one whose named test its package does not declare fails
// as MISSING. Neither is left out, passed or skipped, and no go test runs
// for them.
func TestAUnionLanguageWithNoGoTestIsRed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "g"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "g", "a_test.go"), []byte("package g\n\nimport \"testing\"\n\nfunc TestHere(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := testreg.Test{Key: "roundtrip.fam.u", Family: "fam", Inputs: []byte(`{"tests":{"go":{"package":"./g","test":"TestGone"},"rust":{"package":"./g","test":"TestHere"}}}`)}
	a := newGoTestAdapter(nil, root, 1)
	for lang, want := range map[string]string{
		"lua": "lua has no Go test for roundtrip.fam.u: the case is unanswered in lua (answered in go, rust)",
		"go":  "MISSING: go answers roundtrip.fam.u with ./g TestGone",
	} {
		full := lang + ".unit.roundtrip.fam.u"
		res, err := a.runUnion(context.Background(), lang, []testreg.Instance{{Full: full, Lang: lang, Test: row}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 1 || res[0].Instance != full || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, want) {
			t.Errorf("%s: %+v, want one red result saying %q", lang, res, want)
		}
	}
}

// TestAUnionRowRoutesToTheGoUnionHarness: a row whose inputs name each
// language's Go test is run by go-union whatever its family is called, and
// the corpus adapter answers it for every language.
func TestAUnionRowRoutesToTheGoUnionHarness(t *testing.T) {
	t.Parallel()
	for _, fam := range []string{"wire-json-integers", "align-arrays", "conformance-wire"} {
		row := testreg.Test{Family: fam, Inputs: []byte(`{"tests":{"go":{"package":"./g","test":"TestX"}}}`)}
		if h, ok := harnessFor(row); !ok || h.Name != goUnion.Name {
			t.Errorf("family %s: harness %q %v, want %s", fam, h.Name, ok, goUnion.Name)
		}
		if !(&harnessAdapter{}).Answers(row) {
			t.Errorf("family %s: the corpus adapter does not answer a union row", fam)
		}
	}
}

// TestAUnionCallCarriesItsBuildTags: a language's Go test that needs its
// leg's build tag runs under it, and the call names exactly its tests.
func TestAUnionCallCarriesItsBuildTags(t *testing.T) {
	t.Parallel()
	got := strings.Join(goTestArgs(goTestPackage{pkg: "./internal/codegen/mootable", tags: "schema_leg_moo"}, "./internal/codegen/mootable", map[string]string{"TestB": "b", "TestA": "a"}, "60s"), " ")
	want := "test -count=1 -json -timeout 60s -tags schema_leg_moo -run ^(TestA|TestB)$ ./internal/codegen/mootable"
	if got != want {
		t.Fatalf("args %q, want %q", got, want)
	}
	if got := strings.Join(goTestArgs(goTestPackage{pkg: "./p"}, "./p", map[string]string{"TestA": "a"}, "60s"), " "); strings.Contains(got, "-tags") {
		t.Fatalf("an untagged call carries -tags: %q", got)
	}
}

// TestOnePackageRunsOneGoTestAtATime: the gotest and go-union harnesses
// share a lock per package directory, so their calls on one package never
// overlap.
func TestOnePackageRunsOneGoTestAtATime(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "pkg")
	unlock := lockGoTestPackage(dir)
	m, _ := goTestPackageLocks.Load(dir)
	if m.(*sync.Mutex).TryLock() {
		t.Fatal("a second call takes a package another call holds")
	}
	unlock()
	other := lockGoTestPackage(filepath.Join(t.TempDir(), "other"))
	defer other()
	if !m.(*sync.Mutex).TryLock() {
		t.Fatal("the package stays held after its call, or another package holds it")
	}
	m.(*sync.Mutex).Unlock()
}
