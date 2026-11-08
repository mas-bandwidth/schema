package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The featurewitnesses unit's feature_full row holds a bounded text list with
// a total, an optional text list, a `[]string` list and a `map[string]V` key
// (docs/SPEC-TABLES.md §2.13, §2.13.1). These controls break, one at a time,
// the Luau runtime's handling of each and require the row to go red.

// featurePatched answers the featurewitnesses unit with one runtime file
// edited; each anchor must occur exactly once.
func featurePatched(t *testing.T, file string, edits [][2]string) *generatedUnit {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "featurewitnesses")
	if g.err != nil {
		t.Fatalf("featurewitnesses does not generate for Luau: %v", g.err)
	}
	src := string(g.files[file])
	for _, e := range edits {
		if n := strings.Count(src, e[0]); n != 1 {
			t.Fatalf("the control's anchor %q appears %d times in %s, want exactly once", e[0], n, file)
		}
		src = strings.Replace(src, e[0], e[1], 1)
	}
	out := &generatedUnit{files: map[string][]byte{}, loaded: true, flags: g.flags}
	maps.Copy(out.files, g.files)
	out.files[file] = []byte(src)
	return out
}

// TestLuauFeatureFullLayoutNegativeControl lays a text list element out as
// its bare buffer instead of its element record: the record holding the
// list must disagree with the compiler again.
func TestLuauFeatureFullLayoutNegativeControl(t *testing.T) {
	t.Parallel()
	planted := featurePatched(t, "schema_cookread.luau", [][2]string{{
		"return { size = align_up(((fd.bound or 0) + 1) * w, 4) + 4, align = 4 }",
		"return { size = ((fd.bound or 0) + 1) * w, align = w }",
	}})
	for _, lu := range luauLayoutUnits(t) {
		if lu.key != "featurewitnesses" {
			continue
		}
		bad, _ := layoutDisagreements(t, lu, luauLayoutLines(t, planted, lu.names))
		if len(bad) == 0 {
			t.Fatal("the bare-buffer text list element left every record agreeing with the compiler")
		}
		return
	}
	t.Fatal("featurewitnesses carries no cook reader: the control is blind")
}

// featureCook cooks the feature_full row through the planted unit's Luau cook
// writer and answers its hex beside the tool's.
func featureCook(t *testing.T, planted *generatedUnit) (got, want string) {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["featurewitnesses"]))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "feature-witnesses", "full", "Root.bin"))
	if err != nil {
		t.Fatal(err)
	}
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		t.Fatal(err)
	}
	tool, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	module := ""
	for name, data := range planted.files {
		if base, ok := strings.CutSuffix(name, "Table.luau"); ok && strings.Contains(string(data), "RootDescriptor") {
			module = base + "Table"
		}
	}
	if module == "" {
		t.Fatal("no generated module declares RootDescriptor")
	}
	build := make([]byte, 8)
	binary.LittleEndian.PutUint64(build, ir.BuildVersion(u))
	probe := `local T = require("./` + module + `")
local L = require("./schema_load")
local C = require("./schema_cook")
local inputs = require("./inputs")
C.BuildVersion = buffer.readinteger(inputs.build, 0)
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = L.load("Root", inputs.wire, report)
local c = v and C.Cook(T.RootDescriptor, v, 1)
if c == nil then print("REFUSED") return end
local parts = table.create(buffer.len(c))
for i = 0, buffer.len(c) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(c, i)) end
print(table.concat(parts))
`
	return runProbe(t, planted, probe, map[string][]byte{"wire": wire, "build": build}), hex.EncodeToString(tool)
}

// TestLuauFeatureFullCookMatchesTheTool holds the Luau cook writer to the
// tool's bytes on feature_full: a bounded text list's element records with
// their used lengths, and unbounded text carved in its holder's extent
// (docs/SPEC-TABLES.md §2.13, §2.13.1, §7.6).
func TestLuauFeatureFullCookMatchesTheTool(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	if got, want := featureCook(t, generateUnit(t, units, "featurewitnesses")); got != want {
		t.Fatalf("the Luau cook of feature_full differs from the tool's:\n got %s\nwant %s", got, want)
	}
}

