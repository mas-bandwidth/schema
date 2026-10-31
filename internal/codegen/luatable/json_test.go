package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsonRow is one instance or json-hostile row of MANIFEST.txt, with the
// options a row carries (no-text) and the expected report line for a hostile.
type jsonRow struct {
	caseName string
	unit     string
	root     string
	path     string
	expected string
	noText   bool
}

func jsonManifestRows(t *testing.T, path, kind string) []jsonRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []jsonRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] != kind || len(f) < 5 {
			continue
		}
		row := jsonRow{caseName: f[1], unit: f[2], root: f[3], path: f[4]}
		if kind == "json-hostile" && len(f) >= 6 {
			row.expected = f[5]
			// a rule held first in some ports names them with
			// carried-by=<ports>; a row Lua does not carry owes its
			// uncarried verdict (the harness's Hostile.VerdictFor)
			if len(f) >= 8 && strings.HasPrefix(f[6], "carried-by=") && strings.HasPrefix(f[7], "uncarried=") &&
				!slices.Contains(strings.Split(strings.TrimPrefix(f[6], "carried-by="), ","), "lua") {
				row.expected = strings.TrimPrefix(f[7], "uncarried=")
			}
		}
		for _, extra := range f[5:] {
			if extra == "no-text" {
				row.noText = true
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// jsonInstanceProbe runs both directions in one process: FromJson(json) ->
// Save, and Load(wire) -> ToJson.
func jsonInstanceProbe(module, root string) string {
	return `local T = require("` + module + `")
local J = require("json")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local function hex(s)
  if s == nil then return "nil" end
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local r1 = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v1 = J.FromJsonTolerant(T, "` + root + `", inputs.text, r1)
local saved = save.Save(T, "` + root + `", v1)
local r2 = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v2 = load.LoadTolerant(T, "` + root + `", inputs.wire, r2)
local text = J.ToJson(T, "` + root + `", v2)
print(hex(saved))
print(hex(text))
print((r1.malformed and "1" or "0") .. (r2.malformed and "1" or "0"))
`
}

// jsonHostileProbe reads one hostile text and prints its report line in the
// manifest's shape (docs/SPEC-TABLES.md §16.2, §4).
func jsonHostileProbe(module, root string) string {
	return `local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = J.FromJsonTolerant(T, "` + root + `", inputs.text, report)
if v == nil then report.malformed = true end
if report.malformed then
  print("refused")
else
  print(string.format("%d,%d,%d,%d,%d,%s,read",
    report.unknown, report.kindMismatch, report.widened, report.clamped,
    report.duplicate, tostring(report.malformed)))
end
`
}

// jsonRun generates one unit, optionally patches its emitted json.lua, writes
// the inputs and runs the probe.
func jsonRun(t *testing.T, paths []string, module, root, probe string, inputs map[string][]byte, patch func([]byte) []byte) string {
	t.Helper()
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", module, err)
	}
	if patch != nil {
		src, ok := files["json.lua"]
		if !ok {
			t.Fatal("the emitted unit has no json.lua")
		}
		files["json.lua"] = patch(src)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n")
}

func hexBytes(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		fmt.Fprintf(&b, "%02x", c)
	}
	return b.String()
}

// TestLuaJsonAgreesWithTheCorpus reads every instance row the target generates
// both ways (docs/SPEC-TABLES.md §16.5): FromJson of the JSON text then Save
// equals the wire, and Load of the wire then ToJson equals the JSON text byte
// for byte; every json-hostile row gives the manifest's verdict.
func TestLuaJsonAgreesWithTheCorpus(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}
	refused := map[string]string{}
	readCompared, writeCompared := 0, 0
	instances, skipped := 0, 0
	var mismatches []string
	for _, row := range jsonManifestRows(t, manifestPath, "instance") {
		if row.noText {
			skipped++
			continue
		}
		paths, ok := unitFiles[row.unit]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: no manifest unit", row.caseName))
			continue
		}
		if _, bad := refused[row.unit]; bad {
			skipped++
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if len(ir.TableWideFields(u)) > 0 {
			refused[row.unit] = "carries the wide kinds; the JSON corpus row is not this card's"
			skipped++
			continue
		}
		if _, err := Generate(u); err != nil {
			refused[row.unit] = err.Error()
			skipped++
			continue
		}
		instances++
		textData, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", row.caseName+".json"))
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: read text: %v", row.caseName, err))
			continue
		}
		wireData, err := os.ReadFile(filepath.Join(repo, row.path))
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: read wire: %v", row.caseName, err))
			continue
		}
		out := jsonRun(t, paths, ModuleName(u), row.root, jsonInstanceProbe(ModuleName(u), row.root),
			map[string][]byte{"text": textData, "wire": wireData}, nil)
		parts := strings.Split(out, "\n")
		if len(parts) != 3 {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: bad output %q", row.caseName, out))
			continue
		}
		if parts[2] != "00" {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: a direction reported malformed (%s)", row.caseName, parts[2]))
			continue
		}
		if parts[0] != hexBytes(wireData) {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: FromJson then Save != wire", row.caseName))
		} else {
			readCompared++
		}
		if parts[1] != hexBytes(textData) {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: Load then ToJson != the json text", row.caseName))
		} else {
			writeCompared++
		}
	}
	for _, m := range mismatches {
		t.Error(m)
	}
	if readCompared == 0 || writeCompared == 0 {
		t.Fatalf("no instance round-tripped (read %d, write %d, %d instances, %d skipped)", readCompared, writeCompared, instances, skipped)
	}

	hostileCompared, hostileSkipped := 0, 0
	for _, row := range jsonManifestRows(t, manifestPath, "json-hostile") {
		paths, ok := unitFiles[row.unit]
		if !ok {
			t.Errorf("json-hostile %s: no manifest unit", row.caseName)
			continue
		}
		if _, bad := refused[row.unit]; bad {
			hostileSkipped++
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if len(ir.TableWideFields(u)) > 0 {
			hostileSkipped++
			continue
		}
		if _, err := Generate(u); err != nil {
			refused[row.unit] = err.Error()
			hostileSkipped++
			continue
		}
		textData, err := os.ReadFile(filepath.Join(repo, row.path, row.root+".json"))
		if err != nil {
			t.Errorf("json-hostile %s: read text: %v", row.caseName, err)
			continue
		}
		out := jsonRun(t, paths, ModuleName(u), row.root, jsonHostileProbe(ModuleName(u), row.root),
			map[string][]byte{"text": textData}, nil)
		if row.expected == "refused" {
			if out != "refused" {
				t.Errorf("json-hostile %s: got %q, want refused", row.caseName, out)
			} else {
				hostileCompared++
			}
			continue
		}
		if out != row.expected {
			t.Errorf("json-hostile %s: got %q, want %q", row.caseName, out, row.expected)
		} else {
			hostileCompared++
		}
	}
	names := make([]string, 0, len(refused))
	for unit := range refused {
		names = append(names, unit)
	}
	sort.Strings(names)
	for _, unit := range names {
		t.Logf("refused unit %s: %s", unit, refused[unit])
	}
	t.Logf("instances: %d json-read and %d json-write compared (%d instances, %d no-text/refused skipped)",
		readCompared, writeCompared, instances, skipped)
	t.Logf("json-hostile: %d rows compared (%d skipped by a refused unit)", hostileCompared, hostileSkipped)
}

