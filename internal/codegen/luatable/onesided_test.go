package luatable

import (
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	luacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// A one-sided integer range on a table field is the two-sided declaration
// (docs/SPEC-TABLES.md §4.3, SPEC §4.6): the checker fills the missing side
// from the type's limit, so the Lua descriptor clamps at the same bound the
// generated Go does — including a fill above int64's ceiling, which is an
// unsigned comparison (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS).
//
// The values below are at, inside and one past each declared bound. The Lua
// leg writes each raw value, reads it back through the binary wire, and reads
// its JSON spelling; the generated Go does the same, and every verdict, clamp
// count and written byte must agree.
const oneSidedSchema = `package probe

table OneSided
{
    i8lo int8 | min = -3
    i8hi int8 | max = 7
    i64lo int64 | min = -7
    i64hi int64 | max = 9
    u64lo uint64 = 18446744073709551610 | min = 18446744073709551610
    u64hi uint64 | max = 18446744073709551614
}
`

type oneSidedCase struct {
	name  string
	field string
	value string
}

var oneSidedCases = []oneSidedCase{
	// int8 | min = -3, max = 127
	{"i8lo/past-min", "i8lo", "-4"},
	{"i8lo/at-min", "i8lo", "-3"},
	{"i8lo/inside", "i8lo", "0"},
	{"i8lo/at-max", "i8lo", "127"},
	// int8 | min = -128, max = 7
	{"i8hi/at-min", "i8hi", "-128"},
	{"i8hi/inside", "i8hi", "0"},
	{"i8hi/at-max", "i8hi", "7"},
	{"i8hi/past-max", "i8hi", "8"},
	// int64 | min = -7, max = 9223372036854775807
	{"i64lo/past-min", "i64lo", "-9223372036854775808"},
	{"i64lo/at-min", "i64lo", "-7"},
	{"i64lo/inside", "i64lo", "0"},
	{"i64lo/at-max", "i64lo", "9223372036854775807"},
	// int64 | min = -9223372036854775808, max = 9
	{"i64hi/at-min", "i64hi", "-9223372036854775808"},
	{"i64hi/inside", "i64hi", "0"},
	{"i64hi/at-max", "i64hi", "9"},
	{"i64hi/past-max", "i64hi", "10"},
	// uint64 | min = 18446744073709551610, max = 18446744073709551615
	{"u64lo/past-min", "u64lo", "18446744073709551609"},
	{"u64lo/at-min", "u64lo", "18446744073709551610"},
	{"u64lo/inside", "u64lo", "18446744073709551612"},
	{"u64lo/at-max", "u64lo", "18446744073709551615"},
	// uint64 | min = 0, max = 18446744073709551614
	{"u64hi/at-min", "u64hi", "0"},
	{"u64hi/inside", "u64hi", "5"},
	{"u64hi/at-max", "u64hi", "18446744073709551614"},
	{"u64hi/past-max", "u64hi", "18446744073709551615"},
}

func oneSidedBig(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("%q is not a decimal integer", s)
	}
	return v
}

