package luautable_test

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §3.4 and §22.6): the header and layout of
// testdata/wire/fixed-empty-batch/no-record.bin are refused by every fixed read
// entry, the same file with one record still reads through all of them, and a
// save of zero records is refused before it is written. Only verdicts are
// compared (docs/rulings.json refusal-verdict-only).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestLuauFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted fixed reader and writer)")

	u, err := compiler.New().Load([]string{filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema")})
	if err != nil {
		t.Fatal(err)
	}
	g, mod := luauFixedUnit(t, u)

	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")
	inputs := map[string][]byte{}
	for _, file := range []struct{ path, key string }{
		{"one-record.bin", "one"},
		{"no-record.bin", "none"},
	} {
		data, err := os.ReadFile(filepath.Join(vector, file.path))
		if err != nil {
			t.Fatal(err)
		}
		inputs[file.key] = data
	}

	probe := fmt.Sprintf(`--!strict
local inputs = require("./inputs")
local T = require("./%[1]s")
local F = require("./schema_fixed")
local L = require("./schema_load")
local R = require("./schema_refusal")

local fixed = {
	hash = T.BatchFixedHash,
	recordBytes = T.BatchFixedRecordBytes,
	layout = T.BatchFixedLayout,
	lineage = T.BatchFixedLineage,
	floor = T.BatchFixedFloor,
}

local failures = 0
local function check(ok: boolean, what: string)
	if not ok then
		print("FAIL: " .. what)
		failures += 1
	end
end

-- The verdict is the contract; a refusal's name is this leg's diagnostic and
-- is not compared (docs/rulings.json refusal-verdict-only).
local function verdict(fn: () -> any): string
	local ok, res = pcall(fn)
	if not ok or res == nil then return "refused" end
	return "read"
end

local entries = {
	{ "F.loadWalk", function(d: buffer) return F.loadWalk(T.BatchDescriptor, fixed, d, {}) end },
	{ "F.read", function(d: buffer) return F.read(T.BatchDescriptor, fixed, d, {}) end },
	{ "F.load", function(d: buffer) return F.load(T.BatchDescriptor, fixed, d) end },
	{ "L.loadWalk", function(d: buffer) return L.loadWalk("Batch", d, {}) end },
	{ "L.ReadStrict", function(d: buffer) return L.ReadStrict("Batch", d, {}) end },
	{ "L.load", function(d: buffer) return L.load("Batch", d, {}) end },
	{ "R.ReadStrict", function(d: buffer) return R.ReadStrict(T, "Batch", d, {}) end },
}

for _, e in entries do
	check(verdict(function() return e[2](inputs.one) end) == "read",
		e[1] .. " refused the one-record file")
	check(verdict(function() return e[2](inputs.none) end) == "refused",
		e[1] .. " read a file holding no record")
end

-- A SAVE OF ZERO RECORDS IS REFUSED. The leg refuses a write by its own
-- refusal value in every build, so the call answers a refusal whether that
-- value is returned or raised.
local function saveRefused(): boolean
	local ok, res = pcall(F.save, T.BatchDescriptor, fixed, {})
	if not ok then return true end
	return res == nil
end
check(saveRefused(), "a save of zero records was not refused")

if failures ~= 0 then
	print("FAILURES " .. failures)
else
	print("ok")
end
`, mod)

	out := strings.TrimSpace(string(luauFixedRun(t, g.dir, probe, inputs)))
	if out != "ok" {
		t.Fatalf("the fixed batch rule did not hold:\n%s", out)
	}
}
