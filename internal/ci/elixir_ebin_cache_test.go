package ci

// make/elixir-ebin-cache.sh hands the verify-leg Elixir rows a compiled table
// corpus ebin from a store on the runner host when its inputs are unchanged
// (make/elixir.mk, ELIXIR_EBIN_CACHE). The compile it stands in for is a
// gate (the strict wrapper fails on a warning), so the store must never hand
// back an ebin for anything but the bytes, the toolchain and the compiler
// options that passed, and never an entry whose files are not the files it
// stored. This drives the script with a counting stand-in for the compile,
// inside the test's own directory: no BEAM, no network, no clock. It needs sh,
// flock and sha256sum (every fleet runner), and skips by name without them.

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// outIs holds the ebin the job was handed to exactly these files.
func (r *ebinCacheRig) outIs(want map[string]string) {
	r.t.Helper()
	entries, err := os.ReadDir(filepath.Join(r.dir, "out"))
	if err != nil {
		r.t.Fatal(err)
	}
	if len(entries) != len(want) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		r.t.Errorf("out holds %v, want exactly the %d files %v", names, len(want), want)
	}
	for name, content := range want {
		b, err := os.ReadFile(filepath.Join(r.dir, "out", name))
		if err != nil || string(b) != content {
			r.t.Errorf("out/%s = %q (%v), want %q", name, b, err, content)
		}
	}
}

// entry is the one stored entry's directory.
func (r *ebinCacheRig) entry() string {
	r.t.Helper()
	entries, err := filepath.Glob(filepath.Join(r.dir, "store", "*", ".manifest"))
	if err != nil || len(entries) != 1 {
		r.t.Fatalf("the store does not hold one entry with a manifest: %v %v", entries, err)
	}
	return filepath.Dir(entries[0])
}

func newEbinCacheRig(t *testing.T) *ebinCacheRig {
	t.Helper()
	for _, tool := range []string{"sh", "flock", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on this machine: the script compiles without the store here, as it says", tool)
		}
	}
	r := &ebinCacheRig{
		t:      t,
		script: filepath.Join(repoRoot(t), "make", "elixir-ebin-cache.sh"),
		dir:    t.TempDir(),
		env:    []string{"ELIXIR_EBIN_TOOLCHAIN=Elixir 1.20.4 erts-17.0", "ERL_COMPILER_OPTIONS=[no_ssa_opt]", "GITHUB_ACTIONS="},
	}
	r.write("a.ex", "alpha\n")
	r.write("b.ex", "beta\n")
	return r
}

var compiledEbin = map[string]string{"A.beam": "alpha\n", "B.beam": "beta\n"}

