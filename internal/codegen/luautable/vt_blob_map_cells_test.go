package luautable_test

import (
	"fmt"
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

// vtCellsSchema is the probe unit every cell this card closes rides on in one
// value: blobs (*string, *bytes), string identity (two *string slots whose
// values are equal still save by value as two records), a map, an
// unbounded array, an enum field on the 64-bit id-table wire (kind 30) and a
// wstring(N) field (kind 33).
const vtCellsSchema = `package probe

enum Color { Red, Green, Blue }

table Root
{
    note  *string
    note2 *string
    data  *bytes
    tags  map[string(8)]int32
    nums  []int32
    color Color
    wide  string(4)
}
`

// vtOracleWire builds the Go engine's own form-1 wire for the root of
// vtCellsSchema from its JSON text: the reference every Luau read must match.
func vtOracleWire(t *testing.T, schema, text string) []byte {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the probe unit does not load: %v", err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) {
		t.Fatalf("the oracle did not read the probe text cleanly: %+v", rep)
	}
	w, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the oracle did not encode the probe value: %v", err)
	}
	return w
}

// vtHexstr is the Luau hex printer the probes share.
const vtHexstr = `local function hexstr(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
`

// vtCellsProbe strict-reads two wires of the comprehensive root and prints the
// strict read's verdict and, when it reads, the value's resaved hex. The Go
// side holds the resave to the oracle's own wire, so a whole-value read is
// proven byte for byte against the engine.
func vtCellsProbe() string {
	return `local inputs = require("./inputs")
local T = require("./ProbeTable")
local R = require("./schema_refusal")
local S = require("./schema_save")
local d = T.RootDescriptor
` + vtHexstr + `local function one(name)
	local rep = {}
	local v = R.ReadStrict(T, "Root", inputs[name], rep)
	local line = name .. ".read=" .. (if v == nil then "refused" else "read")
	if v ~= nil then
		local out = S.save(d, v)
		line = line .. "|" .. name .. ".savehex=" .. hexstr(out)
	end
	print(line)
end
one("v1")
one("v2")
`
}

// vtBlobProbe strict-reads the shared-blob wire of the blob probe unit
// (blobSharedSchema): one blob record named by two *string slots, which the
// strict read refuses as blob_shared with the second naming field as witness.
func vtBlobProbe() string {
	return `local inputs = require("./inputs")
local T = require("./BlobTable")
local R = require("./schema_refusal")
local rep = {}
local v = R.ReadStrict(T, "Probe", inputs.shared, rep)
print("blob.strict=" .. tostring(rep.reason) .. "|" .. tostring(rep.field) .. "|" .. tostring(v == nil))
`
}

// vtMapProbe strict-reads the map probe unit's clean wire (two distinct keys)
// and its forged repeated-key wire, which the strict read refuses as malformed
// (docs/plans/strict-only.md decision 5).
func vtMapProbe() string {
	return `local inputs = require("./inputs")
local T = require("./ProbeTable")
local R = require("./schema_refusal")
local function one(name)
	local rep = {}
	local v = R.ReadStrict(T, "Root", inputs[name], rep)
	print(name .. ".strict=" .. (if v == nil then "refused" else "read"))
end
one("clean")
one("repeated")
`
}

