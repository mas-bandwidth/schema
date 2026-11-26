package luatable

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const wide128Schema = `package wide128

table Root {
    a int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    b int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
    c uint128
    d uint128
}
`

var wide128Fields = []string{"a", "b", "c", "d"}

// wide128Cases are the halves a save writes: 0, -1, the kind's min and max,
// 2^64, and one value of each ranged field beyond its declared bounds. A
// scalar half is a uint64; the test splits each decimal value into low/high
// lanes with two's complement for the signed fields.
var wide128Cases = []struct {
	name     string
	values   map[string]string
	offRange bool
}{
	{name: "zero", values: map[string]string{"a": "0", "b": "0", "c": "0", "d": "0"}},
	{name: "negative one", values: map[string]string{"a": "-1", "b": "-1", "c": "1", "d": "1"}},
	{name: "min", values: map[string]string{
		"a": "-170141183460469231731687303715884105728",
		"b": "-1267650600228229401496703205376",
		"c": "0",
		"d": "0",
	}},
	{name: "max", values: map[string]string{
		"a": "170141183460469231731687303715884105727",
		"b": "1267650600228229401496703205376",
		"c": "340282366920938463463374607431768211455",
		"d": "18446744073709551616",
	}},
	{name: "two to the sixty-four", values: map[string]string{
		"a": "18446744073709551616",
		"b": "18446744073709551616",
		"c": "18446744073709551616",
		"d": "18446744073709551616",
	}},
	// b off its declared bounds: the strict read refuses the wire and the
	// writer refuses the value, in the Go engine and in Lua alike
	// (docs/SPEC-TABLES.md §6.5; docs/rulings.json strict-only-read)
	{name: "off its range", offRange: true, values: map[string]string{
		"a": "0",
		"b": "170141183460469231731687303715884105727",
		"c": "340282366920938463463374607431768211455",
		"d": "340282366920938463463374607431768211455",
	}},
}

// wide128TwinSchema is wide128Schema's full-range twin: the same package,
// table, field names and kinds, each ranged field declared over its kind's
// whole range (a ranged 128-bit field must declare one, SPEC.md §4.3). Its
// writer carries a value off the narrow schema's bounds, which that schema's
// writer refuses (tablewire.Encode: invalid_value), so the off-contract wire
// is made by the twin and then read by the narrow schema.
const wide128TwinSchema = `package wide128

table Root {
    a int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    b int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    c uint128
    d uint128
}
`

// wideTwinModel loads a full-range twin schema under the narrow schema's
// file name in its own directory.
func wideTwinModel(t *testing.T, file, schema string) *tabletext.Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), file)
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(luarun.LoadUnit(t, path))
}

// wideSetCell stores v as the named field's wide value.
func wideSetCell(t *testing.T, inst *tabletext.Instance, name string, v *big.Int) {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			inst.Fields[i].Cell.Wide = v
			return
		}
	}
	t.Fatalf("the root has no field %s", name)
}

// wideTwinWire is the twin's wire for the values; for an in-range value it
// must equal the ranged writer's wire byte for byte, which is what makes the
// twin's off-range wire the ranged schema's wire with only the value moved.
func wideTwinWire(t *testing.T, twin *tabletext.Model, values map[string]*big.Int) []byte {
	t.Helper()
	inst := twin.New(twin.Lookup("Root"))
	for name, v := range values {
		wideSetCell(t, inst, name, v)
	}
	wire, err := tablewire.Encode(twin, inst)
	if err != nil {
		t.Fatalf("the full-range twin refused the save: %v", err)
	}
	return wire
}