// TestLuauFeatureFullCookNegativeControl breaks the cook writer twice: once
// numbering unbounded text as a string node instead of carving it in the
// holder's extent, once dropping a text list element's used length. Each
// must stop matching the tool's file.
func TestLuauFeatureFullCookNegativeControl(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string][2]string{
		"text_not_carved":        {"\tif is_text(fd) then\n\t\t-- UNBOUNDED TEXT", "\tif false then\n\t\t-- UNBOUNDED TEXT"},
		"element_length_dropped": {"\t\tput(w, piece_at(e, 2, at), count, 4)\n", "\t\tif e.slot ~= nil and holder ~= nil then put(w, piece_at(e, 2, at), count, 4) end\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, want := featureCook(t, featurePatched(t, "schema_cook.luau", [][2]string{edit})); got == want {
				t.Fatalf("the %s control still matched the tool's cook", name)
			}
		})
	}
}

// TestLuauFeatureFullJsonNegativeControl spells an optional text list's
// elements as their records: feature_full must stop reading or writing the
// corpus bytes.
func TestLuauFeatureFullJsonNegativeControl(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string][2]string{
		"optional_elements_as_records": {"    if fd.opt_text then\n      -- AN OPTIONAL TEXT LIST", "    if false then\n      -- AN OPTIONAL TEXT LIST"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			featureJsonRed(t, name, featurePatched(t, "schema_json.luau", [][2]string{edit}))
		})
	}
}

// featureJsonRed requires the planted unit to miss feature_full's corpus
// bytes in at least one direction.
func featureJsonRed(t *testing.T, name string, planted *generatedUnit) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", "feature_full.json"))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "feature-witnesses", "full", "Root.bin"))
	if err != nil {
		t.Fatal(err)
	}
	out := runProbe(t, planted, jsonInstanceProbe(planted, "Root"), map[string][]byte{"text": text, "wire": wire})
	parts := strings.Split(out, "\n")
	if len(parts) == 3 && parts[0] == hexBytes(wire) && parts[1] == hexBytes(text) && parts[2] == "00" {
		t.Fatalf("the %s control still read and wrote the corpus bytes", name)
	}
}

// textTotalAtValue reads the text-total-at hostile row through the walk and
// writes the value it left: a text list whose elements came from the text.
func textTotalAtValue(t *testing.T, g *generatedUnit) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json-hostile", "text-total-at", "Root.json"))
	if err != nil {
		t.Fatal(err)
	}
	probe := jsonModules(g) + jsonFlags(g) + `local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = json.fromJsonWalk("Root", inputs.text, report)
local w = { reason = "" }
local out = v and json.ToJson("Root", v, w)
if out == nil then print("REFUSED " .. tostring(w.reason)) else print(buffer.tostring(out)) end
`
	return runProbe(t, g, probe, map[string][]byte{"text": text})
}

// TestLuauTextListElementsFromTheTextWriteBack holds a text list read from
// the text to its elements' own lengths (docs/SPEC-TABLES.md §2.13): the value
// text-total-at reads (a total exactly at its bound; text-total-past now
// refuses, strict-only) writes back as the manifest's value text.
func TestLuauTextListElementsFromTheTextWriteBack(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	want, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json-hostile", "text-total-at.value.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := textTotalAtValue(t, generateUnit(t, units, "featurewitnesses")); got != strings.TrimRight(string(want), "\n") {
		t.Fatalf("text-total-at wrote back\n%s\nwant\n%s", got, want)
	}
}

// TestLuauTextListElementsFromTheTextNegativeControl pads each element to its
// bound again: the write back must refuse or differ.
func TestLuauTextListElementsFromTheTextNegativeControl(t *testing.T) {
	t.Parallel()
	planted := featurePatched(t, "schema_json.luau", [][2]string{{
		"      if e.shape == \"string\" and not e.wide then\n        -- a text list element",
		"      if false then\n        -- a text list element",
	}})
	want, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json-hostile", "text-total-at.value.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := textTotalAtValue(t, planted); got == strings.TrimRight(string(want), "\n") {
		t.Fatal("elements padded to their bound still wrote the value text back")
	}
}
