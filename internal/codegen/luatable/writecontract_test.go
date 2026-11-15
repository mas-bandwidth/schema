package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Every table writer answers an off-declaration value with its refusal value,
// without raising or returning any bytes (docs/SPEC.md §5.1).
func TestLuaWritersRefuseOffContract(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lua table writers")
	dir := t.TempDir()
	fields := `
 ranged int16 | min = -2, max = 3
 mask bits(6)
 items [1..3]uint16
 text string(4)
 data bytes(4)
 mode Mode
 choice Choice
 plain float32
 compressed float32 | min = 0.0, max = 1.0, resolution = 0.25
 small_signed int8
 small_unsigned uint8
 enabled bool
 raw fixed(8,8) | min = -2, max = 3
 ranges [2]int16 | min = -2, max = 3
`
	source := "package writecontract\nenum Mode { Red, Blue }\nunion Choice {\n limit int16 | min = -2, max = 3\n}\n" +
		"table Variable {\n" + fields + " extra []uint8\n}\nfixed table Fixed {\n" + fields + "}\n"
	if err := os.WriteFile(filepath.Join(dir, "Contract.schema"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaWriteContractProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, dir, "main.lua", nil))
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "FAIL ") {
			t.Error(line)
		} else {
			t.Log(line)
		}
	}
	if !strings.Contains(out, "write-contract rows complete") {
		t.Fatal("the writer matrix did not complete")
	}
}

const luaWriteContractProbe = `local T = require("WritecontractTables")
local S = require("save")
local L = require("load")
local R = require("retain")
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, T.Announcement, {}))
local function report() return {unknown=0, widened=0, retained=0, retainLost=0} end
local function store() return {list={}, ids={}, cap=4096, idCap=256, used=0} end
local function same(a,b)
 if type(a) ~= type(b) then return false end
 if type(a) ~= "table" then return a == b end
 for k,v in pairs(a) do if not same(v,b[k]) then return false end end
 for k in pairs(b) do if a[k] == nil then return false end end
 return true
end
local rows = {
 {"1-upper", "ranged", 4, 3}, {"1-lower", "ranged", -3, -2},
 {"1-array", "ranges", {0,4}, {-2,3}},
 {"1-arm", "choice", {tag=1,limit=4}, {tag=1,limit=3}},
 {"1-raw", "raw", 769, 768},
 {"2-bits", "mask", 64, 63},
 {"3-max", "items", {1,2,3,4,5}, {1,2,3}},
 {"3-min", "items", {}, {1}},
 {"4-string", "text", "abcde", "abcd"},
 {"4-bytes", "data", "abcde", "abcd"},
 {"5-enum", "mode", "Missing", "Blue"},
 {"6-union", "choice", {tag=7}, {tag=1,limit=3}},
 {"7-nan", "plain", 0/0, 1.5},
 {"7-infinity", "plain", math.huge, 1.5},
 {"7-negative-infinity", "plain", -math.huge, -1.5},
 {"7-float32-width", "plain", 3.5e38, 1.5},
 {"8-utf8", "text", "a"..string.char(255).."b", "ab"..string.char(195,169)},
 {"8-zero", "text", "a\0b", "abcd"},
 {"10-upper", "compressed", 1.25, 1.0},
 {"10-lower", "compressed", -0.25, 0.0},
 {"width-int8-upper", "small_signed", 300, 127},
 {"width-int8-lower", "small_signed", -129, -128},
 {"width-uint8-upper", "small_unsigned", 256, 255},
 {"width-uint8-lower", "small_unsigned", -1, 0},
 {"type-int", "small_signed", 1.5, 1},
 {"type-bool", "enabled", 1, true},
}
local writers = {
 {"Save", "Variable", function(v) return S.Save(T,"Variable",v) end,
  function(b) return L.Load(T,"Variable",b,{}) end},
 {"SaveTyped", "Variable", function(v) return S.SaveTyped(T,"Variable",v) end,
  function(b) return L.Load(T,"Variable",b,{}) end},
 {"SaveRetain", "Variable", function(v) return R.SaveRetain(T,"Variable",v,store(),report()) end,
  function(b) return L.Load(T,"Variable",b,{}) end},
 {"Measure", "Variable", function(v) return S.Measure(T,"Variable",v) end},
 {"MeasureRetain", "Variable", function(v) return R.MeasureRetain(T,"Variable",v,store()) end},
 {"FixedSave", "Fixed", function(v) return T.FixedSave("Fixed",{v}) end,
  function(b) return T.FixedLoad("Fixed",b)[1] end},
 {"WriteFixed", "Fixed", function(v) return T.WriteFixed("Fixed",v) end,
  function(b) return T.ReadFixed("Fixed",b) end},
 {"SaveMessages", "Fixed", function(v) return T.SaveMessages("Fixed",vocab,{v}) end,
  function(b) return T.LoadMessages("Fixed",vocab,b,{})[1] end},
}
for _,w in ipairs(writers) do
 for _,r in ipairs(rows) do
  local label = w[1].."/"..r[1]
  local v = T["New"..w[2]]()
  v[r[2]] = r[3]
  local ok, bytes, why = pcall(w[3],v)
  local outcome = not ok and "raises" or bytes ~= nil and "writes" or "refuses"
  print("ROW "..label.." "..outcome)
  if not ok or bytes ~= nil or type(why) ~= "string" or #why == 0 then
   print("FAIL "..label.." must refuse without bytes or raise")
  end
  local twin = T["New"..w[2]]()
  twin[r[2]] = r[4]
  local good, wire = pcall(w[3],twin)
  if not good or wire == nil then
   print("FAIL "..label.." boundary refused: "..tostring(wire))
  elseif w[4] then
   local read, back = pcall(w[4],wire)
   if not read or type(back) ~= "table" or not same(back[r[2]],r[4]) then
    print("FAIL "..label.." boundary did not read back exactly")
   end
  else
   local saved = S.Save(T,"Variable",twin)
   if type(saved) ~= "string" or wire ~= #saved then print("FAIL "..label.." boundary measure") end
  end
 end
end
print("write-contract rows complete")
`
