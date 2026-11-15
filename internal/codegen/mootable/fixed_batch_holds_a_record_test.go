package mootable_test

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §3.4 and §22.6, docs/rulings.json
// fixed-form-edges): a header and a layout with no record behind them is
// refused by every fixed read entry, and a save of zero records is refused
// before it writes a byte. The vector under testdata/wire/fixed-empty-batch/
// is the same header, layout and integrity trailer twice, once with a record
// and once without.
//
// The module is generated from the vector's own schema and run under the
// pinned moo, so the verdict is the generated reader's and the runtime's, not
// a reading of the emitted text. The verdict is the contract; this test
// compares no reason name (docs/rulings.json refusal-verdict-only).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestMooFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned moo interpreter")

	u := unitFromPath(t, "test/fixed-empty-batch/Batch.schema")
	g := fixedUnit(t, u, nil)

	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")
	one, err := os.ReadFile(filepath.Join(vector, "one-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	none, err := os.ReadFile(filepath.Join(vector, "no-record.bin"))
	if err != nil {
		t.Fatal(err)
	}

	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal F = require(\"./table_fixed\")\nlocal R = require(\"./table_refusal\")\n" +
		g.requireAll() + `
local T = require("./BatchTable")
local d = T.BatchDescriptor
local fixed = T.BatchFixed

local bad = 0
local function fail(why: string)
	print("FAIL " .. why)
	bad += 1
end

-- A FILE WITH NO RECORD IS REFUSED BY EVERY FIXED READ ENTRY, whatever the
-- caller's capacity: a fixed-form file holds at least one record.
local values = F.load(d, fixed, inputs.none, {})
if values ~= nil then
	fail("F.load read a file with no record as a batch of " .. #values)
end
values = F.load(d, fixed, inputs.none, {}, 8)
if values ~= nil then
	fail("F.load, with room for eight records, read a file with no record")
end
local got = R.ReadStrict(T, "Batch", inputs.none, {})
if got ~= nil then
	fail("ReadStrict read a file with no record")
end

-- A ONE-RECORD FILE STILL READS, through both entries.
values = F.load(d, fixed, inputs.one, {})
if values == nil or #values ~= 1 or values[1].A ~= 7 or values[1].Name ~= "hello" then
	fail("F.load did not read the one-record file")
end
got = R.ReadStrict(T, "Batch", inputs.one, {})
if got == nil or got.A ~= 7 or got.Name ~= "hello" then
	fail("ReadStrict did not read the one-record file")
end

-- A SAVE OF ZERO RECORDS IS REFUSED BEFORE ANY BYTE IS WRITTEN, without
-- raising: the leg's own refusal value (docs/rulings.json write-refusal).
local out, why = F.save(d, fixed, {})
if out ~= nil or why == nil then
	fail("a save of zero records was not refused")
end

-- THE CONTROL: one record still saves.
local v = d.new()
v.A = 7
v.Name = "hello"
out, why = F.save(d, fixed, { v })
if out == nil or why ~= nil then
	fail("a one-record save was refused")
end

if bad ~= 0 then
	error("the fixed batch rule did not hold")
end
print("ok")
`

	if got := g.run(t, main, map[string][]byte{"one": one, "none": none}); got != "ok" {
		t.Fatalf("the fixed batch rule did not hold:\n%s", got)
	}
}
