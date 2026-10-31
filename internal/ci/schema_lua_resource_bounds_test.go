package ci

// Bounded memory and time on hostile inputs for the Lua port of schema
// (docs/SPEC-TABLES.md §16.9.4 and docs/PORT-LUA.md, THE REGISTER: the
// allocation gate's instrument is collectgarbage("count")). The generated Lua
// textform reader is run under the pinned PUC-Rio Lua 5.4.8 interpreter on
// hostile documents: huge counts, deep nesting, recursion, giant strings and
// many unknown fields. Each shape must refuse by the documented name, allocate
// at most F times its input plus a fixed slack, and cost a number of VM
// instructions linear in the input (debug.sethook's count hook), never wall
// clock.
//
// The allocation counter is Lua's own GC memstat, collectgarbage("count"),
// the nearest counting allocator the language offers (the VM owns allocation;
// docs/PORT-LUA.md names it as the instrument). TestLuaAllocationCounterCounts
// proves it counts by planting an unbounded allocation. The planted-pattern
// control relaxes the reader's depth cap in a copy of the generated runtime
// and requires the deep shape to stop refusing as text_limit: red with the
// plant in, green with it out.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// luaAllocationFactorF is the proportional budget the resource
// lens documents: a read allocates at most F times the input size. §16.9.4's
// limits are the absolute cap; this factor is the one that scales with input,
// and it is deliberately far above the reader's real cost.
const luaAllocationFactorF = 256.0

// luaAllocationSlackKB is the fixed overhead above F*input, in KB: the frozen
// runtime tables and one refusal's own tables are not chargeable per read.
const luaAllocationSlackKB = 1024.0

// luaResourceBoundsSchema is the unit every hostile shape is read against: a
// variable table with a bounded integer, a large declared array and a
// self-pointer (the recursion shape).
const luaResourceBoundsSchema = `package probe

table H
{
    n int32 | min = 0, max = 100000
    big [200000]int32
    next *H
}
`

// luaRecursionDoc is a 132-deep pointer chain, each node labelled as the text
// form requires for a shared value: the parser descends one frame per node.
func luaRecursionDoc() string {
	var b strings.Builder
	for i := 1; i <= 132; i++ {
		fmt.Fprintf(&b, `("&node" %d :next `, i)
	}
	b.WriteString(`("&node" 133)`)
	b.WriteString(strings.Repeat(")", 132))
	return b.String()
}

// luaResourceBoundsDocs is one document per hostile dimension.
func luaResourceBoundsDocs() map[string]string {
	return map[string]string{
		// deep nesting: more open lists than the depth cap of 128; the reader
		// must refuse as text_limit before it descends.
		"deep-nesting": strings.Repeat("(", 132) + strings.Repeat(")", 132),
		// huge count: far more elements than the default token limit, so the
		// read must refuse rather than build the whole array.
		"huge-count": "(:big " + strings.Repeat("1 ", 100000) + ")",
		// recursion: the parser descends one frame per pointer body; past the
		// cap it must refuse as text_limit.
		"recursion": luaRecursionDoc(),
		// giant string: over the default 65536-byte document limit.
		"giant-string": strings.Repeat("x", 70000),
		// many unknown fields: 2000 undeclared keys, refused by name once the
		// record is resolved instead of being buffered.
		"many-unknown-fields": "(" + strings.Repeat(":zz 0 ", 2000) + ")",
	}
}

// luaResourceBoundsWant is the documented refusal each shape must reach.
func luaResourceBoundsWant() map[string]string {
	return map[string]string{
		"deep-nesting":        "text_limit",
		"huge-count":          "text_limit",
		"recursion":           "text_limit",
		"giant-string":        "text_limit",
		"many-unknown-fields": "text_unknown_key",
	}
}

