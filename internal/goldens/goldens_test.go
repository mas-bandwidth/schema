// Package goldens pins the compiler's output (SPEC §7.2 gates 1 and 2): the
// corpus's generated source byte-for-byte, the protocol id exactly, and the
// corpus's formatter-canonical form. A change to
// any of these is loud by construction; a WIRE-affecting change under an
// unchanged schema is a stop-the-line event, never a quiet re-pin (SPEC §3.1).
//
// Regenerate deliberately with: make update-goldens
package goldens

import (
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var update = flag.Bool("update", false, "rewrite the golden files from current output")

const (
	corpusDir = "../../examples"
	// the fixed-point + 128-bit unit: all nine targets, pinned like the main
	// corpus (the serialize ports all carry the phase-1 surface).
	corpus128Dir = "../../examples128"
	goldenDir    = "../../testdata/golden"
)

// schema is the driver every test here runs on — the public API, with the
// library's load policy: this harness measures the corpus, so it must never
// repair it (TestCorpusIsCanonical is the gate that would be repairing
// itself).
var schema = compiler.New()

func loadCorpus(t *testing.T) *ir.Unit { return loadCorpusDir(t, corpusDir) }

func loadCorpusDir(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := schema.Load(paths)
	if err != nil {
		t.Fatalf("corpus does not compile: %v", err)
	}
	return u
}

// generate emits one target through the same registration door an external
// generator would come through.
func generate(t *testing.T, u *ir.Unit, target string, opts compiler.Options) map[string][]byte {
	t.Helper()
	files, err := schema.Generate(u, target, opts)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestCorpusIsCanonical is the fmt-drift gate: every corpus file must already
// be in schemafmt's one style (the compiler formats before processing, so a
// non-canonical file in git means someone bypassed the tool).
func TestCorpusIsCanonical(t *testing.T) {
	for _, dir := range []string{corpusDir, corpus128Dir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out, err := compiler.Format(p, data)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if string(out) != string(data) {
				t.Errorf("%s is not formatter-canonical — run: make fmt", p)
			}
		}
	}
}

// TestGoldenId pins the protocol id (SPEC §7.2 gate 2) — the tripwire on the
// §3.1 hash procedure: any change to how the id is computed breaks this
// loudly.
func TestGoldenId(t *testing.T) {
	u := loadCorpus(t)
	got := fmt.Sprintf("0x%016x\n", u.ProtocolId)
	path := filepath.Join(goldenDir, "id.txt")
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden id (run: make update-goldens): %v", err)
	}
	if got != string(want) {
		t.Errorf("protocol id moved: got %s want %s — if the schema files changed this is expected once (re-pin deliberately); if they did not, the §3.1 hash procedure changed and that is stop-the-line", got, string(want))
	}
}

// TestGoldenSource pins the generated C++ byte-for-byte (SPEC §7.2 gate 1).
func TestGoldenSource(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "cpp", nil)
	pinDir(t, filepath.Join(goldenDir, "cpp"), files)
}

// TestGoldenSourceGo pins the generated Go byte-for-byte (SPEC §7.2 gate 1,
// second target).
func TestGoldenSourceGo(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "go", nil)
	pinDir(t, filepath.Join(goldenDir, "go"), files)
}

// TestGoldenSourceRust pins the generated Rust byte-for-byte (SPEC §7.2
// gate 1, third target).
func TestGoldenSourceRust(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "rust", nil)
	pinDir(t, filepath.Join(goldenDir, "rust"), files)
}

// TestGoldenSourceC pins the generated C byte-for-byte (SPEC §7.2 gate 1,
// fifth target).
func TestGoldenSourceC(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "c", nil)
	pinDir(t, filepath.Join(goldenDir, "c"), files)
}

// TestGoldenSourceCs pins the generated C# byte-for-byte (SPEC §7.2 gate 1,
// fourth target).
func TestGoldenSourceCs(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "cs", nil)
	pinDir(t, filepath.Join(goldenDir, "cs"), files)
}