// TestLuaJsonNegativeControl patches the emitted runtime in memory — one
// anchor, matched exactly once — so a float32 renders at six significant
// digits, and requires the json-write comparison to go red.
func TestLuaJsonNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "local low, high = single and 6 or 15, single and 9 or 17"
	const sabotaged = "local low, high = 6, 6"
	patch := func(src []byte) []byte {
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the negative-control anchor is matched %d times, want exactly once", n)
		}
		return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	}

	// a generated unit whose float32 needs more than six significant digits:
	// the writer's shortest spelling differs from the sabotaged one
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "F.schema")
	if err := os.WriteFile(schemaPath, []byte(`package f
table F { v float32 }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{schemaPath}
	u := luarun.LoadUnit(t, paths...)
	probe := func(module string) string {
		return `local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = J.FromJsonTolerant(T, "F", inputs.text, report)
local text = J.ToJson(T, "F", v)
if text == nil then print("nil") else print((text:gsub(".", function(c) return string.format("%02x", c:byte()) end))) end
`
	}
	inputs := map[string][]byte{"text": []byte("{ \"v\": 1.2345678 }\n")}
	full := jsonRun(t, paths, ModuleName(u), "F", probe(ModuleName(u)), inputs, nil)
	sab := jsonRun(t, paths, ModuleName(u), "F", probe(ModuleName(u)), inputs, patch)
	if full == "nil" || sab == "nil" {
		t.Fatalf("the synthetic float32 did not write: full %q sabotaged %q", full, sab)
	}
	if full == sab {
		t.Fatalf("the sabotaged json-write still matched: %q", sab)
	}
	t.Logf("a six-significant-digit float32 diverges from the shortest spelling (full %d bytes, sabotaged %d bytes)", len(full)/2, len(sab)/2)

	// the corpus comparison goes red too, wherever it carries a float the
	// shortened spelling cannot reproduce
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, list := range units {
		var files []string
		for _, p := range list {
			files = append(files, filepath.Join(repo, p))
		}
		unitFiles[k] = files
	}
	diverged, compared := 0, 0
	for _, row := range jsonManifestRows(t, manifestPath, "instance") {
		if row.noText {
			continue
		}
		paths, ok := unitFiles[row.unit]
		if !ok {
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			continue
		}
		textData, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", row.caseName+".json"))
		if err != nil {
			continue
		}
		wireData, err := os.ReadFile(filepath.Join(repo, row.path))
		if err != nil {
			continue
		}
		out := jsonRun(t, paths, ModuleName(u), row.root, jsonInstanceProbe(ModuleName(u), row.root),
			map[string][]byte{"text": textData, "wire": wireData}, patch)
		parts := strings.Split(out, "\n")
		if len(parts) != 3 {
			continue
		}
		compared++
		if parts[1] != hexBytes(textData) {
			diverged++
		}
	}
	t.Logf("%d of %d corpus json-write comparisons diverge with float32 at six significant digits", diverged, compared)
}

// TestLuaJsonReadsNullReferences pins docs/SPEC-TABLES.md §16.2's null rows
// where no corpus row reaches them: a `null` map VALUE of a `*bytes` map is a
// present entry holding a null reference, and a `null` payload on a `*bytes`
// union arm selects that arm with a null reference. Neither counts
// kind_mismatch, and each writes back as the same `null`.
func TestLuaJsonReadsNullReferences(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "N.schema")
	if err := os.WriteFile(schemaPath, []byte(`package n
union Payload
{
    data *bytes
    tally int32
}
table N
{
    blobs map[int32]*bytes
    body  Payload
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{dir}
	u := luarun.LoadUnit(t, paths...)
	module := ModuleName(u)
	probe := `local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = J.FromJsonTolerant(T, "N", inputs.text, report)
local has = v ~= nil and v.blobs[7] == false and v.blobs[8] ~= nil and v.blobs[8].value == "\1\2"
local arm = v ~= nil and v.body.tag == 1 and v.body.data == nil
print(string.format("km=%d entries=%s arm=%s", report.kindMismatch, tostring(has), tostring(arm)))
local text = v and J.ToJson(T, "N", v)
print(text or "nil")
`
	inputs := map[string][]byte{"text": []byte("{ \"blobs\": { \"7\": null, \"8\": \"AQI=\" }, \"body\": { \"data\": null } }\n")}
	out := jsonRun(t, paths, module, "N", probe, inputs, nil)
	lines := strings.SplitN(out, "\n", 2)
	if lines[0] != "km=0 entries=true arm=true" {
		t.Fatalf("null references read wrong: %q", out)
	}
	if len(lines) < 2 || !strings.Contains(lines[1], "\"7\": null") || !strings.Contains(lines[1], "\"data\": null") {
		t.Fatalf("null references did not write back as null: %q", out)
	}
	t.Logf("a null *bytes map value and a null *bytes arm read without kind_mismatch and write back as null")
}
