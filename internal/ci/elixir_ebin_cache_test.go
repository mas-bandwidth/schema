package ci

// make/elixir-ebin-cache.sh hands the per-commit Elixir jobs a compiled table
// corpus ebin from a store on the runner host when its inputs are unchanged
// (make/elixir.mk, ELIXIR_EBIN_CACHE). The compile it stands in for is a
// gate (the strict wrapper fails on a warning), so the store must never hand
// back an ebin for anything but the bytes, the toolchain and the compiler
// options that passed. This drives the script with a counting stand-in for
// the compile, inside the test's own directory: no BEAM, no network, no clock.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

type ebinCacheRig struct {
	t      *testing.T
	script string
	dir    string
	env    []string
}

// run calls the script over inputs a.ex and b.ex with a stand-in compile that
// appends a line to compiles.log, writes two files into out and exits status.
func (r *ebinCacheRig) run(status string, extraEnv ...string) (string, error) {
	r.t.Helper()
	if err := os.RemoveAll(filepath.Join(r.dir, "out")); err != nil {
		r.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.dir, "out"), 0o755); err != nil {
		r.t.Fatal(err)
	}
	cmd := testproc.Command(r.t, "sh", r.script, "store", "out", "a.ex", "b.ex", "--",
		"env", "MARK=1", "sh", "-c", `echo ran >> compiles.log; cat a.ex > out/A.beam; cat b.ex > out/B.beam; exit `+status)
	cmd.Dir = r.dir
	cmd.Env = append(append(os.Environ(), r.env...), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *ebinCacheRig) compiles() int {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, "compiles.log"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		r.t.Fatal(err)
	}
	return strings.Count(string(b), "ran\n")
}

func (r *ebinCacheRig) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *ebinCacheRig) outHas(want map[string]string) {
	r.t.Helper()
	for name, content := range want {
		b, err := os.ReadFile(filepath.Join(r.dir, "out", name))
		if err != nil || string(b) != content {
			r.t.Errorf("out/%s = %q (%v), want %q", name, b, err, content)
		}
	}
	if _, err := os.Stat(filepath.Join(r.dir, "out", ".complete")); err == nil {
		r.t.Error("the store's own marker was copied into the ebin")
	}
}

func newEbinCacheRig(t *testing.T) *ebinCacheRig {
	t.Helper()
	slowtest.Gate(t, "shell (the Elixir ebin store, with a stand-in compile)")
	for _, tool := range []string{"flock", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on this machine: the script compiles without the store here, as it says", tool)
		}
	}
	r := &ebinCacheRig{
		t:      t,
		script: filepath.Join(repoRoot(t), "make", "elixir-ebin-cache.sh"),
		dir:    t.TempDir(),
		env:    []string{"ELIXIR_EBIN_TOOLCHAIN=Elixir 1.20.4 erts-17.0", "ERL_COMPILER_OPTIONS=[no_ssa_opt]"},
	}
	r.write("a.ex", "alpha\n")
	r.write("b.ex", "beta\n")
	return r
}

// The second run over the same bytes copies, and every part of the key is a
// miss when it alone changes: an input's bytes, the toolchain, the options.
func TestElixirEbinCacheHitsOnlyTheSameInputs(t *testing.T) {
	r := newEbinCacheRig(t)
	if out, err := r.run("0"); err != nil || r.compiles() != 1 {
		t.Fatalf("the first run did not compile once (%d compiles): %v\n%s", r.compiles(), err, out)
	}
	r.outHas(map[string]string{"A.beam": "alpha\n", "B.beam": "beta\n"})

	out, err := r.run("0")
	if err != nil || r.compiles() != 1 || !strings.Contains(out, "elixir-ebin-cache: hit ") {
		t.Fatalf("the same inputs compiled again or did not report a hit (%d compiles): %v\n%s", r.compiles(), err, out)
	}
	r.outHas(map[string]string{"A.beam": "alpha\n", "B.beam": "beta\n"})

	want := 1
	step := func(what string, extraEnv ...string) {
		t.Helper()
		want++
		out, err := r.run("0", extraEnv...)
		if err != nil || r.compiles() != want || !strings.Contains(out, "elixir-ebin-cache: miss ") {
			t.Fatalf("%s: was handed a stored ebin (%d compiles, want %d): %v\n%s", what, r.compiles(), want, err, out)
		}
	}
	r.write("b.ex", "beta'\n")
	step("one input's bytes changed")
	r.outHas(map[string]string{"B.beam": "beta'\n"})
	step("the toolchain changed", "ELIXIR_EBIN_TOOLCHAIN=Elixir 1.20.5 erts-17.0")
	step("the compiler options changed", "ERL_COMPILER_OPTIONS=")

	// and the three earlier keys are all still there
	if out, err := r.run("0"); err != nil || r.compiles() != want || !strings.Contains(out, "elixir-ebin-cache: hit ") {
		t.Fatalf("the changed input's own entry was not kept (%d compiles, want %d): %v\n%s", r.compiles(), want, err, out)
	}
}

// THE REVERSED WITNESS: a compile that failed stores nothing, so the next run
// over the same bytes compiles again and fails again; the strict gate cannot
// be passed by running it twice.
func TestElixirEbinCacheNeverStoresAFailedCompile(t *testing.T) {
	r := newEbinCacheRig(t)
	for i := 1; i <= 2; i++ {
		out, err := r.run("1")
		if err == nil {
			t.Fatalf("run %d: a failing compile exited 0 through the store:\n%s", i, out)
		}
		if r.compiles() != i {
			t.Fatalf("run %d: %d compiles, want %d: the failed compile was stored and handed back\n%s", i, r.compiles(), i, out)
		}
	}
	if out, err := r.run("0"); err != nil || r.compiles() != 3 {
		t.Fatalf("the compile that passes after two failures did not run (%d compiles): %v\n%s", r.compiles(), err, out)
	}
}

// An entry that lost a file is not handed back: the run compiles.
func TestElixirEbinCacheRefusesADamagedEntry(t *testing.T) {
	r := newEbinCacheRig(t)
	if out, err := r.run("0"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	entries, err := filepath.Glob(filepath.Join(r.dir, "store", "*", "B.beam"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the store does not hold one entry: %v %v", entries, err)
	}
	if err := os.Remove(entries[0]); err != nil {
		t.Fatal(err)
	}
	out, err := r.run("0")
	if err != nil || r.compiles() != 2 {
		t.Fatalf("a damaged entry was handed back (%d compiles): %v\n%s", r.compiles(), err, out)
	}
	r.outHas(map[string]string{"A.beam": "alpha\n", "B.beam": "beta\n"})
}
