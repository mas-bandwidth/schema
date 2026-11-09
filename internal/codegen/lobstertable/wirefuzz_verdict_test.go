package lobstertable_test

// The wire-fuzz verdict pipe for Lobster (docs/SPEC-TABLES.md §4.2, §6.6;
// test/conformance/README.md, "The wire-fuzz driver"). The builder arm is one
// schema_rt.load: the whole value or a refusal, with no region preflight.
// The retain arm is the retaining load/save pair. Accepted reads must write
// back the same canonical bytes as internal/tablewire.
//
// TestLobsterWireFuzzVerdictPipe checks make/lobster-fuzz.mk's gate
// registration, then runs that pipe. The check is before slowtest.Gate, so a
// dropped target fails even when SCHEMA_SLOW is unset. A skip is not a pass:
// SCHEMA_SLOW=1 is what lets the pipe run.
//
// TestLobsterWireFuzzVerdictPipeNegativeControl plants its faults in the
// generated artifacts the VM compiles. A string the emitter already writes,
// with no planted fault, is not a control.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func lobsterWireFuzzVerdictMakefileCheck(mk string) error {
	for _, want := range []string{
		"tables-lobster-wire-fuzz-verdict:",
		"tables-lobster-wire-fuzz-verdict-negative-control:",
		"TestLobsterWireFuzzVerdictPipe$$",
		"TestLobsterWireFuzzVerdictPipeNegativeControl$$",
		"test-lobster: tables-lobster-wire-fuzz-verdict tables-lobster-wire-fuzz-verdict-negative-control",
	} {
		if !strings.Contains(mk, want) {
			return fmt.Errorf("make/lobster-fuzz.mk is missing %q", want)
		}
	}
	return nil
}

func lobsterRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

// resolveLobster picks a pinned v2026.8 binary the bench already has and
// publishes it as LOBSTER. The host's fallback is a relative dist/ path,
// which is absent until dist-lobster downloads; this test does not download.
func resolveLobster(t *testing.T, root string) string {
	t.Helper()
	tried := []string{
		os.Getenv("LOBSTER"),
		"/opt/lobster-v2026.8/bin/lobster",
		"/usr/local/bin/lobster",
		filepath.Join(root, "dist", "lobster-v2026.8", "bin", "lobster"),
	}
	seen := map[string]bool{}
	for _, p := range tried {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("LOBSTER", abs)
		return abs
	}
	t.Fatalf("lobster binary missing (looked at %s)", strings.Join(tried, ", "))
	return ""
}

var (
	lobsterWireFuzzBinsOnce sync.Once
	lobsterWireFuzzHarness  string
	lobsterWireFuzzHost     string
	lobsterWireFuzzBinsErr  error
)

func lobsterWireFuzzBins(t *testing.T, root string) (harness, host string) {
	t.Helper()
	lobsterWireFuzzBinsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "lobster-wirefuzz-bins-")
		if err != nil {
			lobsterWireFuzzBinsErr = err
			return
		}
		lobsterWireFuzzHarness = filepath.Join(dir, "conformance-harness")
		lobsterWireFuzzHost = filepath.Join(dir, "lobster-wirefuzzhost")
		for _, spec := range []struct{ out, pkg string }{
			{lobsterWireFuzzHarness, "./test/conformance/harness"},
			{lobsterWireFuzzHost, "./test/conformance/lobster/wirefuzzhost"},
		} {
			cmd := exec.Command("go", "build", "-o", spec.out, spec.pkg)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil {
				lobsterWireFuzzBinsErr = fmt.Errorf("go build %s: %w\n%s", spec.pkg, err, out)
				return
			}
		}
	})
	if lobsterWireFuzzBinsErr != nil {
		t.Fatal(lobsterWireFuzzBinsErr)
	}
	return lobsterWireFuzzHarness, lobsterWireFuzzHost
}

func lobsterManifestUnits(t *testing.T, root string) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "unit" {
			continue
		}
		units[f[1]] = append([]string(nil), f[2:]...)
	}
	return units
}

// generateLobsterWireUnits writes one directory per unit, the same shape the
// wirefuzz host requires (schema_tables.lobster beside the table modules).
// A unit the emitter refuses fails the test; it is not skipped.
func generateLobsterWireUnits(t *testing.T, root, dest string, keys []string, manifest map[string][]string) {
	t.Helper()
	for _, key := range keys {
		rels, ok := manifest[key]
		if !ok {
			t.Fatalf("manifest has no unit %s", key)
		}
		args := make([]string, len(rels))
		for i, rel := range rels {
			args[i] = filepath.Join(root, rel)
		}
		paths, err := compiler.GatherPaths(args)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("%s does not load: %v", key, err)
		}
		files, err := lobster.Generate(u)
		if err != nil {
			t.Fatalf("%s: lobster.Generate: %v", key, err)
		}
		tables, err := lobstertable.Generate(u)
		if err != nil {
			t.Fatalf("%s: lobstertable.Generate: %v", key, err)
		}
		maps.Copy(files, tables)
		dir := filepath.Join(dest, key)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "schema_tables.lobster")); err != nil {
			t.Fatalf("%s: schema_tables.lobster was not emitted", key)
		}
	}
}

