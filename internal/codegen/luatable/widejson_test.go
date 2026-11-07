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
// the 128-bit edge and the magnitudes past it.
var wideJSONRows = []struct {
	field string
	token string
}{
	{"f16", "1.0"},
	{"f16", "-0.5"},
	{"f16", "0.1"},
	{"f16", "2"},
	{"f16", "15e-1"},
	{"f0", "1e3"},
	{"f16", "-0.0"},
	{"uf16", "-0"},
	{"uf16", "-0.5"},
	{"f16", "1e999999999"},
	{"uf16", "-1e400"},
	{"f16", "1e-999999999"},
	{"f16", "0.0000152587890625"},
	{"u128", "340282366920938463463374607431768211455"},
	{"u128", "340282366920938463463374607431768211456"},
	{"u128", "-1"},
	{"i128", "1.5"},
	{"u128", "3e38"},
	{"i128", "-170141183460469231731687303715884105729"},
	{"f64", "99.99999999976716935634613037109375"},
}

// TestLuaWideKindsJson pins the wide kinds in the JSON text form
// (docs/SPEC-TABLES.md §16.2: 128-bit integers are decimal integers and fixed
// point is whole units, exact in both directions and never through a double).
// For every row the Go reference engine reads the token and writes the
// instance; the Lua leg must read the same text to the same raw halves and
// write the same text, with the same verdict counters.
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
			if ok := model.Read(inst, text, &report); !ok {
				t.Fatalf("the Go engine refused %s: %+v", text, report)
			}
			wantText, err := model.Write(inst)
			if err != nil {
				t.Fatalf("the Go engine refused to write its own value: %v", err)
			}

			var want strings.Builder
			fmt.Fprintf(&want, "%d,%d,%d,%d,%d,%t\n", report.Unknown,
				report.KindMismatch, report.Widened, report.Clamped,
				report.Duplicate, report.Malformed)
			for _, name := range wideJSONFields {
				lo, hi := wide128Lanes(wide128Cell(t, inst, name))
				fmt.Fprintf(&want, "%s %016x %016x\n", name, lo, hi)
			}
			fmt.Fprintf(&want, "%s\n", hexBytes(append(wantText, '\n')))

			probe := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = J.FromJsonTolerant(T, "Root", inputs.text, report)
print(string.format("%d,%d,%d,%d,%d,%s", report.unknown, report.kindMismatch,
  report.widened, report.clamped, report.duplicate, tostring(report.malformed)))
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

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = fmt.Sprintf("%q", name)
	}
	return out
}
