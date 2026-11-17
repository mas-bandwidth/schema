package luautable_test

// THE PINNED WIRE-FUZZ VECTORS AT VARIABLE ROOTS (docs/SPEC-TABLES.md §4.2,
// §6.5). Each vector is replayed through the real Luau wire-fuzz driver and
// held to the Go engine's answer on the same bytes. The roots are the variable
// class, so the named rule is the one the read meets:
//
//   - region_align_extent_uint128 is ACCEPTED and its measured region is part
//     of the answer: the harness compares the driver's LoadMeasure with the
//     engine's FileRegionMeasure, so a wrong region size is a divergence even
//     when both sides accept the value;
//   - keyed_header_count_past_length reaches the keyed body's count, whose
//     spelling runs past its L, and the whole load refuses;
//   - text_ill_formed_repeat_reads_default repeats a text field with an
//     ill-formed payload, reads its declared default, and the whole load
//     refuses because damage discards it.
//
// TestLuauWireFuzzVectorsReachTheirFault requires every clean replay to agree
// with the engine. TestLuauWireFuzzVectorsReachTheirFaultNegativeControl plants
// out the check each vector pins and requires that vector's answer to move: a
// wrong measure for the region vector, and the tolerant read for the two
// refusals.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luauVectorFaultCase struct {
	name    string
	unitKey string
	root    string
	file    string
	// measure marks an accepted vector whose measured region is compared.
	measure bool
	// plantFiles rewrites the emitted load; plantDriver rewrites the driver.
	plantFiles  func(t *testing.T, files map[string][]byte)
	plantDriver func(t *testing.T, src string) string
}

var luauVectorFaultCases = []luauVectorFaultCase{
	{
		name:    "region_align_extent_uint128",
		unitKey: "widelist",
		root:    "Wide",
		file:    "testdata/wire/tables/fuzz-vectors/region_align_extent_uint128.bin",
		measure: true,
		plantDriver: func(t *testing.T, src string) string {
			t.Helper()
			const anchor = "\tif not ok or total == nil then return nil end\n"
			if n := strings.Count(src, anchor); n != 1 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the measure anchor appears %d times in the driver, want 1", n)
			}
			return strings.Replace(src, anchor, anchor+"\ttotal = total + 8 -- PLANTED\n", 1)
		},
	},
	{
		name:       "keyed_header_count_past_length",
		unitKey:    "mapdemo",
		root:       "Depth",
		file:       "testdata/wire/tables/fuzz-vectors/keyed_header_count_past_length.bin",
		plantFiles: luauVectorTolerantRead,
	},
	{
		name:       "text_ill_formed_repeat_reads_default",
		unitKey:    "tblw1",
		root:       "Fleet",
		file:       "testdata/wire/tables/fuzz-vectors/text_ill_formed_repeat_reads_default.bin",
		plantFiles: luauVectorTolerantRead,
	},
}

// luauVectorBatchUnit is the batch probe's unit record. The driver's pipe
// computes the root's alignment and enables region measurement; the batch probe
// is given the same shape here, because this harness compares the measure.
const luauVectorBatchUnit = `local u: Unit = { S = require("./schema_tables"), load = require("./schema_load"), save = require("./schema_save"), align = 8 }`

// luauVectorDriver reads the committed driver and, for an accepted region
// vector, gives the batch probe the alignment and the measure the pipe arm
// computes.
func luauVectorDriver(t *testing.T, measure bool, align int64) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "test", "conformance", "luau", "wirefuzz.luau"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if measure {
		if n := strings.Count(src, luauVectorBatchUnit); n != 1 {
			t.Fatalf("the batch unit record appears %d times in the driver, want 1", n)
		}
		repl := fmt.Sprintf(`local u: Unit = { S = require("./schema_tables"), load = require("./schema_load"), save = require("./schema_save"), align = %d, measure = true }`, align)
		src = strings.Replace(src, luauVectorBatchUnit, repl, 1)
	}
	return src
}

// luauVectorTolerantRead restores the tolerant read the strict cut removed: the
// wire-alone damage no longer refuses, the refusing counters no longer refuse,
// and a malformed walk returns its value. On a repeated text field, the second
// ill-formed occurrence then reads its declared default instead of discarding
// the load.
func luauVectorTolerantRead(t *testing.T, files map[string][]byte) {
	t.Helper()
	src, ok := files["schema_load.luau"]
	if !ok {
		t.Fatal("the emitted unit has no schema_load.luau")
	}
	s := string(src)
	reps := [][2]string{
		{"local damaged = wireDamage(b, top, fr.ids, rep)", "local damaged = false"},
		{"for _, k in REFUSING do", "for _, k in ({}) do"},
		{"\tif report.malformed then return nil end\n\treturn v", "\tif false then return nil end\n\treport.malformed = false\n\treturn v"},
	}
	for _, r := range reps {
		if n := strings.Count(s, r[0]); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the plant anchor %q appears %d times in schema_load.luau, want 1", r[0], n)
		}
		s = strings.Replace(s, r[0], r[1], 1)
	}
	files["schema_load.luau"] = []byte(s)
}

