package luatable

import (
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
	name   string
	values map[string]string
}{
	{"zero", map[string]string{"a": "0", "b": "0", "c": "0", "d": "0"}},
	{"negative one", map[string]string{"a": "-1", "b": "-1", "c": "1", "d": "1"}},
	{"min", map[string]string{
		"a": "-170141183460469231731687303715884105728",
		"b": "-1267650600228229401496703205376",
		"c": "0",
		"d": "0",
	}},
	{"max", map[string]string{
		"a": "170141183460469231731687303715884105727",
		"b": "1267650600228229401496703205376",
		"c": "340282366920938463463374607431768211455",
		"d": "18446744073709551616",
	}},
	{"two to the sixty-four", map[string]string{
		"a": "18446744073709551616",
		"b": "18446744073709551616",
		"c": "18446744073709551616",
		"d": "18446744073709551616",
	}},
	// the reader clamps b to its declared bounds; the writer leaves
	// the halves as they are, as the Go engine's does
	{"clamped", map[string]string{
		"a": "0",
		"b": "170141183460469231731687303715884105727",
		"c": "340282366920938463463374607431768211455",
		"d": "340282366920938463463374607431768211455",
	}},
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
// halves, including the declared range's clamp on a load.
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
	for _, tc := range wide128Cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := model.New(root)
			var saveValues []string
			for _, name := range wide128Fields {
				v := wide128Big(t, tc.values[name])
				for i := range inst.Fields {
					if inst.Fields[i].Def.Name == name {
						inst.Fields[i].Cell.Wide = v
					}
				}
				lo, hi := wide128Lanes(v)
				saveValues = append(saveValues, fmt.Sprintf("v.%s = { lo = 0x%016x, hi = 0x%016x }", name, lo, hi))
			}
			wire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("the Go engine refused the save: %v", err)
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
			fmt.Fprintf(&want, "clamped %d\n", goReport.Clamped)

			probe := `local T = require("` + ModuleName(u) + `")
local save = require("save")
local load = require("load")
local inputs = require("inputs")
local v = T.NewRoot()
` + strings.Join(saveValues, "\n") + `
local wire = save.Save(T, "Root", v)
print((wire:gsub(".", function(c) return string.format("%02x", c:byte()) end)))
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local back = load.LoadTolerant(T, "Root", inputs.wire, report)
for _, name in ipairs({ "a", "b", "c", "d" }) do
  local x = back[name]
  print(string.format("%s %016x %016x", name, x.lo, x.hi))
end
print(string.format("clamped %d", report.clamped))
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
