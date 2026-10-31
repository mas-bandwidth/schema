package luatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE CALLER-OWNED LOAD, MEASURED (docs/PORTING.md M1, I1, I9, I14). A load
// fills storage the caller owns: the caller keeps a loader state
// (load.NewState: the readers, the id table, the queue of pointer slots) and
// the value (`into`, put back to its defaults in place and read into) and hands
// both to each load. The instrument is the VM's own allocation counter,
// collectgarbage("count"), read with the collector stopped over a steady phase
// (after a collection and a warm-up that re-interns the strings the rows hold),
// so a load's allocation is exact bytes and a path that allocates and frees the
// same bytes every iteration still counts.
//
// What a Lua load can allocate nothing for is its whole tree: a counted or
// unbounded array, a list, a map and a union arm are tables the load builds,
// and a pointer's node is a table, a boxed blob is a table, a wide integer is
// a table and a string past 40 bytes is an object. Each row says which of those
// it licenses: a row licensing nothing is held to EXACT ZERO, and a row that
// licenses something to a pinned ceiling a hair above what it measured.

type allocRow struct {
	name    string
	unit    string
	root    string
	wire    string
	ceiling float64 // bytes per load
	licence string  // what the row licenses; empty when the ceiling is zero
}

var allocRows = []allocRow{
	{"by-value record, keyed arrays, strings", "tabledemo", "KeyedConfig", "keyed_config", 0, ""},
	{"scalars and strings", "backenddemo", "LoginRequest", "login_full", 0, ""},
	{"nested records", "backenddemo", "MatchResult", "match_full", 0, ""},
	{"a variable root that reads nothing", "tabledemo", "RootConfig", "root_default", 0, ""},
	{"counted arrays of records, union arms", "tabledemo", "RootConfig", "root_full", 4096,
		"one table per counted element, per array and per union arm"},
	{"pointer nodes and slots", "graphdemo", "Scene", "graph_tree", 3840,
		"one table per node, the node records and the boxes"},
	{"a list of records", "listdemo", "Save", "list_tables", 1536, "one table per list and per element"},
}

// allocProbe is the Lua main of one unit's rows: row i is inputs.wI under the
// root named, loaded N times through one state into one value.
func allocProbe(module string, rows []allocRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "local T = require(%q)\n", module)
	b.WriteString(`local load = require("load")
local inputs = require("inputs")
local N = 100
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local rows = {
`)
	for i, r := range rows {
		fmt.Fprintf(&b, "  { %q, \"w%d\" },\n", r.root, i+1)
	}
	b.WriteString(`}
for i, row in ipairs(rows) do
  local root, wire = row[1], inputs[row[2]]
  local rep, state, into = report(), load.NewState(), T["New" .. root]()
  assert(load.Load(T, root, wire, rep, state, into) ~= nil, root .. " does not load: " .. tostring(rep.reason))
  collectgarbage("collect")
  for _ = 1, 20 do load.Load(T, root, wire, rep, state, into) end
  collectgarbage("stop")
  local before = collectgarbage("count")
  for _ = 1, N do load.Load(T, root, wire, rep, state, into) end
  local after = collectgarbage("count")
  collectgarbage("restart")
  print(i, string.format("%.1f", (after - before) * 1024 / N))
end
`)
	return b.String()
}

// lastRowsByUnit groups the rows by unit, remembering each row's place.
func rowsByUnit() (units []string, byUnit map[string][]int) {
	byUnit = map[string][]int{}
	for i, r := range allocRows {
		if _, ok := byUnit[r.unit]; !ok {
			units = append(units, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], i)
	}
	return units, byUnit
}

