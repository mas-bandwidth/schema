package namebuild

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/languages"
	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

const root = "../.."

var corpus = filepath.Join(root, "testdata", "name-buildability")

var registryPath = filepath.Join(root, "test", "registry", "registry.jsonl")

var update = flag.Bool("update", false, "rewrite the registry's name-buildability rows from cases.tsv")

// TestPlanCoversTheCorpus is the fast half: cases.tsv parses, every row has
// its directory, and the plan runs every target of every row the checker
// accepts.
func TestPlanCoversTheCorpus(t *testing.T) {
	t.Parallel()
	rows, err := ReadRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for _, r := range rows {
		if _, err := ReadCase(filepath.Join(corpus, r.Case)); err != nil {
			t.Fatalf("%s: %v", r.Case, err)
		}
		if r.Verdict != RefuseAtCheck {
			accepted++
		}
	}
	jobs := Plan(rows)
	if len(jobs) != accepted*len(Targets) {
		t.Fatalf("plan has %d jobs, want %d rows x %d targets", len(jobs), accepted, len(Targets))
	}
	if accepted == 0 || !slices.ContainsFunc(rows, func(r Row) bool { return r.Case == "ctl" && r.Verdict == Builds }) {
		t.Fatal("the corpus has no ctl control that builds")
	}
}

// TestTargetsAreTheGenerators holds Targets to the generators the compiler
// registers (internal/languages), so a new generator cannot be left out of
// the nightly and a retired one cannot stay in it.
func TestTargetsAreTheGenerators(t *testing.T) {
	t.Parallel()
	var want []string
	for _, l := range languages.All() {
		want = append(want, l.Target)
	}
	got := slices.Clone(Targets)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("Targets %v, but the generators are %v", got, want)
	}
}

// TestEveryTargetHasItsTest holds this file to one top-level test per
// target, TestNameBuildability_<target>, each a call of nameBuildability
// with its own target: a target with no test is a column of the nightly
// that never runs, and the registry's rows name these tests.
func TestEveryTargetHasItsTest(t *testing.T) {
	t.Parallel()
	f, err := parser.ParseFile(token.NewFileSet(), "targets_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]string{} // test function -> the target it passes
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, TestPrefix) {
			continue
		}
		if len(fn.Body.List) != 1 {
			t.Errorf("%s: want one statement, nameBuildability(t, %q)", fn.Name.Name, strings.TrimPrefix(fn.Name.Name, TestPrefix))
			continue
		}
		target, ok := targetOf(fn.Body.List[0])
		if !ok {
			t.Errorf("%s: want nameBuildability(t, <target>)", fn.Name.Name)
			continue
		}
		calls[fn.Name.Name] = target
	}
	for _, tg := range Targets {
		name := TestName(tg)
		got, ok := calls[name]
		switch {
		case !ok:
			t.Errorf("target %s has no %s", tg, name)
		case got != tg:
			t.Errorf("%s runs target %q, want %q", name, got, tg)
		}
		delete(calls, name)
	}
	for name, tg := range calls {
		t.Errorf("%s runs %q, which is no target", name, tg)
	}
}

// targetOf reads the target out of `nameBuildability(t, "<target>")`.
func targetOf(s ast.Stmt) (string, bool) {
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		return "", false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return "", false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "nameBuildability" {
		return "", false
	}
	lit, ok := call.Args[1].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(lit.Value, `"`), true
}

// TestRegistryCasesAreDistinct holds the registry spelling of the builds
// rows (RegistryCase) to one row per case: two case directories that spell
// the same registry case would be one instance answering for two tests.
func TestRegistryCasesAreDistinct(t *testing.T) {
	t.Parallel()
	rows, err := ReadRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RegistryRows(rows); err != nil {
		t.Fatal(err)
	}
	// the refusal is real: two spellings of one case are refused by name
	_, err = RegistryRows([]Row{{Case: "b_probe", Verdict: Builds}, {Case: "b_Probe", Verdict: Builds}})
	if err == nil || !strings.Contains(err.Error(), "b_probe") || !strings.Contains(err.Error(), "b_Probe") {
		t.Fatalf("two cases spelled b-probe: %v, want a refusal naming both", err)
	}
}

// TestRegistryHoldsEveryBuildsRow holds test/registry/registry.jsonl's
// name-buildability rows to cases.tsv exactly: one codegen row per builds
// case, nightly, naming its TestNameBuildability_<lang> subtest. With
// -update it rewrites them.
func TestRegistryHoldsEveryBuildsRow(t *testing.T) {
	rows, err := ReadRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	want, err := RegistryRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	kept, have := SplitRegistry(string(src))
	if *update {
		out := kept + strings.Join(want, "\n") + "\n"
		if err := os.WriteFile(registryPath, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		have = want
	}
	if !slices.Equal(have, want) {
		for _, l := range want {
			if !slices.Contains(have, l) {
				t.Errorf("registry lacks: %s", l)
			}
		}
		for _, l := range have {
			if !slices.Contains(want, l) {
				t.Errorf("registry has, and cases.tsv does not: %s", l)
			}
		}
		t.Fatalf("the registry's %s rows are not cases.tsv's (rewrite them: go test ./tools/namebuild -run TestRegistryHoldsEveryBuildsRow -update)", Family)
	}
	reg, err := testreg.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Verdict != Builds {
			continue
		}
		key := "codegen." + Family + "." + RegistryCase(r.Case)
		if _, ok := reg.Test(key); !ok {
			t.Errorf("the registry does not load %s", key)
		}
	}
}

// TestReadRowsRefusesNamesGoTestRenames: a case go test would rename as a
// subtest (a space, a "/") and a case named twice are refused by line, so a
// row can never wait on a subtest that runs under another name.
func TestReadRowsRefusesNamesGoTestRenames(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ tsv, says string }{
		"a space":   {"b a\tbuilds\t-\tx\n", "cases.tsv:1: case \"b a\" holds a space"},
		"a slash":   {"ok\tbuilds\t-\tx\nb/a\tbuilds\t-\tx\n", "cases.tsv:2: case \"b/a\" holds"},
		"a repeat":  {"b_a\tbuilds\t-\tx\nb_a\tbuilds\t-\tx\n", "cases.tsv:2: case b_a again (first at line 1)"},
		"the clean": {"b_my-file\tbuilds\t-\tx\n", ""},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "cases.tsv"), []byte(c.tsv), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadRows(dir)
		switch {
		case c.says == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case c.says != "" && (err == nil || !strings.Contains(err.Error(), c.says)):
			t.Errorf("%s: %v, want %q", name, err, c.says)
		}
	}
}
