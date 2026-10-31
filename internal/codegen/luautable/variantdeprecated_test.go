package luautable_test

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDeprecatedVariantReadsDefaultWriteRefused(t *testing.T) {
	t.Parallel()
	liveDir, liveModule, liveFiles := build(t, "internal/codegen/luautable/testdata/variantdeprecated_live.schema")
	oldDir, oldModule, oldFiles := build(t, "internal/codegen/luautable/testdata/variantdeprecated.schema")
	for name, data := range liveFiles {
		if err := os.WriteFile(filepath.Join(oldDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if liveModule == "" || oldModule == "" {
		t.Fatal("table module missing")
	}
	write := `local S = require("./schema_tables")
local Save = require("./schema_save")
local Fixed = require("./schema_fixed")
local T = require("./MOD")
local d = T.RootDescriptor
local v = d.new()
v.Ship = 2; v.Fleet = { 2 }; v.FleetCount = 1
local wire = Save.save(d, v)
local fixed = { hash=T.RootFixedHash, recordBytes=T.RootFixedRecordBytes, layout=T.RootFixedLayout }
local fw = Fixed.save(d, fixed, { v })
local function hex(b) local a = {}; for i=0,buffer.len(b)-1 do a[i+1]=string.format("%02x",buffer.readu8(b,i)) end; return table.concat(a) end
print(hex(wire)); print(hex(fw))
`
	write = strings.ReplaceAll(write, "MOD", liveModule)
	if err := os.WriteFile(filepath.Join(liveDir, "main.luau"), []byte(write), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(luaurun.Run(t, liveDir, "main.luau", nil))), "\n")
	if len(lines) != 2 {
		t.Fatalf("live writer output %q", lines)
	}
	wire, err := decodeHexLine(t, lines[0])
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := goVariantWire(t, "internal/codegen/luautable/testdata/variantdeprecated_live.schema")
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != string(oracle) {
		t.Fatalf("Luau form-1 writer differs from Go oracle (%d vs %d bytes)", len(wire), len(oracle))
	}
	fixedWire, err := decodeHexLine(t, lines[1])
	if err != nil {
		t.Fatal(err)
	}
	textWire := base64.StdEncoding.EncodeToString(wire)
	for _, name := range []string{"load", "save", "fixed", "text"} {
		if err := os.WriteFile(filepath.Join(oldDir, name+".luau"), liveFiles[name+".luau"], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := `local S = require("./schema_tables"); local inputs = require("./inputs")
local Load = require("./schema_load"); local Save = require("./schema_save"); local Text = require("./schema_text"); local Fixed = require("./schema_fixed")
local T = require("./MOD")
local d = T.RootDescriptor; local rep = { malformed=false, refused=0, unknown=0, kindMismatch=0, clamped=0, widened=0, duplicate=0 }
local v = Load.load(d.name, inputs.wire, rep)
print("read " .. tostring(v.Ship) .. " " .. tostring(v.Fleet[1]) .. " " .. tostring(rep.unknown))
local tv = Text.LoadText(T, "Root", buffer.readstring(inputs.text, 0, buffer.len(inputs.text)), rep)
if tv == nil then error("text load nil: " .. tostring(rep.reason)) end
print("textread " .. tostring(tv.Ship) .. " " .. tostring(tv.Fleet[1]) .. " " .. tostring(rep.unknown))
local bad = d.new(); bad.Ship = 2
local _, sr = Save.save(d,bad); local _, mr = Save.measure(d,bad); local _, tr = Text.SaveText(d,bad)
print("write " .. tostring(sr and sr.name) .. " " .. tostring(sr and sr.field) .. " " .. tostring(mr and mr.name) .. " " .. tostring(tr and tr.name) .. " " .. tostring(tr and tr.field))
local fixed = { hash=T.RootFixedHash, recordBytes=T.RootFixedRecordBytes, layout=T.RootFixedLayout }
local ok, err = pcall(function() Fixed.save(d, fixed, { bad }) end)
print("fixed " .. tostring(not ok) .. " " .. tostring(type(err) == "table" and err.name or err) .. " " .. tostring(type(err) == "table" and err.field or "-"))
local fw = Fixed.load(d, fixed, inputs.fixed)
print("fixedread " .. tostring(fw[1].Ship) .. " " .. tostring(fw[1].Fleet[1]))
bad.Ship = 3; bad.Fleet = { 2 }; bad.FleetCount = 1
local _, ar = Save.save(d, bad)
print("arraywrite " .. tostring(ar and ar.name) .. " " .. tostring(ar and ar.field))
local ok2, err2 = pcall(function() Fixed.save(d, fixed, { bad }) end)
print("fixedarray " .. tostring(not ok2) .. " " .. tostring(type(err2) == "table" and err2.name or err2) .. " " .. tostring(type(err2) == "table" and err2.field or "-"))
`
	read = strings.ReplaceAll(read, "MOD", oldModule)
	if err := os.WriteFile(filepath.Join(oldDir, "main.luau"), []byte(read), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luaurun.Run(t, oldDir, "main.luau", map[string][]byte{"wire": wire, "fixed": fixedWire, "text": []byte(textWire)})))
	if !strings.Contains(got, "read 3 0 0") || !strings.Contains(got, "textread 3 0 0") || !strings.Contains(got, "write invalid_value Root.ship invalid_value invalid_value Root.ship") || !strings.Contains(got, "fixed true invalid_value Root.ship") || !strings.Contains(got, "fixedread 3 0") || !strings.Contains(got, "arraywrite invalid_value Root.fleet") || !strings.Contains(got, "fixedarray true invalid_value Root.fleet") {
		t.Fatalf("deprecated behavior: %s", got)
	}
	// The enum constant stays in the language surface, and the table's own
	// descriptor carries the retirement marker the runtime reads (§2.15).
	if !strings.Contains(string(oldFiles[strings.TrimSuffix(oldModule, "Table")+".luau"]), "Bomber = 2") {
		t.Fatal("deprecated enum constant was removed")
	}
	if desc := string(oldFiles[oldModule+".luau"]); !strings.Contains(desc, `"Bomber", `) || !strings.Contains(desc, ", true }") {
		t.Fatalf("deprecated enum descriptor lacks its marker: %s", desc)
	}
	// Reversed witness: the same older-writer wire retains Bomber while live.
	read = strings.ReplaceAll(read, oldModule, liveModule)
	if err := os.WriteFile(filepath.Join(liveDir, "main.luau"), []byte(read), 0o644); err != nil {
		t.Fatal(err)
	}
	witness := strings.TrimSpace(string(luaurun.Run(t, liveDir, "main.luau", map[string][]byte{"wire": wire, "fixed": fixedWire, "text": []byte(textWire)})))
	if !strings.Contains(witness, "read 2 2 0") || !strings.Contains(witness, "fixedread 2 2") {
		t.Fatalf("reversed witness: %s", witness)
	}
	_ = fixedWire

	// THE MESSAGE FORM: an older live writer's batch reads a deprecated
	// variant as the field default (the element default for an element), and
	// the message writers refuse a body that still holds one (§2.15).
	msgBatch := goVariantBatch(t, "internal/codegen/luautable/testdata/variantdeprecated_live.schema", `{"ship":"Bomber","fleet":["Bomber"]}`)
	arrayBatch := goVariantBatch(t, "internal/codegen/luautable/testdata/variantdeprecated_live.schema", `{"ship":"Fighter","fleet":["Bomber"]}`)
	msgScript := `local M = require("./schema_message")
local T = require("./MOD")
local inputs = require("./inputs")
local function bits(n) local b=0; while n~=0 do b=b+1; n=math.floor(n/2) end; return b end
local function getAt(b, off, w)
	local v=0; local sh=off%8; local at=math.floor(off/8); local got=0
	while got<w do
		local take=math.min(8-sh,w-got)
		local byte=buffer.readu8(b,at)
		v=v+(math.floor(byte/2^sh)%2^take)*2^got
		got=got+take; sh=sh+take
		if sh==8 then sh=0; at=at+1 end
	end
	return v
end
local function hex(b) local a={}; for i=0,buffer.len(b)-1 do a[i+1]=string.format("%02x",buffer.readu8(b,i)) end; return table.concat(a) end
local rep = {}
local v = M.NewVocabulary()
if not M.AnnounceRead(v, T.TableMessageAnnouncement, rep) then error("announce") end
local count, bodies = M.LoadMessages(v, "Root", inputs.batch, 256, rep)
if count ~= 1 then error("count " .. tostring(count) .. " " .. tostring(rep.malformed)) end
local b = bodies[1]
local f = T.RootDescriptor.fields[1]
local shipVariant = getAt(b.buffer, b.from + v.refBits, v.refBits)
local shipId = v.entries[shipVariant].id
local shipOrd = 0
for i, p in f.vs do if p[2] == shipId then shipOrd = i end end
local fleetRef = getAt(b.buffer, b.from + 2 * v.refBits, v.refBits)
local shape = v.entries[fleetRef].shape
local off = b.from + 3 * v.refBits + bits(shape.max - shape.min)
local elemRef = getAt(b.buffer, off, v.refBits)
local elemOrd = 0
if elemRef ~= 0 then
	local elemId = v.entries[elemRef].id
	for i, p in f.vs do if p[2] == elemId then elemOrd = i end end
end
print("read " .. tostring(shipOrd) .. " " .. tostring(elemOrd) .. " " .. tostring(rep.unknown))
local out, n = M.SaveMessages(v, "Root", bodies, rep)
print("save " .. tostring(out ~= nil and n == buffer.len(out)))
print("hex " .. hex(out))
local raw = { buffer = inputs.batch, from = b.from, to = b.to }
local out2, n2 = M.SaveMessages(v, "Root", { raw }, rep)
print("refuse " .. tostring(out2 == nil) .. " " .. tostring(n2) .. " " .. tostring(rep.reason) .. " " .. tostring(rep.field))
local m = M.MeasureMessages(v, "Root", { raw }, rep)
print("measure " .. tostring(m) .. " " .. tostring(rep.reason) .. " " .. tostring(rep.field))
`
	runMsg := func(dir, module string, batch []byte) string {
		t.Helper()
		script := strings.ReplaceAll(msgScript, "MOD", module)
		if err := os.WriteFile(filepath.Join(dir, "msgprobe.luau"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(luaurun.Run(t, dir, "msgprobe.luau", map[string][]byte{"batch": batch})))
	}
	oldMsg := runMsg(oldDir, oldModule, msgBatch)
	if !strings.Contains(oldMsg, "read 3 0 0") || !strings.Contains(oldMsg, "save true") || !strings.Contains(oldMsg, "refuse true -1 invalid_value Root.ship") || !strings.Contains(oldMsg, "measure -1 invalid_value Root.ship") {
		t.Fatalf("deprecated message behavior: %s", oldMsg)
	}
	liveMsg := runMsg(liveDir, liveModule, msgBatch)
	if !strings.Contains(liveMsg, "read 2 2 0") || !strings.Contains(liveMsg, "save true") || !strings.Contains(liveMsg, "hex "+hex.EncodeToString(msgBatch)) {
		t.Fatalf("reversed message witness: %s", liveMsg)
	}
	if oldHex := prefixedLine(oldMsg, "hex "); oldHex == hex.EncodeToString(msgBatch) {
		t.Fatalf("the deprecated reader left the live batch unchanged: %s", oldMsg)
	}
	arrayMsg := runMsg(oldDir, oldModule, arrayBatch)
	if !strings.Contains(arrayMsg, "refuse true -1 invalid_value Root.fleet") || !strings.Contains(arrayMsg, "measure -1 invalid_value Root.fleet") {
		t.Fatalf("deprecated message array refusal: %s", arrayMsg)
	}
}

func prefixedLine(s, prefix string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return after
		}
	}
	return ""
}

func decodeHexLine(t *testing.T, line string) ([]byte, error) {
	t.Helper()
	if len(line)%2 != 0 {
		return nil, fmt.Errorf("odd hex length")
	}
	b := make([]byte, len(line)/2)
	for i := range b {
		if _, err := fmt.Sscanf(line[2*i:2*i+2], "%02x", &b[i]); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func goVariantBatch(t *testing.T, schema, json string) []byte {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, schema)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(json), &report) {
		t.Fatalf("Go oracle JSON read: %+v", report)
	}
	if !report.Silent() {
		t.Fatalf("Go oracle JSON report: %+v", report)
	}
	batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func goVariantWire(t *testing.T, schema string) ([]byte, error) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, schema)})
	if err != nil {
		return nil, err
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		return nil, err
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"ship":"Bomber","fleet":["Bomber"]}`), &report) {
		return nil, fmt.Errorf("Go oracle JSON read: %+v", report)
	}
	if !report.Silent() {
		return nil, fmt.Errorf("Go oracle JSON report: %+v", report)
	}
	return tablewire.Encode(m, inst)
}
