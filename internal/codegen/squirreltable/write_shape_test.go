package squirreltable_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

const writeShapeSchema = `package probe

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

// writeShapeProbe prints one line per case: its name and what the writer
// did, `refused` (null, or a negative count), `wrote`, or `raised` with the
// VM's error.
const writeShapeProbe = `local T = dofile("probe/schema_module.nut", true).require("__MOD__");
local lawful = function() {
  local v = T.Root();
  v.Flag = true;
  v.Rec = T.Vec(); v.Rec.X = 1.0;
  v.Step = T.Step(); v.Step.Type = 2; v.Step.Shift = T.Vec();
  local s = T.Step(); s.Type = 3; s.Hop = 4;
  v.Steps = [s];
  v.Vecs = [T.Vec()];
  v.Ints = [1, 2];
  return v;
};
local variable = function(name, mut) {
  local v = lawful();
  v = mut(v);
  local answer = "wrote";
  try { if (T.RootSave(v) == null) answer = "refused"; } catch (e) { answer = "raised:" + e; }
  print(name + " " + answer + "\n");
};
local slot = function() {
  local v = T.Slot();
  v.On = true;
  v.At.X = 2.0;
  return v;
};
local fixed = function(name, mut) {
  local v = mut(slot());
  local answer = "wrote";
  try { if (T.SlotFixedSave(blob(4096), [v]) < 0) answer = "refused"; } catch (e) { answer = "raised:" + e; }
  print(name + " " + answer + "\n");
};
variable("v_lawful", function(v) { return v; });
variable("v_record_int", function(v) { v.Rec = 7; return v; });
variable("v_record_table", function(v) { v.Rec = {}; return v; });
variable("v_record_null", function(v) { v.Rec = null; return v; });
variable("v_union_int", function(v) { v.Step = 7; return v; });
variable("v_union_table", function(v) { v.Step = {}; return v; });
variable("v_arm_int", function(v) { v.Step.Shift = 7; return v; });
variable("v_union_elem_int", function(v) { v.Steps = [7]; return v; });
variable("v_union_elem_bool", function(v) { v.Steps = [true]; return v; });
variable("v_record_elem_int", function(v) { v.Vecs = [7]; return v; });
variable("v_array_int", function(v) { v.Ints = 7; return v; });
variable("v_array_null", function(v) { v.Ints = null; return v; });
variable("v_whole_int", function(v) { return 7; });
variable("v_whole_null", function(v) { return null; });
variable("v_whole_table", function(v) { return {}; });
fixed("f_lawful", function(v) { return v; });
fixed("f_record_int", function(v) { v.At = 7; return v; });
fixed("f_record_null", function(v) { v.At = null; return v; });
fixed("f_union_int", function(v) { v.Path = 7; return v; });
fixed("f_union_table", function(v) { v.Path = {}; return v; });
fixed("f_whole_int", function(v) { return 7; });
fixed("f_whole_null", function(v) { return null; });
`

// TestSquirrelTableWritersRefuseWrongShapes holds the Squirrel table writers
// to docs/SPEC-CONFORMANCE.md DW-TABLE-SHAPE: a value of the WRONG KIND (an
// integer, a bare table or null where a record, a union, an arm's payload or
// an array belongs, anything but the root's instance as the whole value) is
// refused by the writer's own answer — the variable RootSave's null, the
// fixed FixedSave's negative count — never raised by the VM, and a lawful
// value beside them is written.
func TestSquirrelTableWritersRefuseWrongShapes(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	unit := unitFromSchema(t, writeShapeSchema)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unit)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(strings.ReplaceAll(writeShapeProbe, "__MOD__", mod)))
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		name, answer, _ := strings.Cut(line, " ")
		got[name] = answer
	}
	want := map[string]string{"v_lawful": "wrote", "f_lawful": "wrote"}
	for _, name := range []string{
		"v_record_int", "v_record_table", "v_record_null", "v_union_int", "v_union_table", "v_arm_int",
		"v_union_elem_int", "v_union_elem_bool", "v_record_elem_int", "v_array_int", "v_array_null",
		"v_whole_int", "v_whole_null", "v_whole_table",
		"f_record_int", "f_record_null", "f_union_int", "f_union_table", "f_whole_int", "f_whole_null",
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
