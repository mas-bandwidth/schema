package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