// vtRun generates a unit from `schema` under filename `schemaPath`, runs
// `main` with `inputs` under the pinned interpreter, and returns stdout. When
// `from` is non-empty it first sabotages the generated schema_load.luau by
// replacing `from` with `to`, so a negative control can plant a fault in the
// real artifact the test reads.
func vtRun(t *testing.T, schemaPath, schema, main string, inputs map[string][]byte, from, to string) string {
	t.Helper()
	g := generateFiles(t, map[string]string{schemaPath: schema})
	if from != "" {
		p := filepath.Join(g.dir, "schema_load.luau")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("the sabotage anchor %q does not occur exactly once: the runtime moved", from)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n")
}

// vtDropDuplicateRefusal is the sabotage: remove the duplicate-key refusal from
// the strict read's refusing set (schema_load.luau's REFUSING), so a repeated
// map key reads instead of refusing.
const (
	vtDupFrom = `{ "missingRequired", "malformed", "blobShared", "clamped", "kindMismatch", "duplicate" }`
	vtDupTo   = `{ "missingRequired", "malformed", "blobShared", "clamped", "kindMismatch" }`
)

// vtSuite runs every probe of this card — the comprehensive all-cells wires,
// the shared-blob refusal, and the repeated-map-key decision-5 refusal — and
// returns the concatenated verdicts. `from`/`to` sabotage the generated
// runtime for every unit, so the negative control can show the row going red.
func vtSuite(t *testing.T, from, to string) string {
	t.Helper()
	v1 := vtOracleWire(t, vtCellsSchema, `{"note": "hi", "note2": "hi", "data": "YWJj", "tags": {"alpha": 1, "beta": 2}, "nums": [7, -2, 0, 4], "color": "Green", "wide": "ab"}`)
	v2 := vtOracleWire(t, vtCellsSchema, `{"note": "", "note2": "x", "data": "w6k", "tags": {"k": 9}, "nums": [], "color": "Red", "wide": "\ud83d\ude00"}`)
	clean, repeated := repeatedKeyWires(t)
	var b strings.Builder
	b.WriteString(vtRun(t, "Probe.schema", vtCellsSchema, vtCellsProbe(), map[string][]byte{"v1": v1, "v2": v2}, from, to))
	b.WriteString("\n")
	b.WriteString(vtRun(t, "Blob.schema", blobSharedSchema, vtBlobProbe(), map[string][]byte{"shared": blobSharedWire("ab")}, from, to))
	b.WriteString("\n")
	b.WriteString(vtRun(t, "Probe.schema", repeatedKeySchema, vtMapProbe(), map[string][]byte{"clean": clean, "repeated": repeated}, from, to))
	return b.String()
}

func vtWant(t *testing.T) string {
	t.Helper()
	v1 := vtOracleWire(t, vtCellsSchema, `{"note": "hi", "note2": "hi", "data": "YWJj", "tags": {"alpha": 1, "beta": 2}, "nums": [7, -2, 0, 4], "color": "Green", "wide": "ab"}`)
	v2 := vtOracleWire(t, vtCellsSchema, `{"note": "", "note2": "x", "data": "w6k", "tags": {"k": 9}, "nums": [], "color": "Red", "wide": "\ud83d\ude00"}`)
	return fmt.Sprintf("v1.read=read|v1.savehex=%x\nv2.read=read|v2.savehex=%x\nblob.strict=blob_shared|note2|true\nclean.strict=read\nrepeated.strict=refused", v1, v2)
}

// TestLuauVtBlobMapCells pins the Luau table runtime's strict read
// (schema_load.luau ReadStrict) against schema-go's own engine for the six
// variable-table cells this card closes: blobs, string identity (equal strings
// save by value), maps, unbounded arrays, the 64-bit id-table wire (an enum
// field riding as kind 30) and wstring (kind 33).
//
// The Go engine builds every wire. Each comprehensive wire is read whole by
// the strict read and its value re-saves to exactly the oracle's bytes, so a
// whole-value read is proven byte for byte for all six cells at once. The
// shared-blob wire refuses as blob_shared and the forged repeated map key
// refuses as malformed (decision 5): the "whole value or a refusal" rule.
func TestLuauVtBlobMapCells(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	got := vtSuite(t, "", "")
	if want := vtWant(t); got != want {
		t.Fatalf("the strict read's verdicts:\n%s\nwant\n%s", got, want)
	}
}

// TestLuauVtBlobMapCellsNegativeControl drops the duplicate-key refusal from the
// strict read's refusing set in the real schema_load.luau and requires a
// repeated map key, which must refuse, to start reading: the row goes red.
func TestLuauVtBlobMapCellsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	got := vtSuite(t, vtDupFrom, vtDupTo)
	if want := vtWant(t); got == want {
		t.Fatalf("the planted defect left the row green:\n%s", got)
	}
}
