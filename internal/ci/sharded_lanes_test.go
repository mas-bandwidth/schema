package ci

// THE SHARDED LANG-CLEAN ROWS and what keeps them honest (#4530: F-218,
// F-221, F-225, and the nightly that covers the per-commit compiler trade).
//
// ci-fast.yml runs two gates as shards side by side, a lang-clean row per
// shard K/N: the C format gate (make/checks/c-clean-format.sh C_FORMAT_SHARD)
// and the strict compile of the Elixir table corpus with the agreement gate
// over it (make/elixir.mk ELIXIR_TABLE_SHARD). Both deal their parts with
// make/shard.sh. A row stands for its share of the unsharded gate only if the
// shards are the whole gate, each part once; these tests hold that, the rows
// the plan writes, and that no row reads a verdict or an ebin from another
// run. Everything here is sh, make and awk over files in the test's own
// directory, with stand-ins for clang-format and elixirc: no toolchain, no
// network, no clock.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

func needTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on this machine", tool)
		}
	}
}

// shard runs make/shard.sh spec over the lines and returns what it printed.
func runShard(t *testing.T, spec string, lines []string) ([]string, error) {
	t.Helper()
	cmd := testproc.Command(t, "sh", filepath.Join(repoRoot(t), "make", "shard.sh"), spec)
	cmd.Dir = t.TempDir()
	if len(lines) > 0 {
		cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	} else {
		cmd.Stdin = strings.NewReader("")
	}
	out, err := cmd.Output()
	got := strings.Fields(string(out))
	return got, err
}

// TestShardSplitIsAPartition: for every N, shards 1/N .. N/N of a list are
// disjoint, keep the list's order, and together are the list, each line once.
func TestShardSplitIsAPartition(t *testing.T) {
	t.Parallel()
	needTools(t, "sh", "awk")
	for _, size := range []int{0, 1, 5, 42} {
		var list []string
		for i := range size {
			list = append(list, fmt.Sprintf("line%02d", i))
		}
		for n := 1; n <= 6; n++ {
			seen := map[string]int{}
			for k := 1; k <= n; k++ {
				got, err := runShard(t, fmt.Sprintf("%d/%d", k, n), list)
				if err != nil {
					t.Fatalf("size %d, shard %d/%d: %v", size, k, n, err)
				}
				if !slices.IsSorted(got) {
					t.Errorf("size %d, shard %d/%d is not in the list's order: %v", size, k, n, got)
				}
				for _, l := range got {
					seen[l]++
				}
			}
			for _, l := range list {
				if seen[l] != 1 {
					t.Errorf("size %d, N=%d: %s is in %d shards, want exactly 1", size, n, l, seen[l])
				}
			}
			if len(seen) != len(list) {
				t.Errorf("size %d, N=%d: the shards printed %d distinct lines, the list has %d", size, n, len(seen), len(list))
			}
		}
	}
}