// TestGoldenSourceJs pins the generated JavaScript byte-for-byte (SPEC §7.2
// gate 1, sixth target).
func TestGoldenSourceJs(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "js", nil)
	pinDir(t, filepath.Join(goldenDir, "js"), files)
}

// TestGoldenSourceDart pins the generated Dart byte-for-byte (SPEC §7.2
// gate 1, seventh target).
func TestGoldenSourceDart(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "dart", nil)
	pinDir(t, filepath.Join(goldenDir, "dart"), files)
}

// TestGoldenSourceJava pins the generated Java byte-for-byte (SPEC §7.2
// gate 1, eighth target).
func TestGoldenSourceJava(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "java", nil)
	pinDir(t, filepath.Join(goldenDir, "java"), files)
}

// TestGoldenSourceElixir pins the generated Elixir byte-for-byte (SPEC §7.2
// gate 1, ninth target).
func TestGoldenSourceElixir(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "elixir", nil)
	pinDir(t, filepath.Join(goldenDir, "elixir"), files)
}

// TestGoldenSourceLua pins the generated Lua byte-for-byte (SPEC §7.2 gate 1,
// tenth target): the unit modules and the serialize runtime written beside them.
func TestGoldenSourceLua(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "lua", nil)
	pinDir(t, filepath.Join(goldenDir, "lua"), files)
}

// TestGoldenSourceLuau pins the generated Luau byte-for-byte (SPEC §7.2
// gate 1, tenth target).
func TestGoldenSourceLuau(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "luau", nil)
	pinDir(t, filepath.Join(goldenDir, "luau"), files)
}

// TestGoldenSourcePython pins the Python packet source and its runtime.
func TestGoldenSourcePython(t *testing.T) {
	t.Parallel()
	u := loadCorpus(t)
	pinDir(t, filepath.Join(goldenDir, "python"), generate(t, u, "python", nil))
}

// The enum `Count` export moves NEITHER WIRE. Count is generated-code
// surface: it enters no wire-shape projection and no cook projection, so the
// packet wire's protocol id and the table wire's build version must both
// stand exactly where they stood before it existed. The numbers are written
// here as literals rather than read from testdata/golden — a re-pin of those
// files is precisely the mistake this refuses, and a gate that re-pins with
// them would say nothing. What legitimately moves them is a change to a
// PROJECTION itself — a wire-shape edit, or a cook form-version bump — which
// moves the corpus goldens in the same commit; a literal here that has to move
// alone is the defect this names.
func TestExportedSurfaceMovesNeitherWire(t *testing.T) {
	for _, unit := range []struct {
		name         string
		dir          string
		protocolId   uint64
		buildVersion uint64
	}{
		{"examples", corpusDir, 0x2bf51af687ac0abd, 0xf54396dac4647d28},
		{"examples128", corpus128Dir, 0xd2ded2cc8a482aa1, 0x7b5f4ee797ef6bfe},
	} {
		t.Run(unit.name, func(t *testing.T) {
			u := loadCorpusDir(t, unit.dir)
			if u.ProtocolId != unit.protocolId {
				t.Errorf("protocol id = 0x%016x, want 0x%016x — the packet wire moved under an unchanged corpus (SPEC §3.1); a generated-code export must never reach it", u.ProtocolId, unit.protocolId)
			}
			if got := ir.BuildVersion(u); got != unit.buildVersion {
				t.Errorf("build version = 0x%016x, want 0x%016x — the table wire moved under an unchanged corpus (docs/SPEC-TABLES.md §20); a generated-code export must never reach it", got, unit.buildVersion)
			}
		})
	}
}

// TestGoldenLudicrousId pins the fixed-point + 128-bit unit's protocol id.
func TestGoldenLudicrousId(t *testing.T) {
	u := loadCorpusDir(t, corpus128Dir)
	got := fmt.Sprintf("0x%016x\n", u.ProtocolId)
	path := filepath.Join(goldenDir, "ludicrous", "id.txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden id (run: make update-goldens): %v", err)
	}
	if got != string(want) {
		t.Errorf("ludicrous protocol id moved: got %s want %s — if the schema files changed this is expected once (re-pin deliberately); if they did not, the §3.1 hash procedure changed and that is stop-the-line", got, string(want))
	}
}

