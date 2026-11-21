package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// innerCountMinProbe reads the hostile corpus's inner count rows
// (internal/ci/hostilecorpus/corpus, test/tables/inner) through the public
// fixed read and the strict variable read: a child's [2..4]uint8 holding one
// value, inside a parent array that declares no minimum (Loose) or a minimum
// of one (Tight), must refuse in both forms; the same child at two values
// reads, its count intact. A top-level counted array one under its minimum
// (Top, forged from a clean save) refuses in both forms too: the minimum is
// held at every depth (docs/SPEC-TABLES.md §5.1 row 3).
const innerCountMinProbe = `--!strict
local T = require("./InnerTable")
local P = T
local F = require("./schema_fixed")
local L = require("./schema_load")
local S = require("./schema_tables")
local inputs = require("./inputs")
local fails = 0
local function check(ok: boolean, what: string)
	if not ok then fails += 1; print("FAIL: " .. what) end
end
local function newreport(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function fixedRead(root: string, b: buffer): ({ any }?, any)
	local rep: any = {}
	return F.read(S.lookup(root), nil :: any, b, rep), rep
end
local function varRead(root: string, b: buffer): (any, any)
	local rep = newreport()
	return L.load(root, b, rep), rep
end
for _, kind in { "loose", "tight" } do
	local Kind = string.upper(string.sub(kind, 1, 1)) .. string.sub(kind, 2)
	local fixedRoot, varRoot = "Fixed" .. Kind .. "Count", "Var" .. Kind .. "Count"
	local recs = fixedRead(fixedRoot, inputs["fixed-" .. kind .. "-at"])
	check(recs ~= nil and #recs == 1 and recs[1].NestCount == 1 and recs[1].Nest[1].ValsCount == 2,
		fixedRoot .. ": the child at its minimum does not read")
	local bad, brep = fixedRead(fixedRoot, inputs["fixed-" .. kind .. "-below"])
	check(bad == nil, fixedRoot .. ": a child one under its minimum was read")
	check(bad ~= nil or brep.reason == "count_over_bound", fixedRoot .. ": refused " .. tostring(brep.reason) .. ", want count_over_bound")
	local v = varRead(varRoot, inputs["variable-" .. kind .. "-at"])
	check(v ~= nil and v.NestCount == 1 and v.Nest[1].ValsCount == 2, varRoot .. ": the child at its minimum does not read")
	local vbad = varRead(varRoot, inputs["variable-" .. kind .. "-below"])
	check(vbad == nil, varRoot .. ": a child one under its minimum was read")
end
-- the top level: a clean save at the minimum, then its count forged one under
local fixed = { hash = P.FixedTopFixedHash, recordBytes = P.FixedTopFixedRecordBytes, layout = P.FixedTopFixedLayout,
	lineage = P.FixedTopFixedLineage, floor = P.FixedTopFixedFloor }
local top = P.NewFixedTop()
top.ValsCount = 2
local good = F.save(P.FixedTopDescriptor, fixed, { top })
local rec = 16 + 4 + buffer.readu32(good, 16) + 8
check(buffer.readu32(good, rec) == 2, "FixedTop: the count is not the record's first four bytes")
local okRecs = F.read(P.FixedTopDescriptor, fixed, good)
check(okRecs ~= nil and #okRecs == 1 and okRecs[1].ValsCount == 2, "FixedTop: the clean file does not read")
local forged = buffer.create(buffer.len(good))
buffer.copy(forged, 0, good, 0, buffer.len(good))
buffer.writeu32(forged, rec, 1)
buffer.writeu8(forged, rec + 4 + 1, 0)
local trep: any = {}
check(F.read(P.FixedTopDescriptor, fixed, forged, trep) == nil, "FixedTop: a count one under its minimum was read")
local vt = varRead("VarTop", inputs["var-top-below"])
check(vt == nil, "VarTop: a count one under its minimum was read")
local vgood = varRead("VarTop", inputs["var-top-at"])
check(vgood ~= nil and vgood.ValsCount == 2, "VarTop: the count at its minimum does not read")
if fails == 0 then print("ok") end
`

