package moo_test

// THE FIXED FORM'S HOT-PATH EVIDENCE for the one-read cut of
// runtime/table_fixed.moo (docs/plans/strict-only.md): a bool and a present
// byte are checked 0-or-1, a ranged value refuses instead of clamping, an
// ordinal or tag past the writer's set refuses. The load and the save of the
// bench's own fixed corpus are timed against the runtime before the cut
// (testdata/table_fixed_before.moo, sha256 pinned below), interleaved pairs,
// median ns per record. moo has no clock (it is deterministic), so each run
// times the interpreter process from Go and subtracts a run of zero
// iterations. A functional bench, never a unit test: it uses real time, and it
// runs only on a quiet fleet bench with SCHEMA_MOO_FIXED_BENCH=1 set:
//
//   SCHEMA_SLOW=1 SCHEMA_MOO_FIXED_BENCH=1 go test -count=1 -v -timeout 900s \
//     -tags schema_leg_moo ./bench/tables/moo/ \
//     -run '^TestMooFixedHotPathBench$'
//
// THE WORKLOAD IS THE BENCH'S CORPUS, not a probe written down here. The unit
// is generated from bench/corpus/Bench.schema and the paired fixed-form
// wrapper schema, and the timed file is bench/paired/corpus/bench_fixed.bin — the C++
// reference's form-3 save of the paired corpus's sixty-four logical records.
// No field, pinned value or record batch is written down in this file, so it
// is a runner of the sanctioned benchmark rather than a second, hand-written
// measurement of a schema shape (bench/README.md, "The shape gate").

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// benchCorpus is the paired bench corpus's fixed form: the same sixty-four
// logical records the packet and tolerant-wire passes measure, written by the
// C++ reference (test/bench/fixedform_corpus.cpp).
const benchCorpus = "../../paired/corpus/bench_fixed.bin"

const benchBefore = "5c18749305062aade552862f1d1745df172a923adeaf11810197839555bad290"

// generatedUnit is one emitted unit and the directory it lives in.
type generatedUnit struct {
	dir   string
	files map[string][]byte
}

// writeUnit writes an emitted unit into a fresh directory.
func writeUnit(t *testing.T, files map[string][]byte) *generatedUnit {
	t.Helper()
	g := &generatedUnit{dir: t.TempDir(), files: files}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// unitFromCorpus loads the bench's own fixed-form unit.
func unitFromCorpus(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{
		filepath.Join("..", "..", "corpus", "Bench.schema"),
		filepath.Join("..", "..", "corpus", "FixedTable.schema"),
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the bench corpus does not load: %v", err)
	}
	return u
}

// fixedUnit generates a unit for moo with a handed lineage and writes it.
func fixedUnit(t *testing.T, u *ir.Unit) *generatedUnit {
	t.Helper()
	files, err := moo.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := mootable.GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return writeUnit(t, files)
}

// requireAll is the moo lines that load every table module (registering the
// descriptors).
func (g *generatedUnit) requireAll() string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.moo") {
			mods = append(mods, strings.TrimSuffix(name, ".moo"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	return b.String()
}

// benchProgram loads (or saves) the corpus's batch `iters` times. The root
// descriptor is discovered from the generated module rather than named here.
func benchProgram(g *generatedUnit, op string, iters int) string {
	return "--!strict\nlocal F = require(\"./table_fixed\")\n" + g.requireAll() + fmt.Sprintf(`local T = require("./FixedTableTable")
local d: any, fixed: any
for name, value in pairs(T) do
	if string.sub(name, -10) == "Descriptor" then
		d = value
	elseif string.sub(name, -5) == "Fixed" then
		fixed = value
	end
end
local values = F.load(d, fixed, inputs.file) :: { any }
if values == nil then
	print("the bench corpus does not load")
	return
end
local total = 0
for _ = 1, %d do
	if %q == "load" then
		local loaded = F.load(d, fixed, inputs.file) :: { any }
		if loaded == nil then
			print("the bench corpus does not load")
			return
		end
		total += #loaded
	else
		local out = F.save(d, fixed, values) :: buffer
		total += buffer.len(out)
	end
end
print(total)
`, iters, op)
}

func benchRun(t *testing.T, g *generatedUnit, op string, iters int, inputs map[string][]byte) time.Duration {
	t.Helper()
	main := "bench_" + op + fmt.Sprint(iters) + ".moo"
	if err := os.WriteFile(filepath.Join(g.dir, main), []byte(benchProgram(g, op, iters)), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	moorun.Run(t, g.dir, main, inputs)
	return time.Since(start)
}

func benchMedian(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func TestMooFixedHotPathBench(t *testing.T) {
	slowtest.Gate(t, "the pinned moo interpreter, timed (a fleet bench only)")
	if os.Getenv("SCHEMA_MOO_FIXED_BENCH") == "" {
		t.Skip("SCHEMA_MOO_FIXED_BENCH unset: a timed run belongs on a quiet fleet bench")
	}
	before, err := os.ReadFile("testdata/table_fixed_before.moo")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(before)); got != benchBefore {
		t.Fatalf("the baseline is not the runtime before the cut: sha256=%s", got)
	}
	corpus, err := os.ReadFile(benchCorpus)
	if err != nil {
		t.Fatalf("the paired bench corpus is absent (%s): %v", benchCorpus, err)
	}
	u := unitFromCorpus(t)
	gb, ga := fixedUnit(t, u), fixedUnit(t, u)
	if err := os.WriteFile(filepath.Join(gb.dir, "table_fixed.moo"), before, 0o644); err != nil {
		t.Fatal(err)
	}
	const iters, pairs, records = 2000, 8, 64
	inputs := map[string][]byte{"file": corpus}
	for _, op := range []string{"load", "save"} {
		per := func(g *generatedUnit) float64 {
			d := benchRun(t, g, op, iters, inputs) - benchRun(t, g, op, 0, inputs)
			return float64(d.Nanoseconds()) / float64(iters*records)
		}
		var b, a []float64
		for i := range pairs {
			if i%2 == 0 {
				b, a = append(b, per(gb)), append(a, per(ga))
			} else {
				a, b = append(a, per(ga)), append(b, per(gb))
			}
		}
		mb, ma := benchMedian(b), benchMedian(a)
		t.Logf("%s ns/record: before %.0f [%.0f, %.0f] -> after %.0f [%.0f, %.0f]  %+.2f%%",
			op, mb, benchRange(b)[0], benchRange(b)[1], ma, benchRange(a)[0], benchRange(a)[1], 100*(ma-mb)/mb)
	}
}

// benchRange answers the min and max of xs.
func benchRange(xs []float64) [2]float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return [2]float64{s[0], s[len(s)-1]}
}
