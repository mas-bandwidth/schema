package moo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MOO STRICT ANALYZER GATE (make/schema-moo-generated-analyze-strict-gate.mk,
// docs/PORT-MOO.md layer 7). moo-analyze --mode=strict over EVERY unit of the
// schema corpus the Makefile's generate targets read, generated through the
// moo leg, and the vendored serialize.moo runtime: no error and no lint
// warning. moo-analyze exits 0 on a lint warning, so the gate is that its
// output is EMPTY, never the exit code alone.

// strictGateDirs are the corpus directories the Makefile's generate targets
// read whole (Makefile: SCHEMAS, SCHEMAS128, SCHEMAS_WIDE and the
// SCHEMAS_TABLES* wildcards). A unit that declares tables is analyzed over
// both surfaces the leg generates for it (strictGateGenerate below).
var strictGateDirs = []string{
	"examples",
	"examples128",
	"examples-wide",
	"tables/examples",
	"tables/pointers",
	"tables/block",
	"tables/blockhome",
	"tables/messages",
	"tables/stream",
	"tables/blobs",
	"tables/scalars",
	"tables/maps",
	"tables/lists",
	"tables/arms",
	"tables/backend",
	"tables/vocab",
	"tables/vocab9",
}

// strictGateFiles are the bench corpus's units, one at a time, exactly as the
// Makefile's bench targets name them (bench/corpus holds several packages).
var strictGateFiles = [][]string{
	{"bench/corpus/Bench.schema"},
	{"bench/corpus/RealWorld.schema"},
	{"bench/corpus/BenchTable.schema"},
	{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"},
}

// strictGatePerFileGlobs are the directories whose every schema file is a
// unit of its own: the packet gates' corpora and the table conformance rows.
var strictGatePerFileGlobs = []string{
	"test/packet-defaults/*.schema",
	"test/packet-finite/*.schema",
	"test/packet-text/*.schema",
	"test/packet-void/*.schema",
	"test/packet-wide/*.schema",
	"test/tables/*.schema",
}

// strictGateKnownRefusals names, by unit, the corpus units the moo generator
// refuses and why. A refusal is not an analyzer finding (nothing is generated
// to analyze), but it is not silence either: an unlisted refusal fails the
// gate, and a listed unit that starts generating fails it too, so the list
// cannot rot.
var strictGateKnownRefusals = map[string]string{}

// StrictGateTables and StrictGateTableRuntime are the table backend's
// Generate and Runtime (internal/codegen/mootable), installed by
// strictgate_tables_test.go: mootable imports this package, so this file
// cannot import it, and the external test package hands it in.
var (
	StrictGateTables       func(u *ir.Unit) (map[string][]byte, error)
	StrictGateTableRuntime func() map[string][]byte
)

// strictGateGenerate is every surface the moo leg generates for one unit: the
// packet surface, and beside it the table surface (layer 3,
// internal/codegen/mootable), the way the Luau lens copies luautable's output
// beside luau's. A table unit the table backend refuses is the unit's refusal.
func strictGateGenerate(u *ir.Unit) (map[string][]byte, error) {
	out, err := Generate(u)
	if err != nil {
		return nil, err
	}
	if StrictGateTables == nil {
		return nil, fmt.Errorf("the table backend is not installed (strictgate_tables_test.go)")
	}
	tables, err := StrictGateTables(u)
	if err != nil {
		return nil, fmt.Errorf("tables: %w", err)
	}
	for name, data := range tables {
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed by both surfaces", name)
		}
		out[name] = data
	}
	return out, nil
}

// strictGateRuntime is every runtime module the two surfaces embed, by name:
// each is analyzed once, not once per unit.
func strictGateRuntime() map[string][]byte {
	out := Runtime()
	if StrictGateTableRuntime != nil {
		maps.Copy(out, StrictGateTableRuntime())
	}
	return out
}

// strictGateUnits lists every unit as repository-relative paths.
func strictGateUnits(t *testing.T) [][]string {
	t.Helper()
	var units [][]string
	for _, d := range strictGateDirs {
		units = append(units, []string{d})
	}
	units = append(units, strictGateFiles...)
	for _, g := range strictGatePerFileGlobs {
		paths, err := filepath.Glob(repo(filepath.FromSlash(g)))
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) == 0 {
			t.Fatalf("the corpus glob %s matches nothing: the gate would pass over a corpus that moved", g)
		}
		sort.Strings(paths)
		for _, p := range paths {
			rel, _ := filepath.Rel(repo(), p)
			units = append(units, []string{filepath.ToSlash(rel)})
		}
	}
	return units
}

