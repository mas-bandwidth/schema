package ci

// make/elixir-ebin-handoff.sh carries the Elixir table corpus ebin from the
// lang-clean (elixir K/N) rows, which compile it as shards, to the
// verify-leg-elixir rows, which run it whole (ci-fast.yml; make/elixir.mk
// ELIXIR_EBIN_FROM). The compile it stands in for is a gate (the strict
// wrapper fails on a warning), so a take must never build an ebin from a
// shard that is not byte for byte what was compiled, from this tree's
// sources, under these options and this toolchain, or from shards that are
// not the corpus. This drives the script over stand-in ebins inside the
// test's own directory: no BEAM, no network, no clock. It needs sh and
// sha256sum (every fleet runner), and skips by name without them.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const handoffToolchain = "Erlang/OTP 29 [erts-17.0] [source] [64-bit] [smp:16:16] [ds:16:16:10] [async-threads:1] [jit:ns]\n\nElixir 1.20.4 (compiled with Erlang/OTP 29)"

type handoffRig struct {
	t      *testing.T
	script string
	dir    string
}

func (r *handoffRig) path(rel string) string { return filepath.Join(r.dir, filepath.FromSlash(rel)) }

func (r *handoffRig) write(rel, content string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.path(rel)), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(r.path(rel), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// sh runs the script in the rig's tree, as make runs it in the checkout.
func (r *handoffRig) sh(env []string, args ...string) (string, error) {
	r.t.Helper()
	cmd := testproc.Command(r.t, "sh", append([]string{r.script}, args...)...)
	cmd.Dir = r.path("tree")
	cmd.Env = append(append(os.Environ(), "GITHUB_ACTIONS=", "ELIXIR_EBIN_TOOLCHAIN="+handoffToolchain), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// newHandoffRig writes a tree of two units and a driver, and gives two shards
// from stand-in ebins, as the lang-clean (elixir 1/2) and (elixir 2/2) rows
// would.
func newHandoffRig(t *testing.T) *handoffRig {
	t.Helper()
	for _, tool := range []string{"sh", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on this machine", tool)
		}
	}
	r := &handoffRig{t: t, script: filepath.Join(repoRoot(t), "make", "elixir-ebin-handoff.sh"), dir: t.TempDir()}
	r.write("tree/u1/a.ex", "alpha\n")
	r.write("tree/u2/b.ex", "beta\n")
	r.write("tree/driver.ex", "driver\n")
	r.write("e1/A.beam", "alpha-beam")
	r.write("e1/D.beam", "driver@checkout-1")
	r.write("e2/B.beam", "beta-beam")
	r.write("e2/D.beam", "driver@checkout-2")
	for _, s := range []struct{ ebin, dest, unit string }{{"../e1", "../from/s1", "u1"}, {"../e2", "../from/s2", "u2"}} {
		if out, err := r.sh(nil, "give", s.ebin, s.dest, "[no_ssa_opt]", s.unit, "--", s.unit+"/*.ex", "driver.ex"); err != nil {
			t.Fatalf("give %s: %v\n%s", s.unit, err, out)
		}
	}
	return r
}

// take builds ../ebin from ../from the way make/elixir.mk does.
func (r *handoffRig) take(options string, env ...string) (string, error) {
	r.t.Helper()
	return r.sh(env, "take", "../from", "../ebin", options, "u1", "u2", "--", "u1/*.ex", "u2/*.ex", "driver.ex")
}

// ebinIs holds the taken ebin to exactly these files.
func (r *handoffRig) ebinIs(want map[string]string) {
	r.t.Helper()
	entries, err := os.ReadDir(r.path("ebin"))
	if err != nil {
		r.t.Fatal(err)
	}
	if len(entries) != len(want) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		r.t.Errorf("ebin holds %v, want exactly the %d files %v", names, len(want), want)
	}
	for name, content := range want {
		b, err := os.ReadFile(filepath.Join(r.path("ebin"), name))
		if err != nil || string(b) != content {
			r.t.Errorf("ebin/%s = %q (%v), want %q", name, b, err, content)
		}
	}
}

var takenEbin = map[string]string{"A.beam": "alpha-beam", "B.beam": "beta-beam", "D.beam": "driver@checkout-1"}

// handoffCase is one row of testdata/elixir-ebin-handoff/damage.tsv.
type handoffCase struct {
	name, file, damage, expect string
}

func readHandoffCorpus(t *testing.T) []handoffCase {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "elixir-ebin-handoff", "damage.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []handoffCase
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
		rows = append(rows, handoffCase{cols[0], cols[1], cols[2], cols[3]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// apply does one row's damage and returns the options and environment the
// take runs with.
func (c handoffCase) apply(r *handoffRig) (string, []string) {
	r.t.Helper()
	options, env := "[no_ssa_opt]", []string(nil)
	unescape := func(s string) string { return strings.ReplaceAll(s, `\n`, "\n") }
	path := r.path(c.file)
	switch {
	case c.damage == "none":
	case c.damage == "truncate":
		if err := os.Truncate(path, 0); err != nil {
			r.t.Fatal(err)
		}
	case c.damage == "remove":
		if _, err := os.Stat(path); err != nil {
			r.t.Fatalf("%s: nothing to remove: %v", c.file, err)
		}
		if err := os.RemoveAll(path); err != nil {
			r.t.Fatal(err)
		}
	case c.damage == "flip-last-byte":
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			r.t.Fatalf("%s: %v (%d bytes)", path, err, len(b))
		}
		b[len(b)-1] ^= 0x20
		if err := os.WriteFile(path, b, 0o644); err != nil {
			r.t.Fatal(err)
		}
	case strings.HasPrefix(c.damage, "write:"):
		r.write(c.file, unescape(strings.TrimPrefix(c.damage, "write:")))
	case strings.HasPrefix(c.damage, "copy-to:"):
		if err := os.CopyFS(r.path(strings.TrimPrefix(c.damage, "copy-to:")), os.DirFS(path)); err != nil {
			r.t.Fatal(err)
		}
	case strings.HasPrefix(c.damage, "take-options:"):
		options = strings.TrimPrefix(c.damage, "take-options:")
	case strings.HasPrefix(c.damage, "take-toolchain:"):
		env = []string{"ELIXIR_EBIN_TOOLCHAIN=" + unescape(strings.TrimPrefix(c.damage, "take-toolchain:"))}
	default:
		r.t.Fatalf("damage.tsv: case %s has an unknown damage %q", c.name, c.damage)
	}
	return options, env
}

// THE DAMAGE CORPUS (F-217, F-218): a take builds the ebin only from shards
// that are byte for byte what their rows compiled, from this tree's sources,
// under these options and this toolchain, and that are the corpus with each
// unit once. Each committed row damages the handoff or the tree one way (a
// .beam truncated, changed at the same size, replaced, gone or added; the
// manifest gone, empty or forged; a source changed or added; a shard gone,
// all of them gone, or one given twice; other options, another toolchain);
// the take must refuse with the row's reason, leave no ebin and compile
// nothing. The two valid controls, the shards as given and the same
// toolchain on a host with another core count, must take the whole ebin.
func TestElixirEbinHandoffDamageCorpus(t *testing.T) {
	t.Parallel()
	rows := readHandoffCorpus(t)
	controls := 0
	for _, c := range rows {
		if c.expect == "ok" {
			controls++
		}
	}
	if controls == 0 || controls == len(rows) {
		t.Fatalf("damage.tsv holds %d ok controls of %d rows: it needs both a valid control and damage", controls, len(rows))
	}
	for _, c := range rows {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newHandoffRig(t)
			options, env := c.apply(r)
			out, err := r.take(options, env...)
			if c.expect == "ok" {
				if err != nil || !strings.Contains(out, "elixir-ebin-handoff: took 2 shards, 2 units, 3 files") {
					t.Fatalf("the valid shards were not taken: %v\n%s", err, out)
				}
				r.ebinIs(takenEbin)
				return
			}
			if err == nil {
				t.Fatalf("the damaged handoff was taken:\n%s", out)
			}
			if !strings.Contains(out, "elixir-ebin-handoff: take: ") || !strings.Contains(out, c.expect) {
				t.Fatalf("the refusal does not say %q:\n%s", c.expect, out)
			}
			for _, left := range []string{"ebin", "ebin.part"} {
				if _, err := os.Stat(r.path(left)); err == nil {
					t.Errorf("the refused take left %s behind", left)
				}
			}
		})
	}
}

// Under GitHub Actions a refusal is also a workflow error, so it is seen on
// the run page and not only in the step's log.
func TestElixirEbinHandoffRefusalIsAWorkflowError(t *testing.T) {
	t.Parallel()
	r := newHandoffRig(t)
	if err := os.Truncate(r.path("from/s2/ebin/B.beam"), 0); err != nil {
		t.Fatal(err)
	}
	out, err := r.take("[no_ssa_opt]", "GITHUB_ACTIONS=true")
	if err == nil || !strings.Contains(out, "::error title=elixir-ebin-handoff::take: ") {
		t.Fatalf("the refusal under GitHub Actions is not a workflow error: %v\n%s", err, out)
	}
}

// Stand-ins for elixirc under the strict wrapper and for elixir --version in
// the make-level test below. The compile writes one .beam per generated unit
// it is given and the driver's .beam, whose bytes carry the directory it ran
// in as a real one carries its checkout path, and logs that it ran.
const handoffFakeElixirc = `#!/bin/sh
out=
for a in "$@"; do
	case "$prev" in -o) out=$a ;; esac
	prev=$a
done
mkdir -p "$out"
for a in "$@"; do
	case "$a" in
	build/tables-generated-elixir/*) u=${a#build/tables-generated-elixir/}; u=${u%%/*}; cp "$a" "$out/Elixir.$u.beam" ;;
	*driver_impl.ex) echo "driver@$PWD" > "$out/Elixir.Driver.beam" ;;
	esac
done
echo "${ERL_COMPILER_OPTIONS:--}" >> compiles.log
`

const handoffFakeElixir = `#!/bin/sh
printf 'Erlang/OTP 29 [erts-17.0] [source] [64-bit] [smp:%s:%s]\n\nElixir 1.20.4 (compiled with Erlang/OTP 29)\n' "$$" "$$"
`

// handoffTree is a scratch checkout make/elixir.mk can compile, give and
// take in, with the generated corpus written and the stand-ins in place.
func handoffTree(t *testing.T, units []string) string {
	t.Helper()
	dir := elixirMakeDir(t)
	root := repoRoot(t)
	files := map[string]string{
		"build/tables-generated-elixir/.stamp":   "",
		"test/conformance/elixir/driver_impl.ex": "defmodule Driver do end\n",
		"fake-elixirc.sh":                        handoffFakeElixirc,
		"fake-elixir.sh":                         handoffFakeElixir,
	}
	for _, u := range units {
		files["build/tables-generated-elixir/"+u+"/Table.ex"] = "defmodule " + u + " do end\n"
	}
	for _, f := range []string{"make/elixir-ebin-handoff.sh", "make/elixir-strict.sh"} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		files[f] = string(b)
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestElixirEbinHandoffThroughMake runs the handoff the way ci-fast.yml does,
// through make/elixir.mk: every lang-clean (elixir K/N) row the plan writes
// compiles its shard in a checkout of its own and gives it
// (elixir-tables-ebin-handoff, ELIXIR_EBIN_ERL_COMPILER_OPTIONS), and a
// verify-leg-elixir row takes them all into build/elixir-tables-ebin
// (ELIXIR_EBIN_FROM, ERL_COMPILER_OPTIONS in its environment) and compiles
// nothing. The control: the same take asked for other options is refused,
// and still compiles nothing.
func TestElixirEbinHandoffThroughMake(t *testing.T) {
	t.Parallel()
	needTools(t, "bash", "make", "sh", "awk", "sha256sum")
	all, err := makeElixir(t, elixirMakeDir(t), nil, "elixir-table-shard-units")
	if err != nil {
		t.Fatalf("make elixir-table-shard-units: %v\n%s", err, all)
	}
	units := strings.Fields(all)
	specs := shardsOf(t, planCleanRows(t, "tables/stream/Stream.schema"), "elixir")
	shards := t.TempDir()
	common := func(dir string) []string {
		return []string{"-o", "build/tables-generated-elixir/.stamp",
			"ELIXIRC_STRICT=sh " + filepath.Join(dir, "fake-elixirc.sh"),
			"ELIXIR=sh " + filepath.Join(dir, "fake-elixir.sh")}
	}
	clean := []string{"ERL_COMPILER_OPTIONS=", "ELIXIR_EBIN_ERL_COMPILER_OPTIONS=", "ELIXIR_TABLE_SHARD=", "ELIXIR_EBIN_FROM=", "GITHUB_ACTIONS="}
	for i, spec := range specs {
		dir := handoffTree(t, units)
		args := append(common(dir), "elixir-tables-ebin-handoff", "ELIXIR_TABLE_SHARD="+spec, "ELIXIR_EBIN_ERL_COMPILER_OPTIONS=[no_ssa_opt]")
		out, err := makeElixir(t, dir, clean, args...)
		if err != nil || !strings.Contains(out, "elixir-ebin-handoff: gave ") {
			t.Fatalf("lang-clean (elixir %s): make elixir-tables-ebin-handoff: %v\n%s", spec, err, out)
		}
		if err := os.CopyFS(filepath.Join(shards, fmt.Sprintf("elixir-tables-ebin-%d", i)), os.DirFS(filepath.Join(dir, "build", "elixir-ebin-handoff"))); err != nil {
			t.Fatal(err)
		}
	}

	take := func(options string) (string, string, error) {
		dir := handoffTree(t, units)
		args := append(common(dir), "build/elixir-tables-ebin/.stamp", "ELIXIR_EBIN_FROM="+shards)
		out, err := makeElixir(t, dir, append(clean, "ERL_COMPILER_OPTIONS="+options), args...)
		if _, statErr := os.Stat(filepath.Join(dir, "compiles.log")); statErr == nil {
			t.Errorf("the take with options %q compiled", options)
		}
		return dir, out, err
	}
	dir, out, err := take("[no_ssa_opt]")
	want := fmt.Sprintf("elixir-ebin-handoff: took %d shards, %d units, %d files", len(specs), len(units), len(units)+1)
	if err != nil || !strings.Contains(out, want) {
		t.Fatalf("verify-leg-elixir: the take through make did not say %q: %v\n%s", want, err, out)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "build", "elixir-tables-ebin"))
	if err != nil {
		t.Fatal(err)
	}
	var beams []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".beam") {
			beams = append(beams, e.Name())
		}
	}
	if len(beams) != len(units)+1 {
		t.Errorf("the taken ebin holds %d .beam files, want one per unit and the driver's, %d", len(beams), len(units)+1)
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "elixir-tables-ebin", ".stamp")); err != nil {
		t.Errorf("the taken ebin is not stamped current: %v", err)
	}

	_, out, err = take("")
	if err == nil || !strings.Contains(out, "this job asks for ''") {
		t.Fatalf("the control: a take under other options was not refused: %v\n%s", err, out)
	}
}

