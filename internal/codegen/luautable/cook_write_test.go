package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strconv"
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

// The corpus: a pointered root with a shared node (head/alias), a by-value
// child array, a union, an optional, bounded and unbounded arrays, text and
// bytes. Every row is read by schema-go's own text engine, encoded to the
// wire, and the expected cook is schema-go's tablecook.Cook of that instance
// (docs/SPEC-TABLES.md §7.6: a writer is held to the tool's bytes).
const cookWriteSchema = `package cookprobe
union Choice { number uint128
 empty }
table Node { value int32
 name string(16)
 next *Node
 choice Choice }
table Child { id uint8
 samples []uint16
 link *Node }
union Payload { number uint128
 nodes Nodes
 children Children
 empty }
table Nodes { items []*Node }
table Children { items []Child }
table Root { head *Node
 alias *Node
 flag bool
 small uint8
 wide uint128
 label string(12)
 blob bytes(8)
 items []Child
 payload Payload
 other [..2]Payload
 optional ?Child
 x float32
}
`

var cookWriteTexts = []string{
	`{}`,
	`{"head":{"&node":1,"value":5,"name":"a","next":{"value":6,"name":"b"}},"alias":{"&node":1},"flag":true,"small":2,"wide":12345678901234567890,"label":"hello","blob":"Ynl0ZXM=","items":[{"id":7,"samples":[10,20],"link":{"&node":1}},{"id":9,"samples":[]}],"payload":{"children":{"items":[{"id":4,"samples":[8,9]}]}},"other":[{"nodes":{"items":[{"&node":1}]}},{"children":{"items":[{"id":3}]}}],"optional":{"id":11,"samples":[1,2,3]},"x":1}`,
}

func cookWriteUnit(t *testing.T) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cookprobe.schema")
	if err := os.WriteFile(path, []byte(cookWriteSchema), 0o644); err != nil {
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
	return u
}