// luauVectorUnit loads one manifest unit.
func luauVectorUnit(t *testing.T, key string) *ir.Unit {
	t.Helper()
	units, _ := readManifest(t)
	paths, ok := units[key]
	if !ok {
		t.Fatalf("the manifest declares no unit %s", key)
	}
	schemaPaths, err := compiler.GatherPaths(prefix(paths))
	if err != nil {
		t.Fatalf("unit %s: %v", key, err)
	}
	u, err := compiler.New().Load(schemaPaths)
	if err != nil {
		t.Fatalf("unit %s does not load: %v", key, err)
	}
	return u
}

// luauVectorRun stages the unit and the driver, replays one vector, and returns
// the leg's reply beside the engine's answer.
func luauVectorRun(t *testing.T, u *ir.Unit, c luauVectorFaultCase, wire []byte, driver string, edit func(map[string][]byte)) (luauFuzzAnswer, luauFuzzOracle) {
	t.Helper()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", c.unitKey, err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", c.unitKey, err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup(c.root)
	if def == nil {
		t.Fatalf("unit %s declares no table %s", c.unitKey, c.root)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "wirefuzz.luau"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	var mods []string
	for name := range files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	g := &luauFuzzGroup{
		unit:    c.unitKey,
		root:    c.root,
		dir:     dir,
		m:       m,
		def:     def,
		mods:    mods,
		mutants: []luauFuzzMutant{{caseName: c.name, wire: wire}},
	}
	lua, why := luaurun.Interpreter()
	if lua == "" {
		t.Fatalf("luaurun: the Luau interpreter does not resolve: %s", why)
	}
	out, err := luauFuzzRun(lua, g, &luauFuzzChunk{n: 1, mutants: g.mutants})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the probe answered %d lines for one vector:\n%s", len(lines), out)
	}
	ans, err := parseLuauFuzzLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	return ans, luauFuzzEngine(m, def, wire)
}

// luauVectorVerdict is the harness's verdict plus the measured size, which the
// wire-fuzz verdict does not carry.
func luauVectorVerdict(t *testing.T, u *ir.Unit, c luauVectorFaultCase, wire []byte, ans luauFuzzAnswer, o luauFuzzOracle) string {
	t.Helper()
	if v := luauFuzzVerdict(ans, o); v != "" {
		return v
	}
	if c.measure && ans.loaded && !ans.refused && !ans.counts.malformed {
		want, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[c.root], wire)
		if refused || !whole {
			return fmt.Sprintf("the engine could not measure the accepted wire: %d whole=%v refused=%v", want, whole, refused)
		}
		if ans.measure != want {
			return fmt.Sprintf("the measured size differs: Luau says %d, the engine says %d", ans.measure, want)
		}
	}
	return ""
}

// TestLuauWireFuzzVectorsReachTheirFault replays each pinned vector at its
// variable root and holds the reply to the engine, the measured region included.
func TestLuauWireFuzzVectorsReachTheirFault(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luauFuzzCost+" over the pinned vectors at variable roots")
	for _, c := range luauVectorFaultCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := luauVectorUnit(t, c.unitKey)
			wire, err := os.ReadFile(filepath.Join(root, c.file))
			if err != nil {
				t.Fatal(err)
			}
			driver := luauVectorDriver(t, c.measure, ir.TableRegionAlign(u))
			ans, o := luauVectorRun(t, u, c, wire, driver, nil)
			if v := luauVectorVerdict(t, u, c, wire, ans, o); v != "" {
				t.Fatalf("%s.%s: %s", c.unitKey, c.root, v)
			}
			if c.measure {
				if !ans.loaded || ans.refused || ans.counts.malformed || ans.measure < 0 {
					t.Fatalf("%s: the accepted vector did not read with a measure: %+v", c.name, ans)
				}
				return
			}
			if ans.loaded {
				t.Fatalf("%s: the vector was not refused", c.name)
			}
		})
	}
}

// TestLuauWireFuzzVectorsReachTheirFaultNegativeControl plants out the check
// each vector pins and requires that vector's answer to move: the region
// vector's measure, and the two refusals' strict read.
func TestLuauWireFuzzVectorsReachTheirFaultNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luauFuzzCost+" over the pinned vectors' negative control")
	for _, c := range luauVectorFaultCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := luauVectorUnit(t, c.unitKey)
			wire, err := os.ReadFile(filepath.Join(root, c.file))
			if err != nil {
				t.Fatal(err)
			}
			driver := luauVectorDriver(t, c.measure, ir.TableRegionAlign(u))
			if c.plantDriver != nil {
				driver = c.plantDriver(t, driver)
			}
			var edit func(map[string][]byte)
			if c.plantFiles != nil {
				edit = func(files map[string][]byte) { c.plantFiles(t, files) }
			}
			ans, o := luauVectorRun(t, u, c, wire, driver, edit)
			v := luauVectorVerdict(t, u, c, wire, ans, o)
			if v == "" {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s check removed left its vector green", c.name)
			}
			if c.measure {
				if !strings.Contains(v, "measured size differs") {
					t.Fatalf("NEGATIVE CONTROL FAILED: the %s plant went red without comparing the measure: %s", c.name, v)
				}
				return
			}
			if !ans.loaded {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s plant still refuses the wire: %s", c.name, v)
			}
		})
	}
}