// TestGoldenLudicrousSource pins the fixed-point + 128-bit unit's generated
// source byte-for-byte for EVERY registered target — the list is the
// compiler's own, so a tenth language pins its goldens without an edit here.
// Every serialize port carries the phase-1 surface — a backend erroring here
// is a loud failure — and the unit rides the same cross-language wire gates as
// the main corpus
// (test/{c,go,rust,cs}-ludicrous).
func TestGoldenLudicrousSource(t *testing.T) {
	u := loadCorpusDir(t, corpus128Dir)
	for _, target := range compiler.New().Targets() {
		pinDir(t, filepath.Join(goldenDir, "ludicrous", target), generate(t, u, target, nil))
	}
}

// pinDir compares (or, under -update, rewrites) one directory of goldens,
// BOTH WAYS: every emitted file has its golden with the same bytes, and every
// golden on disk is claimed by an emitted file. A golden nothing emits is a
// file the gate would otherwise never read again, so it is red, and -update
// removes it. An emission of no files is red too: it would pin nothing.
func pinDir(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if *update {
		if err := rewritePins(dir, files); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, problem := range pinProblems(dir, files) {
		t.Error(problem)
	}
}

func rewritePins(dir string, files map[string][]byte) error {
	if len(files) == 0 {
		return fmt.Errorf("%s: nothing was emitted, so there is nothing to pin", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for n, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	onDisk, err := goldenFiles(dir)
	if err != nil {
		return err
	}
	for _, n := range onDisk {
		if _, ok := files[n]; !ok {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(n))); err != nil {
				return err
			}
		}
	}
	return nil
}

// pinProblems is pinDir's comparison: one line per missing golden, per golden
// whose bytes differ, and per golden on disk that no emitted file claims.
func pinProblems(dir string, files map[string][]byte) []string {
	if len(files) == 0 {
		return []string{fmt.Sprintf("%s: nothing was emitted, so nothing was compared", dir)}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var problems []string
	for _, n := range names {
		path := filepath.Join(dir, filepath.FromSlash(n))
		want, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("missing golden %s (run: make update-goldens): %v", path, err))
			continue
		}
		if string(files[n]) != string(want) {
			problems = append(problems, fmt.Sprintf("%s: generated source diverged from its golden — deliberate emitter change? re-pin with make update-goldens; if the WIRE moved under an unchanged schema, that is stop-the-line (SPEC §3.1)", path))
		}
	}
	onDisk, err := goldenFiles(dir)
	if err != nil {
		return append(problems, fmt.Sprintf("%s: %v", dir, err))
	}
	for _, n := range onDisk {
		if _, ok := files[n]; !ok {
			problems = append(problems, fmt.Sprintf("%s: a golden no emitted file claims — the emitter no longer writes it, so nothing compares it; remove it with make update-goldens", filepath.Join(dir, filepath.FromSlash(n))))
		}
	}
	return problems
}

// goldenFiles is every file under dir, as slash-separated paths relative to it.
func goldenFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// taggedTargets are the targets a build tag registers. A build without the tag
// cannot emit them, so it only knows their golden directories by name; a build
// with the tag compares them like any other (TestGoldenSourceEveryTarget).
var taggedTargets = map[string]string{"moo": "schema_leg_moo"}

// TestGoldenSourceEveryTarget pins the main corpus for EVERY registered
// target — the list is the compiler's own — and holds the directory listing to
// that list both ways: a target with no golden directory is a missing golden,
// and a directory that is no target's is a set of goldens nothing compares.
// The per-language tests above name twelve targets by hand; goose, lobster,
// ruby, squirrel and unity emitted this corpus with nothing pinning it until
// this test read the list.
func TestGoldenSourceEveryTarget(t *testing.T) {
	u := loadCorpus(t)
	targets := compiler.New().Targets()
	if len(targets) < 17 {
		t.Fatalf("the compiler registers %d targets (%v), want at least 17", len(targets), targets)
	}
	for _, target := range targets {
		pinDir(t, filepath.Join(goldenDir, target), generate(t, u, target, nil))
	}
	if *update {
		return
	}
	for _, problem := range dirProblems(goldenDir, targets, []string{"build-version", "ludicrous", "tables"}) {
		t.Error(problem)
	}
	for _, problem := range dirProblems(filepath.Join(goldenDir, "ludicrous"), targets, nil) {
		t.Error(problem)
	}
}