// The second run over the same bytes copies, and every part of the key is a
// miss when it alone changes: an input's bytes, the toolchain, the options.
func TestElixirEbinCacheHitsOnlyTheSameInputs(t *testing.T) {
	t.Parallel()
	r := newEbinCacheRig(t)
	if out, err := r.run("0"); err != nil || r.compiles() != 1 {
		t.Fatalf("the first run did not compile once (%d compiles): %v\n%s", r.compiles(), err, out)
	}
	r.outIs(compiledEbin)

	out, err := r.run("0")
	if err != nil || r.compiles() != 1 || !strings.Contains(out, "elixir-ebin-cache: hit ") {
		t.Fatalf("the same inputs compiled again or did not report a hit (%d compiles): %v\n%s", r.compiles(), err, out)
	}
	r.outIs(compiledEbin)

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
	r.outIs(map[string]string{"A.beam": "alpha\n", "B.beam": "beta'\n"})
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
	t.Parallel()
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

// damageCase is one row of testdata/elixir-ebin-cache/damage.tsv.
type damageCase struct {
	name, file, damage, expect string
}

func readDamageCorpus(t *testing.T) []damageCase {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "elixir-ebin-cache", "damage.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []damageCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("damage.tsv: %q has %d columns, want 4", line, len(cols))
		}
		rows = append(rows, damageCase{cols[0], cols[1], cols[2], cols[3]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// apply does one row's damage to a stored entry.
func (c damageCase) apply(t *testing.T, entry string) {
	t.Helper()
	path := filepath.Join(entry, c.file)
	switch {
	case c.damage == "none":
	case c.damage == "truncate":
		if err := os.Truncate(path, 0); err != nil {
			t.Fatal(err)
		}
	case c.damage == "remove":
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	case c.damage == "flip-last-byte":
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			t.Fatalf("%s: %v (%d bytes)", path, err, len(b))
		}
		b[len(b)-1] ^= 0x20
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	case strings.HasPrefix(c.damage, "write:"):
		content := strings.ReplaceAll(strings.TrimPrefix(c.damage, "write:"), `\n`, "\n")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("damage.tsv: case %s has an unknown damage %q", c.name, c.damage)
	}
}

// THE DAMAGE CORPUS (F-217): an entry whose files are not the files it stored
// is never handed back. Each committed row damages a stored entry one way
// (a truncated .beam, one byte changed at the same size, a .beam replaced, a
// file gone, a file added, the manifest gone, empty or forged); the next run
// says DISCARDED with the reason, compiles, hands the job the compile's own
// bytes, and stores a whole entry that the run after that hits. The valid
// control row, an undamaged entry, is a hit with no compile. At c7852e0047
// (a count of files and nothing else) the truncated, same-size and replaced
// rows were hits that handed back the damaged .beam.
func TestElixirEbinCacheDamageCorpus(t *testing.T) {
	t.Parallel()
	rows := readDamageCorpus(t)
	if len(rows) < 2 {
		t.Fatalf("damage.tsv holds %d rows: the corpus is not there", len(rows))
	}
	controls := 0
	for _, c := range rows {
		if c.expect == "hit" {
			controls++
		}
	}
	if controls == 0 || controls == len(rows) {
		t.Fatalf("damage.tsv holds %d hit controls of %d rows: it needs both a valid control and damage", controls, len(rows))
	}
	for _, c := range rows {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newEbinCacheRig(t)
			if out, err := r.run("0"); err != nil || r.compiles() != 1 {
				t.Fatalf("the first run did not compile once (%d compiles): %v\n%s", r.compiles(), err, out)
			}
			c.apply(t, r.entry())
			out, err := r.run("0")
			if err != nil {
				t.Fatalf("the run after the damage failed: %v\n%s", err, out)
			}
			if c.expect == "hit" {
				if r.compiles() != 1 || !strings.Contains(out, "elixir-ebin-cache: hit ") || strings.Contains(out, "DISCARDED") {
					t.Fatalf("the undamaged entry was not a hit (%d compiles):\n%s", r.compiles(), out)
				}
				r.outIs(compiledEbin)
				return
			}
			if r.compiles() != 2 {
				t.Fatalf("the damaged entry was handed back (%d compiles, want 2):\n%s", r.compiles(), out)
			}
			if !strings.Contains(out, "elixir-ebin-cache: DISCARDED entry ") || !strings.Contains(out, c.expect) {
				t.Fatalf("the discard was not said with its reason %q:\n%s", c.expect, out)
			}
			r.outIs(compiledEbin)
			// the recompile stored a whole entry again
			out, err = r.run("0")
			if err != nil || r.compiles() != 2 || !strings.Contains(out, "elixir-ebin-cache: hit ") {
				t.Fatalf("the entry stored after the discard is not a hit (%d compiles): %v\n%s", r.compiles(), err, out)
			}
			r.outIs(compiledEbin)
		})
	}
}

// Under GitHub Actions the discard is also a workflow warning, so a damaged
// store is seen on the run page and not only in the step's log.
func TestElixirEbinCacheDiscardIsAWorkflowWarning(t *testing.T) {
	t.Parallel()
	r := newEbinCacheRig(t)
	if out, err := r.run("0"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := os.Truncate(filepath.Join(r.entry(), "B.beam"), 0); err != nil {
		t.Fatal(err)
	}
	out, err := r.run("0", "GITHUB_ACTIONS=true")
	if err != nil || !strings.Contains(out, "::warning title=elixir-ebin-cache::DISCARDED entry ") {
		t.Fatalf("the discard under GitHub Actions is not a workflow warning: %v\n%s", err, out)
	}
}
