package luatable

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
// §5.3 steps 9 and 10, docs/SPEC-TABLES.md §22.6): a header and a layout with
// no record behind them is refused by every fixed read entry, and a save of
// zero records is refused before it writes a byte. The vector under
// testdata/wire/fixed-empty-batch/ is the same header, layout and integrity
// trailer twice, once with a record and once without.
//
// The module is generated from the vector's own schema and run under the
// pinned Lua, so the verdict is the generated reader's. The verdict is the
// contract; a refusal's name is this leg's own diagnostic, so no reason is
// compared.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestLuaFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime over the empty-batch vector)")

	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "test", "fixed-empty-batch", "Batch.schema"))
	g := luaEvoGen(t, u, nil)
	vector := filepath.Join(repo, "testdata", "wire", "fixed-empty-batch")
	one := readFile(t, filepath.Join(vector, "one-record.bin"))
	none := readFile(t, filepath.Join(vector, "no-record.bin"))

	probe := `local T = require(` + luarun.Quote([]byte(g.mod)) + `)
local I = require("inputs")
local failures = 0
local function fail(why)
  print("FAIL: " .. why)
  failures = failures + 1
end

-- A ONE-RECORD FILE STILL READS, by every fixed read entry.
local ok, v = pcall(T.ReadFixed, "Batch", I.one)
if not ok then
  fail("ReadFixed refused a one-record file: " .. tostring(v))
elseif v.a ~= 7 or v.name ~= "hello" then
  fail("ReadFixed read a one-record file as a=" .. tostring(v.a) .. " name=" .. tostring(v.name))
end
for _, cap in ipairs({ "default", 1, 4097 }) do
  local okL, rows
  if cap == "default" then
    okL, rows = pcall(T.FixedLoad, "Batch", I.one)
  else
    okL, rows = pcall(T.FixedLoad, "Batch", I.one, cap)
  end
  if not okL then
    fail("FixedLoad (cap " .. tostring(cap) .. ") refused a one-record file: " .. tostring(rows))
  elseif #rows ~= 1 or rows[1].a ~= 7 or rows[1].name ~= "hello" then
    fail("FixedLoad (cap " .. tostring(cap) .. ") read a one-record file as " .. #rows .. " record(s)")
  end
end

-- THE HEADER AND LAYOUT WITH NO RECORD BEHIND THEM IS REFUSED, by every
-- fixed read entry.
local okR = pcall(T.ReadFixed, "Batch", I.none)
if okR then
  fail("ReadFixed read a file holding no record")
end
for _, cap in ipairs({ "default", 1, 4097 }) do
  local okL, rows
  if cap == "default" then
    okL, rows = pcall(T.FixedLoad, "Batch", I.none)
  else
    okL, rows = pcall(T.FixedLoad, "Batch", I.none, cap)
  end
  if okL then
    fail("FixedLoad (cap " .. tostring(cap) .. ") read a file holding no record as a batch of " .. #rows)
  end
end

-- A SAVE OF ZERO RECORDS IS REFUSED BEFORE ANY BYTE IS WRITTEN, by the leg's
-- own refusal value (nil and a reason), never raising.
local okS, out, why = pcall(T.FixedSave, "Batch", {})
if not okS then
  fail("a save of zero records raised " .. tostring(out) .. " instead of answering nil")
elseif out ~= nil then
  fail("a save of zero records was written (" .. #out .. " bytes)")
elseif type(why) ~= "string" or why == "" then
  fail("a save of zero records was refused without a reason")
end

-- THE CONTROL: ONE RECORD STILL WRITES, byte for byte the one-record file.
if ok then
  local okW, wire = pcall(T.FixedSave, "Batch", { v })
  if not okW or wire == nil then
    fail("a one-record save was refused: " .. tostring(wire))
  elseif wire ~= I.one then
    fail("a one-record save wrote " .. #wire .. " bytes that are not the one-record file")
  end
end

if failures ~= 0 then
  os.exit(1)
end
print("ok")
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := luarun.Try(t, g.dir, "main.lua", map[string][]byte{"one": one, "none": none})
	if err != nil || strings.TrimSpace(stdout) != "ok" {
		t.Fatalf("the fixed batch rule did not hold: %v\n%s%s", err, stdout, stderr)
	}
}
