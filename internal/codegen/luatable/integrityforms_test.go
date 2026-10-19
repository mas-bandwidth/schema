package luatable

// TestLuaIntegrityFixedAndMessageForms holds the Lua fixed and message forms
// to the integrity trailer (docs/SPEC-TABLES.md §2.14, §3.4, §3.3): a fixed
// table declaring `integrity = "crc32c"` writes its form-3 file with one
// little-endian CRC-32C after the last record, and its message-form batch with
// one after the pad; each reader checks the trailer before it decodes a
// record or a body, and a flipped byte or a cut trailer refuses
// integrity_mismatch. The bytes the Go reference's generated form writes are
// the oracle both ways.

import (
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

const integrityFixedFormsSchema = `package integrityfixedforms
fixed table Reading | integrity = "crc32c"
{
    x int32
    y int16
    label string(8)
}
fixed table PlainReading
{
    x int32
    y int16
    label string(8)
}
`

const integrityMessageFormsSchema = `package integritymessageforms
fixed table Ledger | integrity = "crc32c"
{
    n int32
    count uint16
}
fixed table PlainLedger
{
    n int32
    count uint16
}
`

// integrityFixedGoGolden is the Go generated fixed form's ReadingFixedSave of
// the three records of internal/codegen/gotable's TestGoWireIntegrityFixedForm
// (x 1000, y 0, label "ok" and two follow-ons), trailer included. Its plain
// body is the same file without the last four bytes.
const integrityFixedGoGolden = "03000000000000006938893b0968270148000000040000007b8c6cd3fc19b9b80d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c000000000000006938893b09682701e80300000000020000006f6b0000000000006938893b09682701e9030000f9ff0000000000000000000000006938893b09682701ea030000f2ff0800000065696768742063682854cdb3"

// integrityMessageGoPlain is the Go generated PlainSaveMessages of
// {{n 1000, count 7}} (internal/codegen/gotable's
// TestGoWireIntegrityMessageForm): the keyless table's batch does not move.
const integrityMessageGoPlain = "0200813e000020070000"

// integrityMessageGoLedger is the same batch under the declaring table: the
// keyless batch and its Go-written CRC-32C trailer.
const integrityMessageGoLedger = integrityMessageGoPlain + "78f2be34"

// integrityTrailer is the four little-endian CRC-32C bytes §2.14 appends.
func integrityTrailer(body []byte) []byte {
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))
	return out[:]
}

func integrityHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLuaIntegrityFixedAndMessageForms(t *testing.T) {
	t.Parallel()

	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		// THE GO FILE: the oracle's three records. Its first record alone is
		// the one record WriteFixed writes, sealed with the trailer.
		goFile := integrityHex(t, integrityFixedGoGolden)
		layoutLen := binary.LittleEndian.Uint32(goFile[16:20])
		header := 20 + int(layoutLen)
		record := (len(goFile) - 4 - header) / 3
		if header+3*record+4 != len(goFile) {
			t.Fatalf("the Go golden is not a header and three records: %d, %d, %d", header, record, len(goFile))
		}
		one := append([]byte(nil), goFile[:header+record]...)
		one = append(one, integrityTrailer(one)...)
		flipped := append([]byte(nil), goFile...)
		flipped[header+8] ^= 0x01
		cut := append([]byte(nil), goFile[:len(goFile)-4]...)

		schemaDir := t.TempDir()
		schema := filepath.Join(schemaDir, "Reading.schema")
		if err := os.WriteFile(schema, []byte(integrityFixedFormsSchema), 0o644); err != nil {
			t.Fatal(err)
		}
		u := luarun.LoadUnit(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := `local T = require("` + ModuleName(u) + `")
local inputs = require("inputs")
local v = T.NewReading()
v.x = 1000
v.label = "ok"
local wrote = T.WriteFixed("Reading", v)
assert(wrote == inputs.one, "the Lua write is not the Go one-record file and its trailer")
local got = T.ReadFixed("Reading", inputs.go)
assert(got.x == 1000 and got.y == 0 and got.label == "ok", "the Go-written file read the wrong value")
for _, name in ipairs({ "flip", "cut" }) do
  local ok, err = pcall(function() return T.ReadFixed("Reading", inputs[name]) end)
  assert(not ok and tostring(err):find("integrity_mismatch", 1, true), name .. ": want integrity_mismatch, got " .. tostring(err))
end
local ok, err = pcall(function() return T.ReadFixed("Reading", inputs.go:sub(1, 4)) end)
assert(not ok and tostring(err):find("integrity_mismatch", 1, true), "a file shorter than five bytes: " .. tostring(err))
print("fixed-ok")
`
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luarun.Run(t, dir, "main.lua", map[string][]byte{
			"go":   goFile,
			"one":  one,
			"flip": flipped,
			"cut":  cut,
		})
		if got := string(out); got != "fixed-ok\n" {
			t.Fatalf("probe printed %q, want fixed-ok", got)
		}
	})

	t.Run("message", func(t *testing.T) {
		t.Parallel()
		goPlain := integrityHex(t, integrityMessageGoPlain)
		goLedger := integrityHex(t, integrityMessageGoLedger)
		flipped := append([]byte(nil), goLedger...)
		flipped[2] ^= 0x01
		cut := append([]byte(nil), goLedger[:len(goLedger)-4]...)

		schemaDir := t.TempDir()
		schema := filepath.Join(schemaDir, "Ledger.schema")
		if err := os.WriteFile(schema, []byte(integrityMessageFormsSchema), 0o644); err != nil {
			t.Fatal(err)
		}
		u := luarun.LoadUnit(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := `local T = require("` + ModuleName(u) + `")
local inputs = require("inputs")
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local report = { malformed = false, refused = 0 }
assert(T.AnnounceRead(vocab, T.Announcement, report), "announce refused or damaged: " .. tostring(report.reason))
local v = T.NewLedger()
v.n = 1000
v.count = 7
local wrote = T.SaveMessages("Ledger", vocab, { v })
assert(wrote == inputs.ledger, "the Lua batch is not the Go batch and its trailer")
assert(T.MeasureMessages("Ledger", vocab, { v }) == #inputs.ledger, "the keyed measure does not count the trailer")
local p = T.NewPlainLedger()
p.n = 1000
p.count = 7
local plain = T.SaveMessages("PlainLedger", vocab, { p })
assert(plain == inputs.plain, "the keyless batch moved")
assert(T.MeasureMessages("PlainLedger", vocab, { p }) == #inputs.plain, "the keyless measure moved")
local values = T.LoadMessages("Ledger", vocab, inputs.ledger, report)
assert(values ~= nil and #values == 1 and values[1].n == 1000 and values[1].count == 7, "the Go-written batch did not load")
for _, name in ipairs({ "flip", "cut" }) do
  local bad = { malformed = false, refused = 0, reason = "" }
  local got = T.LoadMessages("Ledger", vocab, inputs[name], bad)
  assert(got == nil and bad.malformed and bad.reason == "integrity_mismatch", name .. ": the tolerant read did not count integrity_mismatch")
end
print("message-ok")
`
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luarun.Run(t, dir, "main.lua", map[string][]byte{
			"plain":  goPlain,
			"ledger": goLedger,
			"flip":   flipped,
			"cut":    cut,
		})
		if got := string(out); got != "message-ok\n" {
			t.Fatalf("probe printed %q, want message-ok", got)
		}
	})
}
