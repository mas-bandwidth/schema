package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// wideJSONSchema declares one field of every wide kind family the text form
// spells exactly (docs/SPEC-TABLES.md §16.2): the 128-bit integers and the
// fixed-point family at F = 0, 16 and 32. The declared ranges are the
// engine's clamp after the 128-bit saturation, so both events are compared.
const wideJSONSchema = `package widejson

table Root {
    i128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    u128 uint128
    f0   fixed(32, 0)   | min = -1000000, max = 1000000
    f16  fixed(16, 16)  | min = -1000, max = 1000
    f64  fixed(32, 32)  | min = -1000, max = 1000
    uf16 ufixed(16, 16) | min = 0, max = 1000
}
`

var wideJSONFields = []string{"i128", "u128", "f0", "f16", "f64", "uf16"}

// wideJSONRows are internal/tabletext/wide_test.go's rows, mapped onto the
// schema's fields by kind and F: the integral and exponent forms, the exact
// and the too-fine fraction, the negative zero and the negative unsigned,
// the 128-bit edge and the magnitudes past it. refused is the verdict the
// strict read gives (docs/SPEC-TABLES.md §6.5, §16.2): a too-fine fraction,
// a magnitude past the kind or the declared range, a negative unsigned, and
// a BARE number token for a kind whose storage is wider than 32 bits (the
// 128-bit integers and fixed(32, 32)), which reads as a STRING only. Each
// wide kind's value is carried again in its string spelling, where the
// string grammar's own refusals are pinned too.
var wideJSONRows = []struct {
	field   string
	token   string
	refused bool
}{
	{"f16", "1.0", false},
	{"f16", "-0.5", false},
	{"f16", "0.1", true},
	{"f16", "2", false},
	{"f16", "15e-1", false},
	{"f0", "1e3", false},
	{"f16", "-0.0", false},
	{"uf16", "-0", false},
	{"uf16", "-0.5", true},
	{"f16", "1e999999999", true},
	{"uf16", "-1e400", true},
	{"f16", "1e-999999999", true},
	{"f16", "0.0000152587890625", false},
	{"u128", "340282366920938463463374607431768211455", true},
	{"u128", "340282366920938463463374607431768211456", true},
	{"u128", "-1", true},
	{"i128", "1.5", true},
	{"u128", "3e38", true},
	{"i128", "-170141183460469231731687303715884105729", true},
	{"f64", "99.99999999976716935634613037109375", true},
	{"u128", `"340282366920938463463374607431768211455"`, false},
	{"u128", `"340282366920938463463374607431768211456"`, true},
	{"u128", `"-1"`, true},
	{"i128", `"1.5"`, true},
	{"u128", `"3e38"`, true},
	{"i128", `"-170141183460469231731687303715884105728"`, false},
	{"i128", `"-170141183460469231731687303715884105729"`, true},
	{"f64", `"99.99999999976716935634613037109375"`, false},
}

// TestLuaWideKindsJson pins the wide kinds in the JSON text form
// (docs/SPEC-TABLES.md §16.2: 128-bit integers are decimal integers and fixed
// point is whole units, exact in both directions and never through a double).
// For every row the strict read accepts, the Go reference engine reads the
// token and writes the instance; the Lua leg must read the same text to the
// same raw halves and write the same text, with the same verdict counters.
// For every row it refuses, the Go engine and the Lua leg both refuse.
func TestLuaWideKindsJson(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := filepath.Join(dir, "widejson.schema")
	if err := os.WriteFile(schema, []byte(wideJSONSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	// Generate keeps §15's refusal for a unit carrying the wide kinds; the
	// internal path is the one a backend that carries them uses.
	files, err := generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out := t.TempDir()
	luarun.WriteFiles(t, out, files)

	model := tabletext.NewModel(u)
	root := model.Lookup("Root")
	for _, row := range wideJSONRows {
		t.Run(row.field+" "+row.token, func(t *testing.T) {
			text := []byte(`{ "` + row.field + `": ` + row.token + ` }`)
			inst := model.New(root)
			var report tabletext.Report
			ok := model.Read(inst, text, &report)
			if row.refused {
				// verdict only, never the reason (docs/rulings.json
				// refusal-verdict-only): the oracle and the leg both refuse
				if ok {
					t.Fatalf("the Go engine read %s, which the strict read refuses", text)
				}
				requireLuaJSONRefused(t, out, ModuleName(u), text)
				return
			}
			if !ok {
				t.Fatalf("the Go engine refused %s: %+v", text, report)
			}
			wantText, err := model.Write(inst)
			if err != nil {
				t.Fatalf("the Go engine refused to write its own value: %v", err)
			}

			var want strings.Builder
			fmt.Fprintf(&want, "%d,%d,%d,%d,%d,%t\n", report.Unknown,
				0, report.Widened, 0,
				0, report.Refused)
			for _, name := range wideJSONFields {
				lo, hi := wide128Lanes(wide128Cell(t, inst, name))
				fmt.Fprintf(&want, "%s %016x %016x\n", name, lo, hi)
			}
			fmt.Fprintf(&want, "%s\n", hexBytes(append(wantText, '\n')))

			probe := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local inputs = require("inputs")
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
local v = J.FromJson(T, "Root", inputs.text, report)
print(string.format("%d,%d,%d,%d,%d,%s", report.unknown, report.kindChanged,
  report.widened, report.outOfRange, report.repeatedKey, tostring(report.damaged)))
local signed = { i128 = true, u128 = false, f0 = true, f16 = true, f64 = true, uf16 = false }
for _, name in ipairs({ ` + strings.Join(quoteAll(wideJSONFields), ", ") + ` }) do
  local x = v[name]
  local lo, hi
  if type(x) == "table" then
    lo, hi = x.lo, x.hi
  elseif signed[name] then
    lo, hi = x, (x < 0 and -1 or 0)
  else
    lo, hi = x, 0
  end
  print(string.format("%s %016x %016x", name, lo, hi))
end
local text = J.ToJson(T, "Root", v)
print((text:gsub(".", function(c) return string.format("%02x", c:byte()) end)))
`
			if err := os.WriteFile(filepath.Join(out, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := string(luarun.Run(t, out, "main.lua", map[string][]byte{"text": text}))
			if got != want.String() {
				t.Errorf("Lua wide text differs from the Go engine for %s\n--- got ---\n%s--- want ---\n%s", text, got, want.String())
			}
		})
	}
}

// requireLuaJSONRefused runs the Lua leg's plain JSON read (THE STRICT READ,
// docs/SPEC-TABLES.md §6.5) over text and requires it to refuse: no value.
func requireLuaJSONRefused(t *testing.T, out, module string, text []byte) {
	t.Helper()
	probe := `local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local v = J.FromJson(T, "Root", inputs.text, {})
print(v == nil and "refused" or "read")
`
	if err := os.WriteFile(filepath.Join(out, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(luarun.Run(t, out, "main.lua", map[string][]byte{"text": text}))); got != "refused" {
		t.Errorf("the Lua JSON read answered %q on %s, which the Go engine refuses; want refused", got, text)
	}
}

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = fmt.Sprintf("%q", name)
	}
	return out
}
