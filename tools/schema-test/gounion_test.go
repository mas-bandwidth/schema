package main

import (
	"context"
	"io"
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

// TestAUnionCallRunsUnderItsPackageLockWithItsTags: a union instance's go
// test call carries the build tags its row names, and runs holding its
// package's lock, which the gotest harness's calls take too, so the two
// never run go test on one package at once; its test's pass is its verdict.
func TestAUnionCallRunsUnderItsPackageLockWithItsTags(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":      "module x\n\ngo 1.25\n",
		"g/a_test.go": "package g\n\nimport \"testing\"\n\nfunc TestHere(t *testing.T) {}\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newGoTestAdapter(nil, root, 1)
	var calls [][]string
	a.goTest = func(_ context.Context, dir string, args []string, stdout, _ io.Writer) error {
		calls = append(calls, args)
		m, held := goTestPackageLocks.Load(filepath.Join(dir, args[len(args)-1]))
		if !held {
			t.Errorf("go test %v runs with no lock for its package", args)
		} else if mu := m.(*sync.Mutex); mu.TryLock() {
			mu.Unlock()
			t.Errorf("go test %v runs without holding its package's lock", args)
		}
		_, err := io.WriteString(stdout, `{"Action":"run","Test":"TestHere"}`+"\n"+`{"Action":"pass","Test":"TestHere","Elapsed":0}`+"\n")
		return err
	}
	row := testreg.Test{Key: "roundtrip.fam.u", Family: "fam", Inputs: []byte(`{"tests":{"moo":{"package":"./g","test":"TestHere","tags":"schema_leg_moo"}}}`)}
	res, err := a.runUnion(context.Background(), "moo", []testreg.Instance{{Full: "moo.unit.roundtrip.fam.u", Lang: "moo", Test: row}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Verdict != testreg.Pass {
		t.Fatalf("results %+v, want moo's instance passed", res)
	}
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0], " "), "-tags schema_leg_moo -run ^(TestHere)$ ./g") {
		t.Fatalf("go test calls %q, want one call of TestHere under schema_leg_moo", calls)
	}
}