// TestLuauCookMatchesTheGoCook holds the Luau cook writer to the tool's bytes
// (docs/SPEC-TABLES.md §7.6): every corpus instance the cook carries is loaded
// from its wire by the Luau runtime, cooked, and compared byte for byte with
// schema-go's internal/tablecook output for the same value; CookMeasure is the
// length; a data cycle refuses as Go's Cook does.
func TestLuauCookMatchesTheGoCook(t *testing.T) {
	t.Parallel()
	u := cookWriteUnit(t)
	m := tabletext.NewModel(u)
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
	build := make([]byte, 8)
	binary.LittleEndian.PutUint64(build, ir.BuildVersion(u))
	if err := os.WriteFile(filepath.Join(dir, "build.bin"), build, 0o644); err != nil {
		t.Fatal(err)
	}
	probe := `--!strict
local T = require("./cookprobeTable")
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
if v == nil then print("LOAD_REFUSED") else
	local n = C.CookMeasure(T.RootDescriptor, v)
	local c = C.Cook(T.RootDescriptor, v, 1)
	if c == nil then print("COOK_REFUSED") else
		print(string.format("MEASURE %d %d", n, buffer.len(c)))
		print(hex(c))
	end
end
`
	probeInvalidUnion := `--!strict
local T = require("./cookprobeTable")
local L = require("./schema_load")
local C = require("./schema_cook")
local inputs = require("./inputs")
C.BuildVersion = buffer.readinteger(inputs.build, 0)
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = L.load("Root", inputs.wire, report)
if v ~= nil and v.Head ~= nil then
	v.Head.Choice.Type = 99
	print(string.format("INVALID_UNION %d %s", C.CookMeasure(T.RootDescriptor, v), tostring(C.Cook(T.RootDescriptor, v, 1) == nil)))
end
`
	probeCycle := `--!strict
local T = require("./cookprobeTable")
local L = require("./schema_load")
local C = require("./schema_cook")
local inputs = require("./inputs")
C.BuildVersion = buffer.readinteger(inputs.build, 0)
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = L.load("Root", inputs.wire, report)
if v ~= nil and v.Head ~= nil then
	v.Head.Next = v.Head
	print(string.format("CYCLE %d %s", C.CookMeasure(T.RootDescriptor, v), tostring(C.Cook(T.RootDescriptor, v, 1) == nil)))
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cycle.luau"), []byte(probeCycle), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "invalid_union.luau"), []byte(probeInvalidUnion), 0o644); err != nil {
		t.Fatal(err)
	}
	var cycWire []byte
	for i, text := range cookWriteTexts {
		wantInst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(wantInst, []byte(text), &report) {
			t.Fatalf("row %d: schema-go's text read: %+v", i, report)
		}
		wire, err := tablewire.Encode(m, wantInst)
		if err != nil {
			t.Fatalf("row %d: wire: %v", i, err)
		}
		if i == 1 {
			cycWire = wire
		}
		want, err := tablecook.Cook(m, wantInst, tablecook.Options{})
		if err != nil {
			t.Fatalf("row %d: schema-go's cook: %v", i, err)
		}
		out := string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire, "build": build}))
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) == 1 && lines[0] == "LOAD_REFUSED" {
			t.Fatalf("row %d: the Luau load refused an instance schema-go read", i)
		}
		if len(lines) == 1 && lines[0] == "COOK_REFUSED" {
			t.Fatalf("row %d: the Luau cook refused an instance the tool cooks", i)
		}
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "MEASURE ") {
			t.Fatalf("row %d: unexpected probe output %q", i, out)
		}
		f := strings.Fields(lines[0])
		measure, err := strconv.Atoi(f[1])
		if err != nil {
			t.Fatalf("row %d: measure %q: %v", i, f[1], err)
		}
		length, err := strconv.Atoi(f[2])
		if err != nil {
			t.Fatalf("row %d: length %q: %v", i, f[2], err)
		}
		if measure != len(want) {
			t.Fatalf("row %d: CookMeasure = %d, the tool's file is %d bytes", i, measure, len(want))
		}
		if length != len(want) {
			t.Fatalf("row %d: Cook wrote %d bytes, the tool's file is %d", i, length, len(want))
		}
		got, err := hex.DecodeString(lines[1])
		if err != nil {
			t.Fatalf("row %d: probe line is not hex: %v", i, err)
		}
		if string(got) != string(want) {
			at := 0
			for at < len(got) && at < len(want) && got[at] == want[at] {
				at++
			}
			var g, w byte
			if at < len(got) {
				g = got[at]
			}
			if at < len(want) {
				w = want[at]
			}
			t.Fatalf("row %d: cook differs at byte %d: got %x want %x", i, at, g, w)
		}
	}
	// A data cycle is §7.6's refusal: schema-go's Cook refuses the same value.
	cyclic := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(cyclic, []byte(cookWriteTexts[1]), &report) {
		t.Fatalf("cycle row: schema-go's text read: %+v", report)
	}
	var head *tabletext.Instance
	for i := range cyclic.Fields {
		if cyclic.Fields[i].Def.Name == "head" {
			head = cyclic.Fields[i].Cell.Node
		}
	}
	if head == nil {
		t.Fatal("cycle row: no head")
	}
	for i := range head.Fields {
		if head.Fields[i].Def.Name == "next" {
			head.Fields[i].Cell.Node = head
		}
	}
	if _, err := tablecook.Cook(m, cyclic, tablecook.Options{}); err == nil {
		t.Fatal("schema-go's Cook accepted a data cycle")
	}
	out := string(luaurun.Run(t, dir, "cycle.luau", map[string][]byte{"wire": cycWire, "build": build}))
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) != 3 || f[0] != "CYCLE" || f[1] != "-1" || f[2] != "true" {
		t.Fatalf("cycle refusal: got %q, want CYCLE -1 true", out)
	}

	// An invalid union held by a numbered pointee is invalid_value in both
	// measure and cook, just as it is in schema-go's cook.
	invalid := m.New(m.Lookup("Root"))
	report = tabletext.Report{}
	if !m.Read(invalid, []byte(cookWriteTexts[1]), &report) {
		t.Fatalf("invalid-union row: schema-go's text read: %+v", report)
	}
	var invalidHead *tabletext.Instance
	for i := range invalid.Fields {
		if invalid.Fields[i].Def.Name == "head" {
			invalidHead = invalid.Fields[i].Cell.Node
		}
	}
	if invalidHead == nil {
		t.Fatal("invalid-union row: no head")
	}
	var choice *tabletext.Field
	for i := range invalidHead.Fields {
		if invalidHead.Fields[i].Def.Name == "choice" {
			choice = &invalidHead.Fields[i]
		}
	}
	if choice == nil {
		t.Fatal("invalid-union row: no choice")
	}
	choice.Cell.U = 99
	if _, err := tablecook.Cook(m, invalid, tablecook.Options{}); err == nil {
		t.Fatal("schema-go's Cook accepted an invalid union in a pointee")
	}
	out = string(luaurun.Run(t, dir, "invalid_union.luau", map[string][]byte{"wire": cycWire, "build": build}))
	f = strings.Fields(strings.TrimSpace(out))
	if len(f) != 3 || f[0] != "INVALID_UNION" || f[1] != "-1" || f[2] != "true" {
		t.Fatalf("invalid union refusal: got %q, want INVALID_UNION -1 true", out)
	}
}
