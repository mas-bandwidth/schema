package ci

// make/build-handoff.sh carries a build from the lang-clean (c|elixir K/N)
// rows, which compile it as shards, to the verify-leg-handoff rows, which run
// it whole (ci-fast.yml): the Elixir table corpus ebin (make/elixir.mk
// ELIXIR_EBIN_FROM) and the C conformance driver's objects (make/c.mk
// C_CONFORMANCE_FROM). The compile it stands in for is a gate, so a take must
// never build from a shard that is not byte for byte what was compiled, from
// this tree's sources, under these options and this toolchain, or from shards
// that are not the build. This drives the script over stand-in builds inside
// the test's own directory: no compiler, no network, no clock. It needs sh and
// sha256sum (every fleet runner), and skips by name without them.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	cmd.Env = append(append(os.Environ(), "GITHUB_ACTIONS=", "BUILD_HANDOFF_TOOLCHAIN="+handoffToolchain), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func needHandoffTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"sh", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on this machine", tool)
		}
	}
}

// newHandoffRig writes a tree of two units and a driver, and gives two shards
// from stand-in builds, as the lang-clean (elixir 1/2) and (elixir 2/2) rows
// would. Each build also holds a make stamp, which is never handed over.
func newHandoffRig(t *testing.T) *handoffRig {
	t.Helper()
	needHandoffTools(t)
	r := &handoffRig{t: t, script: filepath.Join(repoRoot(t), "make", "build-handoff.sh"), dir: t.TempDir()}
	r.write("tree/u1/a.ex", "alpha\n")
	r.write("tree/u2/b.ex", "beta\n")
	r.write("tree/driver.ex", "driver\n")
	r.write("e1/A.beam", "alpha-beam")
	r.write("e1/D.beam", "driver@checkout-1")
	r.write("e1/.stamp", "")
	r.write("e2/B.beam", "beta-beam")
	r.write("e2/D.beam", "driver@checkout-2")
	r.write("e2/.stamp", "")
	for _, s := range []struct{ built, dest, unit string }{{"../e1", "../from/s1", "u1"}, {"../e2", "../from/s2", "u2"}} {
		if out, err := r.sh(nil, "give", s.built, s.dest, "[no_ssa_opt]", s.unit, "--", s.unit+"/*.ex", "driver.ex"); err != nil {
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

// dirIs holds a taken build to exactly these files.
func dirIs(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		got = append(got, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Errorf("%s holds %v, want exactly the %d files %v", dir, got, len(want), want)
	}
	for name, content := range want {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || string(b) != content {
			t.Errorf("%s/%s = %q (%v), want %q", dir, name, b, err, content)
		}
	}
}

var takenEbin = map[string]string{"A.beam": "alpha-beam", "B.beam": "beta-beam", "D.beam": "driver@checkout-1"}

// handoffCase is one row of testdata/build-handoff/damage.tsv.
type handoffCase struct {
	name, file, damage, expect string
}

func readHandoffCorpus(t *testing.T) []handoffCase {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "build-handoff", "damage.tsv"))
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
		env = []string{"BUILD_HANDOFF_TOOLCHAIN=" + unescape(strings.TrimPrefix(c.damage, "take-toolchain:"))}
	default:
		r.t.Fatalf("damage.tsv: case %s has an unknown damage %q", c.name, c.damage)
	}
	return options, env
}

