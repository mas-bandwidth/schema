package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
)

// writeRefusesSchema declares one field per SPEC.md §5.1 row 1 to 6 kind the
// packet writer holds, plus a ranged array element and a ranged arm payload.
const writeRefusesSchema = `package writerefuses

enum Team { Red, Blue }

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
    w    int128 | min = -100, max = 100
    f    fixed(8, 8) | min = -10, max = 10
    g    fixed(70, 58) | min = -1000, max = 1000
    b    bits(4)
    b32  bits(32)
    b40  bits(40)
    b63  bits(63)
    xs   [1..3]int32
    s    string(4)
    y    bytes(4)
    e    Team
    u    Pick
    rs   [..3]int32 | min = 0, max = 7
    q    uint64 | min = 0, max = 18446744073709551615
    qq   uint64 | min = 0, max = 9223372036854775900
    es   [..2]Team
    us   [..2]Pick
    fs   [2]fixed(8, 8) | min = -10, max = 10
}
`

// writeRefusesProbe writes one valid Row, then one Row per off-contract value,
// printing "label=refused", "label=accepted" or "label=raised".
const writeRefusesProbe = `local serialize = require("serialize")
local M = require("WriteRefuses")
local function valid()
  local v = M.NewRow()
  v.XsCount = 1
  return v
end
local function row(label, mutate)
  local v = valid()
  mutate(v)
  local stream = serialize.WriteStream.new(4096)
  local ok, result = pcall(M.WriteRow, stream, v)
  if not ok then
    print(label .. "=raised")
  elseif result == false then
    print(label .. "=refused")
  else
    print(label .. "=accepted")
  end
end
row("valid", function(_) end)
row("row1_ranged_int", function(v) v.R = 11 end)
row("row1_int128", function(v) v.W = { lo = 101, hi = 0 } end)
row("row1_int128_kind", function(v) v.W = 5 end)
row("row1_fixed", function(v) v.F = 11 * 256 end)
row("row1_fixed_kind", function(v) v.F = 1.5 end)
row("row1_wide_fixed", function(v) v.G = { lo = 0, hi = 0x7fffffffffffffff } end)
row("row1_wide_fixed_kind", function(v) v.G = 3 end)
row("row1_array_elem", function(v) v.RsCount = 1; v.Rs[1] = 8 end)
row("row1_arm_payload", function(v) v.U.Type = 1; v.U.Arm.N = 6 end)
row("row2_bits_wide", function(v) v.B = 16 end)
row("row2_bits_negative", function(v) v.B = -1 end)
row("row2_bits32_wide", function(v) v.B32 = 1 << 32 end)
row("row2_bits32_negative", function(v) v.B32 = -1 end)
row("row2_bits40_wide", function(v) v.B40 = 1 << 40 end)
row("row2_bits40_negative", function(v) v.B40 = -1 end)
row("row2_bits63_wide", function(v) v.B63 = 1 << 63 end)
row("row3_count_over", function(v) v.XsCount = 4 end)
row("row3_count_under", function(v) v.XsCount = 0 end)
row("row4_string_long", function(v) v.S = "abcde" end)
row("row4_bytes_long", function(v) v.Y = "abcde" end)
row("row5_enum", function(v) v.E = 3 end)
row("row6_union_tag", function(v) v.U.Type = 2 end)
row("row1_u64_low", function(v) v.Qq = -1 end)
row("row5_enum_elem", function(v) v.EsCount = 1; v.Es[1] = 9 end)
row("row6_union_elem", function(v) v.UsCount = 1; v.Us[1].Type = 7 end)
row("row1_fixed_elem", function(v) v.Fs[1] = 4000 end)
row("row1_arm_elem", function(v) v.UsCount = 1; v.Us[1].Type = 1; v.Us[1].Arm.N = 9 end)
row("row1_ranged_float", function(v) v.R = 1.5 end)
row("row5_enum_neg", function(v) v.E = -1 end)
row("row6_union_neg", function(v) v.U.Type = -1 end)
`

// TestLuaPacketWriteRefusesRowsOneToSix pins SPEC.md §5.1 rows 1 to 6 for the
// Lua packet Write: every value off its declaration is REFUSED (Write answers
// false) in every build, never raised, and a valid value is accepted.
func TestLuaPacketWriteRefusesRowsOneToSix(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "WriteRefuses.schema")
	if err := os.WriteFile(path, []byte(writeRefusesSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(writeRefusesProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
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
		if k == "valid" {
			continue
		}
		if got[k] != "refused" {
			t.Errorf("%s: %q, want refused", k, got[k])
		}
	}
	if len(order) != 31 {
		t.Fatalf("probe printed %d rows, want 31:\n%s", len(order), out)
	}
}