// dirProblems names every directory under dir that is neither a registered
// target, a tagged target this build leaves out, nor one of the others named.
func dirProblems(dir string, targets, others []string) []string {
	known := map[string]bool{}
	for _, name := range targets {
		known[name] = true
	}
	for name := range taggedTargets {
		known[name] = true
	}
	for _, name := range others {
		known[name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, e := range entries {
		if e.IsDir() && !known[e.Name()] {
			problems = append(problems, fmt.Sprintf("%s is a directory of goldens for no registered target — nothing compares it", filepath.Join(dir, e.Name())))
		}
	}
	return problems
}

// tableGoldenDir is the tables units' Table-source pins: one directory per
// (unit, target), written by `make update-goldens` from build/tables-generated
// and its C and C# siblings (Makefile update-goldens, make/c.mk, make/cs.mk).
const tableGoldenDir = goldenDir + "/tables"

// tableUnit is one directory under testdata/golden/tables: the unit it pins
// and the target that emitted it.
type tableUnit struct {
	name   string // the golden directory's name
	dir    string // the unit's source directory
	target string
}

// tableUnits derives the pinned tables units FROM THE DIRECTORY LISTING, so a
// directory of goldens nobody compares cannot exist: a hand-kept list of the
// units is how six maps headers went stale on main across two merges (#683 —
// tables/maps, tables/arms and tables/lists were pinned and compared by
// nothing here). The names map by rule: `<unit>` is tables/<unit> emitted by
// the C++ reference, and `<unit>-c` and `<unit>-cs` are the same unit emitted
// by the C and C# targets (make/c.mk, make/cs.mk). A directory that fits no rule is still a unit
// under tables/, so a planted one is loaded and fails loudly rather than
// being skipped.
func tableUnits(goldenTables string) ([]tableUnit, error) {
	entries, err := os.ReadDir(goldenTables)
	if err != nil {
		return nil, err
	}
	var units []tableUnit
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		unit := tableUnit{name: name, target: "cpp"}
		switch {
		case strings.HasSuffix(name, "-cs"):
			unit.dir, unit.target = "../../tables/"+strings.TrimSuffix(name, "-cs"), "cs"
		case strings.HasSuffix(name, "-c"):
			unit.dir, unit.target = "../../tables/"+strings.TrimSuffix(name, "-c"), "c"
		default:
			unit.dir = "../../tables/" + name
		}
		units = append(units, unit)
	}
	return units, nil
}

// tableUnitsFor is tableUnits as TestGoldenTableSource reads it. Under -update
// a missing testdata/golden/tables is created first, so the documented repair
// (`make update-goldens`) writes every required directory from nothing; it
// used to stop on the missing parent before reading the writers' lists
// (finding F-056). Without -update a missing parent stays an error: the
// comparison never passes on a tree that pins nothing.
func tableUnitsFor(goldenTables string, update bool) ([]tableUnit, error) {
	if update {
		if err := os.MkdirAll(goldenTables, 0o755); err != nil {
			return nil, err
		}
	}
	return tableUnits(goldenTables)
}

// TestTableGoldenRootIsCreatedOnUpdate is F-056's regression test: with
// testdata/golden present and its tables directory absent, the update path
// creates the directory and lists no unit (TestGoldenTableSource then writes
// every required one); the compare path refuses.
func TestTableGoldenRootIsCreatedOnUpdate(t *testing.T) {
	golden := filepath.Join(t.TempDir(), "golden")
	if err := os.Mkdir(golden, 0o755); err != nil {
		t.Fatal(err)
	}
	tables := filepath.Join(golden, "tables")
	if _, err := tableUnitsFor(tables, false); !os.IsNotExist(err) {
		t.Errorf("compare with no tables directory: error %v, want not-exist", err)
	}
	if _, err := os.Stat(tables); !os.IsNotExist(err) {
		t.Errorf("the compare path created %s", tables)
	}
	units, err := tableUnitsFor(tables, true)
	if err != nil {
		t.Fatalf("update with no tables directory: %v", err)
	}
	if len(units) != 0 {
		t.Errorf("update listed %v in a directory it just created", units)
	}
	if st, err := os.Stat(tables); err != nil || !st.IsDir() {
		t.Errorf("update did not create %s: %v", tables, err)
	}
}

// TestGoldenTableSource pins every tables unit's emitted Table sources
// byte-for-byte (SPEC §7.2 gate 1), one directory of testdata/golden/tables
// at a time, the set derived from the listing. What each directory pins is
// the Makefile's choice (update-goldens copies *Table.h and *Table.cpp; the
// C and C# legs their own sets); what this gate holds is that EVERY pinned
// file in EVERY directory is what the compiler emits today — so a Table
// emitter change that moves a pinned file is red on the PR that made it,
// whichever unit it landed in.
func TestGoldenTableSource(t *testing.T) {
	listed, err := tableUnitsFor(tableGoldenDir, *update)
	if err != nil {
		t.Fatal(err)
	}
	required := requiredTableUnits(t, "../../Makefile")
	if !*update {
		// The set of directories is held to the writers' own lists, both
		// ways. The units compared below come from the listing, so without
		// this a whole directory of pins could leave the tree and take its
		// comparison with it.
		for _, problem := range tableDirProblems(required, listed) {
			t.Error(problem)
		}
	}
	// under -update a required directory that is gone is written again
	units := listed
	have := map[string]bool{}
	for _, unit := range listed {
		have[unit.name] = true
	}
	for _, unit := range required {
		if !have[unit.name] {
			units = append(units, unit)
		}
	}
	for _, unit := range units {
		t.Run(unit.name, func(t *testing.T) {
			u := loadCorpusDir(t, unit.dir)
			files := generate(t, u, unit.target, nil)
			dir := filepath.Join(tableGoldenDir, unit.name)
			entries, err := os.ReadDir(dir)
			if err != nil && (!*update || !os.IsNotExist(err)) {
				t.Fatal(err)
			}
			pinned := map[string][]byte{}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				got, ok := files[e.Name()]
				if !ok {
					t.Errorf("%s pins %s and the %s target no longer emits it for %s — a pinned file the emitter dropped is stop-the-line, not a stale pin", dir, e.Name(), unit.target, unit.dir)
					continue
				}
				pinned[e.Name()] = got
			}
			// The other way: every emitted file the writer rule pins is on
			// disk. Without this a deleted pin leaves the listing shorter
			// and the gate green. -update writes the missing pin, so the
			// documented repair (make update-goldens) repairs it.
			for name := range files {
				if _, ok := pinned[name]; ok || !tablePinRule(unit.target, name) {
					continue
				}
				if *update {
					pinned[name] = files[name]
					continue
				}
				t.Errorf("%s: the %s target emits %s for %s and no golden pins it (run: make update-goldens)", dir, unit.target, name, unit.dir)
			}
			pinDir(t, dir, pinned)
		})
	}
}

