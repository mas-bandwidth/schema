// Resource bounds for the luau port of schema: hostile inputs cost bounded
// memory and time. The bounds are the ones docs/SPEC-TABLES.md documents —
// the depth cap of 128 and the default text_limit byte/token/node/allocation
// limits of §16.9.4 — asserted in the port's own language, which is what this
// card is: the generated Luau readers run under the pinned interpreter, and
// the allocation counter is Luau's GC memstat, collectgarbage("count"), the
// nearest counting allocator the language offers (there is no host allocator
// hook to wrap from inside a generated Luau module).
//
// The hostile shapes the spec names are all here: huge counts, deep nesting,
// recursion (deep nesting is the recursion path — the parser descends one
// frame per open paren), giant strings and many unknown fields. Every shape is
// fed to the generated textform reader with the DEFAULT limits, so it must
// refuse by name (text_limit, or the first text_unknown_key) rather than
// allocate or recurse without bound. The planted-pattern control proves the
// harness sees the depth guard: with the guard relaxed (the plant) the deep
// nesting is no longer refused, and the test goes red; with the guard in place
// it is green.
package ci

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// allocationFactorF is the documented allocation factor, in bytes, of a read:
// a read allocates at most F times the input size (docs/SPEC-TABLES.md §16.9.4
// and §21.7). Luau's memstat reports kilobytes with one decimal, so the byte
// budget is asserted with a small fixed slack of the reader's own constant
// overhead; the factor itself is what scales with input.
const allocationFactorF = 256.0

// luauAllocSlackKB is the fixed overhead allowed above the F-multiple, in KB:
// warm caches, the interpreter startup and the frozen runtime tables are not
// chargeable to one hostile read.
const luauAllocSlackKB = 1024.0

// luauResourceBoundsSchema is the table unit every hostile shape is read
// against: a variable table whose text form carries a bounded int and a
// declared-array field, enough that a hostile document has somewhere to be
// ugly.
const luauResourceBoundsSchema = `package probe
table H {
  n int32 | min = 0, max = 100000
  big [200000]int32
}
`

func luauResourceBoundsDocs() map[string]string {
	return map[string]string{
		// deep nesting (also the recursion shape): the parser descends one
		// frame per paren, so 300 levels must trip the depth cap of 128
		// before any resolve runs. The nesting is over the declared `big`
		// array, so a well-formed document reaches the cap rather than a
		// syntax refusal.
		"deep": "(:big " + strings.Repeat("(", 300) + strings.Repeat(")", 301),
		// huge count: 9000 elements in one declared array. They pass the
		// declared bound (200000) but every element is a token, and 9000
		// elements blow the 8192-token default limit, so the read must refuse
		// as text_limit rather than parse and allocate the whole list.
		"hugecount": "(:big " + strings.Repeat("1 ", 9000) + ")",
		// giant string (and over-long document): the byte limit fires before
		// any parse, so a 70 002-byte quoted string refuses as text_limit.
		"giantstring": `"` + strings.Repeat("x", 70000) + `"`,
		// many unknown fields: 2000 undeclared keys; the first one must refuse
		// as text_unknown_key instead of being buffered.
		"unknownfields": "(:n 0 " + strings.Repeat(":x 0 ", 2000) + ")",
	}
}

// luauResourceBoundsMain builds main.luau for the hostile-shape probe: it
// registers the table module, wraps every document in a guarded read under
// the default limits, and prints one tab-separated line per document: name,
// the verdict (OK or REFUSED <name> or CRASH), and the KB the read allocated,
// measured with the port's GC memstat.
func luauResourceBoundsMain() []byte {
	var b strings.Builder
	b.WriteString("require(\"./HTable\")\n")
	b.WriteString("local SCHEMA_JSON_FLAGS = {}\n")
	b.WriteString("local json = require(\"./schema_json\")\njson.SetFlags(SCHEMA_JSON_FLAGS)\n")
	b.WriteString("local textform = require(\"./schema_textform\")\ntextform.SetFlags(SCHEMA_JSON_FLAGS)\n")
	b.WriteString("local root = \"H\"\n")
	b.WriteString("local docs = {\n")
	for _, name := range []string{"deep", "hugecount", "giantstring", "unknownfields"} {
		fmt.Fprintf(&b, "\t{ name = %q, text = %q },\n", name, luauResourceBoundsDocs()[name])
	}
	b.WriteString("}\n")
	b.WriteString("local limits = nil\n")
	b.WriteString(`for _, d in docs do
	collectgarbage("collect")
	local kb0 = collectgarbage("count")
	local okp, doc, v, err = pcall(function() return textform.LoadTextForm(root, d.text, limits) end)
	local kb1 = collectgarbage("count")
	local kb = string.format("%.1f", kb1 - kb0)
	if not okp then
		print(d.name .. "\tCRASH\t" .. tostring(doc) .. "\t" .. kb)
	elseif err ~= nil then
		print(d.name .. "\tREFUSED\t" .. err.name .. "\t" .. kb)
	else
		print(d.name .. "\tOK\t\t" .. kb)
	end
end
`)
	return []byte(b.String())
}

func runLuauResourceBounds(t *testing.T, dir string) map[string][]string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), luauResourceBoundsMain(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if fields := strings.Split(line, "\t"); len(fields) >= 3 {
			got[fields[0]] = fields
		}
	}
	return got
}

