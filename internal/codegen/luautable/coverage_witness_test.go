package luautable_test

// THE COVERAGE WITNESSES: one test per runtime defect the conformance leg
// found when its json, cook-write and retain surfaces were wired
// (test/conformance/luau/main.luau). Each holds the Luau runtime to the Go
// reference engine's bytes on the smallest schema that reaches the defect,
// so planting the old behaviour back fails here by name:
//
//   - schema_cook.luau placed a string's int32 length companion by aligning against
//     the FIELD's start rather than the record's (a string(N) at an odd
//     offset), stored bits(N) at its wire kind's width rather than the
//     uint32/uint64 ir/blocklayout.go stores it at, and never wrote a float64;
//   - the descriptor carried no flags variant names, so schema_json.luau wrote a
//     flags mask as an integer rather than its §16.2 name array.
//
// The retain witnesses are corpus rows and live in retain_corpus_test.go.

import (
	"encoding/binary"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// witnessSchema puts a string(5) at offset 1 (its length companion at 8, not
// at 1 + align_up(6, 4) = 9), a bits(6) beside it, a float64, and a flags
// mask, in a fixed table and nested in one.
const witnessSchema = `package witness
flags Perks { Shielded, Cloaked, Turbo }
type Gear {
 grade uint8
 tag string(5)
 channel bits(6)
}
fixed table Root {
 small uint8
 name string(5)
 channel bits(6)
 precision float64
 perks Perks
 gear Gear
 kit [2]Perks
}
`

const witnessText = `{"small":3,"name":"aged","channel":33,"precision":2.5,"perks":["Cloaked","Turbo"],"gear":{"grade":1,"tag":"ab","channel":5},"kit":[["Shielded"],[]]}`

// witnessUnit writes the schema, generates the Luau type and table surface
// into a fresh directory and returns the unit, the reference model and the
// directory.
func witnessUnit(t *testing.T) (*ir.Unit, *tabletext.Model, string) {
	t.Helper()
	src := t.TempDir()
	path := filepath.Join(src, "witness.schema")
	if err := os.WriteFile(path, []byte(witnessSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return u, tabletext.NewModel(u), dir
}

// witnessWire is the reference engine's wire of witnessText.
func witnessWire(t *testing.T, m *tabletext.Model) (*tabletext.Instance, []byte) {
	t.Helper()
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(witnessText), &report) {
		t.Fatalf("schema-go's text read: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return inst, wire
}

// TestLuauCookLayoutAgreesWithTheReference cooks the witness in both byte
// orders and holds each file to internal/tablecook's.
func TestLuauCookLayoutAgreesWithTheReference(t *testing.T) {
	t.Parallel()
	u, m, dir := witnessUnit(t)
	inst, wire := witnessWire(t, m)
	build := make([]byte, 8)
	binary.LittleEndian.PutUint64(build, ir.BuildVersion(u))
	probe := `--!strict
local T = require("./witnessTable")
local L = require("./schema_load")
local C = require("./schema_cook")
local inputs = require("./inputs")
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
C.BuildVersion = buffer.readinteger(inputs.build, 0)
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = L.load("Root", inputs.wire, report)
assert(v ~= nil, "the witness wire did not load")
for _, order in { 1, 2 } do
	local c = C.Cook(T.RootDescriptor, v, order)
	assert(c ~= nil, "the cook refused the witness")
	print(hex(c))
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire, "build": build}))), "\n")
	if len(lines) != 2 {
		t.Fatalf("the probe printed %d lines, want 2", len(lines))
	}
	for i, big := range []bool{false, true} {
		want, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		got, err := hex.DecodeString(lines[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			at := 0
			for at < len(got) && at < len(want) && got[at] == want[at] {
				at++
			}
			t.Errorf("big=%v: the Luau cook differs from internal/tablecook at byte %d (%d bytes, want %d)", big, at, len(got), len(want))
		}
	}
}

// TestLuauJsonSpellsFlagsFromTheDescriptor writes the witness's text with no
// SetFlags handed in: the names come from the descriptor alone, and the text
// is the reference writer's, byte for byte. Read back, it saves the same wire.
func TestLuauJsonSpellsFlagsFromTheDescriptor(t *testing.T) {
	t.Parallel()
	_, m, dir := witnessUnit(t)
	inst, wire := witnessWire(t, m)
	want, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	probe := `--!strict
require("./witnessTable")
local J = require("./schema_json")
local L = require("./schema_load")
local S = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function report(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local v = L.load("Root", inputs.wire, report())
assert(v ~= nil, "the witness wire did not load")
local text = J.ToJson("Root", v, report())
assert(text ~= nil, "ToJson refused the witness")
print(hex(text))
local back = J.FromJson("Root", text, report())
assert(back ~= nil, "FromJson refused the written text")
print(hex((S.save(St.lookup("Root"), back))))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire}))), "\n")
	if len(lines) != 2 {
		t.Fatalf("the probe printed %d lines, want 2", len(lines))
	}
	got, err := hex.DecodeString(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	// the reference model's Write is the document; the text form ends it in
	// exactly one newline (docs/SPEC-TABLES.md §16.1), as the corpus does
	if string(got) != strings.TrimRight(string(want), "\n")+"\n" {
		t.Errorf("the Luau text differs from the reference writer's:\n%s\nwant:\n%s", got, want)
	}
	saved, err := hex.DecodeString(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != string(wire) {
		t.Errorf("the text read back saves %x, want %x", saved, wire)
	}
}
