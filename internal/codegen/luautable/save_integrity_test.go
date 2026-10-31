package luautable_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
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
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauSaveWritesIntegrityTrailer holds the variable-form save and measure
// for a root table that declares `integrity = "crc32c"` (docs/SPEC-TABLES.md
// §2.14; docs/PORT-LUAU.md, THE TYPE MAPPING): Luau save appends the four-byte
// little-endian CRC-32C trailer after the id table, measure counts those four
// bytes, Luau ReadStrict accepts the saved file, and the saved bytes equal
// internal/tablewire's encoder with §2.14's trailer. A sibling table without
// `integrity` writes no trailer.
func TestLuauSaveWritesIntegrityTrailer(t *testing.T) {
	t.Parallel()
	const src = `package seal

table Ledger | integrity = "crc32c"
{
    id   uint32
    name string(32)
}

table Plain
{
    id   uint32
    name string(32)
}
`
	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "Seal.schema")
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
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

	// the reference save is internal/tablewire's encoder, which writes the
	// form byte, root body and id table (§3); §2.14's trailer is the CRC-32C
	// of every byte before it, four bytes little-endian, after the id table.
	m := tabletext.NewModel(u)
	referenceSave := func(tableName string, seal bool) []byte {
		inst := m.New(m.Lookup(tableName))
		var r tabletext.Report
		if !m.Read(inst, []byte(`{"id": 42, "name": "ledger-entry"}`), &r) || !r.Silent() {
			t.Fatalf("%s: model read failed: %+v", tableName, r)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: tablewire.Encode: %v", tableName, err)
		}
		if seal {
			sum := crc32.Checksum(wire, crc32.MakeTable(crc32.Castagnoli))
			wire = binary.LittleEndian.AppendUint32(wire, sum)
		}
		return wire
	}
	wantLedger := referenceSave("Ledger", true)
	wantPlain := referenceSave("Plain", false)

	main := `local save = require("./schema_save")
local refusal = require("./schema_refusal")
local St = require("./schema_tables")
local T = require("./SealTable")

local function fill(d)
	local v = d.new()
	v.Id = 42
	local s = "ledger-entry"
	buffer.writestring(v.Name, 0, s)
	v.NameLength = #s
	return v
end

local function probe(root)
	local d = St.lookup(root)
	local v = fill(d)
	local out = save.save(d, v)
	local m = save.measure(d, v)
	assert(out ~= nil and m ~= nil, root .. ": save or measure returned nil")
	local rep = {}
	local back = refusal.ReadStrict(T, root, out, rep)
	local verdict = if back ~= nil then "ok" else tostring(rep.reason)
	local parts = table.create(buffer.len(out))
	for i = 0, buffer.len(out) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
	end
	print(root .. " " .. verdict .. " " .. tostring(m) .. " " .. table.concat(parts))
end

probe("Ledger")
probe("Plain")
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected Luau output:\n%s", out)
	}
	checkLine := func(line, wantRoot string, wantWire []byte) {
		t.Helper()
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] != wantRoot {
			t.Fatalf("unexpected line %q", line)
		}
		verdict := fields[1]
		measure, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatalf("%s: bad measure %q: %v", wantRoot, fields[2], err)
		}
		gotWire, err := hex.DecodeString(fields[3])
		if err != nil {
			t.Fatalf("%s: bad hex %q: %v", wantRoot, fields[3], err)
		}
		if verdict != "ok" {
			t.Errorf("%s: ReadStrict on saved bytes returned %s, want ok", wantRoot, verdict)
		}
		if measure != len(gotWire) {
			t.Errorf("%s: measure = %d, len(saved) = %d", wantRoot, measure, len(gotWire))
		}
		if !bytes.Equal(gotWire, wantWire) {
			t.Errorf("%s: saved %d bytes (%x), want %d bytes (%x)", wantRoot, len(gotWire), gotWire, len(wantWire), wantWire)
		}
	}
	checkLine(lines[0], "Ledger", wantLedger)
	checkLine(lines[1], "Plain", wantPlain)
}
