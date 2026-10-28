package luautable_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// jsonRow is one instance or json-hostile row of MANIFEST.txt: the unit, the
// root, the file the row names (a wire file or the hostile tree's directory),
// the manifest's verdict for a hostile row, and whether the row is text-less
// (docs/SPEC-TABLES.md §16.7: a graph past the depth cap, or a blob named
// twice).
type jsonRow struct {
	kind     string
	caseName string
	unit     string
	root     string
	path     string
	expected string
	noText   bool
}

// jsonManifestRows reads the instance and json-hostile lines of the tables
// manifest (test/conformance/README.md, the row kinds).
func jsonManifestRows(t *testing.T) []jsonRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []jsonRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if fields[0] != "instance" && fields[0] != "json-hostile" {
			continue
		}
		if len(fields) < 5 {
			continue
		}
		r := jsonRow{kind: fields[0], caseName: fields[1], unit: fields[2], root: fields[3], path: fields[4]}
		if r.kind == "json-hostile" && len(fields) >= 6 {
			r.expected = fields[5]
			// carried-by=<ports> uncarried=<verdict> (docs/SPEC-TABLES.md §17):
			// a port the row does not name still owes the uncarried verdict.
			if len(fields) >= 8 {
				if carried, ok := strings.CutPrefix(fields[6], "carried-by="); ok && !slices.Contains(strings.Split(carried, ","), "luau") {
					r.expected = strings.TrimPrefix(fields[7], "uncarried=")
				}
			}
		}
		for _, extra := range fields[5:] {
			if extra == "no-text" {
				r.noText = true
			}
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// jsonModules is the Luau prologue that registers every table module of a
// unit, so a root's descriptor is in schema_tables' registry.
func jsonModules(g *generatedUnit) string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	return b.String()
}

