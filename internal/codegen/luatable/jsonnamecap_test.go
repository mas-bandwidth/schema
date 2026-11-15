package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// nameCapSchema is the small unit the read-closed vectors read
// (testdata/wire/json-read-closed/MANIFEST.tsv): a flags field, an enum, a
// union, a map keyed by string and a string field, plus a pointered root for
// the `&node` name's site.
const nameCapSchema = `package closed

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// nameCapRows are the manifest rows of the schema-name cap: 255 bytes read
// and 256 refuse at each name site, and a long map key and a long string
// value read because they are data. The depth and UTF-8 rows of the same
// corpus are other rules.
var nameCapRows = map[string]bool{
	"field-key-255": true, "field-key-256": true,
	"skipped-key-255": true, "skipped-key-256": true,
	"enum-name-255": true, "enum-name-256": true,
	"flags-element-255": true, "flags-element-256": true,
	"union-arm-255": true, "union-arm-256": true,
	"ampersand-255": true, "ampersand-256": true,
	"map-key-long": true, "string-value-long": true,
}

// TestLuaJsonNameCap holds docs/SPEC-TABLES.md §16.2's schema-name cap in
// the Lua JSON read: an object key naming a field, an enum variant name, a
// flags element, a union arm key and a pointer's first key are read at 255
// decoded bytes and refuse the whole load at 256, inside a skipped value too,
// and an unknown enum name over 255 bytes refuses rather than reading None. A
// map key and a string value are never refused for their length. The verdict
// is the contract, so no reason is compared.
func TestLuaJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, root, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
		if !nameCapRows[name] {
			continue
		}
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, verdict)
		}
		body, err := os.ReadFile(filepath.Join(dir, jsonFile))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = body
		rows = append(rows, row{name: name, root: root, verdict: verdict})
	}
	if len(rows) != len(nameCapRows) {
		t.Fatalf("the manifest names %d of the %d name-cap rows", len(rows), len(nameCapRows))
	}

	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "Closed.schema")
	if err := os.WriteFile(schemaPath, []byte(nameCapSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schemaPath)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Closed.schema: %v", err)
	}
	runDir := t.TempDir()
	luarun.WriteFiles(t, runDir, files)
	var probe strings.Builder
	probe.WriteString(`local T = require("` + ModuleName(u) + `")
local J = require("json")
local inputs = require("inputs")
local function verdict(name, root)
  local report = {}
  local ok, v = pcall(J.FromJson, T, root, inputs[name], report)
  if not ok then return "error: " .. tostring(v) end
  if v ~= nil and report.refused == 0 then return "read" end
  return "refused"
end
`)
	for _, r := range rows {
		probe.WriteString(`print("` + r.name + `=" .. verdict("` + r.name + `", "` + r.root + `"))` + "\n")
	}
	if err := os.WriteFile(filepath.Join(runDir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(luarun.Run(t, runDir, "main.lua", inputs)), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	for _, r := range rows {
		if got[r.name] != r.verdict {
			t.Errorf("%s (root %s): the Lua JSON read answered %q, want %q", r.name, r.root, got[r.name], r.verdict)
		}
	}
}
