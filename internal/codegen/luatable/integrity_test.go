package luatable

// TestLuaIntegrityVariableForm holds the Lua variable form to the integrity
// trailer (docs/SPEC-TABLES.md §2.14, §3.4): a table declaring
// `integrity = "crc32c"` writes the four little-endian CRC-32C bytes of every
// byte before them after its id table, the tolerant read and the measure take
// the trailer off before the framing walk (it is outside the region measure),
// and a flipped body byte or a cut trailer refuses integrity_mismatch. The
// bytes written by the Go reference engine are the oracle both ways.

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const integrityLedgerSchema = `package integrityprobe

table Ledger | integrity = "crc32c"
{
    balance int64
    label string(16)
    count uint16
}
`

func TestLuaIntegrityVariableForm(t *testing.T) {
	t.Parallel()
	schemaDir := t.TempDir()
	schema := filepath.Join(schemaDir, "Ledger.schema")
	if err := os.WriteFile(schema, []byte(integrityLedgerSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// THE GO ENGINE WRITES THE FILE: the reference encoder's bytes, then the
	// §2.14 trailer the Go leg's generated save adds.
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Ledger"))
	var report tabletext.Report
	if !model.Read(inst, []byte(`{"balance":1000,"label":"ok","count":7}`), &report) || !report.Silent() {
		t.Fatalf("the value did not place cleanly: %+v", report)
	}
	goFile, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// ONE FLIPPED BODY BYTE and a file with its trailer cut off.
	flipped := append([]byte(nil), goFile...)
	flipped[1] ^= 0x01
	cut := append([]byte(nil), goFile[:len(goFile)-4]...)

	// The trailer is outside the region measure, so the oracle measures the
	// file without it.
	measure, whole, refused := tablewire.FileRegionMeasure(u, u.Tables["Ledger"], goFile)
	if !whole || refused {
		t.Fatalf("FileRegionMeasure refused the Go file: whole=%v refused=%v", whole, refused)
	}
	wantMeasure := strconv.FormatInt(measure, 10)

	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local save = require("save")
local refusal = require("refusal")
local inputs = require("inputs")
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
local v = load.Load(T, "Ledger", inputs.go, report)
assert(v ~= nil, "the Go-written file did not load")
assert(v.balance == 1000 and v.label == "ok" and v.count == 7, "the Go-written file read the wrong value")
assert(not report.damaged and report.unknown == 0 and report.kindChanged == 0 and report.outOfRange == 0 and report.repeatedKey == 0, "the Go-written file moved the report")
local saved = save.Save(T, "Ledger", v)
assert(saved == inputs.go, "the Lua save is not the Go-written file")
assert(save.Measure(T, "Ledger", v) == #inputs.go, "the save measure is not the saved length")
assert(tostring(load.LoadMeasure(T, "Ledger", inputs.go)) == inputs.measure, "LoadMeasure is not the Go region measure")
for _, name in ipairs({ "flip", "cut" }) do
  local ok, err = pcall(function() return refusal.ReadStrict(T, "Ledger", inputs[name]) end)
  assert(not ok and tostring(err):find("integrity_mismatch", 1, true), name .. ": want integrity_mismatch, got " .. tostring(err))
  local bad = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
  local got = load.Load(T, "Ledger", inputs[name], bad)
  assert(got == nil and bad.damaged and bad.reason == "integrity_mismatch", name .. ": the tolerant read did not count integrity_mismatch")
end
local strict = refusal.ReadStrict(T, "Ledger", inputs.go)
assert(strict.balance == 1000, "ReadStrict did not read the descriptor's integrity")
print("ok")
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{
		"go":      goFile,
		"flip":    flipped,
		"cut":     cut,
		"measure": []byte(wantMeasure),
	})
	if got := string(out); got != "ok\n" {
		t.Fatalf("probe printed %q, want ok", got)
	}
}

// TestLuaIntegrityEngineOutputSingleTrailer takes the Go engine's output as the
// complete file: it already ends with the one configured trailer, so nothing is
// appended. The framing is checked here independently (the last four bytes are
// the little-endian CRC-32C of every byte before them), the Lua reader accepts
// the file as is, and flipping one trailer byte is refused integrity_mismatch.
func TestLuaIntegrityEngineOutputSingleTrailer(t *testing.T) {
	t.Parallel()
	schemaDir := t.TempDir()
	schema := filepath.Join(schemaDir, "Ledger.schema")
	if err := os.WriteFile(schema, []byte(integrityLedgerSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Ledger"))
	var report tabletext.Report
	if !model.Read(inst, []byte(`{"balance":1000,"label":"ok","count":7}`), &report) || !report.Silent() {
		t.Fatalf("the value did not place cleanly: %+v", report)
	}
	file, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// The engine output is the whole file: its last four bytes are the CRC-32C
	// of the bytes before them, and the bytes before them do not themselves end
	// in a second trailer.
	if len(file) < 8 {
		t.Fatalf("engine output is %d bytes, too short to hold a trailer", len(file))
	}
	body := file[:len(file)-4]
	table := crc32.MakeTable(crc32.Castagnoli)
	if got, want := binary.LittleEndian.Uint32(file[len(file)-4:]), crc32.Checksum(body, table); got != want {
		t.Fatalf("engine trailer is %08x, want the CRC-32C %08x of the bytes before it", got, want)
	}
	if got, want := binary.LittleEndian.Uint32(body[len(body)-4:]), crc32.Checksum(body[:len(body)-4], table); got == want {
		t.Fatalf("engine output carries a second trailer before the final one")
	}

	flipped := append([]byte(nil), file...)
	flipped[len(flipped)-1] ^= 0x01

	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local refusal = require("refusal")
local inputs = require("inputs")
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
local v = load.Load(T, "Ledger", inputs.go, report)
assert(v ~= nil and not report.damaged, "the engine output did not load as is")
assert(v.balance == 1000 and v.label == "ok" and v.count == 7, "the engine output read the wrong value")
local ok, err = pcall(function() return refusal.ReadStrict(T, "Ledger", inputs.flip) end)
assert(not ok and tostring(err):find("integrity_mismatch", 1, true), "want integrity_mismatch, got " .. tostring(err))
print("ok")
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{"go": file, "flip": flipped})
	if got := string(out); got != "ok\n" {
		t.Fatalf("probe printed %q, want ok", got)
	}
}
