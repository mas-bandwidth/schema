package ci

// EVERY LEG ANSWERS EVERY ROOT OF THE FIXED CORPUS (EVERY TEST IS A SHARED
// CORPUS, Glenn 2026-11-24). The fixed form's cases are defined once, in the
// C++ reference's corpus manifest (build/fixedform-corpus/manifest.txt), and a
// leg answers every root it names: its test walks the manifest and reports
// each answer to internal/fixedvector.EveryRoot, which holds the answered set
// to the manifest's set, by name, when the test ends. A leg whose fixed-form
// tests read a hand-picked list of files answers what it picked and nothing the
// reference grows, so this check reads every internal/codegen/*table package
// and refuses one that never calls fixedvector.EveryRoot, unless the leg is in
// fixedCorpusEveryRootOwed with its cause. The ledger is an exact set: a
// listed leg that calls EveryRoot is red until its line is removed.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixedCorpusEveryRootOwed is every leg whose fixed-form tests do not yet
// answer every root of the corpus manifest through fixedvector.EveryRoot.
var fixedCorpusEveryRootOwed = map[string]string{
	"cpptable":      "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"cstable":       "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"ctable":        "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"darttable":     "TestFixedCorpusRoundTripsByteIdenticalDart walks every manifest row but does not report its answers to fixedvector.EveryRoot",
	"elixirtable":   "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"goosetable":    "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"gotable":       "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"javatable":     "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"jstable":       "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"lobstertable":  "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"luautable":     "hand-picked corpus files (fixedCorpusVectors, luauVersionRows); no test answers every manifest root",
	"mootable":      "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"pythontable":   "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"rubytable":     "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"rusttable":     "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"squirreltable": "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
	"unitytable":    "hand-picked corpus files in its fixed-form tests; no test answers every manifest root",
}

// callsEveryRoot reports whether any _test.go file in dir calls
// fixedvector.EveryRoot (a call in the syntax tree, never a comment).
func callsEveryRoot(dir string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return false, err
	}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return false, err
		}
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return !found
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "fixedvector" && sel.Sel.Name == "EveryRoot" {
				found = true
			}
			return !found
		})
		if found {
			return true, nil
		}
	}
	return false, nil
}

// everyRootGaps is every leg package under codegen (a directory named
// *table) that answers a hand-picked subset and is not ledgered, every
// ledgered leg that now answers every root, and every ledger line naming no
// leg.
func everyRootGaps(codegen string, owed map[string]string) ([]string, error) {
	entries, err := os.ReadDir(codegen)
	if err != nil {
		return nil, err
	}
	var gaps []string
	legs := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "table") {
			continue
		}
		leg := e.Name()
		legs[leg] = true
		every, err := callsEveryRoot(filepath.Join(codegen, leg))
		if err != nil {
			return nil, err
		}
		cause, listed := owed[leg]
		switch {
		case every && listed:
			gaps = append(gaps, leg+": answers every corpus root through fixedvector.EveryRoot; remove its fixedCorpusEveryRootOwed line (owed: "+cause+")")
		case !every && !listed:
			gaps = append(gaps, leg+": its fixed-form tests never call fixedvector.EveryRoot, so they answer a hand-picked subset of the corpus manifest's roots, not every root")
		}
	}
	for leg := range owed {
		if !legs[leg] {
			gaps = append(gaps, leg+": fixedCorpusEveryRootOwed names a leg package that does not exist")
		}
	}
	slices.Sort(gaps)
	return gaps, nil
}

func TestEveryLegAnswersEveryFixedCorpusRoot(t *testing.T) {
	t.Parallel()
	gaps, err := everyRootGaps(filepath.Join(repoRoot(t), "internal", "codegen"), fixedCorpusEveryRootOwed)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gaps {
		t.Error(g)
	}
	for _, leg := range slices.Sorted(func(yield func(string) bool) {
		for k := range fixedCorpusEveryRootOwed {
			if !yield(k) {
				return
			}
		}
	}) {
		t.Logf("owed: %s: %s", leg, fixedCorpusEveryRootOwed[leg])
	}
}

// TestEveryLegAnswersEveryFixedCorpusRootNegativeControl plants each fault the
// check exists for in a tree of its own and requires it red: a leg that never
// calls EveryRoot (a comment naming it does not count), a ledgered leg that
// does, and a ledger line naming no leg.
func TestEveryLegAnswersEveryFixedCorpusRootNegativeControl(t *testing.T) {
	t.Parallel()
	write := func(t *testing.T, dir, leg, src string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, leg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, leg, "fixed_test.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const every = "package x\nfunc f() { _ = fixedvector.EveryRoot(nil, \"x\", nil) }\n"
	const picked = "package x\n// fixedvector.EveryRoot is not called here\nvar files = []string{\"fx1.bin\"}\n"
	for _, c := range []struct {
		name  string
		legs  map[string]string
		owed  map[string]string
		wants []string
	}{
		{"clean", map[string]string{"atable": every, "btable": picked}, map[string]string{"btable": "owed"}, nil},
		{"hand-picked", map[string]string{"atable": every, "btable": picked}, nil, []string{"btable: its fixed-form tests never call"}},
		{"stale ledger", map[string]string{"atable": every}, map[string]string{"atable": "owed"}, []string{"atable: answers every corpus root"}},
		{"ledger names no leg", map[string]string{"atable": every}, map[string]string{"gonetable": "owed"}, []string{"gonetable: fixedCorpusEveryRootOwed names a leg package that does not exist"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for leg, src := range c.legs {
				write(t, dir, leg, src)
			}
			gaps, err := everyRootGaps(dir, c.owed)
			if err != nil {
				t.Fatal(err)
			}
			if len(gaps) != len(c.wants) {
				t.Fatalf("gaps %q, want %d naming %q", gaps, len(c.wants), c.wants)
			}
			for i, w := range c.wants {
				if !strings.HasPrefix(gaps[i], w) {
					t.Errorf("gap %q, want it to begin %q", gaps[i], w)
				}
			}
		})
	}
}