// requiredTableUnits is the set of table golden directories the writers are
// told to write: the Makefile's PINS_SOURCE_CPP_DIRS, PINS_SOURCE_C_DIRS and
// PINS_SOURCE_CS_DIRS, as `<unit>`, `<unit>-c` and `<unit>-cs`. A list that is
// missing or empty fails the test: the required set is never nothing.
func requiredTableUnits(t *testing.T, makefile string) []tableUnit {
	t.Helper()
	data, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatal(err)
	}
	units, err := tableUnitsFromMakefile(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return units
}

func tableUnitsFromMakefile(makefile string) ([]tableUnit, error) {
	var units []tableUnit
	for _, list := range []struct{ variable, suffix, target string }{
		{"PINS_SOURCE_CPP_DIRS", "", "cpp"},
		{"PINS_SOURCE_C_DIRS", "-c", "c"},
		{"PINS_SOURCE_CS_DIRS", "-cs", "cs"},
	} {
		var names []string
		found := 0
		for line := range strings.SplitSeq(makefile, "\n") {
			name, value, ok := strings.Cut(line, ":=")
			if ok && strings.TrimSpace(name) == list.variable {
				found++
				names = strings.Fields(value)
			}
		}
		if found != 1 || len(names) == 0 {
			return nil, fmt.Errorf("the Makefile assigns %s %d time(s) with %d unit(s); want one assignment naming at least one unit", list.variable, found, len(names))
		}
		for _, name := range names {
			units = append(units, tableUnit{name: name + list.suffix, dir: "../../tables/" + name, target: list.target})
		}
	}
	return units, nil
}