// What a give hands over:the shard's ebin, its units, the SHA-256 of what it
// was compiled from, its options, its toolchain without the host's core
// count, and a manifest of every .beam.
func TestElixirEbinHandoffGiveWritesTheShard(t *testing.T) {
	t.Parallel()
	r := newHandoffRig(t)
	want := map[string]string{
		"from/s1/units.txt":     "u1\n",
		"from/s1/options.txt":   "[no_ssa_opt]\n",
		"from/s1/toolchain.txt": "Erlang/OTP 29 [erts-17.0]\nElixir 1.20.4 (compiled with Erlang/OTP 29)\n",
		"from/s1/ebin/A.beam":   "alpha-beam",
	}
	for rel, content := range want {
		b, err := os.ReadFile(r.path(rel))
		if err != nil || string(b) != content {
			t.Errorf("%s = %q (%v), want %q", rel, b, err, content)
		}
	}
	for rel, lines := range map[string][]string{
		"from/s1/inputs.sha256": {"  u1/a.ex", "  driver.ex"},
		"from/s1/.manifest":     {"  ./A.beam", "  ./D.beam"},
	} {
		b, err := os.ReadFile(r.path(rel))
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(got) != len(lines) {
			t.Errorf("%s holds %d lines, want %d:\n%s", rel, len(got), len(lines), b)
			continue
		}
		for i, suffix := range lines {
			if !strings.HasSuffix(got[i], suffix) || len(got[i]) != 64+len(suffix) {
				t.Errorf("%s line %d = %q, want a SHA-256 then %q", rel, i+1, got[i], suffix)
			}
		}
	}
}