// TestShardSpecCorpus runs the committed spec rows (testdata/shards/specs.tsv):
// the valid controls print their lines, and every spec that is not a shard is
// refused with exit 2 and no output, so a row asked for a shard that does not
// exist cannot pass on no work.
func TestShardSpecCorpus(t *testing.T) {
	t.Parallel()
	needTools(t, "sh", "awk")
	f, err := os.Open(filepath.Join("testdata", "shards", "specs.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	list := []string{"a", "b", "c", "d", "e"}
	var valid, refused int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		spec, expect, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("specs.tsv: %q has no tab", line)
		}
		got, err := runShard(t, spec, list)
		switch expect {
		case "refused":
			refused++
			exit, isExit := errors.AsType[*exec.ExitError](err)
			if !isExit || exit.ExitCode() != 2 || len(got) != 0 {
				t.Errorf("spec %q: printed %v with %v, want exit 2 and nothing", spec, got, err)
			}
		default:
			valid++
			want := []string{}
			if expect != "(none)" {
				want = strings.Split(expect, ",")
			}
			if err != nil || !slices.Equal(got, want) {
				t.Errorf("spec %q: printed %v (%v), want %v", spec, got, err, want)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if valid == 0 || refused == 0 {
		t.Fatalf("specs.tsv holds %d valid and %d refused rows: it needs both", valid, refused)
	}
}

// planCleanRows runs the plan's selector over a diff and returns the
// lang-clean rows it writes.
func planCleanRows(t *testing.T, files string) []struct{ Lang, Shard string } {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          echo \"broad=$broad\"")
	if !ok {
		t.Fatal("fast workflow output boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s' "$clean_rows_json"`)
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
	cmd.Env = append(scriptEnv(os.Environ()), "files="+files)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plan script: %v: %s", err, out)
	}
	var rows []struct{ Lang, Shard string }
	if err := json.Unmarshal([]byte(selectorOutput(out)), &rows); err != nil {
		t.Fatalf("clean_rows %q is not JSON rows: %v", selectorOutput(out), err)
	}
	return rows
}

// shardsOf returns the shard specs K/N the plan writes for one language, and
// checks they are 1/N .. N/N of one N. A row named otherwise (elixir
// "offsets") is a gate of its own, not a shard.
func shardsOf(t *testing.T, rows []struct{ Lang, Shard string }, lang string) []string {
	t.Helper()
	var specs []string
	for _, r := range rows {
		if r.Lang == lang && strings.Contains(r.Shard, "/") {
			specs = append(specs, r.Shard)
		}
	}
	if len(specs) < 2 {
		t.Fatalf("the plan writes %d %s rows %v; the gate is sharded", len(specs), lang, specs)
	}
	for i, s := range specs {
		if want := fmt.Sprintf("%d/%d", i+1, len(specs)); s != want {
			t.Fatalf("%s row %d is shard %q, want %q: the rows are not 1/N .. N/N", lang, i+1, s, want)
		}
	}
	return specs
}

// TestLangCleanPlanWritesTheShardRows: the plan writes the C and Elixir gates
// as shards 1/N .. N/N, the other languages as one row each, and a diff to
// the splitter itself selects both sharded gates.
func TestLangCleanPlanWritesTheShardRows(t *testing.T) {
	t.Parallel()
	needTools(t, "bash", "awk")
	rows := planCleanRows(t, "tables/stream/Stream.schema")
	shardsOf(t, rows, "c")
	shardsOf(t, rows, "elixir")
	// beside the Elixir shards, one offsets row and nothing else; C has none
	others := map[string][]string{}
	for _, r := range rows {
		if (r.Lang == "c" || r.Lang == "elixir") && !strings.Contains(r.Shard, "/") {
			others[r.Lang] = append(others[r.Lang], r.Shard)
		}
	}
	if !slices.Equal(others["elixir"], []string{"offsets"}) || len(others["c"]) != 0 {
		t.Errorf("rows beside the shards: %v, want only elixir [offsets]", others)
	}
	for _, lang := range []string{"js", "rust"} {
		n := 0
		for _, r := range rows {
			if r.Lang == lang {
				n++
				if r.Shard != "" {
					t.Errorf("%s row has shard %q; it is one row", lang, r.Shard)
				}
			}
		}
		if n != 1 {
			t.Errorf("the plan writes %d %s rows, want 1", n, lang)
		}
	}
	split := planCleanRows(t, "make/shard.sh")
	shardsOf(t, split, "c")
	shardsOf(t, split, "elixir")
	if got := planCleanRows(t, "README.md"); len(got) != 0 {
		t.Errorf("a docs-only diff writes lang-clean rows %v", got)
	}
}

// makeElixir runs make on make/elixir.mk alone in dir.
func makeElixir(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := testproc.Command(t, "make", append([]string{"--no-print-directory", "-s", "-f", filepath.Join(repoRoot(t), "make", "elixir.mk")}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// elixirMakeDir is a scratch tree make/elixir.mk can run in: the splitter at
// the path the makefile calls it by.
func elixirMakeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "shard.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "make"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "make", "shard.sh"), src, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestElixirTableShardsAreTheCorpus: the Elixir rows the plan writes, each a
// shard of make/elixir.mk's table units, are together the whole corpus, each
// unit once and each row a non-empty share; a shard past N is a make error,
// not an empty compile.
func TestElixirTableShardsAreTheCorpus(t *testing.T) {
	t.Parallel()
	needTools(t, "bash", "make", "sh", "awk")
	dir := elixirMakeDir(t)
	all, err := makeElixir(t, dir, nil, "elixir-table-shard-units")
	if err != nil {
		t.Fatalf("make elixir-table-shard-units: %v\n%s", err, all)
	}
	units := strings.Fields(all)
	if len(units) < 10 {
		t.Fatalf("make/elixir.mk lists %d table units: %v", len(units), units)
	}
	seen := map[string]int{}
	specs := shardsOf(t, planCleanRows(t, "internal/codegen/elixirtable/block.go"), "elixir")
	for _, spec := range specs {
		out, err := makeElixir(t, dir, nil, "elixir-table-shard-units", "ELIXIR_TABLE_SHARD="+spec)
		if err != nil {
			t.Fatalf("shard %s: %v\n%s", spec, err, out)
		}
		got := strings.Fields(out)
		if len(got) == 0 {
			t.Errorf("shard %s compiles no unit", spec)
		}
		for _, u := range got {
			seen[u]++
		}
	}
	for _, u := range units {
		if seen[u] != 1 {
			t.Errorf("unit %s is in %d of the plan's %d Elixir shards, want exactly 1", u, seen[u], len(specs))
		}
	}
	if len(seen) != len(units) {
		t.Errorf("the shards name %d units, the corpus has %d", len(seen), len(units))
	}
	past := fmt.Sprintf("%d/%d", len(specs)+1, len(specs))
	if out, err := makeElixir(t, dir, nil, "elixir-table-shard-units", "ELIXIR_TABLE_SHARD="+past); err == nil || !strings.Contains(out, "is not a shard") {
		t.Errorf("ELIXIR_TABLE_SHARD=%s was not a make error: %v\n%s", past, err, out)
	}
	tooMany := fmt.Sprintf("%d/%d", len(units)+1, len(units)+1)
	if out, err := makeElixir(t, dir, nil, "elixir-table-shard-units", "ELIXIR_TABLE_SHARD="+tooMany); err == nil || !strings.Contains(out, "deals no unit") {
		t.Errorf("ELIXIR_TABLE_SHARD=%s, an empty shard, was not a make error: %v\n%s", tooMany, err, out)
	}
}

// The stand-in for elixirc under the strict wrapper: it logs one line per
// compile, "<ERL_COMPILER_OPTIONS or -> <table units in its arguments>", and
// writes one .beam into the -o directory.
const fakeElixircStrict = `#!/bin/sh
out=
units=0
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out=$2; shift ;;
	build/tables-generated-elixir/*) units=$((units + 1)) ;;
	esac
	shift
done
echo "${ERL_COMPILER_OPTIONS:--} $units" >> compiles.log
mkdir -p "$out" && echo beam > "$out/Elixir.Probe.beam"
`

// TestElixirEbinRebuildsWhenItsBuildChanges runs the committed steps of
// testdata/elixir-ebin-config/steps.tsv in one working tree (F-225): the ebin
// is rebuilt whenever the options or the shard it is asked for differ from the
// ones it was built under, and is taken as current only when they are the
// same. The stand-in compile records what it was run with.
func TestElixirEbinRebuildsWhenItsBuildChanges(t *testing.T) {
	t.Parallel()
	needTools(t, "make", "sh", "awk")
	dir := elixirMakeDir(t)
	all, err := makeElixir(t, dir, nil, "elixir-table-shard-units")
	if err != nil {
		t.Fatalf("make elixir-table-shard-units: %v\n%s", err, all)
	}
	for _, u := range strings.Fields(all) {
		p := filepath.Join(dir, "build", "tables-generated-elixir", u)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "Table.ex"), []byte("defmodule X do end\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"build/tables-generated-elixir/.stamp":   "",
		"test/conformance/elixir/driver_impl.ex": "defmodule Driver do end\n",
		"elixirc-strict.sh":                      fakeElixircStrict,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	f, err := os.Open(filepath.Join("testdata", "elixir-ebin-config", "steps.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	compiles := 0
	var current, compiled int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("steps.tsv: %q has %d columns, want 4", line, len(cols))
		}
		step, vars, envOpts, expect := cols[0], cols[1], cols[2], cols[3]
		args := []string{"-o", "build/tables-generated-elixir/.stamp", "build/elixir-tables-ebin/.stamp", "ELIXIRC_STRICT=sh " + filepath.Join(dir, "elixirc-strict.sh")}
		if vars != "-" {
			args = append(args, strings.Fields(vars)...)
		}
		env := []string{"ERL_COMPILER_OPTIONS=", "ELIXIR_EBIN_ERL_COMPILER_OPTIONS=", "ELIXIR_TABLE_SHARD=", "ELIXIR_EBIN_FROM="}
		if envOpts != "-" {
			env = append(env, "ERL_COMPILER_OPTIONS="+envOpts)
		}
		out, err := makeElixir(t, dir, env, args...)
		if err != nil {
			t.Fatalf("step %s: make: %v\n%s", step, err, out)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "compiles.log"))
		logLines := strings.Split(strings.TrimSpace(string(log)), "\n")
		if strings.TrimSpace(string(log)) == "" {
			logLines = nil
		}
		if expect == "current" {
			current++
			if len(logLines) != compiles {
				t.Fatalf("step %s: the ebin was rebuilt (%q); it was built for these options and this shard\n%s", step, logLines[len(logLines)-1], out)
			}
			continue
		}
		compiled++
		fields := strings.Fields(expect)
		if len(fields) != 3 || fields[0] != "compile" {
			t.Fatalf("steps.tsv: step %s expects %q", step, expect)
		}
		if len(logLines) != compiles+1 {
			t.Fatalf("step %s: %d compiles, want %d: the ebin built for other options or another shard was taken as current\n%s", step, len(logLines), compiles+1, out)
		}
		compiles++
		got := strings.Fields(logLines[len(logLines)-1])
		if want, _ := strconv.Atoi(fields[2]); len(got) != 2 || got[0] != fields[1] || got[1] != strconv.Itoa(want) {
			t.Fatalf("step %s: the compile ran as %q, want options %s over %s units", step, logLines[len(logLines)-1], fields[1], fields[2])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if current == 0 || compiled == 0 {
		t.Fatalf("steps.tsv holds %d current and %d compile steps: it needs both", current, compiled)
	}
}

// The stand-in for clang-format: --version prints a pinned banner; otherwise
// it logs the file it was given and writes it with every "DRIFT" replaced, and
// for a header (.h, which clang-format reads as C++) also writes
// "operator =" as "operator=" — the real formatter's difference (F-221).
const fakeClangFormatByName = `#!/bin/sh
here=$(dirname "$0")
if [ "$1" = "--version" ]; then echo "clang-format version 23.1.3"; exit 0; fi
for a in "$@"; do f=$a; done
echo "$f" >> "$here/calls.log"
case "$f" in
*.h) sed -e 's/DRIFT/fixed/g' -e 's/operator =/operator=/g' "$f" ;;
*) sed 's/DRIFT/fixed/g' "$f" ;;
esac
`

type cFormatGate struct {
	t   *testing.T
	dir string
}

func newCFormatGate(t *testing.T) *cFormatGate {
	t.Helper()
	needTools(t, "sh", "awk", "xargs", "find", "sort", "cmp")
	g := &cFormatGate{t: t, dir: t.TempDir()}
	g.write("clang-format", fakeClangFormatByName)
	if err := os.Chmod(filepath.Join(g.dir, "clang-format"), 0o755); err != nil {
		t.Fatal(err)
	}
	g.write(".clang-format", "BasedOnStyle: LLVM\n")
	return g
}

func (g *cFormatGate) write(name, content string) {
	g.t.Helper()
	p := filepath.Join(g.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

// run runs the gate over the trees and returns its output, whether it was
// green, and the files the formatter was started on. The environment carries
// C_FORMAT_CACHE, as the per-commit lane did at c7852e0047, so a script that
// still read a store would answer from it.
func (g *cFormatGate) run(shard, allow string, trees ...string) (string, bool, []string) {
	g.t.Helper()
	calls := filepath.Join(g.dir, "calls.log")
	if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
		g.t.Fatal(err)
	}
	cmd := testproc.Command(g.t, "sh", append([]string{filepath.Join(repoRoot(g.t), "make", "checks", "c-clean-format.sh"), allow}, trees...)...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(),
		"CLANG_FORMAT="+filepath.Join(g.dir, "clang-format"),
		"OUT="+filepath.Join(g.dir, "out"),
		"C_FORMAT_SHARD="+shard,
		"C_FORMAT_CACHE="+filepath.Join(g.dir, "store"))
	out, err := cmd.CombinedOutput()
	b, readErr := os.ReadFile(calls)
	if readErr != nil && !os.IsNotExist(readErr) {
		g.t.Fatal(readErr)
	}
	return string(out), err == nil, strings.Fields(string(b))
}

// TestCFormatJudgesEachFileByItsOwnName (F-221) runs the gate over the
// committed pair make/checks/testdata/c-format-same-bytes: one content, clean
// as a.c and drifting as a.h. Over source/ (the .c alone) it is green; over
// pair/ it is red naming a.h and not a.c, on the first run and every run
// after, whatever the earlier runs judged. At c7852e0047, with the verdict
// store keyed by bytes, the pair after the source was green.
func TestCFormatJudgesEachFileByItsOwnName(t *testing.T) {
	t.Parallel()
	g := newCFormatGate(t)
	fixture := filepath.Join(repoRoot(t), "make", "checks", "testdata", "c-format-same-bytes")
	for _, name := range []string{"pair/a.c", "pair/a.h", "source/a.c"} {
		b, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		g.write(name, string(b))
	}
	a, _ := os.ReadFile(filepath.Join(g.dir, "pair/a.c"))
	h, _ := os.ReadFile(filepath.Join(g.dir, "pair/a.h"))
	if string(a) != string(h) || !strings.Contains(string(a), "operator =") {
		t.Fatalf("the committed pair is not one content holding \"operator =\": a.c %q, a.h %q", a, h)
	}
	g.write("empty.txt", "")
	for i := 1; i <= 2; i++ {
		out, green, calls := g.run("", "empty.txt", "source")
		if !green || len(calls) != 1 {
			t.Fatalf("run %d: the .c alone: green=%v, %d formatter runs, want green with 1\n%s", i, green, len(calls), out)
		}
		out, green, calls = g.run("", "empty.txt", "pair")
		if green || len(calls) != 2 {
			t.Fatalf("run %d: the pair: green=%v, %d formatter runs, want red with 2: a header was judged by a .c file's verdict\n%s", i, green, len(calls), out)
		}
		if !strings.Contains(out, "clang-format drift in pair/a.h") || strings.Contains(out, "pair/a.c") {
			t.Fatalf("run %d: the pair is red, but not naming a.h alone:\n%s", i, out)
		}
	}
}

// TestCFormatShardsAreTheGate: the plan's C shards, run over one tree, format
// every file exactly once between them, and their verdicts are the unsharded
// gate's: a drift off the ratchet and a listed file that became clean are each
// named by exactly one shard, and the "no longer generated" line by all.
func TestCFormatShardsAreTheGate(t *testing.T) {
	t.Parallel()
	g := newCFormatGate(t)
	var files []string
	for i := range 11 {
		name := fmt.Sprintf("tree/f%02d.c", i)
		files = append(files, name)
		g.write(name, fmt.Sprintf("int f%02d;\n", i))
	}
	g.write("tree/f03.c", "int DRIFT03;\n")                             // drift, not listed: red
	g.write("tree/f07.c", "int DRIFT07;\n")                             // drift, listed: fine
	g.write("allow.txt", "# ratchet\ntree/f07.c\ntree/f09.c\ngone.c\n") // f09 is clean: red; gone.c: red

	whole, green, calls := g.run("", "allow.txt", "tree")
	if green || len(calls) != len(files) {
		t.Fatalf("the unsharded gate: green=%v with %d formatter runs, want red with %d\n%s", green, len(calls), len(files), whole)
	}
	specs := shardsOf(t, planCleanRows(t, "internal/codegen/ctable/wire.go"), "c")
	formatted := map[string]int{}
	said := map[string]int{}
	reds := 0
	for _, spec := range specs {
		out, green, calls := g.run(spec, "allow.txt", "tree")
		if !green {
			reds++
		}
		for _, c := range calls {
			formatted[c]++
		}
		for _, s := range []string{"clang-format drift in tree/f03.c", "tree/f09.c is clean now", "gone.c is listed"} {
			if strings.Contains(out, s) {
				said[s]++
			}
		}
		if green && !strings.Contains(out, "(shard "+spec+":") {
			t.Errorf("shard %s does not say which shard it judged:\n%s", spec, out)
		}
	}
	for _, f := range files {
		if formatted[f] != 1 {
			t.Errorf("%s was formatted by %d of the %d shards, want exactly 1", f, formatted[f], len(specs))
		}
	}
	if len(formatted) != len(files) {
		t.Errorf("the shards formatted %d distinct files, the tree has %d", len(formatted), len(files))
	}
	if said["clang-format drift in tree/f03.c"] != 1 || said["tree/f09.c is clean now"] != 1 {
		t.Errorf("a file's verdict was said by other than one shard: %v", said)
	}
	if said["gone.c is listed"] != len(specs) {
		t.Errorf("the ratchet's missing file was named by %d of %d shards, want all", said["gone.c is listed"], len(specs))
	}
	if reds == 0 {
		t.Error("every shard was green over a tree the unsharded gate is red over")
	}
	if out, green, calls := g.run(fmt.Sprintf("%d/%d", len(specs)+1, len(specs)), "allow.txt", "tree"); green || len(calls) != 0 || !strings.Contains(out, "is not a shard") {
		t.Errorf("a shard past N: green=%v, %d formatter runs, want red with none\n%s", green, len(calls), out)
	}
}

// TestLangCleanRowsKeepNoStore: no lang-clean row, and neither sharded gate's
// engine, reads a verdict or an ebin kept from another run, so a row's time
// on the fleet is its cold time and its verdict is this run's (F-218, F-221).
func TestLangCleanRowsKeepNoStore(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	jobs := yamlJobs(workflows(t, root)["ci-fast.yml"])
	body, ok := jobs["lang-clean"]
	if !ok {
		t.Fatal("ci-fast.yml has no lang-clean job")
	}
	for name, job := range map[string]string{"lang-clean": body, "verify-leg": jobs["verify-leg"], "verify-leg-elixir": jobs["verify-leg-elixir"]} {
		for _, s := range []string{"C_FORMAT_CACHE", "ELIXIR_EBIN_CACHE", ".cache/schema-ci", "actions/cache"} {
			if strings.Contains(job, s) {
				t.Errorf("ci-fast.yml %s names %s: its rows keep nothing between runs", name, s)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "make", "elixir-ebin-cache.sh")); err == nil {
		t.Error("make/elixir-ebin-cache.sh is back: no row keeps an ebin between runs")
	}
	for _, s := range []string{"include: ${{ fromJSON(needs.plan.outputs.clean_rows) }}", `C_FORMAT_SHARD="$SHARD"`, `ELIXIR_TABLE_SHARD="$SHARD"`} {
		if !strings.Contains(body, s) {
			t.Errorf("ci-fast.yml lang-clean does not carry %q: its rows are not the plan's shards", s)
		}
	}
	engine, err := os.ReadFile(filepath.Join(root, "make", "checks", "c-clean-format.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(engine), "C_FORMAT_CACHE") {
		t.Error("make/checks/c-clean-format.sh reads C_FORMAT_CACHE: a verdict kept by bytes judged a header by a .c file's verdict (F-221)")
	}
}

// TestVerifyLegElixirTakesTheShards: the per-commit verify rows that run the
// whole Elixir table corpus ebin (conformance, fuzz) never compile it, which
// is past one runner's two minutes cold (F-218): they are verify-leg-elixir,
// which needs lang-clean, downloads the shards the lang-clean (elixir K/N) rows
// uploaded, and builds the ebin from them through make/elixir-ebin-handoff.sh
// (ELIXIR_EBIN_FROM) under the compiler options the shards were compiled
// with. verify-leg runs neither gate for elixir.
func TestVerifyLegElixirTakesTheShards(t *testing.T) {
	t.Parallel()
	jobs := yamlJobs(workflows(t, repoRoot(t))["ci-fast.yml"])
	leg, clean, take := jobs["verify-leg"], jobs["lang-clean"], jobs["verify-leg-elixir"]
	if leg == "" || clean == "" || take == "" {
		t.Fatal("ci-fast.yml lacks verify-leg, lang-clean or verify-leg-elixir")
	}
	for _, g := range []string{"conformance", "fuzz"} {
		if !strings.Contains(leg, "          - leg: elixir\n            gate: "+g+"\n") {
			t.Errorf("verify-leg does not exclude (elixir, %s): it would compile the whole corpus ebin past the cap", g)
		}
	}
	for _, s := range []string{
		"needs: [plan, lang-clean]",
		"!cancelled()",
		`contains(needs.plan.outputs.verify_legs, '"elixir"')`,
		"gate: [conformance, fuzz]",
		"timeout-minutes: 2",
		"pattern: elixir-tables-ebin-*",
		`ELIXIR_EBIN_FROM="$SHARDS"`,
		`ERL_COMPILER_OPTIONS='[no_ssa_opt]' make "verify-elixir-${{ matrix.gate }}"`,
	} {
		if !strings.Contains(take, s) {
			t.Errorf("verify-leg-elixir does not carry %q", s)
		}
	}
	for _, s := range []string{
		"elixir-tables-ebin-handoff tables-elixir-accessor-descriptor-agreement",
		`ELIXIR_TABLE_SHARD="$SHARD" ELIXIR_EBIN_ERL_COMPILER_OPTIONS='[no_ssa_opt]'`,
		"name: elixir-tables-ebin-${{ strategy.job-index }}",
		"path: build/elixir-ebin-handoff",
		"include-hidden-files: true",
		"if-no-files-found: error",
	} {
		if !strings.Contains(clean, s) {
			t.Errorf("lang-clean does not carry %q: the Elixir shards are not handed over", s)
		}
	}
}

// TestElixirEbinFromIsTheWholeCorpus: a job that takes the shards takes the
// whole corpus; asking for a shard of it as well is a make error.
func TestElixirEbinFromIsTheWholeCorpus(t *testing.T) {
	t.Parallel()
	needTools(t, "make", "sh", "awk")
	dir := elixirMakeDir(t)
	out, err := makeElixir(t, dir, nil, "elixir-table-shard-units", "ELIXIR_EBIN_FROM=shards", "ELIXIR_TABLE_SHARD=1/4")
	if err == nil || !strings.Contains(out, "ELIXIR_EBIN_FROM takes the whole corpus") {
		t.Fatalf("ELIXIR_EBIN_FROM with ELIXIR_TABLE_SHARD was not a make error: %v\n%s", err, out)
	}
	if out, err := makeElixir(t, dir, nil, "elixir-table-shard-units", "ELIXIR_EBIN_FROM=shards"); err != nil {
		t.Fatalf("the control, ELIXIR_EBIN_FROM alone, is a make error: %v\n%s", err, out)
	}
}

// TestElixirVerifyNightlyRunsTheOptimizedBEAM: the per-commit Elixir rows
// compile their BEAM without the SSA optimizer and take the corpus ebin from
// the lang-clean shards. That trade is accepted only because ci-full.yml's nightly
// elixir-verify runs the same three gates on the BEAM a user's elixirc writes:
// it must exist, run on the schedule, run every gate, set no compiler option
// and no store, and be read by the nightly verdict.
func TestElixirVerifyNightlyRunsTheOptimizedBEAM(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)
	full := wfs["ci-full.yml"]
	if !strings.Contains(full, "\n  schedule:") || !strings.Contains(full, "- cron:") {
		t.Fatal("ci-full.yml has no schedule: the nightly half of the Elixir trade does not run")
	}
	jobs := yamlJobs(full)
	body, ok := jobs["elixir-verify"]
	if !ok {
		t.Fatal("ci-full.yml has no elixir-verify job: the per-commit [no_ssa_opt] compile and the handed-over shards would be the Elixir verify gates' only coverage")
	}
	ev := ciEvent{Name: "schedule", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}
	if runs, err := jobRunsInWorkflow(full, "elixir-verify", ev); err != nil || !runs {
		t.Fatalf("ci-full.yml elixir-verify does not run on the schedule (%v)", err)
	}
	gates := map[string]bool{}
	for line := range strings.SplitSeq(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "gate: ["); ok {
			for g := range strings.SplitSeq(strings.TrimSuffix(v, "]"), ",") {
				gates[strings.TrimSpace(g)] = true
			}
		}
	}
	for _, g := range []string{"conformance", "bench", "fuzz"} {
		if !gates[g] {
			t.Errorf("ci-full.yml elixir-verify does not run the %s gate", g)
		}
	}
	if !strings.Contains(body, `make "verify-elixir-${{ matrix.gate }}"`) {
		t.Error("ci-full.yml elixir-verify does not run make verify-elixir-<gate>")
	}
	for _, s := range []string{"no_ssa_opt", "ELIXIR_EBIN_CACHE", "ELIXIR_EBIN_FROM", "ELIXIR_EBIN_ERL_COMPILER_OPTIONS", "ELIXIR_TABLE_SHARD", "ERL_COMPILER_OPTIONS="} {
		if strings.Contains(body, s) {
			t.Errorf("ci-full.yml elixir-verify carries %s: it must compile the BEAM as shipped, whole, with no store", s)
		}
	}
	if !strings.Contains(body, `if [ -n "${ERL_COMPILER_OPTIONS:-}" ]`) {
		t.Error("ci-full.yml elixir-verify no longer refuses an ERL_COMPILER_OPTIONS in its environment")
	}
	verdict, ok := jobs["verdict"]
	if !ok || !slices.Contains(jobNeeds(verdict), "elixir-verify") {
		t.Error("ci-full.yml verdict does not need elixir-verify: a red or skipped nightly Elixir row would not redden the nightly")
	}
}
