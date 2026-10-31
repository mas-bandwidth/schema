package luautable_test

// TestLuauMessageFormIntegrity holds the message form's integrity trailer
// (docs/SPEC-TABLES.md §2.14, §3.3): a fixed table that declares
// `integrity = "crc32c"` saves its batch as §3.3's bytes with four CRC-32C
// bytes after the padded bit stream, MeasureMessages counts them, and
// LoadMessages checks them before it decodes the first body, so a single-bit
// flip refuses integrity_mismatch (the form byte keeps §3.3's own refusal,
// because it is read first). A table without the key writes the bytes it
// wrote before. The expected bytes and verdicts are schema-go's engine,
// internal/tablewire's EncodeMessages and the CRC-32C of those bytes.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const integrityMessageSchema = `package probe
fixed table Ledger | integrity = "crc32c"
{
    n int32
    count uint16
}
fixed table Plain
{
    n int32
    count uint16
}
`

func TestLuauMessageFormIntegrity(t *testing.T) {
	t.Parallel()
	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte(integrityMessageSchema), 0o644); err != nil {
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

	// schema-go's own writer is the oracle: internal/tablewire's message
	// batch for the same value and unit, then §2.14's little-endian CRC-32C
	// trailer over every batch byte before it.
	model := tabletext.NewModel(u)
	instance := func(table string) *tabletext.Instance {
		t.Helper()
		inst := model.New(model.Lookup(table))
		var r tabletext.Report
		if !model.Read(inst, []byte(`{"n": 1000, "count": 7}`), &r) || !r.Silent() {
			t.Fatalf("%s: model read failed: %+v", table, r)
		}
		return inst
	}
	plainWire, err := tablewire.EncodeMessages(model, []*tabletext.Instance{instance("Plain")})
	if err != nil {
		t.Fatalf("tablewire.EncodeMessages(Plain): %v", err)
	}
	ledgerWire, err := tablewire.EncodeMessages(model, []*tabletext.Instance{instance("Ledger")})
	if err != nil {
		t.Fatalf("tablewire.EncodeMessages(Ledger): %v", err)
	}
	if !bytes.Equal(plainWire, ledgerWire) {
		t.Fatalf("the keyless and declaring batches differ before the trailer: %x vs %x", plainWire, ledgerWire)
	}
	wantLedger := append([]byte(nil), ledgerWire...)
	wantLedger = binary.LittleEndian.AppendUint32(wantLedger, crc32.Checksum(ledgerWire, crc32.MakeTable(crc32.Castagnoli)))

	main := `local M = require("./schema_message")
local T = require("./ProbeTable")
local inputs = require("./inputs")

local v = M.NewVocabulary()
local report = { malformed = false, verdict = 0, reason = "", unknown = 0 }
if not M.AnnounceRead(v, T.TableMessageAnnouncement, report) then
	print("announce_fail " .. tostring(report.reason))
	return
end