// jsonFlags is the Luau preamble that hands the emitted runtime the flags
// variant names the descriptor does not carry (docs/SPEC-TABLES.md §16.2, the
// flags row: an array of variant names, in bit order).
func jsonFlags(g *generatedUnit) string {
	var names []string
	for k := range g.flags {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("local SCHEMA_JSON_FLAGS = {\n")
	for _, k := range names {
		fmt.Fprintf(&b, "[%q] = {", k)
		for i, v := range g.flags[k] {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", v)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// jsonInstanceProbe runs both directions of §16.1 in one process: FromJson of
// the text then Save, and Load of the wire then ToJson. It prints the saved
// wire as hex, the written text as hex, and one character per direction saying
// whether it reported malformed.
func jsonInstanceProbe(g *generatedUnit, rootName string) string {
	return jsonModules(g) + jsonFlags(g) + fmt.Sprintf(`local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local r1 = report()
local v1 = json.FromJson(%q, inputs.text, r1)
local saved = nil
if v1 ~= nil then saved = save.save(St.lookup(%q), v1) end
local r2 = report()
local v2 = load.load(%q, inputs.wire, r2)
local text = nil
if v2 ~= nil then text = json.ToJson(%q, v2) end
print(hex(saved))
print(hex(text))
print((r1.malformed and "1" or "0") .. (r2.malformed and "1" or "0"))
`, rootName, rootName, rootName, rootName)
}

// jsonHostileProbe reads one hostile text and prints the manifest's verdict
// line (`u,k,w,c,d,m,verdict`, or `refused`), docs/SPEC-TABLES.md §16.2, §4.
func jsonHostileProbe(g *generatedUnit, rootName string) string {
	return jsonModules(g) + jsonFlags(g) + fmt.Sprintf(`local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = json.FromJsonTolerant(%q, inputs.text, report)
if v == nil then report.malformed = true end
if report.malformed then
	print("refused")
else
	print(string.format("%%d,%%d,%%d,%%d,%%d,%%s,read",
		report.unknown, report.kindMismatch, report.widened, report.clamped,
		report.duplicate, tostring(report.malformed)))
end
`, rootName)
}

func hexBytes(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		fmt.Fprintf(&b, "%02x", c)
	}
	return b.String()
}

// TestLuauJsonAgreesWithTheCorpus reads every instance row the target
// generates both ways (docs/SPEC-TABLES.md §16.5): FromJson of the JSON text
// then Save equals the wire file, and Load of the wire then ToJson equals the
// JSON file byte for byte; every json-hostile row gives the manifest's
// verdict (test/conformance/README.md, json-hostile).
func TestLuauJsonAgreesWithTheCorpus(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	cache := map[string]*generatedUnit{}
	readCompared, writeCompared, hostileCompared := 0, 0, 0
	instances, skipped, ungenerated := 0, 0, 0
	var mismatches []string
	for _, row := range jsonManifestRows(t) {
		if _, ok := units[row.unit]; !ok {
			continue
		}
		if row.kind == "instance" && row.noText {
			skipped++
			continue
		}
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g.err != nil {
			ungenerated++
			continue
		}
		if row.kind == "instance" {
			text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", row.caseName+".json"))
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("instance %s: read text: %v", row.caseName, err))
				continue
			}
			wire, err := os.ReadFile(filepath.Join(root, row.path))
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("instance %s: read wire: %v", row.caseName, err))
				continue
			}
			instances++
			if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonInstanceProbe(g, row.root)), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": text, "wire": wire})), "\n")
			parts := strings.Split(out, "\n")
			if len(parts) != 3 {
				mismatches = append(mismatches, fmt.Sprintf("instance %s: bad output %q", row.caseName, out))
				continue
			}
			if parts[2] != "00" {
				mismatches = append(mismatches, fmt.Sprintf("instance %s: a direction reported malformed (%s)", row.caseName, parts[2]))
				continue
			}
			if parts[0] != hexBytes(wire) {
				mismatches = append(mismatches, fmt.Sprintf("instance %s: FromJson then Save != the wire", row.caseName))
			} else {
				readCompared++
			}
			if parts[1] != hexBytes(text) {
				mismatches = append(mismatches, fmt.Sprintf("instance %s: Load then ToJson != the json text", row.caseName))
			} else {
				writeCompared++
			}
			continue
		}
		text, err := os.ReadFile(filepath.Join(root, row.path, row.root+".json"))
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("hostile %s: read text: %v", row.caseName, err))
			continue
		}
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonHostileProbe(g, row.root)), 0o644); err != nil {
			t.Fatal(err)
		}
		got := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": text})), "\n")
		if got != row.expected {
			mismatches = append(mismatches, fmt.Sprintf("hostile %s: got %s, want %s", row.caseName, got, row.expected))
			continue
		}
		hostileCompared++
	}
	sort.Strings(mismatches)
	for _, m := range mismatches {
		t.Error(m)
	}
	if instances == 0 {
		t.Fatal("no instance row compared")
	}
	if readCompared == 0 || writeCompared == 0 {
		t.Fatalf("no comparison in both directions: json-read %d, json-write %d", readCompared, writeCompared)
	}
	if hostileCompared == 0 {
		t.Fatal("no json-hostile row compared")
	}
	t.Logf("%d instances compared (json-read %d, json-write %d), %d json-hostile rows compared, %d text-less instances skipped, %d rows of units the target refuses to generate",
		instances, readCompared, writeCompared, hostileCompared, skipped, ungenerated)
}

// TestLuauJsonNegativeControl patches the emitted runtime in memory — one
// anchor in the real float writer, matched exactly once — so a float32 renders
// at six significant digits (C's %#.6g, not the shortest text), and requires
// the json-write comparison to go red while json-read and both reports stay
// green (docs/SPEC-TABLES.md §16.5, the per-backend negative control and its
// second half; §16.1, the float spelling is C's %.*g).
func TestLuauJsonNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "local text = format_g(v, digits)"
	const patched = `local text = single and string.format("%#.6g", v) or format_g(v, digits)`
	src, err := os.ReadFile(filepath.Join("runtime", "schema_json.luau"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the float writer anchor %q is not in the runtime exactly once", anchor)
	}
	units, _ := readManifest(t)
	g := generateUnit(t, units, "tabledemo")
	if g.err != nil {
		t.Fatalf("tabledemo does not generate: %v", g.err)
	}
	bad := strings.Replace(string(src), anchor, patched, 1)
	if bad == string(src) {
		t.Fatal("the patch changed nothing")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "schema_json.luau"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", "root_full.json"))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "root_full.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonInstanceProbe(g, "RootConfig")), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": text, "wire": wire})), "\n")
	parts := strings.Split(out, "\n")
	if len(parts) != 3 {
		t.Fatalf("bad output %q", out)
	}
	if parts[2] != "00" {
		t.Fatalf("the patched runtime reports malformed (%s), so the control would go red for the wrong reason", parts[2])
	}
	if parts[0] != hexBytes(wire) {
		t.Fatalf("the patched runtime broke json-read too, so the control is not the float spelling alone")
	}
	if parts[1] == "nil" {
		t.Fatalf("the patched runtime wrote no text, so the control would go red for the wrong reason")
	}
	if parts[1] == hexBytes(text) {
		t.Fatalf("the patched runtime still writes the pinned text byte for byte")
	}
	t.Logf("the json-write comparison went red:%d bytes written, %d pinned", len(parts[1])/2, len(text))
}

