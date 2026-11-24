package luatable

import (
	"strings"
	"testing"
)

const luaWriteShapeSchema = `package probe

type Vec
{
    x float32
}

union Step
{
    idle
    shift Vec
    hop   int32
}

table Root
{
    flag  bool
    rec   Vec
    step  Step
    steps [..4]Step
    vecs  []Vec
    ints  [..4]int32
}

fixed table Slot
{
    on   bool
    at   Vec
    path Step
}
`

// luaWriteShapeProbe prints one line per case: its name and what the writer
// did, `refused` (nil or false), `wrote`, or `raised` with the error.
const luaWriteShapeProbe = `local T = require("ProbeTables")
local save = require("save")
local shift = T.Unions.Step.arms[2]
local hop = T.Unions.Step.arms[3]
local function lawful()
  local v = T.NewRoot()
  v.flag = true
  v.rec = { x = 1.0 }
  v.step = { tag = 2, [shift.slot] = { x = 2.0 } }
  v.steps = { { tag = 3, [hop.slot] = 4 } }
  v.vecs = { { x = 3.0 } }
  v.ints = { 1, 2 }
  return v
end
local function answer(name, fn, ...)
  local ok, out = pcall(fn, ...)
  if not ok then print(name .. " raised:" .. tostring(out))
  elseif out == nil or out == false then print(name .. " refused")
  else print(name .. " wrote") end
end
local function variable(name, mut)
  local v = mut(lawful())
  answer(name, save.Save, T, "Root", v)
end
local function slot()
  local v = T.NewSlot()
  v.on = true
  v.at = { x = 2.0 }
  return v
end
local function fixed(name, mut)
  answer(name, T.FixedSave, "Slot", { mut(slot()) })
end
variable("v_lawful", function(v) return v end)
variable("v_record_int", function(v) v.rec = 7; return v end)
variable("v_record_text", function(v) v.rec = "x"; return v end)
variable("v_union_int", function(v) v.step = 7; return v end)
variable("v_union_text", function(v) v.step = "x"; return v end)
variable("v_arm_int", function(v) v.step[shift.slot] = 7; return v end)
variable("v_union_elem_int", function(v) v.steps = { 7 }; return v end)
variable("v_record_elem_int", function(v) v.vecs = { 7 }; return v end)
variable("v_array_int", function(v) v.ints = 7; return v end)
variable("v_whole_int", function(v) return 7 end)
variable("v_whole_text", function(v) return "x" end)
variable("v_whole_nil", function(v) return nil end)
fixed("f_lawful", function(v) return v end)
fixed("f_record_int", function(v) v.at = 7; return v end)
fixed("f_union_int", function(v) v.path = 7; return v end)
fixed("f_arm_int", function(v) v.path = { tag = 2, [shift.slot] = 7 }; return v end)
fixed("f_whole_int", function(v) return 7 end)
fixed("f_whole_text", function(v) return "x" end)
`

// TestLuaTableWritersRefuseWrongShapes holds the Lua table writers to
// docs/SPEC-CONFORMANCE.md DW-TABLE-SHAPE: a value of the WRONG KIND (a
// number or a string where a record, a union, an arm's payload or an array
// belongs, anything but the root's table as the whole value) is refused by
// the writer's own answer, nil or false, never raised; a lawful value beside
// them is written.
func TestLuaTableWritersRefuseWrongShapes(t *testing.T) {
	t.Parallel()
	u := luaVariantUnit(t, luaWriteShapeSchema)
	out := string(luaVariantRun(t, u, luaWriteShapeProbe, map[string][]byte{}))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		name, verdict, _ := strings.Cut(line, " ")
		got[name] = verdict
	}
	want := map[string]string{"v_lawful": "wrote", "f_lawful": "wrote"}
	for _, name := range []string{
		"v_record_int", "v_record_text", "v_union_int", "v_union_text", "v_arm_int", "v_union_elem_int",
		"v_record_elem_int", "v_array_int", "v_whole_int", "v_whole_text", "v_whole_nil",
		"f_record_int", "f_union_int", "f_arm_int", "f_whole_int", "f_whole_text",
	} {
		want[name] = "refused"
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: the writer answered %q, want %s", name, got[name], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the probe printed %d cases, want %d:\n%s", len(got), len(want), out)
	}
}