// strictGateLoad loads one unit; a path that is a directory contributes every
// schema file in it. A load error is returned, not fatal: the caller decides
// whether it is a named refusal.
func strictGateLoad(t *testing.T, unit []string) (u *ir.Unit, err error) {
	t.Helper()
	var paths []string
	for _, p := range unit {
		abs := repo(filepath.FromSlash(p))
		info, statErr := os.Stat(abs)
		if statErr != nil {
			return nil, statErr
		}
		if info.IsDir() {
			found, _ := filepath.Glob(filepath.Join(abs, "*.schema"))
			if len(found) == 0 {
				return nil, fmt.Errorf("no schema files under %s", p)
			}
			sort.Strings(found)
			paths = append(paths, found...)
			continue
		}
		paths = append(paths, abs)
	}
	return unitFromFiles(t, paths), nil
}

// strictGateAnalyzer resolves moo-analyze and holds it to the pin: the
// directory it lives in carries the PIN file `make dist-moo` writes (and
// .github/actions/moo checks), and it must name moorun.Pin, and the moo
// beside it must report the pinned release. moo-analyze has no --version of
// its own, so the build's PIN file and its sibling interpreter are the
// evidence. A gate run on another analyzer proves nothing about the pin; it
// refuses BY NAME.
func strictGateAnalyzer(t *testing.T) string {
	t.Helper()
	analyze, why := moorun.Analyzer()
	if analyze == "" {
		t.Fatalf("tool missing: moo-analyze: %s", why)
	}
	if err := strictGatePinned(analyze); err != nil {
		t.Fatalf("schema-moo-generated-analyze-strict-gate: REFUSED: %v", err)
	}
	return analyze
}

// strictGateRelease is what the pinned moo prints for --version.
const strictGateRelease = "moo 1.3.0 (Luau 0.741)"

func strictGatePinned(analyze string) error {
	dir := filepath.Dir(analyze)
	pin, err := os.ReadFile(filepath.Join(dir, "PIN"))
	if err != nil {
		return fmt.Errorf("the analyzer %s is not the pin: no PIN file beside it (make dist-moo writes one naming %s)", analyze, moorun.Pin)
	}
	if got := strings.TrimSpace(string(pin)); got != moorun.Pin {
		return fmt.Errorf("the analyzer %s is moo %s, not the pin %s (MOO_PIN in make/moo.mk)", analyze, got, moorun.Pin)
	}
	out, err := exec.Command(filepath.Join(dir, "moo"), "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the moo beside the analyzer %s does not run: %w", analyze, err)
	}
	if got := strings.TrimSpace(string(out)); got != strictGateRelease {
		return fmt.Errorf("the moo beside the analyzer %s reports %q, not the pinned %q", analyze, got, strictGateRelease)
	}
	return nil
}