// tableDirProblems compares the required table golden directories with the
// ones on disk, both ways.
func tableDirProblems(required, listed []tableUnit) []string {
	want := map[string]tableUnit{}
	for _, unit := range required {
		want[unit.name] = unit
	}
	var problems []string
	seen := map[string]bool{}
	for _, unit := range listed {
		seen[unit.name] = true
		req, ok := want[unit.name]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s/%s is a directory of table goldens no writer list names (Makefile PINS_SOURCE_*_DIRS): nothing rewrites it", tableGoldenDir, unit.name))
		case req != unit:
			problems = append(problems, fmt.Sprintf("%s/%s is read as %s emitted by %s, and the writer lists make it %s emitted by %s", tableGoldenDir, unit.name, unit.dir, unit.target, req.dir, req.target))
		}
	}
	for _, unit := range required {
		if !seen[unit.name] {
			problems = append(problems, fmt.Sprintf("%s/%s is missing: the writer lists require the %s target's pins for %s (run: make update-goldens)", tableGoldenDir, unit.name, unit.target, unit.dir))
		}
	}
	sort.Strings(problems)
	return problems
}

// TestTableGoldenDirectoriesAreTheWritersLists is the control on the set: a
// whole directory of pins removed is named (the full golden set used to pass
// with testdata/golden/tables/examples-cs moved away), and so is one no list
// names. The real tree must have neither.
func TestTableGoldenDirectoriesAreTheWritersLists(t *testing.T) {
	required := requiredTableUnits(t, "../../Makefile")
	listed, err := tableUnits(tableGoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	if problems := tableDirProblems(required, listed); len(problems) != 0 {
		t.Fatalf("the tree: %q", problems)
	}
	var without []tableUnit
	for _, unit := range listed {
		if unit.name != "examples-cs" {
			without = append(without, unit)
		}
	}
	if len(without) != len(listed)-1 {
		t.Fatal("testdata/golden/tables/examples-cs is not in the listing; this control names a directory that has moved")
	}
	problems := strings.Join(tableDirProblems(required, without), "\n")
	if !strings.Contains(problems, "examples-cs is missing") {
		t.Errorf("a listing without examples-cs reports %q", problems)
	}
	extra := append(append([]tableUnit(nil), listed...), tableUnit{name: "planted", dir: "../../tables/planted", target: "cpp"})
	problems = strings.Join(tableDirProblems(required, extra), "\n")
	if !strings.Contains(problems, "planted is a directory of table goldens no writer list names") {
		t.Errorf("a listing with a planted directory reports %q", problems)
	}

	for _, c := range []struct{ name, makefile, want string }{
		{"a list removed", "PINS_SOURCE_CPP_DIRS := a\nPINS_SOURCE_C_DIRS := a\n", "PINS_SOURCE_CS_DIRS 0 time(s)"},
		{"an empty list", "PINS_SOURCE_CPP_DIRS := a\nPINS_SOURCE_C_DIRS :=\nPINS_SOURCE_CS_DIRS := a\n", "PINS_SOURCE_C_DIRS 1 time(s) with 0 unit(s)"},
		{"a list assigned twice", "PINS_SOURCE_CPP_DIRS := a\nPINS_SOURCE_CPP_DIRS := b\nPINS_SOURCE_C_DIRS := a\nPINS_SOURCE_CS_DIRS := a\n", "PINS_SOURCE_CPP_DIRS 2 time(s)"},
	} {
		if _, err := tableUnitsFromMakefile(c.makefile); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one holding %q", c.name, err, c.want)
		}
	}
	units, err := tableUnitsFromMakefile("PINS_SOURCE_CPP_DIRS := a b\nPINS_SOURCE_C_DIRS   := a\nPINS_SOURCE_CS_DIRS  := b\n")
	want := []tableUnit{{"a", "../../tables/a", "cpp"}, {"b", "../../tables/b", "cpp"}, {"a-c", "../../tables/a", "c"}, {"b-cs", "../../tables/b", "cs"}}
	if err != nil || fmt.Sprint(units) != fmt.Sprint(want) {
		t.Errorf("units %v (err %v), want %v", units, err, want)
	}
}

