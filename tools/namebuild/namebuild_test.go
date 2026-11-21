package namebuild

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const root = "../.."

var corpus = filepath.Join(root, "testdata", "name-buildability")

// TestPlanCoversTheCorpus is the fast half: cases.tsv parses, every row has
// its directory, and the plan runs every target of every row the checker
// accepts, a refusing target only where the row names it.
func TestPlanCoversTheCorpus(t *testing.T) {
	t.Parallel()
	rows, err := ReadRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	accepted, refusing := 0, 0
	for _, r := range rows {
		if _, err := ReadCase(filepath.Join(corpus, r.Case)); err != nil {
			t.Fatalf("%s: %v", r.Case, err)
		}
		if r.Verdict != RefuseAtCheck {
			accepted++
		}
		if r.Verdict == RefuseAtGenerate {
			refusing += len(r.Names)
		}
	}
	jobs := Plan(rows)
	if len(jobs) != accepted*len(Targets) {
		t.Fatalf("plan has %d jobs, want %d rows x %d targets", len(jobs), accepted, len(Targets))
	}
	n := 0
	for _, j := range jobs {
		if j.Refuse {
			n++
		}
	}
	if n != refusing {
		t.Fatalf("plan expects %d refusals at generate, cases.tsv names %d", n, refusing)
	}
	if len(Targets) != 18 {
		t.Fatalf("%d targets, want 18", len(Targets))
	}
	if accepted == 0 || !slices.ContainsFunc(rows, func(r Row) bool { return r.Case == "ctl" && r.Verdict == Builds }) {
		t.Fatal("the corpus has no ctl control that builds")
	}
}

// TestNameBuildability is the nightly: every builds row generates and builds
// and loads in all 18 targets, and every refuse-at-generate row is refused by
// exactly the targets it names and builds in the rest. Its last log line is
// the per-target summary.
func TestNameBuildability(t *testing.T) {
	slowtest.Gate(t, "every target's toolchain (the name-buildability nightly)")
	rows, err := ReadRows(corpus)
	if err != nil {
		t.Fatal(err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	sibling := func(name, def string) string {
		if v := os.Getenv(name); v != "" {
			p, err := filepath.Abs(v)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		return filepath.Join(absRoot, "..", def)
	}
	env := Env{
		Root: absRoot,
		Tool: func(name, def string) string {
			if v := os.Getenv(name); v != "" {
				return v
			}
			return def
		},
		Serialize:   sibling("SERIALIZE", "serialize"),
		SerializeC:  sibling("SERIALIZE_C", "serialize.c"),
		SerializeCS: sibling("SERIALIZE_CS", "serialize.cs"),
		SerializeGo: slowtest.SerializeGo(t, root),
		SerializeRS: sibling("SERIALIZE_RS", "serialize.rs"),
	}
	for _, d := range []string{env.Serialize, env.SerializeC, env.SerializeCS, env.SerializeRS} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("serialize runtime %s: %v (clone the pinned sibling, or set its SERIALIZE_* variable)", d, err)
		}
	}

	work := t.TempDir()
	schema := filepath.Join(work, "schema")
	build := testproc.Command(t, "go", "build", "-tags", "schema_leg_moo", "-o", schema, "./cmd/schema")
	build.Dir = absRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the compiler: %v\n%s", err, out)
	}

	run := func(t *testing.T) Runner {
		return func(dir string, extra []string, name string, args ...string) (string, error) {
			cmd := testproc.Command(t, name, args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), extra...)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
	}

	type result struct{ pass, fail int }
	var mu sync.Mutex
	summary := map[string]*result{}
	for _, tg := range Targets {
		summary[tg] = &result{}
	}
	var failures []string
	record := func(j Job, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			summary[j.Target].pass++
			return
		}
		summary[j.Target].fail++
		failures = append(failures, fmt.Sprintf("%-28s %-9s %s", j.Case, j.Target, FirstError(err)))
	}
	// Unity builds share the dialect's serialize project under test/unity,
	// so they take turns; every other build owns its directory.
	var unity sync.Mutex

	t.Run("rows", func(t *testing.T) {
		for _, j := range Plan(rows) {
			t.Run(j.Case+"/"+j.Target, func(t *testing.T) {
				t.Parallel()
				err := runJob(t, run(t), env, schema, work, j, &unity)
				record(j, err)
				if err != nil {
					t.Error(err)
				}
			})
		}
	})

	var sb strings.Builder
	sb.WriteString("name-buildability per-target summary (jobs passed / failed):\n")
	total := result{}
	for _, tg := range Targets {
		r := summary[tg]
		total.pass += r.pass
		total.fail += r.fail
		fmt.Fprintf(&sb, "  %-9s %4d pass %4d fail\n", tg, r.pass, r.fail)
	}
	fmt.Fprintf(&sb, "  %-9s %4d pass %4d fail\n", "all", total.pass, total.fail)
	slices.Sort(failures)
	for _, f := range failures {
		sb.WriteString("  FAIL " + f + "\n")
	}
	t.Log(sb.String())
	// NAMEBUILD_SUMMARY names a file the summary is written to as well, so
	// a run that passes (and so prints no test log) still shows it
	if p := os.Getenv("NAMEBUILD_SUMMARY"); p != "" {
		if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
			t.Error(err)
		}
	}
}

// runJob generates one case in one target and builds and loads it, or, for
// a job that must refuse, requires the generation to refuse.
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
	if j.Refuse {
		if genErr == nil {
			return fmt.Errorf("FAILED: generate --lang %s accepted it; cases.tsv says it refuses", j.Target)
		}
		return nil
	}
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
		_ = os.RemoveAll(dir)
	}
	return err
}
