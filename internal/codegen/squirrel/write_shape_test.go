package squirrel_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel/sqrun"
)

// writeShapeSchema declares one field of every kind whose Squirrel value has
// a shape the writer must check before the runtime touches it: records,
// unions and arm payloads, arrays, bools, floats, ranged integers on the
// runtime's int32 and int64 paths, and the 128-bit [lo, hi] pairs.
const writeShapeSchema = `package writeshape

type Arm
{
    n int32 | min = -5, max = 5
}

union Pick
{
    arm Arm
}

type Row
{
    r    int32 | min = -10, max = 10
    r64  int64 | min = -10, max = 10000000000
    w    int128 | min = -100, max = 100
    wu   uint128
    f    fixed(8, 8) | min = -10, max = 10
    g    fixed(70, 58) | min = -1000, max = 1000
    ok   bool
    fl   float32
    cf   float32 | min = -1, max = 1, resolution = 0.01
    db   float64
    a    Arm
    u    Pick
    xs   [1..3]int32
    fs   [2]int8
    arms [..2]Arm
    us   [..2]Pick
    bs   [3]uint8
}
`

// writeShapeProbe writes one valid Row, then one Row per value of the wrong
// shape, printing "label=refused", "label=accepted" or "label=raised".
const writeShapeProbe = `local schema = dofile("schema_module.nut", true);
local serialize = dofile("serialize.nut", true);
local M = schema.require("WriteShape");

local write = function(label, v) {
  local res;
  try {
    res = M.WriteRow(serialize.WriteStream(blob(4096)), v);
  } catch (e) {
    print(label + "=raised\n");
    return;
  }
  print(label + "=" + (res == false ? "refused" : "accepted") + "\n");
};

local row = function(label, mutate) {
  local v = M.Row();
  v.XsCount = 1;
  mutate(v);
  write(label, v);
};

row("valid", function(v) {});
row("shape_union_value", function(v) { v.U = 1; });
row("shape_union_table", function(v) { v.U = { Type = 0 }; });
row("shape_arm_payload", function(v) { v.U.Type = 1; v.U.Arm = 1; });
row("shape_struct_field", function(v) { v.A = "a"; });
row("shape_struct_table", function(v) { v.A = { N = 0 }; });
row("shape_array", function(v) { v.Xs = 1; });
row("shape_array_short", function(v) { v.XsCount = 2; v.Xs = [0]; });
row("shape_fixed_array", function(v) { v.Fs = [0]; });
row("shape_byte_array", function(v) { v.Bs = 1; });
row("shape_byte_array_short", function(v) { v.Bs = blob(2); });
row("shape_array_union_elem", function(v) { v.UsCount = 1; v.Us[0] = 5; });
row("shape_array_struct_elem", function(v) { v.ArmsCount = 1; v.Arms[0] = true; });
row("shape_bool", function(v) { v.Ok = 1; });
row("shape_float", function(v) { v.Fl = "1.5"; });
row("shape_compressed_float", function(v) { v.Cf = "0.5"; });
row("shape_double", function(v) { v.Db = null; });
row("shape_ranged_int32", function(v) { v.R = 1.5; });
row("shape_ranged_int32_text", function(v) { v.R = "1"; });
row("shape_ranged_int64", function(v) { v.R64 = "1"; });
row("shape_int128", function(v) { v.W = 5; });
row("shape_int128_pair", function(v) { v.W = [1.5, 0]; });
row("shape_uint128", function(v) { v.Wu = "x"; });
row("shape_fixed", function(v) { v.F = 1.5; });
row("shape_fixed128", function(v) { v.G = 3; });
write("shape_value", 7);
write("shape_value_other_record", M.Arm());
`

// TestSquirrelWriteRefusesEveryValueShape pins the Squirrel writer's shape
// checks (SPEC §5.1; the dynamic legs refuse a bad value in every build):
// every value of the wrong shape is refused (Write answers false), never
// thrown from an index or from the serialize runtime, and a valid value is
// accepted.
func TestSquirrelWriteRefusesEveryValueShape(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "WriteShape.schema")
	if err := os.WriteFile(path, []byte(writeShapeSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(loadUnit(t, path))
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, squirrel.Serialize())
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(writeShapeProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := sqrun.Run(t, dir, "main.nut", nil)
	got := map[string]string{}
	var order []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
		order = append(order, k)
	}
	if got["valid"] != "accepted" {
		t.Fatalf("valid row: %q, want accepted\n%s", got["valid"], out)
	}
	for _, k := range order {
		if k != "valid" && got[k] != "refused" {
			t.Errorf("%s: %q, want refused", k, got[k])
		}
	}
	if len(order) != 27 {
		t.Fatalf("probe printed %d rows, want 27:\n%s", len(order), out)
	}
}
