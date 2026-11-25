package namebuild

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/languages"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
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
	f, err := parser.ParseFile(token.NewFileSet(), "namebuild_test.go", nil, 0)
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

// The per-target nightly: every builds row generates in the target, and the
// output builds and loads with the target's own toolchain. Each row is a
// subtest named by its case, and each subtest is one registry instance,
// <lang>.nightly.codegen.name-buildability.<case> (RegistryRows). A row a
// target cannot build yet fails, by name, until it builds: there is no list
// of expected failures.
func TestNameBuildability_c(t *testing.T)        { nameBuildability(t, "c") }
func TestNameBuildability_cpp(t *testing.T)      { nameBuildability(t, "cpp") }
func TestNameBuildability_cs(t *testing.T)       { nameBuildability(t, "cs") }
func TestNameBuildability_unity(t *testing.T)    { nameBuildability(t, "unity") }
func TestNameBuildability_dart(t *testing.T)     { nameBuildability(t, "dart") }
func TestNameBuildability_rust(t *testing.T)     { nameBuildability(t, "rust") }
func TestNameBuildability_go(t *testing.T)       { nameBuildability(t, "go") }
func TestNameBuildability_java(t *testing.T)     { nameBuildability(t, "java") }
func TestNameBuildability_js(t *testing.T)       { nameBuildability(t, "js") }
func TestNameBuildability_python(t *testing.T)   { nameBuildability(t, "python") }
func TestNameBuildability_ruby(t *testing.T)     { nameBuildability(t, "ruby") }
func TestNameBuildability_elixir(t *testing.T)   { nameBuildability(t, "elixir") }
func TestNameBuildability_lua(t *testing.T)      { nameBuildability(t, "lua") }
func TestNameBuildability_luau(t *testing.T)     { nameBuildability(t, "luau") }
func TestNameBuildability_moo(t *testing.T)      { nameBuildability(t, "moo") }
func TestNameBuildability_squirrel(t *testing.T) { nameBuildability(t, "squirrel") }
func TestNameBuildability_goose(t *testing.T)    { nameBuildability(t, "goose") }
func TestNameBuildability_lobster(t *testing.T)  { nameBuildability(t, "lobster") }

// shared is what every target's test needs once per run: the compiler, the
// toolchains and runtimes, and the work folder (removed by TestMain).
var shared struct {
	once   sync.Once
	err    error
	env    Env
	schema string
	work   string
	unity  sync.Mutex // Unity builds share test/unity's serialize project
}

// summary is the per-target tally of the jobs the run ran (TestMain).
var summary struct {
	mu       sync.Mutex
	pass     map[string]int
	fail     map[string]int
	failures []string
}

