package luatable

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const wideFixedSchema = `package widefixed

table Root {
    a fixed(16, 16) | min = -1000, max = 1000
    b ufixed(8, 8) = 10 | min = 10, max = 200
    c fixed(64, 64) | min = -1000, max = 1000
}
`

// wideFixedTwinSchema is wideFixedSchema's full-range twin (wide128TwinSchema
// says why): the same names, kinds and default, each field declared over its
// kind's whole-unit range, so its writer carries the raw values one unit past
// the narrow bounds that the narrow schema refuses.
const wideFixedTwinSchema = `package widefixed

table Root {
    a fixed(16, 16) | min = -32768, max = 32767
    b ufixed(8, 8) = 10 | min = 0, max = 255
    c fixed(64, 64) | min = -9223372036854775808, max = 9223372036854775807
}
`

type wideFixedField struct {
	name string
	wide bool
}

var wideFixedFields = []wideFixedField{
	{"a", false},
	{"b", false},
	{"c", true},
}

// wideFixedCases are raw scaled integers (units × 2^F): the declared defaults,
// the declared bounds of fixed(16, 16), ufixed(8, 8) and fixed(64, 64), and
// one raw unit past each declared bound, which both writers and both readers
// refuse (docs/SPEC-TABLES.md §6.5).
var wideFixedCases = []struct {
	name     string
	values   map[string]string
	offRange bool
}{
	{name: "default", values: map[string]string{
		"a": "0",
		"b": "2560",
		"c": "0",
	}},
	{name: "min", values: map[string]string{
		"a": "-65536000",
		"b": "2560",
		"c": "-18446744073709551616000",
	}},
	{name: "max", values: map[string]string{
		"a": "65536000",
		"b": "51200",
		"c": "18446744073709551616000",
	}},
	{name: "one below min", offRange: true, values: map[string]string{
		"a": "-65536001",
		"b": "2559",
		"c": "-18446744073709551616001",
	}},
	{name: "one above max", offRange: true, values: map[string]string{
		"a": "65536001",
		"b": "51201",
		"c": "18446744073709551616001",
	}},
}

// TestLuaWideFixedPointVariableForm pins the variable form\x27s fixed(I, F) and
// ufixed(I, F) storage (docs/SPEC-TABLES.md §3 kinds 20-29; docs/PORT-LUA.md,
// FIXED-POINT): a Lua integer for I + F <= 64 and { lo = <int>, hi = <int> }
// for I + F == 128, raw scaled units on the wire, with declared bounds
// shifted onto the raw scale; a raw value past them refuses on save and load.
func TestLuaWideFixedPointVariableForm(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := filepath.Join(dir, "widefixed.schema")
	if err := os.WriteFile(schema, []byte(wideFixedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out := t.TempDir()
	luarun.WriteFiles(t, out, files)

	model := tabletext.NewModel(u)
	root := model.Lookup("Root")
	twin := wideTwinModel(t, "widefixed.schema", wideFixedTwinSchema)
	for _, tc := range wideFixedCases {
		t.Run(tc.name, func(t *testing.T) {
			inst := model.New(root)
			var saveValues []string
			values := map[string]*big.Int{}
			for _, f := range wideFixedFields {
				v := wide128Big(t, tc.values[f.name])
				values[f.name] = v
				wideSetCell(t, inst, f.name, v)
				if f.wide {
					lo, hi := wide128Lanes(v)
					saveValues = append(saveValues, fmt.Sprintf("v.%s = { lo = 0x%016x, hi = 0x%016x }", f.name, lo, hi))
				} else {
					saveValues = append(saveValues, fmt.Sprintf("v.%s = %s", f.name, lua.IntLiteral(v)))
				}
			}
			twinWire := wideTwinWire(t, twin, values)
			if tc.offRange {
				requireWideOffRangeRefused(t, model, inst, twinWire, out, ModuleName(u), strings.Join(saveValues, "\n"))
				return
			}
			wire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("the Go engine refused the save: %v", err)
			}
			if !bytes.Equal(wire, twinWire) {
				t.Fatalf("the full-range twin's wire differs from the ranged one\n twin %x\n want %x", twinWire, wire)
			}
			back := model.New(root)
			var goReport tabletext.Report
			if ok, err := tablewire.Decode(model, back, wire, &goReport); err != nil || !ok {
				t.Fatalf("the Go engine refused its own wire: ok=%v err=%v report=%+v", ok, err, goReport)
			}

			var want strings.Builder
			fmt.Fprintf(&want, "%x\n", wire)
			for _, name := range []string{"a", "b"} {
				fmt.Fprintf(&want, "%s %d\n", name, wide128Cell(t, back, name).Int64())
			}
			lo, hi := wide128Lanes(wide128Cell(t, back, "c"))
			fmt.Fprintf(&want, "c %016x %016x\n", lo, hi)
			fmt.Fprintf(&want, "clamped %d\n", 0)

			probe := `local T = require("` + ModuleName(u) + `")
local save = require("save")
local load = require("load")
local inputs = require("inputs")
local v = T.NewRoot()
` + strings.Join(saveValues, "\n") + `
local wire = save.Save(T, "Root", v)
print((wire:gsub(".", function(c) return string.format("%02x", c:byte()) end)))
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
local back = load.Load(T, "Root", inputs.wire, report)
for _, name in ipairs({ "a", "b" }) do
  print(string.format("%s %d", name, back[name]))
end
local x = back.c
print(string.format("c %016x %016x", x.lo, x.hi))
print(string.format("clamped %d", report.outOfRange))
`
			if err := os.WriteFile(filepath.Join(out, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := string(luarun.Run(t, out, "main.lua", map[string][]byte{"wire": wire}))
			if got != want.String() {
				t.Errorf("Lua wide fixed-point value differs from the Go engine\x27s wire and values\n--- got ---\n%s--- want ---\n%s", got, want.String())
			}
		})
	}
}
