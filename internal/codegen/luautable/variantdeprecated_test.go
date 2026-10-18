package luautable_test

import (
	"encoding/base64"
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
local Save = require("./save")
local Fixed = require("./fixed")
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
local Load = require("./load"); local Save = require("./save"); local Text = require("./text"); local Fixed = require("./fixed")
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
	if !strings.Contains(string(oldFiles[strings.TrimSuffix(oldModule, "Table")+".luau"]), "Deprecated: wire identity retained") || !strings.Contains(string(oldFiles[strings.TrimSuffix(oldModule, "Table")+".luau"]), "Bomber = 2") {
		t.Fatal("deprecated enum constant lacks its Luau marker")
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