// plantGenerated replaces the one occurrence of anchor in a generated file.
// The host compiles that file; the fault has to be in the artifact it runs.
func plantGenerated(t *testing.T, path, anchor, repl string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if n := strings.Count(src, anchor); n != 1 {
		t.Fatalf("%s: anchor %q count = %d, want 1", path, anchor, n)
	}
	next := strings.Replace(src, anchor, repl, 1)
	if next == src {
		t.Fatalf("%s: plant did not change the artifact", path)
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runLobsterWireFuzz starts the harness with its working directory at the
// repo root. startWireLeg does not set Dir or Env, so the host inherits both:
// the program path test/conformance/lobster/wirefuzz.lobster is relative to
// that directory, and LOBSTER is how the host finds the interpreter.
// The retain arm must not pass --builder to the host. The host's builder arm
// carries only form 1, retain 0, variable roots.
func runLobsterWireFuzz(t *testing.T, root, harness, host, units, mode string, extra []string) (string, error) {
	t.Helper()
	leaks := filepath.Join(root, "test", "conformance", "lobster", "wirefuzz-known-leaks.txt")
	args := []string{"wire-fuzz"}
	var driver string
	switch mode {
	case "builder":
		args = append(args, "--builder")
		driver = fmt.Sprintf("%s --builder --units %s --known-leaks %s", host, units, leaks)
	case "retain":
		args = append(args, "--retain")
		driver = fmt.Sprintf("%s --units %s --known-leaks %s", host, units, leaks)
	default:
		t.Fatalf("unknown wire-fuzz mode %q", mode)
	}
	args = append(args, "--driver", driver)
	args = append(args, extra...)
	cmd := exec.Command(harness, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestLobsterWireFuzzVerdictPipe(t *testing.T) {
	root := lobsterRepoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "make", "lobster-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := lobsterWireFuzzVerdictMakefileCheck(string(mk)); err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "the Lobster toolchain (it runs the wire-fuzz verdict pipe)")
	resolveLobster(t, root)
	harness, host := lobsterWireFuzzBins(t, root)
	dest := t.TempDir()
	generateLobsterWireUnits(t, root, dest, []string{"tblw1", "tblg1", "widelist"}, lobsterManifestUnits(t, root))
	for _, mode := range []string{"builder", "retain"} {
		t.Run(mode, func(t *testing.T) {
			failed := filepath.Join(t.TempDir(), "failed.bin")
			out, err := runLobsterWireFuzz(t, root, harness, host, dest, mode, []string{
				"--seed", "1",
				"--n", "20",
				"--failed", failed,
			})
			if err != nil {
				t.Fatalf("%s verdict pipe failed: %v\n%s", mode, err, out)
			}
			if !strings.Contains(out, "0 divergences") {
				t.Fatalf("%s: missing 0 divergences in output:\n%s", mode, out)
			}
		})
	}
}

func TestLobsterWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	root := lobsterRepoRoot(t)
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "lobster-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(mkBytes)
	if err := lobsterWireFuzzVerdictMakefileCheck(mk); err != nil {
		t.Fatalf("baseline make/lobster-fuzz.mk failed check: %v", err)
	}
	sabotaged := strings.ReplaceAll(mk, "tables-lobster-wire-fuzz-verdict", "tables-lobster-wire-fuzz-gone")
	if sabotaged == mk {
		t.Fatal("NEGATIVE CONTROL FAILED: make/lobster-fuzz.mk plant did not alter the text")
	}
	if err := lobsterWireFuzzVerdictMakefileCheck(sabotaged); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: dropping tables-lobster-wire-fuzz-verdict left the makefile check green")
	}

	slowtest.Gate(t, "the Lobster toolchain (it runs the wire-fuzz verdict pipe negative controls)")
	resolveLobster(t, root)
	harness, host := lobsterWireFuzzBins(t, root)
	manifest := lobsterManifestUnits(t, root)

	// The non-minimal LEB128 refusal lives in the generated load.lobster.
	// Removing it makes the leg accept a spelling the strict oracle refuses.
	t.Run("strict-acceptance", func(t *testing.T) {
		dest := t.TempDir()
		generateLobsterWireUnits(t, root, dest, []string{"tblw1"}, manifest)
		plantGenerated(t, filepath.Join(dest, "tblw1", "load.lobster"),
			"if i > 0 and x == 0:", "if false:")
		failed := filepath.Join(t.TempDir(), "failed.bin")
		out, err := runLobsterWireFuzz(t, root, harness, host, dest, "builder", []string{
			"--seed", "1",
			"--n", "1",
			"--failed", failed,
		})
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: non-minimal LEB128 plant left tblw1 verdict pipe green:\n%s", out)
		}
		if !strings.Contains(out, "read acceptance differs: the leg says true, the oracle says false") {
			t.Fatalf("NEGATIVE CONTROL FAILED: tblw1 went red for an unexpected reason:\n%s", out)
		}
	})

	// Commenting out register_retain_tail drops retained unknown fields from
	// the accepted write-back. The replay is a real vector that carries them.
	t.Run("retain-writeback", func(t *testing.T) {
		dest := t.TempDir()
		generateLobsterWireUnits(t, root, dest, []string{"tblrt1"}, manifest)
		plantGenerated(t, filepath.Join(dest, "tblrt1", "retain_unknown.lobster"),
			"register_retain_tail(RetainTailImpl { retain: retain, report: report })",
			"// negative control: omit retained fields")
		failed := filepath.Join(t.TempDir(), "failed.bin")
		out, err := runLobsterWireFuzz(t, root, harness, host, dest, "retain", []string{
			"--replay", "testdata/wire/tables/retain_rt2.bin",
			"--unit", "tblrt1",
			"--root", "Node",
			"--failed", failed,
		})
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: omitting retained fields left the replay green:\n%s", out)
		}
		if !strings.Contains(out, "the decoded value differs:") {
			t.Fatalf("NEGATIVE CONTROL FAILED: retain plant went red for an unexpected reason:\n%s", out)
		}
	})
}