// luaResourceBoundsProbe prints one tab-separated line per shape: name,
// verdict (OK / REFUSED / CRASH), refusal name, allocated KB, step ticks
// (thousands of VM instructions) and input bytes.
const luaResourceBoundsProbe = `local T = require("ProbeTables")
local TF = require("textform")
local inputs = require("inputs")
local steps = 0
debug.sethook(function() steps = steps + 1 end, "", 1000)
local names = { "deep-nesting", "huge-count", "recursion", "giant-string", "many-unknown-fields" }
for _, name in ipairs(names) do
  collectgarbage("collect")
  local kb0 = collectgarbage("count")
  steps = 0
  local ok, err = pcall(TF.Load, T, "H", inputs[name])
  local ticks = steps
  local kb1 = collectgarbage("count")
  local kb = string.format("%.1f", kb1 - kb0)
  local bytes = #inputs[name]
  if ok then
    print(name .. "\tOK\t\t" .. kb .. "\t" .. ticks .. "\t" .. bytes)
  elseif type(err) == "table" and err.name then
    print(name .. "\tREFUSED\t" .. err.name .. "\t" .. kb .. "\t" .. ticks .. "\t" .. bytes)
  else
    print(name .. "\tCRASH\t" .. tostring(err) .. "\t" .. kb .. "\t" .. ticks .. "\t" .. bytes)
  end
end
`