// jsonElemProbe reads one JSON text under Box and prints the read's
// kind_mismatch and unknown counts, then the text ToJson writes from the read value, then
// the text ToJson writes after Save and Load of that value
// (docs/SPEC-TABLES.md §16.5, both directions in one process).
const jsonElemProbe = `require("./BoxTable")
local json = require("./schema_json")
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local r = report()
local v = json.FromJsonTolerant("Box", inputs.text, r)
if v == nil then print("refused"); return end
print(string.format("km=%d u=%d", r.kindMismatch, r.unknown))
print(buffer.tostring(json.ToJson("Box", v)))
local back = load.load("Box", save.save(St.lookup("Box"), v), report())
if back == nil then print("reload refused"); return end
print(buffer.tostring(json.ToJson("Box", back)))
`

// TestLuauJsonReadKeepsFalseElements pins the false sentinel of the JSON
// reader (docs/SPEC-TABLES.md §16.2, the enum, map and `*bytes` rows): an
// enum "None" (or an unknown name, counted unknown) read as a map value or a
// list element is the enum zero element, held as false and kept, so the map
// keeps its entry and the list its length
// (`elem and false or nil` is always nil in Lua: it deleted the entry and cut
// the list at the hole); a false bool element is kept; and a `*bytes` map
// value that is null, or not base64 (kind_mismatch), stays an entry holding a
// null reference. Each text round-trips byte for byte through FromJson and
// ToJson, and again through Save and Load.
func TestLuauJsonReadKeepsFalseElements(t *testing.T) {
	t.Parallel()
	const schema = "package p\nenum E { A, B }\ntable Box\n{\n\tm map[string]E\n\tl []E\n\tb []bool\n\tz map[string]*bytes\n}\n"
	g := generateFiles(t, map[string]string{"Box.schema": schema})
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonElemProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	const canonical = "{\n  \"m\": {\n    \"one\": \"B\",\n    \"zero\": \"None\"\n  },\n  \"l\": [\n    \"A\",\n    \"None\",\n    \"B\"\n  ],\n  \"b\": [\n    true,\n    false,\n    true\n  ],\n  \"z\": {\n    \"bad\": null,\n    \"ok\": \"AQI=\"\n  }\n}\n"
	for _, tc := range []struct {
		name, text, km string
	}{
		{"enum-zero-map-value-and-list-element", canonical, "km=0 u=0"},
		{"enum-unknown-name-list-element", strings.Replace(canonical, "\"A\",\n    \"None\"", "\"A\",\n    \"Q\"", 1), "km=0 u=1"},
		{"enum-unknown-name-map-value", strings.Replace(canonical, `"zero": "None"`, `"zero": "Q"`, 1), "km=0 u=1"},
		{"bytes-map-value-not-base64", strings.Replace(canonical, `"bad": null`, `"bad": "!!"`, 1), "km=1 u=0"},
	} {
		out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(tc.text)}))
		head, rest, _ := strings.Cut(out, "\n")
		if head != tc.km {
			t.Errorf("%s: FromJson reported %q, want %q (output %q)", tc.name, head, tc.km, out)
			continue
		}
		if want := canonical + "\n" + canonical + "\n"; rest != want {
			t.Errorf("%s: ToJson, then Save, Load and ToJson, wrote\n%s\nwant the text, entries and length kept, twice:\n%s", tc.name, rest, want)
		}
	}
}
