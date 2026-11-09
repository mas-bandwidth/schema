package elixirtable

// THE ELIXIR FIXED-FORM HARNESS UNDER THE STRICT READ (docs/SPEC-TABLES.md
// §3.4, §4; docs/FIXED-FORM-ALGORITHM.md §5): the strict read is one read —
// the whole value or a refusal — so FixedRuntime.report() carries only
// %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, layout_hash: 0}, and
// a value, length, enum ordinal or union tag past its declared bound refuses
// the whole load rather than clamping. test/elixir-fixedform/main.exs holds
// the generated fixed-form units to that contract over the C++ reference
// corpus, and its negative control below takes the range/ordinal refusal out
// of the compiled runtime and requires those strict assertions to go red by
// name.
//
// It compiles and runs the generated units with the real Elixir and OTP
// toolchain, so it is a functional test: it skips by name without SCHEMA_SLOW
// and fails by name under SCHEMA_REQUIRE_CORPUS=1 when the toolchain or
// corpus is missing.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

var fixedFormHarnessUnits = []struct {
	name string
	path string
}{
	{"fx1", "test/tables/FX1.schema"},
	{"fx2", "test/tables/FX2.schema"},
	{"fe1", "test/tables/FE1.schema"},
	{"fe2", "test/tables/FE2.schema"},
	{"fu1", "test/tables/FU1.schema"},
	{"fu2", "test/tables/FU2.schema"},
	{"p1", "test/tables/P1.schema"},
	{"p3", "test/tables/P3.schema"},
	{"examples", "tables/examples"},
}

func schemaFilesIn(t *testing.T, target string) []string {
	t.Helper()
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		return []string{target}
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
			paths = append(paths, filepath.Join(target, e.Name()))
		}
	}
	return paths
}

func elixircBinary(t *testing.T, elixirBin string) string {
	t.Helper()
	if env := os.Getenv("ELIXIRC"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		if bin, err := exec.LookPath(env); err == nil {
			return bin
		}
	}
	sibling := filepath.Join(filepath.Dir(elixirBin), "elixirc")
	if _, err := os.Stat(sibling); err == nil {
		return sibling
	}
	if found, err := exec.LookPath("elixirc"); err == nil {
		return found
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no elixirc is on this bench: not at %s and not on PATH", sibling)
	}
	t.Skip("no elixirc: unpack it in dist/ or put it on PATH — see make/elixir.mk")
	return ""
}

var (
	fixedFormHarnessOnce sync.Once
	fixedFormHarnessDir  string
	fixedFormHarnessErr  error
)

func buildFixedFormHarnessUnits(t *testing.T, root, elixircBin, otpPath string) string {
	t.Helper()
	fixedFormHarnessOnce.Do(func() {
		base, err := os.MkdirTemp("", "elixir-fixedform-strict-*")
		if err != nil {
			fixedFormHarnessErr = err
			return
		}
		ebin := filepath.Join(base, "ebin")
		if err := os.MkdirAll(ebin, 0o700); err != nil {
			fixedFormHarnessErr = err
			return
		}
		type unitJob struct {
			name   string
			srcDir string
			outDir string
			names  []string
		}
		var jobs []unitJob
		for _, u := range fixedFormHarnessUnits {
			paths := schemaFilesIn(t, filepath.Join(root, u.path))
			unit := loadUnit(t, paths...)
			files := generateElixir(t, unit)
			srcDir := filepath.Join(base, "gen", u.name)
			outDir := filepath.Join(base, "unit-ebin", u.name)
			if err := os.MkdirAll(srcDir, 0o700); err != nil {
				fixedFormHarnessErr = err
				return
			}
			if err := os.MkdirAll(outDir, 0o700); err != nil {
				fixedFormHarnessErr = err
				return
			}
			var names []string
			for name, data := range files {
				if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
					continue
				}
				if err := os.WriteFile(filepath.Join(srcDir, name), data, 0o600); err != nil {
					fixedFormHarnessErr = err
					return
				}
				names = append(names, name)
			}
			jobs = append(jobs, unitJob{
				name:   u.name,
				srcDir: srcDir,
				outDir: outDir,
				names:  sortedRuntimeFirst(names),
			})
		}
		errCh := make(chan error, len(jobs))
		var wg sync.WaitGroup
		for _, job := range jobs {
			wg.Add(1)
			go func(j unitJob) {
				defer wg.Done()
				args := append([]string{"--ignore-module-conflict", "-o", j.outDir}, j.names...)
				cmd := exec.Command(elixircBin, args...)
				cmd.Dir = j.srcDir
				cmd.Env = append(os.Environ(), "PATH="+otpPath+string(os.PathListSeparator)+os.Getenv("PATH"))
				if out, err := cmd.CombinedOutput(); err != nil {
					errCh <- fmt.Errorf("elixirc %s: %w\n%s", j.name, err, out)
				}
			}(job)
		}
		wg.Wait()
		close(errCh)
		if err := <-errCh; err != nil {
			fixedFormHarnessErr = err
			return
		}
		for _, job := range jobs {
			entries, err := os.ReadDir(job.outDir)
			if err != nil {
				fixedFormHarnessErr = err
				return
			}
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".beam") {
					continue
				}
				b, err := os.ReadFile(filepath.Join(job.outDir, e.Name()))
				if err != nil {
					fixedFormHarnessErr = err
					return
				}
				if err := os.WriteFile(filepath.Join(ebin, e.Name()), b, 0o600); err != nil {
					fixedFormHarnessErr = err
					return
				}
			}
		}
		fixedFormHarnessDir = base
	})
	if fixedFormHarnessErr != nil {
		t.Fatal(fixedFormHarnessErr)
	}
	return fixedFormHarnessDir
}