// measureAllocRows runs every row, the files of each unit possibly patched,
// and answers bytes per load by row.
func measureAllocRows(t *testing.T, patch func(unit string) map[string][]byte) []float64 {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	out := make([]float64, len(allocRows))
	units, byUnit := rowsByUnit()
	for _, unit := range units {
		b := buildManifestUnit(t, unit)
		files := b.files
		if patch != nil {
			if p := patch(unit); p != nil {
				files = p
			}
		}
		var rows []allocRow
		inputs := map[string][]byte{}
		for k, idx := range byUnit[unit] {
			r := allocRows[idx]
			rows = append(rows, r)
			data, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", r.wire+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			inputs["w"+strconv.Itoa(k+1)] = data
		}
		stdout, stderr, err := try(t, files, allocProbe(b.module, rows), inputs)
		if err != nil {
			t.Fatalf("unit %s: %v\n%s", unit, err, stderr)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
			var k int
			var bytes float64
			if _, err := fmt.Sscanf(line, "%d %f", &k, &bytes); err != nil {
				t.Fatalf("unit %s: probe line %q", unit, line)
			}
			out[byUnit[unit][k-1]] = bytes
		}
	}
	return out
}

// THE PINNED RUNTIME (docs/PORTING.md I14). A floor measured on whatever
// binary a PATH lookup found says nothing about the runtime the claim is for:
// the gate reads the interpreter's own version first and, on any other than the
// pinned one, refuses before it measures. SCHEMA_LUA_ALLOC_ANY_LUA=1 prints the
// rows under NOT CERTIFIED and never asserts them.
func allocPinRefusal(have, want string) error {
	if have == want {
		return nil
	}
	return fmt.Errorf("the allocation gate refuses to certify: the interpreter is %q and the pin is %q (make dist-lua); nothing was measured", have, want)
}

func interpreterVersion(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(luarun.Interpreter(t), "-v").Output()
	if err != nil {
		t.Fatalf("lua -v: %v", err)
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		t.Fatalf("lua -v printed %q", out)
	}
	return f[1]
}

// pinned answers whether the gate may certify; off the pin it refuses unless
// the escape hatch is set, in which case it says it does not certify.
func pinned(t *testing.T) bool {
	t.Helper()
	if err := allocPinRefusal(interpreterVersion(t), luarun.PinnedVersion); err != nil {
		if os.Getenv("SCHEMA_LUA_ALLOC_ANY_LUA") == "" {
			t.Fatal(err)
		}
		t.Logf("NOT CERTIFIED: %v", err)
		return false
	}
	return true
}

func TestLuaLoadAllocationFloor(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	certify := pinned(t)
	got := measureAllocRows(t, nil)
	for i, r := range allocRows {
		licence := "licenses nothing"
		if r.ceiling > 0 {
			licence = "licenses " + r.licence
		}
		t.Logf("%-42s %9.1f bytes a load (ceiling %.0f, %s)", r.name, got[i], r.ceiling, licence)
		if certify && got[i] > r.ceiling {
			t.Errorf("row %q allocates %.1f bytes a load against a ceiling of %.0f: a read that fills caller-owned storage allocates only what the row licenses (docs/PORTING.md M1)", r.name, got[i], r.ceiling)
		}
	}
}

// TestLuaLoadAllocationRefusesOffThePin is I14's negative control: another
// version must refuse, naming both, and before anything is measured.
func TestLuaLoadAllocationRefusesOffThePin(t *testing.T) {
	t.Parallel()
	for _, have := range []string{"5.4.7", "5.3.6", "5.5.0"} {
		err := allocPinRefusal(have, luarun.PinnedVersion)
		if err == nil || !strings.Contains(err.Error(), have) || !strings.Contains(err.Error(), luarun.PinnedVersion) || !strings.Contains(err.Error(), "nothing was measured") {
			t.Errorf("interpreter %s: the gate did not refuse by name: %v", have, err)
		}
	}
	if err := allocPinRefusal(luarun.PinnedVersion, luarun.PinnedVersion); err != nil {
		t.Errorf("the pinned interpreter was refused: %v", err)
	}
}

