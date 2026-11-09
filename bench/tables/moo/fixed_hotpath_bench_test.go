package moo_test

// THE FIXED FORM'S HOT-PATH EVIDENCE for the one-read cut of
// runtime/table_fixed.moo (docs/plans/strict-only.md): a bool and a present
// byte are checked 0-or-1, a ranged value refuses instead of clamping, an
// ordinal or tag past the writer's set refuses. The load and the save of a
// representative record are timed against the runtime before the cut
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
// The measurement lives in the moo runner directory (bench/tables/moo) so the
// shape gate sees it as the sanctioned runner machinery it is, not a second
// benchmark parked under the compiler internals. It keeps the card
// moo-bench-fixed-hot-path's owed before/after numbers reproducible.

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

const benchSchema = `package probe
enum Tier { Bronze, Silver, Gold }
union Pick
{
    a int32
    b uint16
}
fixed table Rec
{
    flag  bool
    live  bool
    hp    int32 = 50 | min = 0, max = 100
    tier  Tier
    pick  Pick
    opt   ?uint16 | min = 0, max = 9000
    marks [..8]int32 | min = -1000, max = 1000
    name  string(16)
    seq   uint64
}
`

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

// unitFromSource loads one schema source as a unit.
func unitFromSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
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

// benchProgram loads (or saves) a batch of records `iters` times.
func benchProgram(g *generatedUnit, op string, iters int) string {
	return "--!strict\nlocal F = require(\"./table_fixed\")\n" + g.requireAll() + fmt.Sprintf(`local T = require("./ProbeTable")
local d, fixed = T.RecDescriptor, T.RecFixed
local values: { any } = {}
for i = 1, 64 do
	local v = T.NewRec()
	v.Flag = true
	v.Hp = 70
	v.Tier = 2
	v.Pick.Type = 1
	v.Pick.A = i
	v.OptPresent = true
	v.Opt = 42
	v.Marks = { 1, -2, 3, -4, 5, 0, 0, 0 }
	v.MarksCount = 5
	v.Name = "player"
	values[i] = v
end
local file = F.save(d, fixed, values) :: buffer
local n = 0
for _ = 1, %[2]d do
	if %[1]q == "load" then
		n += #(F.load(d, fixed, file) :: { any })
	else
		n += buffer.len(F.save(d, fixed, values) :: buffer)
	end
end
print(n)
`, op, iters)
}

func benchRun(t *testing.T, g *generatedUnit, op string, iters int) time.Duration {
	t.Helper()
	main := "bench_" + op + fmt.Sprint(iters) + ".moo"
	if err := os.WriteFile(filepath.Join(g.dir, main), []byte(benchProgram(g, op, iters)), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	moorun.Run(t, g.dir, main, nil)
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
	u := unitFromSource(t, benchSchema)
	gb, ga := fixedUnit(t, u), fixedUnit(t, u)
	if err := os.WriteFile(filepath.Join(gb.dir, "table_fixed.moo"), before, 0o644); err != nil {
		t.Fatal(err)
	}
	const iters, pairs, records = 2000, 8, 64
	for _, op := range []string{"load", "save"} {
		per := func(g *generatedUnit) float64 {
			d := benchRun(t, g, op, iters) - benchRun(t, g, op, 0)
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
