package luatable

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// A FLOAT32 TOKEN LANDS ON THE NEAREST FLOAT32, rounded once
// (docs/SPEC-TABLES.md §16.1, §16.2: "parsing to the nearest double and
// narrowing rounds twice"). Each token here is one whose nearest double is
// EXACTLY a float32 midpoint, so the double-then-narrow path rounds a second
// time onto the even neighbor; the token's own digits say which side of the
// midpoint it lies. The oracle is Go's strconv.ParseFloat(token, 32), which
// rounds the decimal once. The reversed witness puts the double-then-narrow
// path back and holds that the same probe goes red on exactly the tokens off
// a midpoint.
var float32Tokens = []string{
	"7.0064923216240853547e-46",  // just above 2^-150: 0x00000001
	"-7.0064923216240853547e-46", // its negative: 0x80000001
	"7.006492321624085354618647916449580656401309709382578858785341419448955413429303007433190941810607910156250e-46", // 2^-150 exactly: the even neighbor, 0
	"7.0064923216240853546e-46",   // just below 2^-150: 0
	"1.0000000596046447753906251", // just above 1 + 2^-24: 1 + 2^-23
	"1.000000059604644775390625",  // 1 + 2^-24 exactly: the even neighbor, 1
	"1.0000000596046447753906249", // just below: 1
	"1.000000178813934326171875",  // 1 + 3*2^-24 exactly: the even neighbor, 1 + 2^-22
	"1.0000001788139343261718749", // just below it: 1 + 2^-23
	"16777217.000000000000000001", // just above 2^24 + 1: 2^24 + 2
	"16777217",                    // 2^24 + 1 exactly: the even neighbor, 2^24
	"1.401298464324817e-45",       // off every midpoint: 0x00000001
	"3.4028234663852886e38",       // FLT_MAX
}

const float32TokenProbe = `local T = require("ProbeTables")
local J = require("json")
for _, token in ipairs(TOKENS) do
  local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local v = J.FromJsonTolerant(T, "F", '{"v": ' .. token .. '}', r)
  print(string.format("%08x %s %d", string.unpack("<I4", string.pack("<f", v.v)), tostring(r.malformed), r.kindMismatch))
end
`

func float32TokenRun(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	quoted := make([]string, len(float32Tokens))
	for i, tok := range float32Tokens {
		quoted[i] = strconv.Quote(tok)
	}
	main := "TOKENS = {" + strings.Join(quoted, ", ") + "}\n" + float32TokenProbe
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n"), "\n")
}

func float32TokenOracle(t *testing.T) []string {
	t.Helper()
	out := make([]string, len(float32Tokens))
	for i, tok := range float32Tokens {
		f, err := strconv.ParseFloat(tok, 32)
		if err != nil {
			t.Fatalf("%s: %v", tok, err)
		}
		out[i] = fmt.Sprintf("%08x false 0", math.Float32bits(float32(f)))
	}
	return out
}

func float32TokenUnit(t *testing.T) map[string][]byte {
	t.Helper()
	work := t.TempDir()
	path := filepath.Join(work, "probe.schema")
	if err := os.WriteFile(path, []byte("package probe\n\ntable F\n{\n    v float32\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Generate(luarun.LoadUnit(t, path))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return files
}

func TestLuaFloat32TokenRoundsOnce(t *testing.T) {
	t.Parallel()
	got := float32TokenRun(t, float32TokenUnit(t))
	want := float32TokenOracle(t)
	if len(got) != len(want) {
		t.Fatalf("the probe answered %d lines for %d tokens: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: Lua lands %s, the nearest float32 is %s", float32Tokens[i], got[i], want[i])
		}
	}
}

func TestLuaFloat32TokenDoubleRoundingNegativeControl(t *testing.T) {
	t.Parallel()
	files := float32TokenUnit(t)
	const anchor = "x = nearest_f32(token, x, narrow)"
	src := string(files["json.lua"])
	if !strings.Contains(src, anchor) {
		t.Fatalf("the plant's anchor is absent from json.lua: %q", anchor)
	}
	files["json.lua"] = []byte(strings.ReplaceAll(src, anchor, "x = narrow"))
	got := float32TokenRun(t, files)
	want := float32TokenOracle(t)
	var red []string
	for i := range want {
		if i < len(got) && got[i] != want[i] {
			red = append(red, float32Tokens[i])
		}
	}
	// the tokens off a midpoint on the far side from the even neighbor are
	// exactly the ones a second rounding moves
	wantRed := []string{float32Tokens[0], float32Tokens[1], float32Tokens[4], float32Tokens[8], float32Tokens[9]}
	if strings.Join(red, " ") != strings.Join(wantRed, " ") {
		t.Fatalf("double-then-narrow is red on %q, want exactly %q", red, wantRed)
	}
}