// THE DAMAGE CORPUS (F-217, F-218): a take builds only from shards that are
// byte for byte what their rows compiled, from this tree's sources, under
// these options and this toolchain, and that are the build with each unit
// once. Each committed row damages the handoff or the tree one way (a built
// file truncated, changed at the same size, replaced, gone or added; the
// manifest gone, empty or forged; a source changed or added; a shard gone,
// all of them gone, or one given twice; other options, another toolchain);
// the take must refuse with the row's reason, leave nothing built and compile
// nothing. The valid controls (the shards as given, the same toolchain on a
// host with another core count, a stamp added to a shard) must take the whole
// build and nothing else.
func TestBuildHandoffDamageCorpus(t *testing.T) {
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
				if err != nil || !strings.Contains(out, "build-handoff: took 2 shards, 2 units, 3 files") {
					t.Fatalf("the valid shards were not taken: %v\n%s", err, out)
				}
				dirIs(t, r.path("ebin"), takenEbin)
				return
			}
			if err == nil {
				t.Fatalf("the damaged handoff was taken:\n%s", out)
			}
			if !strings.Contains(out, "build-handoff: take: ") || !strings.Contains(out, c.expect) {
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
func TestBuildHandoffRefusalIsAWorkflowError(t *testing.T) {
	t.Parallel()
	r := newHandoffRig(t)
	if err := os.Truncate(r.path("from/s2/built/B.beam"), 0); err != nil {
		t.Fatal(err)
	}
	out, err := r.take("[no_ssa_opt]", "GITHUB_ACTIONS=true")
	if err == nil || !strings.Contains(out, "::error title=build-handoff::take: ") {
		t.Fatalf("the refusal under GitHub Actions is not a workflow error: %v\n%s", err, out)
	}
}

// What a give hands over: the shard's built files and not its stamp, its
// units, the SHA-256 of what it was compiled from, its options, its toolchain
// without the host's core count, and a manifest of every built file.
func TestBuildHandoffGiveWritesTheShard(t *testing.T) {
	t.Parallel()
	r := newHandoffRig(t)
	want := map[string]string{
		"from/s1/units.txt":     "u1\n",
		"from/s1/options.txt":   "[no_ssa_opt]\n",
		"from/s1/toolchain.txt": "Erlang/OTP 29 [erts-17.0]\nElixir 1.20.4 (compiled with Erlang/OTP 29)\n",
	}
	for rel, content := range want {
		b, err := os.ReadFile(r.path(rel))
		if err != nil || string(b) != content {
			t.Errorf("%s = %q (%v), want %q", rel, b, err, content)
		}
	}
	dirIs(t, r.path("from/s1/built"), map[string]string{"A.beam": "alpha-beam", "D.beam": "driver@checkout-1"})
	for rel, lines := range map[string][]string{
		"from/s1/inputs.sha256": {"  driver.ex", "  u1/a.ex"},
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

// A C toolchain banner (cc --version) is held whole, every non-empty line:
// another compiler or version is refused, the same one is taken. Objects sit
// in a tree of directories, which a take keeps.
func TestBuildHandoffCToolchainAndTree(t *testing.T) {
	t.Parallel()
	needHandoffTools(t)
	r := &handoffRig{t: t, script: filepath.Join(repoRoot(t), "make", "build-handoff.sh"), dir: t.TempDir()}
	gcc := "BUILD_HANDOFF_TOOLCHAIN=cc (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0\nCopyright (C) 2023 Free Software Foundation, Inc.\n\n"
	r.write("tree/a.c", "int a;\n")
	r.write("tree/b.c", "int b;\n")
	r.write("tree/h.h", "#define H 1\n")
	r.write("o1/test/a.c.o", "a-object")
	r.write("o2/test/b.c.o", "b-object")
	for _, s := range []struct{ built, dest, unit string }{{"../o1", "../from/s1", "a.c"}, {"../o2", "../from/s2", "b.c"}} {
		if out, err := r.sh([]string{gcc}, "give", s.built, s.dest, "-O2", s.unit, "--", s.unit, "h.h"); err != nil {
			t.Fatalf("give %s: %v\n%s", s.unit, err, out)
		}
	}
	take := func(env string) (string, error) {
		return r.sh([]string{env}, "take", "../from", "../objs", "-O2", "a.c", "b.c", "--", "*.c", "h.h")
	}
	out, err := take("BUILD_HANDOFF_TOOLCHAIN=clang version 18.1.3\nTarget: x86_64-pc-linux-gnu\n")
	if err == nil || !strings.Contains(out, "was compiled by 'cc (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0") {
		t.Fatalf("a take by another C compiler was not refused: %v\n%s", err, out)
	}
	out, err = take(gcc)
	if err != nil || !strings.Contains(out, "build-handoff: took 2 shards, 2 units, 2 files") {
		t.Fatalf("the C shards were not taken: %v\n%s", err, out)
	}
	dirIs(t, r.path("objs"), map[string]string{"test/a.c.o": "a-object", "test/b.c.o": "b-object"})
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
	for _, f := range []string{"make/build-handoff.sh", "make/elixir-strict.sh"} {
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
// verify-leg-handoff (elixir) row takes them all into build/elixir-tables-ebin
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
		if err != nil || !strings.Contains(out, "build-handoff: gave ") {
			t.Fatalf("lang-clean (elixir %s): make elixir-tables-ebin-handoff: %v\n%s", spec, err, out)
		}
		if err := os.CopyFS(filepath.Join(shards, fmt.Sprintf("handoff-elixir-%d", i)), os.DirFS(filepath.Join(dir, "build", "elixir-ebin-handoff"))); err != nil {
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
	want := fmt.Sprintf("build-handoff: took %d shards, %d units, %d files", len(specs), len(units), len(units)+1)
	if err != nil || !strings.Contains(out, want) {
		t.Fatalf("verify-leg-handoff (elixir): the take through make did not say %q: %v\n%s", want, err, out)
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

// A stand-in for cc in the C make-level test below. --version prints the
// banner in $FAKE_CC_BANNER; -E (make/c.mk's flag probe) fails; -c writes an
// object naming its source and logs the compile; anything else is the link,
// which writes the objects it was given, in order, to -o.
const handoffFakeCC = `#!/bin/sh
case "$1" in --version) printf '%s\n\nCopyright (C) the test\n' "${FAKE_CC_BANNER:-fakecc (test) 1.0}"; exit 0 ;; esac
out= src= compile=no prev=
for a in "$@"; do
	case "$prev" in -o) out=$a ;; esac
	case "$a" in -c) compile=yes ;; -E) exit 1 ;; *.c) src=$a ;; esac
	prev=$a
done
if [ "$compile" = yes ]; then
	printf 'object of %s\n' "$src" > "$out"
	echo "$src" >> compiles.log
	exit 0
fi
for a in "$@"; do
	case "$a" in *.o) cat "$a" ;; esac
done > "$out"
`

// makeC runs make/c.mk in dir with the stand-in compiler, the generated
// corpus taken as current, and env last in the environment.
func makeC(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := testproc.Command(t, "make", append([]string{"--no-print-directory", "-s", "-j4", "-f", filepath.Join(repoRoot(t), "make", "c.mk"),
		"-o", "build/tables-generated-c/.stamp", "CC=" + filepath.Join(dir, "fake-cc.sh")}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "C_CONFORMANCE_SHARD=", "C_CONFORMANCE_FROM=", "C_FORMAT_SHARD=", "GITHUB_ACTIONS=", "MAKEFLAGS=", "MAKELEVEL=", "FAKE_CC_BANNER="), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestCConformanceObjectsHandoffThroughMake runs the C handoff the way
// ci-fast.yml does, through make/c.mk: every lang-clean (c K/N) row the plan
// writes compiles its shard of the conformance driver's objects in a checkout
// of its own and gives it (c-conformance-objs-handoff, C_CONFORMANCE_SHARD),
// and a verify-leg-handoff (c) row takes them all (C_CONFORMANCE_FROM) and
// links build/conformance-c and build/wire-fuzz-c from them with no compile.
// The controls: a take by another compiler is refused and compiles nothing;
// the shards together compile every source exactly once; asking for a shard
// and a take at once, or a shard past N, is a make error.
func TestCConformanceObjectsHandoffThroughMake(t *testing.T) {
	t.Parallel()
	needTools(t, "bash", "make", "sh", "awk", "sha256sum", "tar")
	parent := t.TempDir()
	write := func(p, content string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	// the runtime sibling, at make/c.mk's default ../serialize.c
	write(filepath.Join(parent, "serialize.c", "serialize.h"), "/* runtime */\n", 0o644)
	root := repoRoot(t)
	var sources []string
	tree := func(name string) string {
		dir := filepath.Join(parent, name)
		for _, f := range []string{"make/shard.sh", "make/build-handoff.sh"} {
			b, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(dir, f), string(b), 0o755)
		}
		write(filepath.Join(dir, "fake-cc.sh"), handoffFakeCC, 0o755)
		write(filepath.Join(dir, "build", "tables-generated-c", ".stamp"), "", 0o644)
		write(filepath.Join(dir, "build", "tables-generated-c", "w1", "W1Table.h"), "/* generated header */\n", 0o644)
		write(filepath.Join(dir, "test", "conformance", "c", "driver.h"), "/* driver header */\n", 0o644)
		for _, s := range sources {
			write(filepath.Join(dir, s), "/* "+s+" */\n", 0o644)
		}
		return dir
	}
	all, err := makeC(t, tree("list"), nil, "c-conformance-shard-sources")
	if err != nil {
		t.Fatalf("make c-conformance-shard-sources: %v\n%s", err, all)
	}
	sources = strings.Fields(all)
	if len(sources) < 20 || !slices.Contains(sources, "test/conformance/c/main.c") || !slices.Contains(sources, "test/c-tables/wire_fuzz_main.c") {
		t.Fatalf("make/c.mk hands over %d sources, want the driver's and wire_fuzz_main.c: %v", len(sources), sources)
	}

	specs := shardsOf(t, planCleanRows(t, "make/c.mk"), "c")
	if len(specs) < 2 {
		t.Fatalf("the plan writes %d lang-clean (c) shards: %v", len(specs), specs)
	}
	shards := filepath.Join(parent, "shards")
	compiled := map[string]int{}
	for i, spec := range specs {
		dir := tree(fmt.Sprintf("clean-%d", i))
		// another gate on the same row writes beside the corpus (the zero-cost
		// gate's cards/): no object reads it, so it is not an input, and the
		// verify row, which never has it, still takes the shards
		write(filepath.Join(dir, "build", "tables-generated-c", "cards", "Card.h"), fmt.Sprintf("/* card %d */\n", i), 0o644)
		out, err := makeC(t, dir, nil, "c-conformance-objs-handoff", "C_CONFORMANCE_SHARD="+spec)
		if err != nil || !strings.Contains(out, "build-handoff: gave ") {
			t.Fatalf("lang-clean (c %s): make c-conformance-objs-handoff: %v\n%s", spec, err, out)
		}
		log, err := os.ReadFile(filepath.Join(dir, "compiles.log"))
		if err != nil {
			t.Fatalf("lang-clean (c %s) compiled nothing: %v", spec, err)
		}
		for s := range strings.FieldsSeq(string(log)) {
			compiled[s]++
		}
		if err := os.CopyFS(filepath.Join(shards, fmt.Sprintf("handoff-c-%d", i)), os.DirFS(filepath.Join(dir, "build", "c-conformance-handoff"))); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range sources {
		if compiled[s] != 1 {
			t.Errorf("the shards compiled %s %d times, want once", s, compiled[s])
		}
	}
	if len(compiled) != len(sources) {
		t.Errorf("the shards compiled %d sources, the driver has %d", len(compiled), len(sources))
	}

	take := func(name, banner string) (string, string, error) {
		dir := tree(name)
		var env []string
		if banner != "" {
			env = []string{"FAKE_CC_BANNER=" + banner}
		}
		out, err := makeC(t, dir, env, "build/conformance-c", "build/wire-fuzz-c", "C_CONFORMANCE_FROM="+shards)
		if _, statErr := os.Stat(filepath.Join(dir, "compiles.log")); statErr == nil {
			t.Errorf("the take %s compiled", name)
		}
		return dir, out, err
	}
	dir, out, err := take("verify", "")
	want := fmt.Sprintf("build-handoff: took %d shards, %d units, %d files", len(specs), len(sources), len(sources))
	if err != nil || !strings.Contains(out, want) {
		t.Fatalf("verify-leg-handoff (c): the take through make did not say %q: %v\n%s", want, err, out)
	}
	for bin, has := range map[string][2]string{
		"conformance-c": {"object of test/conformance/c/main.c", "object of test/c-tables/wire_fuzz_main.c"},
		"wire-fuzz-c":   {"object of test/c-tables/wire_fuzz_main.c", "object of test/conformance/c/main.c"},
	} {
		b, err := os.ReadFile(filepath.Join(dir, "build", bin))
		if err != nil {
			t.Fatalf("build/%s was not linked: %v", bin, err)
		}
		if !strings.Contains(string(b), has[0]) || strings.Contains(string(b), has[1]) {
			t.Errorf("build/%s is not linked from the taken objects (want %q, not %q):\n%s", bin, has[0], has[1], b)
		}
	}

	_, out, err = take("other-compiler", "othercc (test) 2.0")
	if err == nil || !strings.Contains(out, "was compiled by 'fakecc (test) 1.0") {
		t.Fatalf("the control: a take by another compiler was not refused: %v\n%s", err, out)
	}

	for _, bad := range [][]string{
		{"C_CONFORMANCE_SHARD=1/" + fmt.Sprint(len(specs)), "C_CONFORMANCE_FROM=" + shards},
		{"C_CONFORMANCE_SHARD=" + fmt.Sprintf("%d/%d", len(specs)+1, len(specs))},
	} {
		if out, err := makeC(t, tree("bad"), nil, append([]string{"c-conformance-shard-sources"}, bad...)...); err == nil {
			t.Errorf("make c-conformance-shard-sources %v was not a make error:\n%s", bad, out)
		}
	}
}
