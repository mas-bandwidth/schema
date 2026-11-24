package ci

// EVERY LEG ANSWERS EVERY ROOT OF THE FIXED CORPUS (EVERY TEST IS A SHARED
// CORPUS, 2026-11-24, docs/rulings.json every-test-is-a-shared-corpus). The
// fixed form's cases are defined once, in the C++ reference's corpus manifest
// (build/fixedform-corpus/manifest.txt), and a leg answers every root it
// names: a test walks the manifest through internal/fixedvector.EveryRoot,
// which judges each answer against the reference's line (the refusal it
// names, or the records and every value it wrote) and holds the answered set
// to the manifest's set when the test ends.
//
// What proves the walk ran is the RUN, not the source: the leg's TestMain
// calls fixedvector.Main(m.Run(), <leg>, <TestName>), which is red unless that
// very run left the named test's record of a complete walk.
// This check holds every internal/codegen/*table package to that wiring:
//
//   - the package's TestMain(m *testing.M) calls fixedvector.Main with its own
//     m.Run() and a literal test name;
//   - the named test is a func TestX(t *testing.T) in the package whose body
//     calls fixedvector.EveryRoot with that t.
//
// A leg not wired so is red unless it is in fixedCorpusEveryRootOwed with its
// cause. The ledger is an exact set: a listed leg that is wired is red until
// its line is removed, and a line naming no leg is red.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// fixedCorpusEveryRootOwed is every leg whose tests do not yet answer every
// root of the corpus manifest through fixedvector.EveryRoot, with the cause.
var fixedCorpusEveryRootOwed = map[string]string{
	"cpptable":      "hand-picked corpus files (cppVersionRows; R27 reads fx1.bin alone); no test answers every manifest root",
	"cstable":       "hand-picked corpus files (csVersionRows; R27 checks the harness source text); no test answers every manifest root",
	"ctable":        "hand-picked corpus files (cVersionRows); no test answers every manifest root",
	"darttable":     "TestFixedCorpusRoundTripsByteIdenticalDart and fixedroadmap_t12 walk every manifest row through the Dart reader but do not hand their answers to fixedvector.EveryRoot, so no landed value is compared with the manifest's values=",
	"elixirtable":   "hand-picked corpus files (versionRows; R27 reads fx1 and the old_ brackets); no test answers every manifest root",
	"goosetable":    "hand-picked corpus files (gooseVersionRows and the shared vectors); no test answers every manifest root",
	"gotable":       "hand-picked corpus files (fixedOracleVectors, versionRows; R27 reads fx1); no test answers every manifest root",
	"javatable":     "t18R27 (fixedroadmap_t18_test.go) walks every manifest row through the Java reader and compares values, but loads 71 of 105: the 34 roots with a union in their closure (dead_* 22, deprecated_* 6, fu1, fu2, old_/new_union_append, old_/new_union_arm_payload_widen) are logged unloaded because the registered java target refuses a union closure; it does not report to fixedvector.EveryRoot",
	"jstable":       "hand-picked corpus files (jsVersionRows; R27 reads one row); no test answers every manifest root",
	"lobstertable":  "hand-picked corpus files (fixedCorpus, lobsterVersionRows); no test answers every manifest root",
	"luautable":     "hand-picked corpus files (fixedCorpusVectors, luauVersionRows); no test answers every manifest root",
	"mootable":      "hand-picked corpus files (mooVersionRows; the manifest is a lookup only); no test answers every manifest root",
	"pythontable":   "hand-picked corpus files (the fixedproof rows only); no test answers every manifest root",
	"rubytable":     "hand-picked corpus files (the grown-bound rows only); no test answers every manifest root",
	"rusttable":     "t12R27 walks every manifest line but checks the header and framing arithmetic in Go, not through the Rust reader; the Rust reader reads hand-picked files (versionRows)",
	"squirreltable": "hand-picked corpus files (fixedVectors, 4 files); no test answers every manifest root",
	"unitytable":    "hand-picked corpus files (the grown-bound rows and the fixedproof suite); no test answers every manifest root",
}

// fixedvectorCall reports whether call is fixedvector.<name>(...).
func fixedvectorCall(call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "fixedvector" && sel.Sel.Name == name
}

// firstParam is the name of a function's first parameter when its type is
// *testing.<typ>, else "".
func firstParam(fn *ast.FuncDecl, typ string) string {
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) != 1 || fn.Recv != nil {
		return ""
	}
	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "testing" || sel.Sel.Name != typ {
		return ""
	}
	return params[0].Names[0].Name
}

// callsWith reports whether body calls fixedvector.<name> with the identifier
// arg as its first argument, and returns each such call.
func callsWith(body *ast.BlockStmt, name, arg string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !fixedvectorCall(call, name) || len(call.Args) == 0 {
			return true
		}
		if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == arg {
			out = append(out, call)
		}
		return true
	})
	return out
}

// mainCalls is every fixedvector.Main(<m>.Run(), leg, test) call in body,
// m being TestMain's own parameter.
func mainCalls(body *ast.BlockStmt, m string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !fixedvectorCall(call, "Main") || len(call.Args) != 3 {
			return true
		}
		run, ok := call.Args[0].(*ast.CallExpr)
		if !ok || len(run.Args) != 0 {
			return true
		}
		sel, ok := run.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Run" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == m {
			out = append(out, call)
		}
		return true
	})
	return out
}