// luauResourceBoundsUnit generates the hostile-unit's Luau table surface and
// runtimes into a fresh directory.
func luauResourceBoundsUnit(t *testing.T, root string) string {
	t.Helper()
	schema := filepath.Join(t.TempDir(), "H.schema")
	if err := os.WriteFile(schema, []byte(luauResourceBoundsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, _, err := luauGenerateUnit(t, root, []string{schema})
	if err != nil {
		t.Fatalf("generate luau resource-bounds unit: %v", err)
	}
	return dir
}

// TestLuauAllocationCounterCounts is STEP 1's counter proof: it plants an
// unbounded allocation (a loop that grows a table without limit) and requires
// the port's allocation counter — collectgarbage("count") — to rise with it.
// If the counter did not count, the resource-bounds assertions below would be
// measuring nothing.
func TestLuauAllocationCounterCounts(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (the pinned release)")
	dir := t.TempDir()
	main := `local kb0 = collectgarbage("count")
local t = {}
for i = 1, 500000 do t[i] = string.rep("x", 16) end
local kb1 = collectgarbage("count")
print(string.format("%.1f %.1f", kb0, kb1))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, dir, "main.luau", nil))
	var kb0, kb1 float64
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%f %f", &kb0, &kb1); err != nil {
		t.Fatalf("the counter probe did not print two KB numbers: %q (%v)", out, err)
	}
	if kb1-kb0 < 1024.0 {
		t.Fatalf("the allocation counter did not see the planted unbounded allocation: memstat rose %0.1f KB, want >= 1024 KB", kb1-kb0)
	}
}

// TestLuauResourceBoundsHostileShapes is STEP 2: one shape per hostile
// dimension, each read under the default limits. Every shape must refuse by
// name — the depth cap of 128 and the default text_limit bounds — and allocate
// at most the documented factor times the input, plus the reader's fixed
// overhead.
func TestLuauResourceBoundsHostileShapes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (the pinned release)")
	root := repoRoot(t)
	dir := luauResourceBoundsUnit(t, root)
	got := runLuauResourceBounds(t, dir)

	want := map[string]string{
		"deep":          "text_limit",
		"hugecount":     "text_limit",
		"giantstring":   "text_limit",
		"unknownfields": "text_unknown_key",
	}
	for name, wantRefusal := range want {
		fields, ok := got[name]
		if !ok {
			t.Fatalf("shape %s: the probe wrote no line", name)
		}
		if fields[1] != "REFUSED" || fields[2] != wantRefusal {
			t.Fatalf("shape %s: verdict %s (%s), want REFUSED %s", name, fields[1], fields[2], wantRefusal)
		}
		kb, err := parseKB(fields[3])
		if err != nil {
			t.Fatalf("shape %s: bad KB %q: %v", name, fields[3], err)
		}
		inputKB := float64(len(luauResourceBoundsDocs()[name])) / 1024.0
		budget := allocationFactorF*inputKB + luauAllocSlackKB
		if kb > budget {
			t.Fatalf("shape %s allocated %.1f KB, over the documented budget F*input + slack = %.1f KB (F=%0.0f)", name, kb, budget, allocationFactorF)
		}
		t.Logf("luau resource-bounds shape=%s input=%dB allocated=%.1fKB budget=%.1fKB", name, len(luauResourceBoundsDocs()[name]), kb, budget)
	}
}

// TestLuauResourceBoundsPlantedPattern is the planted-bad-pattern control. It
// proves the deep-nesting assertion has a blade: with the depth guard in place
// the deep shape refuses as text_limit, and when the guard is removed — the
// plant, written only into the scratch copy — the same shape is no longer
// refused as text_limit, so the positive test is red when the plant is in and
// green when it is out. The plant edits the generated runtime rather than the
// limits: §16.9.4 clamps a caller's Depth to 128, so no limit argument can
// relax the guard.
func TestLuauResourceBoundsPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (the pinned release)")
	root := repoRoot(t)
	dir := luauResourceBoundsUnit(t, root)

	guarded := runLuauResourceBounds(t, dir)
	if f := guarded["deep"]; f[1] != "REFUSED" || f[2] != "text_limit" {
		t.Fatalf("with the depth guard in place the deep shape must refuse text_limit, got %s (%s)", f[1], f[2])
	}

	plantLuauDepthGuard(t, dir)
	planted := runLuauResourceBounds(t, dir)
	f := planted["deep"]
	if f[1] == "CRASH" {
		t.Fatalf("the planted pattern (depth guard removed) crashed instead of parsing: %s", f[2])
	}
	if f[1] == "REFUSED" && f[2] == "text_limit" {
		t.Fatalf("the planted pattern (depth guard removed) did not change the verdict: the harness cannot see the guard")
	}
}

// luauDepthGuard is the generated textform reader's depth check; the plant
// below removes it, the defect the control above proves the positive test sees.
const luauDepthGuard = "depth + 1 > self.limits.depth"

// plantLuauDepthGuard rewrites the generated textform reader in the scratch
// unit so the depth guard of §16.9.4 no longer fires.
func plantLuauDepthGuard(t *testing.T, dir string) {
	t.Helper()
	target := filepath.Join(dir, "schema_textform.luau")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(luauDepthGuard)) {
		t.Fatalf("%s carries no depth guard %q to plant against", target, luauDepthGuard)
	}
	data = bytes.Replace(data, []byte(luauDepthGuard), []byte("depth + 1 > 100000"), 1)
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func parseKB(s string) (float64, error) {
	var kb float64
	if _, err := fmt.Sscanf(s, "%f", &kb); err != nil {
		return 0, err
	}
	return math.Round(kb*10) / 10, nil
}