// luaResourceBoundsUnit generates the hostile unit's Lua table surface and
// runtimes into a fresh directory, applying plant when it is non-empty: the
// plant is a textual edit of the emitted runtime (the negative control raises
// the depth cap), never a schema change.
func luaResourceBoundsUnit(t *testing.T, plantFrom, plantTo string) string {
	t.Helper()
	schemaDir := t.TempDir()
	schema := filepath.Join(schemaDir, "H.schema")
	if err := os.WriteFile(schema, []byte(luaResourceBoundsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := luatable.Generate(u)
	if err != nil {
		t.Fatalf("generate lua resource-bounds unit: %v", err)
	}
	if plantFrom != "" {
		rt := string(files["textform.lua"])
		if !strings.Contains(rt, plantFrom) {
			t.Fatalf("the emitted textform runtime does not carry the planted pattern %q", plantFrom)
		}
		files["textform.lua"] = []byte(strings.ReplaceAll(rt, plantFrom, plantTo))
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaResourceBoundsProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type luaResourceBoundsResult struct {
	verdict string
	reason  string
	kb      float64
	ticks   int
	bytes   int
}

func runLuaResourceBounds(t *testing.T, dir string) map[string]luaResourceBoundsResult {
	t.Helper()
	inputs := map[string][]byte{}
	for name, text := range luaResourceBoundsDocs() {
		inputs[name] = []byte(text)
	}
	out := string(luarun.Run(t, dir, "main.lua", inputs))
	got := map[string]luaResourceBoundsResult{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			t.Fatalf("bad resource-bounds probe line %q", line)
		}
		var r luaResourceBoundsResult
		r.verdict, r.reason = fields[1], fields[2]
		if _, err := fmt.Sscanf(fields[3], "%f", &r.kb); err != nil {
			t.Fatalf("%s: bad KB %q: %v", fields[0], fields[3], err)
		}
		if _, err := fmt.Sscanf(fields[4], "%d", &r.ticks); err != nil {
			t.Fatalf("%s: bad ticks %q: %v", fields[0], fields[4], err)
		}
		if _, err := fmt.Sscanf(fields[5], "%d", &r.bytes); err != nil {
			t.Fatalf("%s: bad bytes %q: %v", fields[0], fields[5], err)
		}
		got[fields[0]] = r
	}
	return got
}

// TestLuaAllocationCounterCounts is STEP 1: prove the port's allocation
// counter counts by planting an unbounded allocation (a table grown in a loop)
// and requiring collectgarbage("count") to rise with it. If the counter did
// not count, the budget assertions below would measure nothing.
func TestLuaAllocationCounterCounts(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	dir := t.TempDir()
	probe := `local kb0 = collectgarbage("count")
local t = {}
for i = 1, 500000 do t[i] = string.rep("x", 16) end
local kb1 = collectgarbage("count")
print(string.format("%.1f %.1f", kb0, kb1))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	var kb0, kb1 float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil))), "%f %f", &kb0, &kb1); err != nil {
		t.Fatalf("the counter probe did not print two KB numbers: %v", err)
	}
	if kb1-kb0 < 1024.0 {
		t.Fatalf("the allocation counter did not see the planted unbounded allocation: memstat rose %.1f KB, want >= 1024 KB", kb1-kb0)
	}
	t.Logf("allocation counter counts: planted 500000-entry table raised memstat %.1f KB", kb1-kb0)
}

// TestLuaResourceBoundsHostileShapes is STEP 2: one shape per hostile
// dimension, each read with the default limits. Every shape must refuse by
// the documented name, allocate at most F times its input plus slack, and cost
// at most a linear number of VM step ticks.
func TestLuaResourceBoundsHostileShapes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	dir := luaResourceBoundsUnit(t, "", "")
	got := runLuaResourceBounds(t, dir)
	want := luaResourceBoundsWant()
	for _, name := range []string{"deep-nesting", "huge-count", "recursion", "giant-string", "many-unknown-fields"} {
		r, ok := got[name]
		if !ok {
			t.Fatalf("shape %s: the probe wrote no line", name)
		}
		if r.verdict != "REFUSED" || r.reason != want[name] {
			t.Fatalf("shape %s: %s %s, want REFUSED %s", name, r.verdict, r.reason, want[name])
		}
		inputKB := float64(r.bytes) / 1024.0
		budget := luaAllocationFactorF*inputKB + luaAllocationSlackKB
		if r.kb > budget {
			t.Fatalf("shape %s allocated %.1f KB, over the F*input + slack budget %.1f KB (F=%.0f)", name, r.kb, budget, luaAllocationFactorF)
		}
		// VM instructions linear in the input: ticks count thousands of VM
		// instructions, so this is a step bound, never a wall-clock bound.
		stepBudget := 4*r.bytes + 65536
		if r.ticks > stepBudget {
			t.Fatalf("shape %s cost %d step ticks, over the linear budget %d", name, r.ticks, stepBudget)
		}
		t.Logf("lua resource-bounds shape=%s input=%dB allocated=%.1fKB budget=%.1fKB ticks=%d stepBudget=%d",
			name, r.bytes, r.kb, budget, r.ticks, stepBudget)
	}
}

// TestLuaResourceBoundsPlantedPattern is the planted-bad-pattern control: with
// the depth cap in place the deep-nesting shape refuses as text_limit; with
// the cap raised in a copy of the emitted runtime (the plant) it no longer
// does. The test is red when the harness cannot see the guard.
func TestLuaResourceBoundsPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	guarded := runLuaResourceBounds(t, luaResourceBoundsUnit(t, "", ""))["deep-nesting"]
	if guarded.verdict != "REFUSED" || guarded.reason != "text_limit" {
		t.Fatalf("with the depth guard in place the deep-nesting shape must refuse text_limit, got %s %s", guarded.verdict, guarded.reason)
	}
	planted := runLuaResourceBounds(t, luaResourceBoundsUnit(t, "local MAX_DEPTH = 128", "local MAX_DEPTH = 100000"))["deep-nesting"]
	if planted.verdict == "OK" || planted.reason == "text_limit" {
		t.Fatalf("the planted pattern (depth cap raised) did not change the deep-nesting verdict: %s %s; the harness cannot see the guard", planted.verdict, planted.reason)
	}
	t.Logf("planted-bad-pattern control: guarded REFUSED text_limit, planted %s %s", planted.verdict, planted.reason)
}

// TestLuaResourceBoundsLane pins the make target and the workflow that run the
// tests above: the heavy sweep is nightly and the planted control rides every
// push; a lane with no blade is not a gate.
func TestLuaResourceBoundsLane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk := readCI(t, root, "make/schema-lua-resource-bounds.mk")
	for _, target := range []string{
		"schema-lua-resource-bounds:",
		"schema-lua-resource-bounds-negative-control:",
	} {
		if !strings.Contains(mk, target) {
			t.Errorf("make/schema-lua-resource-bounds.mk has no %s target", target)
		}
	}
	for _, pin := range []string{
		"TestLuaAllocationCounterCounts",
		"TestLuaResourceBoundsHostileShapes",
		"TestLuaResourceBoundsPlantedPattern",
		"collectgarbage",
		"debug.sethook",
	} {
		if !strings.Contains(mk, pin) {
			t.Errorf("make/schema-lua-resource-bounds.mk does not carry %q", pin)
		}
	}
	wf := readCI(t, root, ".github/workflows/schema-lua-resource-bounds.yml")
	for _, want := range []string{
		"cron:", "workflow_dispatch:", "push:", "pull_request:",
		"make schema-lua-resource-bounds-negative-control",
		"make schema-lua-resource-bounds",
		"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the lua resource-bounds workflow does not carry %q", want)
		}
	}
}