func runFixedFormHarnessScript(t *testing.T, root, elixirBin, otpPath, ebin, corpus string) (string, error) {
	t.Helper()
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	script := filepath.Join(root, "test", "elixir-fixedform", "main.exs")
	cmd := exec.CommandContext(ctx, elixirBin, "-pa", ebin, script, corpus)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+otpPath+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestElixirFixedFormHarnessStrict(t *testing.T) {
	slowtest.Gate(t, "the Elixir fixed-form harness (it compiles and runs test/elixir-fixedform/main.exs)")
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	elixircBin := elixircBinary(t, elixirBin)
	otpPath := otpBin(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	base := buildFixedFormHarnessUnits(t, root, elixircBin, otpPath)
	out, err := runFixedFormHarnessScript(t, root, elixirBin, otpPath, filepath.Join(base, "ebin"), corpus)
	if err != nil {
		t.Fatalf("test/elixir-fixedform/main.exs failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "fixed form: the Elixir leg matches the C++ reference") {
		t.Fatalf("test/elixir-fixedform/main.exs did not print its completion banner:\n%s", out)
	}
}

func TestElixirFixedFormHarnessStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Elixir fixed-form harness negative control")
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	elixircBin := elixircBinary(t, elixirBin)
	otpPath := otpBin(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	base := buildFixedFormHarnessUnits(t, root, elixircBin, otpPath)
	cleanEbin := filepath.Join(base, "ebin")
	scratchEbin := t.TempDir()
	entries, err := os.ReadDir(cleanEbin)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".beam") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(cleanEbin, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scratchEbin, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	const anchor = "def range_check(_report, _n), do: throw({:refuse, :out_of_range})"
	const replacement = "def range_check(report, _n), do: report"
	for _, unit := range []string{"fe1", "p1"} {
		runtimePath := filepath.Join(base, "gen", unit, "FixedRuntime.ex")
		src, err := os.ReadFile(runtimePath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(src, []byte(anchor)) {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s does not contain %q", runtimePath, anchor)
		}
		sabotaged := bytes.Replace(src, []byte(anchor), []byte(replacement), 1)
		sabDir := t.TempDir()
		sabFile := filepath.Join(sabDir, "FixedRuntime.ex")
		if err := os.WriteFile(sabFile, sabotaged, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(elixircBin, "--ignore-module-conflict", "-o", scratchEbin, sabFile)
		cmd.Env = append(os.Environ(), "PATH="+otpPath+string(os.PathListSeparator)+os.Getenv("PATH"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("elixirc sabotaged %s: %v\n%s", unit, err, out)
		}
	}

	out, err := runFixedFormHarnessScript(t, root, elixirBin, otpPath, scratchEbin, corpus)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: test/elixir-fixedform/main.exs stayed green with range_check refusal taken out:\n%s", out)
	}
	for _, want := range []string{
		"FAIL a tag past the last arm refuses on the strict read",
		"FAIL identity: an ordinal past the last variant refuses on the strict read",
		"FAIL a length past the declared bound refuses on the strict read",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("NEGATIVE CONTROL FAILED: sabotaged run did not report %q:\n%s", want, out)
		}
	}
}
