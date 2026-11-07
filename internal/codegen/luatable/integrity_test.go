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
	body, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	goFile := make([]byte, len(body)+4)
	copy(goFile, body)
	binary.LittleEndian.PutUint32(goFile[len(body):], crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))

	// ONE FLIPPED BODY BYTE and a file with its trailer cut off.
	flipped := append([]byte(nil), goFile...)
	flipped[1] ^= 0x01
	cut := append([]byte(nil), goFile[:len(goFile)-4]...)

	// The trailer is outside the region measure, so the oracle measures the
	// file without it.
	measure, whole, refused := tablewire.FileRegionMeasure(u, u.Tables["Ledger"], body)
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
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "Ledger", inputs.go, report)
assert(v ~= nil, "the Go-written file did not load")
assert(v.balance == 1000 and v.label == "ok" and v.count == 7, "the Go-written file read the wrong value")
assert(not report.malformed and report.unknown == 0 and report.kindMismatch == 0 and report.clamped == 0 and report.duplicate == 0, "the Go-written file moved the report")
local saved = save.Save(T, "Ledger", v)
assert(saved == inputs.go, "the Lua save is not the Go-written file")
assert(save.Measure(T, "Ledger", v) == #inputs.go, "the save measure is not the saved length")
assert(tostring(load.LoadMeasure(T, "Ledger", inputs.go)) == inputs.measure, "LoadMeasure is not the Go region measure")
for _, name in ipairs({ "flip", "cut" }) do
  local ok, err = pcall(function() return refusal.ReadStrict(T, "Ledger", inputs[name]) end)
  assert(not ok and tostring(err):find("integrity_mismatch", 1, true), name .. ": want integrity_mismatch, got " .. tostring(err))
  local bad = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local got = load.LoadTolerant(T, "Ledger", inputs[name], bad)
  assert(got == nil and bad.malformed and bad.reason == "integrity_mismatch", name .. ": the tolerant read did not count integrity_mismatch")
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
