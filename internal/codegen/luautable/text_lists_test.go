package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// textListsSchema is the text-list half of TestGoTableStringArraysAndJSONArrayRoot
// (docs/SPEC-TABLES.md §2.13): a counted and an unbounded list of string(M),
// each in the plain and the ?string(M) spelling. The array root and a total are
// other cards and are not here.
const textListsSchema = `package probe

table Lists
{
    rules [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
    bases []?string(8)
}
`

// textListsProbe loads one wire through the emitted schema_load.luau, saves it through
// schema_save.luau, prints the report counters, the saved bytes and the measure, then
// loads and saves the saved bytes again.
const textListsProbe = `local inputs = require("./inputs")
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
require("./ListsTable")
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function hexstr(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local report = fresh()
local v = load.loadWalk("Lists", inputs.wire, report)
if report.malformed then
 print("DAMAGE " .. tostring(v == nil))
 print(string.format("%d,%d,%d,%d,%d,%s", report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, tostring(report.malformed)))
 return
end
if report.refused > 0 then print("REFUSED"); return end
local d = St.lookup("Lists")
local out = save.save(d, v)
local measure = save.measure(d, v)
if out == nil or measure == nil then print("SAVENIL"); return end
print(hexstr(out))
print(string.format("%d,%d,%d,%d,%d,%s", report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, tostring(report.malformed)))
print(measure)
local again = load.loadWalk("Lists", out, fresh())
local out2 = save.save(d, again)
if out2 == nil then print("RESAVENIL"); return end
print(hexstr(out2))
`

// textListModel compiles the same source with schema-go's front end and binds
// the compiler's own instance model: the oracle for every expected byte and
// verdict in this test.
func textListModel(t *testing.T) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Lists.schema")
	if err := os.WriteFile(path, []byte(textListsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return tabletext.NewModel(u)
}

// textListWire encodes one JSON text through schema-go's model.
func textListWire(t *testing.T, m *tabletext.Model, text string) []byte {
	t.Helper()
	inst := m.New(m.Lookup("Lists"))
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) || !report.Silent() {
		t.Fatalf("the oracle text did not place: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the oracle refuses the value: %v", err)
	}
	return wire
}

// textListsBoundFree is the BOUND-FREE TWIN of textListsSchema: the same names
// and kinds with no bound on the rules elements, so the engine's writer (which
// refuses an element past M, invalid_value) can still write the off-contract
// wire the reads are held to.
var textListsBoundFree = strings.Replace(textListsSchema, "rules [..4]string(8)", "rules [..4]string", 1)

// textListRawWire encodes one instance built by hand so a wire can carry what
// no JSON text places: an element past M, or an element that is not UTF-8.
// The element is written through the bound-free twin as a same-length
// placeholder and spliced in; the twin writes the bounded schema's own bytes
// wherever the bounded writer accepts the value, which is checked here.
func textListRawWire(t *testing.T, m *tabletext.Model, value string) []byte {
	t.Helper()
	if textListsBoundFree == textListsSchema {
		t.Fatal("the bound-free twin did not drop the rules bound")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Lists.schema")
	if err := os.WriteFile(path, []byte(textListsBoundFree), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the bound-free twin does not load: %v", err)
	}
	twin := tabletext.NewModel(u)
	placeholder := strings.Repeat("a", len(value))
	encode := func(m *tabletext.Model) ([]byte, error) {
		inst := m.New(m.Lookup("Lists"))
		idx := -1
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == "rules" {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatal("no rules field")
		}
		fv := &inst.Fields[idx]
		fv.Count = 1
		fv.Elems[0].Tab.Fields[0].Cell.Str = []byte(placeholder)
		fv.Elems[0].Tab.Fields[0].Count = len(placeholder)
		return tablewire.Encode(m, inst)
	}
	wire, err := encode(twin)
	if err != nil {
		t.Fatalf("the raw value does not encode through the bound-free twin: %v", err)
	}
	if bounded, err := encode(m); err == nil && !bytes.Equal(bounded, wire) {
		t.Fatalf("the bound-free twin wrote %x, the bounded schema %x", wire, bounded)
	}
	return bytes.Replace(wire, []byte(placeholder), []byte(value), 1)
}

// textListReadsProbe prints every Luau read's verdict on one wire: the plain
// load, the walk it is built on, and the strict read.
const textListReadsProbe = `local inputs = require("./inputs")
local load = require("./schema_load")
local R = require("./schema_refusal")
require("./ListsTable")
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
print(tostring(load.load("Lists", inputs.wire, fresh()) ~= nil) .. " " ..
	tostring(load.loadWalk("Lists", inputs.wire, fresh()) ~= nil) .. " " ..
	tostring(R.ReadStrict(nil, "Lists", inputs.wire, fresh()) ~= nil))
`

// requireTextListRefused holds a wire the Go engine refuses to the refusal of
// every Luau read (load, loadWalk, ReadStrict), by verdict alone
// (docs/rulings.json strict-only-read, refusal-verdict-only).
func requireTextListRefused(t *testing.T, g *generatedUnit, m *tabletext.Model, wire []byte) {
	t.Helper()
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Lists")), wire, &report); ok && err == nil && !report.Refused {
		t.Fatalf("the Go load reads the wire; strict-only refuses it: %+v", report)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "reads.luau"), []byte(textListReadsProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "reads.luau", map[string][]byte{"wire": wire})))
	if out != "false false false" {
		t.Errorf("the Go load refuses the wire, and the Luau reads (load, loadWalk, ReadStrict) answer %q; every read owes the refusal", out)
	}
}