// tablePinRule is the writer rule of each tables target: the emitted files
// `make update-goldens` copies into testdata/golden/tables (Makefile
// pins-source-cpp, pins-source-c, pins-source-cs).
func tablePinRule(target, name string) bool {
	suffixes := map[string][]string{
		"cpp": {"Table.h", "Table.cpp"},
		"c":   {"Table.h", "Table.c", "View.h", "View.c"},
		"cs":  {"Table.cs"},
	}[target]
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// TestTablePinRuleNamesWhatTheWritersCopy is the control on tablePinRule.
func TestTablePinRuleNamesWhatTheWritersCopy(t *testing.T) {
	for _, c := range []struct {
		target, name string
		want         bool
	}{
		{"cpp", "UnitsTable.h", true}, {"cpp", "UnitsTable.cpp", true}, {"cpp", "Units.h", false},
		{"c", "UnitsTable.c", true}, {"c", "UnitsView.h", true}, {"c", "Units.c", false},
		{"cs", "UnitsTable.cs", true}, {"cs", "Units.cs", false}, {"zig", "UnitsTable.zig", false},
	} {
		if got := tablePinRule(c.target, c.name); got != c.want {
			t.Errorf("tablePinRule(%s, %s) = %v, want %v", c.target, c.name, got, c.want)
		}
	}
}

// TestGoldenTablesAreDerivedFromTheListing holds tableUnits to its rule: a
// planted directory enters the compared set (there is no list to leave it off
// of), and the units #683 found uncompared are in the real set.
func TestGoldenTablesAreDerivedFromTheListing(t *testing.T) {
	planted := t.TempDir()
	for _, name := range []string{"maps", "planted", "planted-c", "planted-cs"} {
		if err := os.Mkdir(filepath.Join(planted, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(planted, "id.txt"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := tableUnits(planted)
	if err != nil {
		t.Fatal(err)
	}
	want := []tableUnit{
		{"maps", "../../tables/maps", "cpp"},
		{"planted", "../../tables/planted", "cpp"},
		{"planted-c", "../../tables/planted", "c"},
		{"planted-cs", "../../tables/planted", "cs"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("tableUnits over a planted listing = %v, want %v", got, want)
	}
	if _, err := os.Stat("../../tables/planted"); err == nil {
		t.Fatal("tables/planted exists — the planted directory has a unit behind it and this test no longer plants anything")
	}

	real, err := tableUnits(tableGoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"maps", "arms", "lists"} {
		found := false
		for _, unit := range real {
			if unit.name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("testdata/golden/tables/%s is not in the compared set — #683's uncompared unit is uncompared again", name)
		}
	}
}

// pinnedUnits is every compilation unit this package pins by the name its
// golden files are stored under: the two corpora, then every tables unit
// the goldens listing names (tableUnits), once each. The C and C# directories
// are the SAME unit emitted by another target, so those fold into the unit
// already named — excluded explicitly here, not by a
// list that has to remember them.
func pinnedUnits(t *testing.T) []struct{ name, dir string } {
	t.Helper()
	units := []struct{ name, dir string }{
		{"examples", corpusDir},
		{"examples128", corpus128Dir},
	}
	tables, err := tableUnits(tableGoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range tables {
		if unit.target != "cpp" {
			continue
		}
		units = append(units, struct{ name, dir string }{"tables-" + unit.name, unit.dir})
	}
	return units
}

// TestWireLawBumpMovesEveryId holds the promise the codec law line is for
// (SPEC §3.1): a compiler change that moves the BYTES under an unchanged
// rendering bumps ir.WireLaw, and EVERY id in existence moves with it. The
// constant cannot be changed from a test, so the equivalent is proven over the
// artifact the id is taken from: for every unit here the id is exactly the
// digest over its projection text, and the same text under the next law
// number digests differently. No unit can sit out a bump.
func TestWireLawBumpMovesEveryId(t *testing.T) {
	law := fmt.Sprintf("schema-wire-law %d\n", ir.WireLaw)
	next := fmt.Sprintf("schema-wire-law %d\n", ir.WireLaw+1)
	for _, unit := range pinnedUnits(t) {
		t.Run(unit.name, func(t *testing.T) {
			u := loadCorpusDir(t, unit.dir)
			text := ir.WireProjection(u)
			if !strings.Contains(text, law) {
				t.Fatalf("%s does not carry the codec law line — a rounding-rule change could not reach its id", unit.name)
			}
			if got := digest(text); got != u.ProtocolId {
				t.Fatalf("%s: the id is not the digest over its projection (0x%016x vs 0x%016x) — §3.1's procedure has moved", unit.name, u.ProtocolId, got)
			}
			if digest(strings.Replace(text, law, next, 1)) == u.ProtocolId {
				t.Errorf("%s kept its id across a codec law bump", unit.name)
			}
		})
	}
}

// digest is §3.1's procedure: the low 64 bits of SHA-256 over the projection,
// the final eight bytes big-endian.
func digest(projection string) uint64 {
	sum := sha256.Sum256([]byte(projection))
	return binary.BigEndian.Uint64(sum[24:])
}

// TestGoldenBuildVersion pins the BUILD VERSION and the COOK PROJECTION it
// hashes, per unit (docs/SPEC-TABLES.md §20.8). The number is what a distributed
// store's tuple is keyed by and what a block's prologue carries, so a change
// to how it is computed has to break every pinned value loudly — and the TEXT
// is pinned beside it, so a port reproduces the projection and not only the
// number.
//
// A unit that declares no table is pinned too: its projection is its three
// header lines alone, which is the case a reader has to be able to check by
// eye.
func TestGoldenBuildVersion(t *testing.T) {
	for _, unit := range pinnedUnits(t) {
		t.Run(unit.name, func(t *testing.T) {
			u := loadCorpusDir(t, unit.dir)
			got := fmt.Sprintf("0x%016x\n%s", ir.BuildVersion(u), ir.CookProjection(u))
			path := filepath.Join(goldenDir, "build-version", unit.name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden build version (run: make update-goldens): %v", err)
			}
			if got != string(want) {
				t.Errorf("the build version or its cook projection moved for %s — if the schema files changed this is expected once (re-pin deliberately); if they did not, §20.2's procedure changed and that is stop-the-line", unit.name)
			}
		})
	}
}

// TestUsagePageBuildVersion holds the ONE build version docs/USAGE.md prints.
// The page shows a worked `schema build-version tables/block/` run, and a
// number pasted into prose moves with nothing — a reader who runs the command
// beside the page has to get the page's answer, so the page is read back here
// and compared against the unit itself.
func TestUsagePageBuildVersion(t *testing.T) {
	const command = "$ schema build-version tables/block/"
	page, err := os.ReadFile("../../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(page), "\n")
	printed := ""
	for i, line := range lines {
		if strings.TrimSpace(line) == command && i+1 < len(lines) {
			printed = strings.TrimSpace(lines[i+1])
			break
		}
	}
	if printed == "" {
		t.Fatalf("docs/USAGE.md no longer shows a %q run with its answer beneath it — this gate names a line that has moved", command)
	}
	want := fmt.Sprintf("0x%016x", ir.BuildVersion(loadCorpusDir(t, "../../tables/block")))
	if printed != want {
		t.Errorf("docs/USAGE.md prints %s for tables/block and the unit's build version is %s — paste the current one", printed, want)
	}
}
