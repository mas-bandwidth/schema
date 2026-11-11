package squirreltable_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// fixedSaveRefusalUnit holds one field per row of docs/SPEC.md §5.1 that a
// fixed record carries: a ranged integer (row 1), a bits(N) (row 2), a counted
// array (row 3), a string(N) (rows 4 and 8), an enum (row 5), a union (row 6),
// a plain float (row 7), a ranged float (row 10), and a bare int8 and an
// optional int32 whose storage width bounds them.
const fixedSaveRefusalUnit = `package fsw

enum Mode { Idle, Run }

union Pick
{
    a uint8
    b int16
}

fixed table Rec
{
    r     uint64 = 1 | min = 1, max = 10
    bi    bits(6)
    arr   [..3]uint16
    s     string(4)
    mode  Mode
    pick  Pick
    ratio float32 | min = 0, max = 1, resolution = 0.01
    small int8
    oi    ?int32
    plain float64
}
`

// fixedSaveRefusalProbe runs every row through RecFixedSave. An off-contract
// value must be refused (a negative answer) with nothing raised; its lawful
// twin at the boundary must be written and read back exactly by RecFixedLoad.
const fixedSaveRefusalProbe = `local S = dofile("schema_module.nut", true);
local T = S.require("FswTables");
local same;
same = function(a, b) {
  if (typeof a != typeof b) return false;
  if (typeof a == "array") {
    if (a.len() != b.len()) return false;
    foreach (i, x in a) if (!same(x, b[i])) return false;
    return true;
  }
  return a == b;
};
local nan = casti2f(0x7ff8000000000000);
local inf = casti2f(0x7ff0000000000000);
local rows = [
  // row 1: a ranged integer off its min/max
  ["r_over", false, function(r) { r.R = 99; }, null],
  ["r_under", false, function(r) { r.R = 0; }, null],
  ["r_negative", false, function(r) { r.R = -1; }, null],
  ["r_at_max", true, function(r) { r.R = 10; }, function(r) { return r.R; }],
  ["r_at_min", true, function(r) { r.R = 1; }, function(r) { return r.R; }],
  // row 2: a bits(6) value wider than six bits
  ["bi_over", false, function(r) { r.Bi = 64; }, null],
  ["bi_far_over", false, function(r) { r.Bi = 1125899906842624; }, null],
  ["bi_negative", false, function(r) { r.Bi = -1; }, null],
  ["bi_at_max", true, function(r) { r.Bi = 63; }, function(r) { return r.Bi; }],
  // row 3: a counted array past its bound, and an element its storage cannot carry
  ["arr_over", false, function(r) { r.Arr = [1, 2, 3, 4]; }, null],
  ["arr_element_over", false, function(r) { r.Arr = [65536]; }, null],
  ["arr_element_negative", false, function(r) { r.Arr = [-1]; }, null],
  ["arr_at_bound", true, function(r) { r.Arr = [0, 1, 65535]; }, function(r) { return r.Arr; }],
  // rows 4 and 8: a string(4) past its bound, and ill-formed text
  ["s_over", false, function(r) { r.S = "abcde"; }, null],
  ["s_ill_formed", false, function(r) { r.S = "\xff"; }, null],
  ["s_at_bound", true, function(r) { r.S = "abcd"; }, function(r) { return r.S; }],
  // row 5: an enum value no variant names
  ["mode_unknown_name", false, function(r) { r.Mode = "Nope"; }, null],
  ["mode_ordinal", false, function(r) { r.Mode = 7; }, null],
  ["mode_last", true, function(r) { r.Mode = "Run"; }, function(r) { return r.Mode; }],
  // row 6: a union tag outside its arm set, and an arm its storage cannot carry
  ["pick_tag_over", false, function(r) { r.Pick.Type = 3; }, null],
  ["pick_tag_negative", false, function(r) { r.Pick.Type = -1; }, null],
  ["pick_arm_over", false, function(r) { r.Pick.Type = 1; r.Pick.A = 256; }, null],
  ["pick_last_arm", true, function(r) { r.Pick.Type = 2; r.Pick.B = -32768; }, function(r) { return [r.Pick.Type, r.Pick.B]; }],
  // row 7: a NaN or an infinity in a plain float
  ["plain_nan", false, function(r) { r.Plain = nan; }, null],
  ["plain_inf", false, function(r) { r.Plain = inf; }, null],
  ["plain_large", true, function(r) { r.Plain = 1.0e308; }, function(r) { return r.Plain; }],
  // row 10: a ranged float off its range
  ["ratio_over", false, function(r) { r.Ratio = 1.5; }, null],
  ["ratio_under", false, function(r) { r.Ratio = -0.5; }, null],
  ["ratio_at_max", true, function(r) { r.Ratio = 1.0; }, function(r) { return r.Ratio; }],
  // a bare integer its storage width cannot carry
  ["small_over", false, function(r) { r.Small = 300; }, null],
  ["small_under", false, function(r) { r.Small = -129; }, null],
  ["small_at_max", true, function(r) { r.Small = 127; }, function(r) { return r.Small; }],
  ["small_at_min", true, function(r) { r.Small = -128; }, function(r) { return r.Small; }],
  ["oi_over", false, function(r) { r.OiPresent = true; r.Oi = 1099511627781; }, null],
  ["oi_at_max", true, function(r) { r.OiPresent = true; r.Oi = 2147483647; }, function(r) { return [r.OiPresent, r.Oi]; }],
  ["oi_at_min", true, function(r) { r.OiPresent = true; r.Oi = -2147483648; }, function(r) { return [r.OiPresent, r.Oi]; }],
  // a value of the wrong Squirrel type
  ["small_string", false, function(r) { r.Small = "x"; }, null],
  ["r_float", false, function(r) { r.R = 1.5; }, null],
  ["bi_null", false, function(r) { r.Bi = null; }, null],
  ["pick_tag_string", false, function(r) { r.Pick.Type = "a"; }, null],
  ["ratio_string", false, function(r) { r.Ratio = "a"; }, null],
  ["oi_string", false, function(r) { r.OiPresent = true; r.Oi = "x"; }, null],
];
foreach (row in rows) {
  local verdict = "";
  try {
    local r = T.Rec();
    row[2](r);
    local out = blob(1024);
    local n = T.RecFixedSave(out, [r]);
    if (n < 0) {
      verdict = "refused";
    } else if (!row[1]) {
      verdict = "wrote";
    } else {
      local file = blob(n);
      for (local i = 0; i < n; i++) file[i] = out[i];
      local back = T.Rec();
      local got = T.RecFixedLoad(file, [back], null);
      if (got != 1) verdict = "unreadable:" + T.FixedReason;
      else if (!same(row[3](r), row[3](back))) verdict = "changed";
      else verdict = "wrote";
    }
  } catch (e) {
    verdict = "raised:" + e;
  }
  print(row[0] + "=" + verdict + "\n");
}
`