// everyRootWiring reads a leg package's _test.go files and reports whether
// it is wired: a TestMain handing its own m to fixedvector.Main with a
// literal test name, and that test calling fixedvector.EveryRoot with its own
// t. why says what is missing when it is not.
func everyRootWiring(dir string) (wired bool, why string, err error) {
	files, err := provesran.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return false, "", err
	}
	fset := token.NewFileSet()
	tests := map[string]*ast.FuncDecl{}
	var named []string
	mainFound := false
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return false, "", err
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if fn.Name.Name == "TestMain" {
				m := firstParam(fn, "M")
				if m == "" {
					continue
				}
				mainFound = true
				for _, call := range mainCalls(fn.Body, m) {
					lit, ok := call.Args[2].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if name, err := strconv.Unquote(lit.Value); err == nil {
						named = append(named, name)
					}
				}
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Test") && firstParam(fn, "T") != "" {
				tests[fn.Name.Name] = fn
			}
		}
	}
	switch {
	case !mainFound:
		return false, "it has no TestMain(m *testing.M), so no run of it is required to leave an every-root record (fixedvector.Main)", nil
	case len(named) == 0:
		return false, "its TestMain never hands its m.Run() to fixedvector.Main with a literal test name, so no run of it is required to leave an every-root record", nil
	}
	for _, name := range named {
		fn, ok := tests[name]
		if !ok {
			return false, "its TestMain names " + name + " to fixedvector.Main, and the package has no func " + name + "(t *testing.T)", nil
		}
		if len(callsWith(fn.Body, "EveryRoot", firstParam(fn, "T"))) == 0 {
			return false, name + " never calls fixedvector.EveryRoot with its own t", nil
		}
	}
	return true, "", nil
}

// everyRootGaps is every leg package under codegen (a directory named
// *table) that is not wired through fixedvector.Main and is not ledgered,
// every ledgered leg that is wired, and every ledger line naming no leg.
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
		wired, why, err := everyRootWiring(filepath.Join(codegen, leg))
		if err != nil {
			return nil, err
		}
		cause, listed := owed[leg]
		switch {
		case wired && listed:
			gaps = append(gaps, leg+": its runs are held to an every-root record through fixedvector.Main and EveryRoot; remove its fixedCorpusEveryRootOwed line (owed: "+cause+")")
		case !wired && !listed:
			gaps = append(gaps, leg+": does not answer every corpus root: "+why)
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
// calls EveryRoot (a comment naming it does not count), a call outside a test
// (the hollow plant: a helper no test runs), a TestMain that never hands its
// m.Run() to fixedvector.Main, a Main naming a test the package lacks or one that never
// calls EveryRoot with its own t, a ledgered leg that is wired, and a ledger
// line naming no leg.
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
	const head = "package x\nimport (\n\t\"os\"\n\t\"testing\"\n)\n"
	const main = "func TestMain(m *testing.M) { os.Exit(fixedvector.Main(m.Run(), \"x\", \"TestWalk\")) }\n"
	const walk = "func TestWalk(t *testing.T) { a, _ := fixedvector.EveryRoot(t, \"x\", \"corpus\"); _ = a }\n"
	const wired = head + main + walk
	const picked = head + "// fixedvector.EveryRoot is not called here\nvar files = []string{\"fx1.bin\"}\n"
	const hollow = head + "func r4905Hollow() { _, _ = fixedvector.EveryRoot(nil, \"go\", \"\") }\n"
	const plainMain = head + "func TestMain(m *testing.M) { os.Exit(m.Run()) }\n" + walk
	const mainNamesNone = head + "func TestMain(m *testing.M) { os.Exit(fixedvector.Main(m.Run(), \"x\", \"TestGone\")) }\n" + walk
	const mainOtherCode = head + "func TestMain(m *testing.M) { os.Exit(fixedvector.Main(0, \"x\", \"TestWalk\")) }\n" + walk
	const walkNoCall = head + main + "func TestWalk(t *testing.T) { helper() }\nfunc helper() { _, _ = fixedvector.EveryRoot(nil, \"x\", \"corpus\") }\n"
	const walkOtherT = head + main + "var other *testing.T\nfunc TestWalk(t *testing.T) { _, _ = fixedvector.EveryRoot(other, \"x\", \"corpus\") }\n"
	for _, c := range []struct {
		name  string
		legs  map[string]string
		owed  map[string]string
		wants []string
	}{
		{"clean", map[string]string{"atable": wired, "btable": picked}, map[string]string{"btable": "owed"}, nil},
		{"hand-picked", map[string]string{"atable": wired, "btable": picked}, nil, []string{"btable: does not answer every corpus root: it has no TestMain"}},
		{"hollow call outside a test", map[string]string{"atable": hollow}, nil, []string{"atable: does not answer every corpus root: it has no TestMain"}},
		{"TestMain without Main", map[string]string{"atable": plainMain}, nil, []string{"atable: does not answer every corpus root: its TestMain never hands its m.Run() to fixedvector.Main"}},
		{"Main judges no run", map[string]string{"atable": mainOtherCode}, nil, []string{"atable: does not answer every corpus root: its TestMain never hands its m.Run() to fixedvector.Main"}},
		{"Main names a missing test", map[string]string{"atable": mainNamesNone}, nil, []string{"atable: does not answer every corpus root: its TestMain names TestGone"}},
		{"test never calls EveryRoot", map[string]string{"atable": walkNoCall}, nil, []string{"atable: does not answer every corpus root: TestWalk never calls fixedvector.EveryRoot with its own t"}},
		{"EveryRoot with another t", map[string]string{"atable": walkOtherT}, nil, []string{"atable: does not answer every corpus root: TestWalk never calls fixedvector.EveryRoot with its own t"}},
		{"stale ledger", map[string]string{"atable": wired}, map[string]string{"atable": "owed"}, []string{"atable: its runs are held to an every-root record"}},
		{"ledger names no leg", map[string]string{"atable": wired}, map[string]string{"gonetable": "owed"}, []string{"gonetable: fixedCorpusEveryRootOwed names a leg package that does not exist"}},
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