// strictGateAnalyze runs ONE analyzer process over files (paths relative to
// dir) in strict mode with GNU-format diagnostics, and returns its output.
func strictGateAnalyze(analyze, dir string, files []string) ([]byte, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no files to analyze")
	}
	cmd := exec.Command(analyze, append([]string{"--mode=strict", "--formatter=gnu"}, files...)...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// strictGateWrite writes every generated unit under one root, one directory
// per unit, and returns the root and the modules to analyze: every generated
// module, and ONE copy of each distinct runtime module (the runtime is the
// same bytes in every unit; analyzing it once per unit proves nothing more).
func strictGateWrite(t *testing.T, generated []map[string][]byte) (string, []string, int) {
	t.Helper()
	root := t.TempDir()
	runtime := strictGateRuntime()
	seen := map[string]bool{}
	var modules, runtimes []string
	files := 0
	for i, out := range generated {
		dir := fmt.Sprintf("u%03d", i)
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range out {
			if err := os.WriteFile(filepath.Join(root, dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			files++
			if !bytes.HasPrefix(data, []byte("--!strict\n")) {
				t.Errorf("%s/%s does not open with --!strict: the analyzer would hold it to nonstrict", dir, name)
			}
			rel := dir + "/" + name
			if _, ok := runtime[name]; ok {
				sum := sha256.Sum256(data)
				key := name + ":" + hex.EncodeToString(sum[:])
				if !seen[key] {
					seen[key] = true
					runtimes = append(runtimes, rel)
				}
				continue
			}
			modules = append(modules, rel)
		}
	}
	sort.Strings(modules)
	sort.Strings(runtimes)
	return root, append(modules, runtimes...), files
}

// TestMooGeneratedAnalyzeStrictGate is the gate: every corpus unit generated
// through the moo leg, plus the vendored runtime, and moo-analyze
// --mode=strict SILENT over all of it.
func TestMooGeneratedAnalyzeStrictGate(t *testing.T) {
	slowtest.Gate(t, "moo-analyze (the pinned analyzer, over the whole corpus)")
	t.Parallel()
	analyze := strictGateAnalyzer(t)

	units := strictGateUnits(t)
	refusals := map[string]string{}
	var generated []map[string][]byte
	for _, unit := range units {
		key := strings.Join(unit, "+")
		u, err := strictGateLoad(t, unit)
		if err != nil {
			t.Fatalf("load %s: %v", key, err)
		}
		out, err := strictGateGenerate(u)
		if err != nil {
			refusals[key] = err.Error()
			continue
		}
		generated = append(generated, out)
	}
	if len(generated) == 0 {
		t.Fatal("no moo output was generated: the gate would pass over nothing")
	}
	root, files, written := strictGateWrite(t, generated)
	// The runtime is in the analyzed set by name, every module of both
	// surfaces' runtimes.
	for name := range strictGateRuntime() {
		found := false
		for _, f := range files {
			if filepath.Base(f) == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the runtime module %s is not in the analyzed set", name)
		}
	}
	out, err := strictGateAnalyze(analyze, root, files)
	if len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("moo-analyze --mode=strict is not silent over the generated corpus (%d files):\n%s", len(files), out)
	}
	if err != nil {
		t.Fatalf("moo-analyze --mode=strict failed with no diagnostics: %v", err)
	}
	for key, why := range refusals {
		if _, ok := strictGateKnownRefusals[key]; !ok {
			t.Errorf("unit %s is refused and strictGateKnownRefusals does not name it: %s", key, why)
		}
	}
	for key := range strictGateKnownRefusals {
		if _, ok := refusals[key]; !ok {
			t.Errorf("unit %s is listed as refused and now generates; take it off strictGateKnownRefusals", key)
		}
	}
	t.Logf("moo-analyze --mode=strict: %d units, %d generated files, %d analyzed (runtime once), %d named refusals, silent",
		len(generated), written, len(files), len(refusals))
}

// TestMooGeneratedAnalyzeStrictGatePlantedPattern is the gate's negative
// control: one real generated unit, analyzed clean through the gate's own
// invocation, then ONE defect planted in a copy of a generated module, two
// ways. A strict type error (the analyzer exits nonzero) and an unused local
// (a lint warning, on which moo-analyze EXITS 0): each must red the gate with
// exactly one diagnostic, on the planted line, of its own kind. The second is
// why the gate reads output rather than the exit code.
func TestMooGeneratedAnalyzeStrictGatePlantedPattern(t *testing.T) {
	slowtest.Gate(t, "moo-analyze (the pinned analyzer, over the whole corpus)")
	t.Parallel()
	analyze := strictGateAnalyzer(t)
	u, err := strictGateLoad(t, []string{"examples"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := strictGateGenerate(u)
	if err != nil {
		t.Fatal(err)
	}
	const plant = "moo_strict_gate_planted_pattern"
	for _, c := range []struct {
		name, src, kind string
		exits           bool
	}{
		{"type-error", "local " + plant + ": number = \"this string is not a number\"\nprint(" + plant + ")", "TypeError", true},
		{"lint-warning", "local " + plant + " = 1", "LocalUnused", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, files, _ := strictGateWrite(t, []map[string][]byte{out})
			if got, err := strictGateAnalyze(analyze, root, files); err != nil || len(bytes.TrimSpace(got)) != 0 {
				t.Fatalf("the clean generated unit is not silent (err %v):\n%s", err, got)
			}
			target := filepath.Join(root, "u000", "Wire.moo")
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			at := bytes.LastIndex(data, []byte("\nreturn M\n"))
			if at < 0 {
				t.Fatalf("%s has no final `return M` to plant before", target)
			}
			line := bytes.Count(data[:at], []byte("\n")) + 2
			data = append(append(append([]byte(nil), data[:at]...), "\n"+c.src+"\n"...), data[at:]...)
			if err := os.WriteFile(target, data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := strictGateAnalyze(analyze, root, files)
			if len(bytes.TrimSpace(got)) == 0 {
				t.Fatalf("moo-analyze --mode=strict stayed silent over the planted %s: the gate has no blade", c.kind)
			}
			if c.exits != (err != nil) {
				t.Errorf("moo-analyze exit (err %v) is not what a %s gives; the gate reads output either way", err, c.kind)
			}
			// EXACTLY ITS ROW. The analyzer reports a module once per path
			// it reaches it by (as a listed file, and again as a require
			// target under its absolute path), so the row is the diagnostic
			// with its path stripped: every line is the one plant.
			want := fmt.Sprintf("u000/Wire.moo:%d.", line)
			rows := map[string]bool{}
			for l := range strings.SplitSeq(strings.TrimSpace(string(got)), "\n") {
				i := strings.Index(l, want)
				if i < 0 || !strings.Contains(l, c.kind) || !strings.Contains(l, plant) && c.kind != "TypeError" {
					t.Fatalf("a diagnostic that is not the plant (want a %s at %s):\n%s", c.kind, want, got)
				}
				rows[l[i:]] = true
			}
			if len(rows) != 1 {
				t.Fatalf("want exactly one diagnostic, the plant at %s; got %d:\n%s", want, len(rows), got)
			}
			for r := range rows {
				t.Logf("negative control: the planted %s reds the gate on exactly its row: %s", c.kind, r)
			}
		})
	}
}

// TestMooGeneratedAnalyzeStrictGatePinNegativeControl is the version check's
// negative control: the real analyzer, copied beside a PIN file naming
// another commit, must be REFUSED by name; and beside the true PIN and the
// true moo it must be accepted, so the refusal is the PIN and nothing else.
func TestMooGeneratedAnalyzeStrictGatePinNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo-analyze (the pinned analyzer, over the whole corpus)")
	t.Parallel()
	analyze := strictGateAnalyzer(t)
	src := filepath.Dir(analyze)
	link := func(dir, pin string) string {
		t.Helper()
		for _, tool := range []string{"moo", "moo-analyze"} {
			if err := os.Symlink(filepath.Join(src, tool), filepath.Join(dir, tool)); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "PIN"), []byte(pin+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, "moo-analyze")
	}
	good := link(t.TempDir(), moorun.Pin)
	if err := strictGatePinned(good); err != nil {
		t.Fatalf("the true pin was refused, so a refusal below would prove nothing: %v", err)
	}
	const other = "7c0ff3e83d8e7f4a71f4b06c9530862197271d41"
	bad := link(t.TempDir(), other)
	err := strictGatePinned(bad)
	if err == nil {
		t.Fatalf("an analyzer whose PIN names %s was accepted as the pin %s: the version check has no blade", other, moorun.Pin)
	}
	if !strings.Contains(err.Error(), other) || !strings.Contains(err.Error(), moorun.Pin) {
		t.Fatalf("the refusal does not name both commits: %v", err)
	}
	t.Logf("negative control: %v", err)
}