// TestSquirrelFixedSaveRefusesOffContract holds docs/SPEC.md §5.1 for the
// Squirrel fixed writer, an every-build leg: <Root>FixedSave refuses a value
// its declaration does not admit (rows 1 to 8 and 10, a bare integer its
// storage width cannot carry, and a value of the wrong Squirrel type) without
// raising, and the lawful twin at each boundary is written and read back
// exactly. The Go engine's fixed writer answers the same verdicts first.
func TestSquirrelFixedSaveRefusesOffContract(t *testing.T) {
	t.Parallel()
	u := fixedSynthetic(t, fixedSaveRefusalUnit)

	t.Run("go", func(t *testing.T) {
		t.Parallel()
		m := tabletext.NewModel(u)
		field := func(inst *tabletext.Instance, name string) *tabletext.Field {
			for i := range inst.Fields {
				if inst.Fields[i].Def.Name == name {
					return &inst.Fields[i]
				}
			}
			t.Fatalf("no field %s", name)
			return nil
		}
		rows := []struct {
			name   string
			lawful bool
			set    func(inst *tabletext.Instance)
		}{
			{"r_over", false, func(i *tabletext.Instance) { field(i, "r").Cell.U = 99 }},
			{"r_under", false, func(i *tabletext.Instance) { field(i, "r").Cell.U = 0 }},
			{"r_at_max", true, func(i *tabletext.Instance) { field(i, "r").Cell.U = 10 }},
			{"bi_over", false, func(i *tabletext.Instance) { field(i, "bi").Cell.U = 64 }},
			{"bi_at_max", true, func(i *tabletext.Instance) { field(i, "bi").Cell.U = 63 }},
			{"arr_over", false, func(i *tabletext.Instance) { field(i, "arr").Count = 4 }},
			{"arr_element_over", false, func(i *tabletext.Instance) {
				f := field(i, "arr")
				f.Count = 1
				f.Elems[0].U = 65536
			}},
			{"arr_at_bound", true, func(i *tabletext.Instance) {
				f := field(i, "arr")
				f.Count = 3
				f.Elems[2].U = 65535
			}},
			{"s_over", false, func(i *tabletext.Instance) { field(i, "s").Cell.Str = []byte("abcde") }},
			{"s_ill_formed", false, func(i *tabletext.Instance) { field(i, "s").Cell.Str = []byte{0xff} }},
			{"s_at_bound", true, func(i *tabletext.Instance) { field(i, "s").Cell.Str = []byte("abcd") }},
			{"mode_ordinal", false, func(i *tabletext.Instance) { field(i, "mode").Cell.U = 7 }},
			{"mode_last", true, func(i *tabletext.Instance) { field(i, "mode").Cell.U = 2 }},
			{"pick_tag_over", false, func(i *tabletext.Instance) { field(i, "pick").Cell.U = 3 }},
			{"pick_last_arm", true, func(i *tabletext.Instance) { field(i, "pick").Cell.U = 2 }},
			{"plain_nan", false, func(i *tabletext.Instance) { field(i, "plain").Cell.F = math.NaN() }},
			{"plain_inf", false, func(i *tabletext.Instance) { field(i, "plain").Cell.F = math.Inf(1) }},
			{"plain_large", true, func(i *tabletext.Instance) { field(i, "plain").Cell.F = 1e308 }},
			{"ratio_over", false, func(i *tabletext.Instance) { field(i, "ratio").Cell.F = 1.5 }},
			{"ratio_under", false, func(i *tabletext.Instance) { field(i, "ratio").Cell.F = -0.5 }},
			{"ratio_at_max", true, func(i *tabletext.Instance) { field(i, "ratio").Cell.F = 1 }},
			{"small_over", false, func(i *tabletext.Instance) { field(i, "small").Cell.U = 300 }},
			{"small_at_max", true, func(i *tabletext.Instance) { field(i, "small").Cell.U = 127 }},
			{"oi_over", false, func(i *tabletext.Instance) {
				f := field(i, "oi")
				f.Present = true
				f.Cell.U = 1<<40 + 5
			}},
			{"oi_at_max", true, func(i *tabletext.Instance) {
				f := field(i, "oi")
				f.Present = true
				f.Cell.U = math.MaxInt32
			}},
		}
		root := m.Lookup("Rec")
		if root == nil {
			t.Fatal("no table Rec")
		}
		for _, row := range rows {
			inst := m.New(root)
			row.set(inst)
			out, err := tablewire.EncodeFixed(m, inst)
			if row.lawful {
				if err != nil || out == nil {
					t.Errorf("%s: EncodeFixed refused a lawful value: %v", row.name, err)
				}
				continue
			}
			if out != nil || err == nil {
				t.Errorf("%s: EncodeFixed answered %x %v, want a refusal", row.name, out, err)
			}
		}
	})

	t.Run("squirrel", func(t *testing.T) {
		t.Parallel()
		sqrun.Require(t)
		dir := t.TempDir()
		writeGenerated(t, dir, u)
		if err := os.WriteFile(filepath.Join(dir, "probe.nut"), []byte(fixedSaveRefusalProbe), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "probe.nut", nil)), "\n"), "\n")
		if len(lines) != strings.Count(fixedSaveRefusalProbe, "\n  [\"") {
			t.Fatalf("the probe answered %d rows, want %d:\n%s", len(lines), strings.Count(fixedSaveRefusalProbe, "\n  [\""), strings.Join(lines, "\n"))
		}
		for _, line := range lines {
			name, verdict, _ := strings.Cut(line, "=")
			want := "refused"
			if strings.Contains(fixedSaveRefusalProbe, "[\""+name+"\", true,") {
				want = "wrote"
			}
			if verdict != want {
				t.Errorf("%s: RecFixedSave answered %q, want %q", name, verdict, want)
			}
		}
	})
}