func TestMain(m *testing.M) {
	code := m.Run()
	// the summary goes to the file NAMEBUILD_SUMMARY names, which make
	// name-buildability prints whether the run passed or failed, and to
	// standard output when no file is named
	if text, ok := summaryText(); ok {
		if p := os.Getenv("NAMEBUILD_SUMMARY"); p != "" {
			if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				code = 1
			}
		} else {
			fmt.Print(text)
		}
	}
	if shared.work != "" {
		if err := os.RemoveAll(shared.work); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

// summaryText is the per-target summary of the jobs this run ran, and false
// when it ran none.
func summaryText() (string, bool) {
	summary.mu.Lock()
	defer summary.mu.Unlock()
	if summary.pass == nil {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("name-buildability per-target summary (jobs passed / failed):\n")
	var pass, fail int
	for _, tg := range Targets {
		p, f := summary.pass[tg], summary.fail[tg]
		if p+f == 0 {
			continue
		}
		pass += p
		fail += f
		fmt.Fprintf(&sb, "  %-9s %4d pass %4d fail\n", tg, p, f)
	}
	fmt.Fprintf(&sb, "  %-9s %4d pass %4d fail\n", "all", pass, fail)
	slices.Sort(summary.failures)
	for _, f := range summary.failures {
		sb.WriteString("  FAIL " + f + "\n")
	}
	return sb.String(), true
}

func record(j Job, err error) {
	summary.mu.Lock()
	defer summary.mu.Unlock()
	if summary.pass == nil {
		summary.pass = map[string]int{}
		summary.fail = map[string]int{}
	}
	if err == nil {
		summary.pass[j.Target]++
		return
	}
	summary.fail[j.Target]++
	summary.failures = append(summary.failures, fmt.Sprintf("%-28s %-9s %s", j.Case, j.Target, FirstError(err)))
}

// setup builds the compiler once and finds the toolchains and runtimes; a
// runtime that is absent fails every test that needs it, by name.
func setup(t *testing.T) {
	t.Helper()
	shared.once.Do(func() { shared.err = setupOnce(t) })
	if shared.err != nil {
		t.Fatal(shared.err)
	}
}

func setupOnce(t *testing.T) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	sibling := func(name, def string) (string, error) {
		if v := os.Getenv(name); v != "" {
			return filepath.Abs(v)
		}
		return filepath.Join(absRoot, "..", def), nil
	}
	env := Env{
		Root: absRoot,
		Tool: func(name, def string) string {
			if v := os.Getenv(name); v != "" {
				return v
			}
			return def
		},
	}
	for _, s := range []struct {
		dst       *string
		name, def string
	}{
		{&env.Serialize, "SERIALIZE", "serialize"},
		{&env.SerializeC, "SERIALIZE_C", "serialize.c"},
		{&env.SerializeCS, "SERIALIZE_CS", "serialize.cs"},
		{&env.SerializeGo, "SERIALIZE_GO", "serialize.go"},
		{&env.SerializeRS, "SERIALIZE_RS", "serialize.rs"},
	} {
		d, err := sibling(s.name, s.def)
		if err != nil {
			return err
		}
		if _, err := os.Stat(d); err != nil {
			return fmt.Errorf("serialize runtime %s: %w (clone the pinned sibling, or set %s)", d, err, s.name)
		}
		*s.dst = d
	}
	work, err := os.MkdirTemp("", "namebuild-")
	if err != nil {
		return err
	}
	shared.work = work
	schema := filepath.Join(work, "schema")
	build := testproc.Command(t, "go", "build", "-tags", "schema_leg_moo", "-o", schema, "./cmd/schema")
	build.Dir = absRoot
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("building the compiler: %w\n%s", err, out)
	}
	shared.env = env
	shared.schema = schema
	return nil
}

// nameBuildability is one target's column: every builds row, one subtest
// each, generated, built and loaded.
func nameBuildability(t *testing.T, target string) {
	t.Parallel()
	rows, err := ReadRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	setup(t)
	run := func(t *testing.T) Runner {
		return func(dir string, extra []string, name string, args ...string) (string, error) {
			cmd := testproc.Command(t, name, args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), extra...)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
	}
	for _, j := range Plan(rows) {
		if j.Target != target {
			continue
		}
		t.Run(j.Case, func(t *testing.T) {
			t.Parallel()
			err := runJob(t, run(t), shared.env, shared.schema, shared.work, j, &shared.unity)
			record(j, err)
			if err != nil {
				t.Error(err)
			}
		})
	}
}

// runJob generates one builds case in one target and builds and loads it; a
// refuse-at-check row has no job (Plan), since the checker refuses it.
func runJob(t *testing.T, run Runner, env Env, schema, work string, j Job, unity *sync.Mutex) error {
	c, err := ReadCase(filepath.Join(corpus, j.Case))
	if err != nil {
		return err
	}
	dir := filepath.Join(work, j.Case, j.Target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src, err := filepath.Abs(filepath.Join(corpus, j.Case))
	if err != nil {
		return err
	}
	out, genErr := run(work, nil, schema, "generate", "--lang", j.Target, "--out", dir, src)
	if genErr != nil {
		return fmt.Errorf("FAILED: generate --lang %s refused: %w\n%s", j.Target, genErr, out)
	}
	if j.Target == "unity" {
		unity.Lock()
		defer unity.Unlock()
	}
	err = Build(run, env, j.Target, c, dir)
	if err == nil {
		// a passing case leaves nothing behind: the Rust targets and the
		// .NET objects are the bulk of the run's disk
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return rmErr
		}
	}
	return err
}