type textListRun struct {
	saved   []byte
	report  string
	measure int
	resaved []byte
}

// runTextListProbe loads wire under the emitted unit, saves it, loads the saved
// bytes and saves again.
func runTextListProbe(t *testing.T, g *generatedUnit, wire []byte) textListRun {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(textListsProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
	lines := strings.Split(out, "\n")
	if len(lines) == 2 && strings.HasPrefix(lines[0], "DAMAGE ") {
		if lines[0] != "DAMAGE true" {
			t.Fatalf("damaged text list returned a usable value: %s", out)
		}
		return textListRun{report: lines[1]}
	}
	if len(lines) != 4 {
		t.Fatalf("unexpected probe output %q", out)
	}
	if lines[0] == "REFUSED" || lines[0] == "SAVENIL" || lines[3] == "RESAVENIL" {
		t.Fatalf("the probe refused: %s", out)
	}
	saved, err := hex.DecodeString(lines[0])
	if err != nil {
		t.Fatalf("bad saved line %q: %v", lines[0], err)
	}
	resaved, err := hex.DecodeString(lines[3])
	if err != nil {
		t.Fatalf("bad resaved line %q: %v", lines[3], err)
	}
	measure, err := strconv.Atoi(lines[2])
	if err != nil {
		t.Fatalf("bad measure line %q: %v", lines[2], err)
	}
	return textListRun{saved: saved, report: lines[1], measure: measure, resaved: resaved}
}

// goReportText renders schema-go's verdict in the probe's own spelling.
func goReportText(r tabletext.Report) string {
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t", r.Unknown, 0, r.Widened, 0, 0, r.Refused)
}

// TestLuauTableTextLists holds docs/SPEC-TABLES.md §2.13 in the Luau table
// surface over the text-list tables of TestGoTableStringArraysAndJSONArrayRoot:
// every value (an element at M included) saves the Go engine's bytes and loads
// back equal; an element past M, and an element that is not UTF-8, are refused
// by the Go load and by every Luau read (strict only, docs/rulings.json
// strict-only-read; verdict only). Every expected wire and verdict is computed here from
// internal/tabletext and internal/tablewire, never typed by hand.
func TestLuauTableTextLists(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"Lists.schema": textListsSchema})
	m := textListModel(t)

	t.Run("round trip", func(t *testing.T) {
		wire := textListWire(t, m, `{"rules":["a","b"],"first":["x",null],"repos":["r1",""],"bases":["yo",null,""]}`)
		got := runTextListProbe(t, g, wire)
		if got.report != goReportText(tabletext.Report{}) {
			t.Errorf("the load report is %s, want the Go engine's %s", got.report, goReportText(tabletext.Report{}))
		}
		if string(got.saved) != string(wire) {
			t.Errorf("the save is not the Go writer's bytes: %d saved, %d written; %s", len(got.saved), len(wire), firstDiff(got.saved, wire))
		}
		if got.measure != len(wire) {
			t.Errorf("measure %d != the Go writer's %d bytes", got.measure, len(wire))
		}
		if string(got.resaved) != string(wire) {
			t.Errorf("the loaded value does not load back equal: %d bytes; %s", len(got.resaved), firstDiff(got.resaved, wire))
		}
	})

	// STRICT ONLY (docs/rulings.json strict-only-read): an element past M is
	// refused by the Go load and by every Luau read; nothing is clamped.
	t.Run("element past M refuses", func(t *testing.T) {
		requireTextListRefused(t, g, m, textListRawWire(t, m, "123456789"))
	})

	// An element AT M is in contract: it saves the Go writer's bytes and
	// loads back equal.
	t.Run("element at M", func(t *testing.T) {
		raw := textListRawWire(t, m, "12345678")
		back := m.New(m.Lookup("Lists"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, back, raw, &report); !ok || err != nil || report.Refused {
			t.Fatalf("the Go load refuses the wire: ok=%v err=%v report=%+v", ok, err, report)
		}
		got := runTextListProbe(t, g, raw)
		if got.report != goReportText(report) {
			t.Errorf("the load report is %s, want the Go engine's %s", got.report, goReportText(report))
		}
		if string(got.saved) != string(raw) {
			t.Errorf("the save is not the Go writer's bytes: %d saved, %d written; %s", len(got.saved), len(raw), firstDiff(got.saved, raw))
		}
		if string(got.resaved) != string(raw) {
			t.Errorf("the value does not load back equal: %d bytes; %s", len(got.resaved), firstDiff(got.resaved, raw))
		}
	})

	t.Run("ill-formed UTF-8 is malformed", func(t *testing.T) {
		raw := textListRawWire(t, m, "\xff\xfe")
		var report tabletext.Report
		// damage discards the whole load (docs/SPEC-TABLES.md §4): the Go
		// load refuses it, and the leg owes the refusal and nothing else
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Lists")), raw, &report); ok || !tablewire.ReadRefusal(err) {
			t.Fatalf("the Go load does not refuse the damaged load: ok=%v err=%v", ok, err)
		}
		if !report.Refused {
			t.Fatalf("the Go load does not call the element malformed: %+v", report)
		}
		got := runTextListProbe(t, g, raw)
		if !strings.HasSuffix(got.report, ",true") {
			t.Errorf("the load report is %s, and a damaged load owes malformed", got.report)
		}
		requireTextListRefused(t, g, m, raw)
	})
}