local function hex(b)
	local p = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		p[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(p)
end

local count, lbody = M.LoadMessages(v, "Ledger", inputs.ledger, 256, report)
print("load_ledger " .. tostring(count) .. " " .. tostring(report.malformed) .. " " .. tostring(report.reason))
if count < 0 then return end
local pcount, pbody = M.LoadMessages(v, "Plain", inputs.plain, 256, report)
print("load_plain " .. tostring(pcount) .. " " .. tostring(report.malformed) .. " " .. tostring(report.reason))
if pcount < 0 then return end
local lm = M.MeasureMessages(v, "Ledger", lbody, report)
local pm = M.MeasureMessages(v, "Plain", pbody, report)
local lb, ln = M.SaveMessages(v, "Ledger", lbody, report)
if lb == nil then print("ledger_save_fail " .. tostring(report.reason)) return end
local pb, pn = M.SaveMessages(v, "Plain", pbody, report)
if pb == nil then print("plain_save_fail " .. tostring(report.reason)) return end
print("measure " .. tostring(lm) .. " " .. tostring(pm))
print("ledger " .. tostring(ln) .. " " .. hex(lb))
print("plain " .. tostring(pn) .. " " .. hex(pb))
local missing = M.LoadMessages(v, "Ledger", inputs.plain, 256, report)
print("load_missing " .. tostring(missing) .. " " .. tostring(report.malformed) .. " " .. tostring(report.reason))
for i = 0, buffer.len(lb) - 1 do
	for bit = 0, 7 do
		local bad = buffer.create(buffer.len(lb))
		buffer.copy(bad, 0, lb, 0, buffer.len(lb))
		buffer.writeu8(bad, i, bit32.bxor(buffer.readu8(bad, i), bit32.lshift(1, bit)))
		local rep = { malformed = false, verdict = 0, reason = "", unknown = 0 }
		local c = M.LoadMessages(v, "Ledger", bad, 256, rep)
		print("flip " .. tostring(i) .. " " .. tostring(bit) .. " " .. tostring(c) .. " " .. tostring(rep.malformed) .. " " .. tostring(rep.reason))
	end
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"ledger": wantLedger, "plain": plainWire})))
	lines := strings.Split(out, "\n")
	if len(lines) < 7 {
		t.Fatalf("the probe printed %q", out)
	}

	wantMeasure := len(wantLedger)
	gotPlainMeasure, gotLedgerMeasure := -1, -1
	if _, err := fmt.Sscanf(lines[2], "measure %d %d", &gotLedgerMeasure, &gotPlainMeasure); err != nil {
		t.Fatalf("measure line %q: %v", lines[2], err)
	}
	if gotLedgerMeasure != wantMeasure {
		t.Errorf("Ledger MeasureMessages = %d, want %d (the batch and its trailer)", gotLedgerMeasure, wantMeasure)
	}
	if gotPlainMeasure != len(plainWire) {
		t.Errorf("Plain MeasureMessages = %d, want %d (the batch alone)", gotPlainMeasure, len(plainWire))
	}

	checkSaved := func(line, root string, want []byte) []byte {
		t.Helper()
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != root {
			t.Fatalf("unexpected %s save line %q", root, line)
		}
		got, err := hex.DecodeString(fields[2])
		if err != nil {
			t.Fatalf("%s: bad hex %q: %v", root, fields[2], err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s SaveMessages = %s, want %s", root, hex.EncodeToString(got), hex.EncodeToString(want))
		}
		return got
	}
	gotLedger := checkSaved(lines[3], "ledger", wantLedger)
	checkSaved(lines[4], "plain", plainWire)
	if lines[0] != "load_ledger 1 false " {
		t.Errorf("LoadMessages of the saved Ledger batch = %q, want count 1 and no damage", lines[0])
	}
	if lines[1] != "load_plain 1 false " {
		t.Errorf("LoadMessages of the saved Plain batch = %q, want count 1 and no damage", lines[1])
	}
	if lines[5] != "load_missing -1 true integrity_mismatch" {
		t.Errorf("LoadMessages of the keyless batch under the declaring root = %q, want the trailer refusal", lines[5])
	}

	seen := 0
	for _, line := range lines[6:] {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "flip" {
			t.Fatalf("unexpected flip line %q", line)
		}
		at, bit := 0, 0
		if _, err := fmt.Sscanf(line, "flip %d %d", &at, &bit); err != nil {
			t.Fatalf("flip line %q: %v", line, err)
		}
		wantReason := "integrity_mismatch"
		wantMalformed := "true"
		if at == 0 {
			// §2.14 keeps THE FIRST BYTE's rule: the form byte is read before
			// the trailer, so a flip there is §3.3's newer_form.
			wantReason = "newer_form"
			wantMalformed = "false"
		}
		want := "-1 " + wantMalformed + " " + wantReason
		if got := strings.Join(fields[3:], " "); got != want {
			t.Errorf("byte %d bit %d: LoadMessages %q, want %q", at, bit, got, want)
		}
		seen++
	}
	if want := len(gotLedger) * 8; seen != want {
		t.Errorf("tested %d single-bit flips, want %d", seen, want)
	}
}
