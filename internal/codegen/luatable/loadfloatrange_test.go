package luatable

// A RANGED FLOAT IS HELD TO ITS DECLARED RANGE ON THE VARIABLE READ
// (docs/SPEC-TABLES.md, the VARIABLE row of the float table: "the READ
// refuses a non-finite value and then a finite one outside the reader's
// declared range (§4)"; docs/SPEC.md §5.1 row 10). No reader clamps: the
// strict Load refuses a finite compressed float32 below its min or above its
// max, as a field and as an element of a list, and reads every value from min
// to max inclusive.
//
// The wire is the leg's own Save of an in-range value; the test then patches
// the float's four bytes in Go, so the write contract plays no part.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const lfrSchema = `package probe

table Field
{
    cf float32 | min = 0.0, max = 10.0, resolution = 0.01
}

table Elements
{
    xs []float32 | min = 0.0, max = 10.0, resolution = 0.01
}
`

// lfrSaveProbe saves each root at the in-range value 5.0 and prints
// <root>|<hex of the wire>.
func lfrSaveProbe(module string) string {
	return `local T = require("` + module + `")
local save = require("save")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local f = T.NewField()
f.cf = 5.0
print("Field|" .. hex(assert(save.Save(T, "Field", f), "save Field")))
local l = T.NewElements()
l.xs = { 5.0 }
print("Elements|" .. hex(assert(save.Save(T, "Elements", l), "save Elements")))
`
}

// lfrLoadProbe runs the strict Load over every input wire, named
// <root>|<value>, and prints <name>|refused or <name>|read|<value read>.
func lfrLoadProbe(module string, names []string) string {
	var b strings.Builder
	b.WriteString(`local T = require("` + module + `")
local load = require("load")
local inputs = require("inputs")
local names = {
`)
	for _, n := range names {
		fmt.Fprintf(&b, "  %q,\n", n)
	}
	b.WriteString(`}
for _, name in ipairs(names) do
  local root = name:match("^([^|]+)")
  local v = load.Load(T, root, inputs[name], {})
  if v == nil then
    print(name .. "|refused")
  else
    local x
    if root == "Field" then
      x = v.cf
    else
      assert(#v.xs == 1, name .. ": the list read back " .. #v.xs .. " elements")
      x = v.xs[1]
    end
    print(name .. "|read|" .. string.format("%.9g", x))
  end
end
`)
	return b.String()
}

func TestLuaLoadHoldsCompressedFloatRange(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")
	tmp := t.TempDir()
	schemaPath := filepath.Join(tmp, "probe.schema")
	if err := os.WriteFile(schemaPath, []byte(lfrSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schemaPath)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "lua")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	luarun.WriteFiles(t, dir, files)
	module := ModuleName(u)
	if err := os.WriteFile(filepath.Join(dir, "save.main.lua"), []byte(lfrSaveProbe(module)), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luarun.Run(t, dir, "save.main.lua", nil))), "\n") {
		root, h, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("the save probe printed %q", line)
		}
		wire, err := hex.DecodeString(h)
		if err != nil {
			t.Fatalf("the save probe printed %q: %v", line, err)
		}
		saved[root] = wire
	}

	f32 := func(v float32) []byte {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		return b[:]
	}
	saveBits := f32(5.0)

	type lfrCase struct {
		value  float32
		refuse bool
	}
	cases := []lfrCase{
		{11.0, true},
		{-1.0, true},
		{0.0, false},
		{5.0, false},
		{10.0, false},
	}
	inputs := map[string][]byte{}
	var names []string
	want := map[string]string{}
	for _, root := range []string{"Field", "Elements"} {
		wire, ok := saved[root]
		if !ok {
			t.Fatalf("the save probe printed no %s wire", root)
		}
		if n := bytes.Count(wire, saveBits); n != 1 {
			t.Fatalf("the %s wire holds the saved float's bits %d times, want once: %x", root, n, wire)
		}
		at := bytes.Index(wire, saveBits)
		for _, c := range cases {
			patched := bytes.Clone(wire)
			copy(patched[at:], f32(c.value))
			name := fmt.Sprintf("%s|%g", root, c.value)
			inputs[name] = patched
			names = append(names, name)
			if c.refuse {
				want[name] = "refused"
			} else {
				want[name] = fmt.Sprintf("read|%g", c.value)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "load.main.lua"), []byte(lfrLoadProbe(module, names)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luarun.Run(t, dir, "load.main.lua", inputs))), "\n") {
		i := strings.Index(line, "|")
		j := strings.Index(line[i+1:], "|")
		if i < 0 || j < 0 {
			t.Fatalf("the load probe printed %q", line)
		}
		got[line[:i+1+j]] = line[i+1+j+1:]
	}
	for _, name := range names {
		if got[name] != want[name] {
			t.Errorf("Load of %s: got %q, want %q", name, got[name], want[name])
		}
	}
}
