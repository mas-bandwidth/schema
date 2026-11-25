//go:build namebuild_targets

// The per-target tests build every accepted name with every target's
// toolchain: minutes of every toolchain, so they are the registry's nightly
// rows (codegen.name-buildability.<case>), run through make
// name-buildability-run with -tags namebuild_targets. A plain go test of this
// package never compiles them, so no fast lane runs them; a registry run
// without the tag finds no such test and fails every instance by name.

package namebuild

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

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