// innerCountMinTop is the top-level control, appended to the inner unit: the
// same [2..4]uint8 as the corpus's CountChild, on a fixed root and on a
// variable root.
const innerCountMinTop = `
fixed table FixedTop
{
    vals [2..4]uint8
}

table VarTop
{
    vals [2..4]uint8
}
`

// varTopWire is VarTop's variable wire from the Go engine: at its minimum
// under the declared schema, and one under it written through a twin that
// declares no minimum (the same ids, so the same wire), as the corpus's
// inner rows are made (internal/ci/hostilecorpus/inner_inputs_test.go).
func varTopWire(t *testing.T) (at, below []byte) {
	t.Helper()
	encode := func(src, text string) []byte {
		dir := t.TempDir()
		path := filepath.Join(dir, "Top.schema")
		if err := os.WriteFile(path, []byte("package top\n"+src), 0o644); err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load([]string{path})
		if err != nil {
			t.Fatalf("the top unit does not load: %v", err)
		}
		m := tabletext.NewModel(u)
		inst := m.New(m.Lookup("VarTop"))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("the engine did not place %s cleanly: %+v", text, rep)
		}
		out, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	const tight = "table VarTop\n{\n    vals [2..4]uint8"
	if strings.Count(innerCountMinTop, tight) != 1 {
		t.Fatal("the top unit does not declare VarTop's minimum exactly once")
	}
	loose := strings.Replace(innerCountMinTop, tight, "table VarTop\n{\n    vals [..4]uint8", 1)
	return encode(innerCountMinTop, `{"vals":[7,0]}`), encode(loose, `{"vals":[7]}`)
}

func innerCountMinRun(t *testing.T, from, to string) string {
	t.Helper()
	inner, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "inner", "Inner.schema"))
	if err != nil {
		t.Fatal(err)
	}
	g := generateFiles(t, map[string]string{"Inner.schema": string(inner) + innerCountMinTop})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}
	if from != "" {
		path := filepath.Join(g.dir, "schema_fixed.luau")
		if rest, ok := strings.CutPrefix(from, "load:"); ok {
			path, from = filepath.Join(g.dir, "schema_load.luau"), rest
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("the sabotage anchor %q does not occur exactly once: the runtime moved", from)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	corpus := filepath.Join("..", "..", "ci", "hostilecorpus", "corpus", "inputs")
	inputs := map[string][]byte{}
	for key, name := range map[string]string{
		"fixed-loose-at": "inner-fixed-loose-count-at-min", "fixed-loose-below": "inner-fixed-loose-count-below-min",
		"fixed-tight-at": "inner-fixed-tight-count-at-min", "fixed-tight-below": "inner-fixed-tight-count-below-min",
		"variable-loose-at": "inner-variable-loose-count-at-min", "variable-loose-below": "inner-variable-loose-count-below-min",
		"variable-tight-at": "inner-variable-tight-count-at-min", "variable-tight-below": "inner-variable-tight-count-below-min",
	} {
		b, err := os.ReadFile(filepath.Join(corpus, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[key] = b
	}
	inputs["var-top-at"], inputs["var-top-below"] = varTopWire(t)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(innerCountMinProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
}

// TestLuauHoldsTheCountMinimumAtEveryDepth: SPEC-TABLES §5.1 row 3 one record
// down (F-187's class in the Luau port). The public fixed read and the strict
// variable read refuse a child's counted array one under its minimum inside a
// parent array, whatever the parent declares, and at the top level; at the
// minimum both read.
func TestLuauHoldsTheCountMinimumAtEveryDepth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	if out := innerCountMinRun(t, "", ""); out != "ok" {
		t.Fatalf("count minimum:\n%s", out)
	}
}

// TestLuauHoldsTheCountMinimumNegativeControl takes each read's minimum check
// out of the emitted runtime, one at a time, and requires the probe red.
func TestLuauHoldsTheCountMinimumNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	plants := []struct{ name, from, to string }{
		{"fixed", "if count < (f.min or 0) then error(", "if false then error("},
		{"variable", "load:elseif fd.array == \"counted\" and count < (fd.min or 0) then", "elseif false then"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			if out := innerCountMinRun(t, p.from, p.to); out == "ok" || !strings.Contains(out, "FAIL:") {
				t.Fatalf("the %s plant left the probe green:\n%s", p.name, out)
			}
		})
	}
}