// TestLuaLoadAllocationNegativeControl plants one allocation per record, in two
// places, and localizes it: a planted string in the pointer queue turns the
// rows that queue a slot and no other (the pointer row and the list row), one
// in the array reader turns the rows that read an array and no other (the
// pointer row and the empty root stay green).
func TestLuaLoadAllocationNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	if !pinned(t) {
		return
	}
	clean := measureAllocRows(t, nil)
	plants := []struct {
		name, anchor, plant string
		red                 []string
	}{
		{"an allocation per queued pointer slot",
			"local function queue(ctx, holder, key, index, d, null)\n  local n = ctx.np + 1\n",
			"local function queue(ctx, holder, key, index, d, null)\n  local _ = string.rep(\"q\", 1000 + ctx.np)\n  local n = ctx.np + 1\n",
			[]string{"pointer nodes and slots", "a list of records"}},
		{"an allocation per array body",
			"array_elements = function(T, ctx, fd, sub, ek, count, prior, reuse)\n  local report = ctx.report\n",
			"array_elements = function(T, ctx, fd, sub, ek, count, prior, reuse)\n  local _ = string.rep(\"a\", 2000)\n  local report = ctx.report\n",
			[]string{"by-value record, keyed arrays, strings", "scalars and strings", "nested records", "counted arrays of records, union arms", "a list of records"}},
	}
	for _, p := range plants {
		got := measureAllocRows(t, func(unit string) map[string][]byte {
			return buildManifestUnit(t, unit).patched(t, "load.lua", p.anchor, p.plant)
		})
		want := map[string]bool{}
		for _, r := range p.red {
			want[r] = true
		}
		for i, r := range allocRows {
			red := got[i] > r.ceiling
			if want[r.name] && !red {
				t.Errorf("%s: row %q stayed green (%.1f bytes a load, ceiling %.0f, clean %.1f)", p.name, r.name, got[i], r.ceiling, clean[i])
			}
			if !want[r.name] && red {
				t.Errorf("%s: row %q turned (%.1f bytes a load) and is not this plant's", p.name, r.name, got[i])
			}
		}
	}
}

// THE READER IS A CURSOR AND A LIMIT (docs/PORTING.md M1). A nested body, an
// arm, an array body, a map entry and a keyed slot are bounded by narrowing the
// one reader's limit and putting it back (load.lua narrow and restore), never
// by building a view: a sub-view is a table, and the limit is an integer. The
// body of the read, from its forward declarations to the pointer resolution,
// builds no reader.
func readWalk(src string) (string, bool) {
	a := strings.Index(src, "local read_body\n")
	b := strings.Index(src, "-- RESOLUTION (§3.1, decodenodes.go resolveCell)")
	if a < 0 || b < a {
		return "", false
	}
	return src[a:b], true
}

func TestLuaLoadNarrowsTheReaderInPlace(t *testing.T) {
	t.Parallel()
	src, err := runtime.ReadFile("runtime/load.lua")
	if err != nil {
		t.Fatal(err)
	}
	walk, ok := readWalk(string(src))
	if !ok {
		t.Fatal("the read walk is not where the gate looks for it")
	}
	if n := strings.Count(walk, "new_reader("); n != 0 {
		t.Errorf("the read walk builds %d readers: a nested body is bounded by narrowing the limit", n)
	}
	if !strings.Contains(walk, "narrow(") || !strings.Contains(walk, "restore(") {
		t.Error("the read walk narrows no limit")
	}
	// the reversed witness: a sub-view planted into the nested body's read
	planted := strings.Replace(string(src), "    local np0 = ctx.np\n    local at0 = r.at\n    local lim, bad = narrow(r, n)\n",
		"    local np0 = ctx.np\n    local at0 = r.at\n    local view = new_reader(r.b, r.at, r.at + n)\n    local lim, bad = narrow(r, n)\n", 1)
	if planted == string(src) {
		t.Fatal("the plant's anchor is absent")
	}
	if w, _ := readWalk(planted); !strings.Contains(w, "new_reader(") {
		t.Error("the scan did not see the planted sub-view")
	}
}