// requireWideOffRangeRefused holds an off-range value to the strict-only
// contract on both sides (docs/SPEC-TABLES.md §6.5; docs/rulings.json
// strict-only-read, refusal-verdict-only): the Go engine's writer refuses
// the value, its reader refuses the twin's wire, and the Lua save and the
// Lua load refuse them too. Only verdicts are compared, never reasons.
func requireWideOffRangeRefused(t *testing.T, model *tabletext.Model, inst *tabletext.Instance, twinWire []byte, out, module, saveValues string) {
	t.Helper()
	if _, err := tablewire.Encode(model, inst); err == nil {
		t.Fatal("the Go engine wrote a value off its declared range")
	}
	back := model.New(model.Lookup("Root"))
	var goReport tabletext.Report
	if ok, err := tablewire.Decode(model, back, twinWire, &goReport); err == nil && ok {
		t.Fatalf("the Go engine read a wire off its declared range: report=%+v", goReport)
	}
	probe := `local T = require("` + module + `")
local save = require("save")
local load = require("load")
local inputs = require("inputs")
local v = T.NewRoot()
` + saveValues + `
local wire = save.Save(T, "Root", v)
print(wire == nil and "save refused" or "save wrote")
local back = load.Load(T, "Root", inputs.wire, {})
print(back == nil and "load refused" or "load read")
`
	if err := os.WriteFile(filepath.Join(out, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := string(luarun.Run(t, out, "main.lua", map[string][]byte{"wire": twinWire})), "save refused\nload refused\n"; got != want {
		t.Errorf("Lua answered an off-range value the Go engine refuses\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func wide128Big(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("%q is not a decimal integer", s)
	}
	return v
}

// wide128Lanes splits a 128-bit value into its low and high 64-bit halves,
// two's complement for a negative value (docs/PORT-LUA.md, 64-BIT AND 128-BIT
// INTEGERS; docs/SPEC-TABLES.md §3 kind 18/19).
func wide128Lanes(v *big.Int) (lo, hi uint64) {
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	lo = new(big.Int).And(u, mask).Uint64()
	hi = new(big.Int).Rsh(u, 64).Uint64()
	return lo, hi
}

func wide128Cell(t *testing.T, inst *tabletext.Instance, name string) *big.Int {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return tabletext.WideValue(&inst.Fields[i].Cell)
		}
	}
	t.Fatalf("the root has no field %s", name)
	return nil
}

// TestLuaWide128BitVariableForm pins the variable form's int128 and uint128
// storage (docs/SPEC-TABLES.md §3 kinds 18/19; docs/PORT-LUA.md, THE TYPE
// MAPPING): { lo = <int>, hi = <int> }, low half first on the wire. Every
// field's Lua save is byte-identical with the Go reference engine's
// (internal/tablewire) and a Lua load of those Go bytes answers the same
// halves. A value off a declared range is refused by both writers, and the
// wire carrying it (made by the full-range twin) by both readers.
func TestLuaWide128BitVariableForm(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := filepath.Join(dir, "wide128.schema")
	if err := os.WriteFile(schema, []byte(wide128Schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	// the card keeps Generate's refusal; the internal path is the one this
	// test drives, exactly as docs/SPEC-TABLES.md §15 says a port that carries
	// the kinds does.
	files, err := generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out := t.TempDir()
	luarun.WriteFiles(t, out, files)

	model := tabletext.NewModel(u)
	root := model.Lookup("Root")
	twin := wideTwinModel(t, "wide128.schema", wide128TwinSchema)
	for _, tc := range wide128Cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := model.New(root)
			var saveValues []string
			values := map[string]*big.Int{}
			for _, name := range wide128Fields {
				v := wide128Big(t, tc.values[name])
				values[name] = v
				wideSetCell(t, inst, name, v)
				lo, hi := wide128Lanes(v)
				saveValues = append(saveValues, fmt.Sprintf("v.%s = { lo = 0x%016x, hi = 0x%016x }", name, lo, hi))
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
			for _, name := range wide128Fields {
				lo, hi := wide128Lanes(wide128Cell(t, back, name))
				fmt.Fprintf(&want, "%s %016x %016x\n", name, lo, hi)
			}
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
for _, name in ipairs({ "a", "b", "c", "d" }) do
  local x = back[name]
  print(string.format("%s %016x %016x", name, x.lo, x.hi))
end
print(string.format("clamped %d", report.outOfRange))
`
			if err := os.WriteFile(filepath.Join(out, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := string(luarun.Run(t, out, "main.lua", map[string][]byte{"wire": wire}))
			if got != want.String() {
				t.Errorf("Lua wide 128-bit value differs from the Go engine's wire and halves\n--- got ---\n%s--- want ---\n%s", got, want.String())
			}
		})
	}
}