// TestLuaOneSidedRange pins a one-sided integer range on the Lua leg against
// the generated Go: the filled bound clamps a binary read and a JSON read,
// and the bytes a writer emits agree.
func TestLuaOneSidedRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schema, []byte(oneSidedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate lua: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)

	var cases strings.Builder
	for _, c := range oneSidedCases {
		v := oneSidedBig(t, c.value)
		text := fmt.Sprintf(`{"%s":%s}`, c.field, c.value)
		fmt.Fprintf(&cases, "  { name = %s, field = %s, lit = %s, text = %s },\n",
			luarun.Quote([]byte(c.name)), luarun.Quote([]byte(c.field)),
			luacodegen.IntLiteral(v), luarun.Quote([]byte(text)))
	}
	probe := `local T = require("` + ModuleName(u) + `")
local save = require("save")
local load = require("load")
local J = require("json")
local function hex(s)
  if s == nil then return "nil" end
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local cases = {
` + cases.String() + `}
for _, c in ipairs(cases) do
  local v = T.NewOneSided()
  v[c.field] = c.lit
  local raw = save.Save(T, "OneSided", v)
  local jr = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local jv = J.FromJson(T, "OneSided", c.text, jr)
  local jsave = jv and save.Save(T, "OneSided", jv) or nil
  local jtext = jv and J.ToJson(T, "OneSided", jv) or nil
  local br = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local bv = load.Load(T, "OneSided", raw, br)
  local btext = bv and J.ToJson(T, "OneSided", bv) or nil
  print(table.concat({ c.name, hex(raw), tostring(jr.malformed), tostring(jr.clamped),
    hex(jsave), hex(jtext), tostring(br.malformed), tostring(br.clamped), hex(btext) }, "|"))
end
`
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	luaOut := strings.TrimRight(string(luarun.Run(t, luaDir, "main.lua", nil)), "\n")
	luaLines := strings.Split(luaOut, "\n")

	// The generated Go oracle reads the same JSON text and the raw wires the
	// Lua writer emitted, so both legs' verdicts, clamp counts and bytes are
	// compared directly.
	var goCases strings.Builder
	for i, c := range oneSidedCases {
		fields := strings.SplitN(luaLines[i], "|", 3)
		if len(fields) < 2 {
			t.Fatalf("lua case %s: bad line %q", c.name, luaLines[i])
		}
		text := fmt.Sprintf(`{"%s":%s}`, c.field, c.value)
		fmt.Fprintf(&goCases, "  {name: %q, text: %q, wire: %q},\n", c.name, text, fields[1])
	}
	goProbe := `package probe
import("encoding/hex";"fmt";"testing";"unsafe")
func TestOneSidedOracle(t *testing.T) {
 cases := []struct{name, text, wire string}{
` + goCases.String() + ` }
 for _, c := range cases {
  var b OneSidedBuilder
  if !b.Init() { t.Fatalf("%s: builder init", c.name) }
  var jr TableReport
  jok := OneSidedFromJsonTolerant(&b, []byte(c.text), &jr)
  jsave, jtext := "nil", "nil"
  if jok && b.Lock() {
   r := b.AsConst()
   if n := OneSidedMeasure(r); n >= 0 {
    out := make([]byte, n)
    if OneSidedSave(r, out) == n { jsave = hex.EncodeToString(out) }
   }
   if n := OneSidedToJsonMeasure(r); n >= 0 {
    out := make([]byte, n)
    if OneSidedToJson(r, out) == n { jtext = hex.EncodeToString(out) }
   }
  }
  b.Shutdown()
  wire, err := hex.DecodeString(c.wire)
  if err != nil { t.Fatalf("%s: wire: %v", c.name, err) }
  size := OneSidedLoadMeasure(wire)
  raw := make([]byte, size+16)
  off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15
  region := raw[off : off+uintptr(size)]
  var br TableReport
  bv := OneSidedLoadTolerant(region, wire, &br)
  btext := "nil"
  if bv != nil {
   if n := OneSidedToJsonMeasure(bv); n >= 0 {
    out := make([]byte, n)
    if OneSidedToJson(bv, out) == n { btext = hex.EncodeToString(out) }
   }
  }
  fmt.Printf("CASE|%s|%t|%d|%s|%s|%t|%d|%s\n", c.name, jr.Malformed, jr.Clamped, jsave, jtext, br.Malformed, br.Clamped, btext)
 }
}
`
	goFiles, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate go tables: %v", err)
	}
	goDir := t.TempDir()
	goFiles["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	goFiles["onesided_oracle_test.go"] = []byte(goProbe)
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-v", "-count=1", "-timeout", "600s", "-run", "^TestOneSidedOracle$", ".")
	cmd.Dir = goDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go oracle: %v\n%s", err, out)
	}
	var goLines []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "CASE|") {
			goLines = append(goLines, strings.TrimPrefix(line, "CASE|"))
		}
	}
	if len(goLines) != len(oneSidedCases) {
		t.Fatalf("generated Go oracle printed %d lines, want %d:\n%s", len(goLines), len(oneSidedCases), out)
	}

	// The Go probe prints one line per case, in order. Compare every field but
	// the raw wire, which is the Lua writer's input to both binary reads.
	for i, c := range oneSidedCases {
		lua := strings.Split(luaLines[i], "|")
		if len(lua) != 9 {
			t.Fatalf("lua case %s: %q has %d fields, want 9", c.name, luaLines[i], len(lua))
		}
		gof := strings.Split(goLines[i], "|")
		if len(gof) != 8 {
			t.Fatalf("go case %s: %q has %d fields, want 8", c.name, goLines[i], len(gof))
		}
		// Lua's fields 0 and 2..8 answer the Go probe's 0..7: the raw wire
		// (Lua's field 1) is the shared input, not an answer.
		want := []string{lua[0], lua[2], lua[3], lua[4], lua[5], lua[6], lua[7], lua[8]}
		for j := range want {
			if gof[j] != want[j] {
				t.Errorf("%s: field %d: lua %q, generated go %q (lua %q, go %q)",
					c.name, j, want[j], gof[j], luaLines[i], goLines[i])
			}
		}
	}
}
