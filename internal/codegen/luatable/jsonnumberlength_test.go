package luatable

// A JSON NUMBER IS READ BY ITS VALUE, HOWEVER MANY BYTES IT SPANS
// (docs/SPEC-TABLES.md §16.2 Numbers): a token's length is never why a read
// refuses, placed or skipped under an unknown key. The document's own size is
// the only bound. Each row's verdict and value are held to the compiler's own
// engine (internal/tabletext) on the same text.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

type jsonNumberLengthRow struct {
	name       string
	text       string
	wantOK     bool
	experience uint64
	precision  float64
	unknown    int
}

func jsonNumberLengthRows() []jsonNumberLengthRow {
	zeros := func(n int) string { return strings.Repeat("0", n) }
	return []jsonNumberLengthRow{
		{"experience_511_bytes", `{"experience":1.` + zeros(509) + `}`, true, 1, 0, 0},
		{"experience_512_bytes", `{"experience":1.` + zeros(510) + `}`, true, 1, 0, 0},
		{"experience_513_bytes", `{"experience":1.` + zeros(511) + `}`, true, 1, 0, 0},
		{"experience_602_bytes", `{"experience":1.` + zeros(600) + `}`, true, 1, 0, 0},
		{"precision_602_bytes", `{"precision":1.` + zeros(600) + `}`, true, 0, 1.0, 0},
		{"unknown_5000_digits", `{"not_a_field":1` + zeros(4999) + `}`, true, 0, 0, 1},
		{"experience_601_digits", `{"experience":1` + zeros(600) + `}`, false, 0, 0, 0},
	}
}

const jsonNumberLengthProbe = `local T = require("tables")
local J = require("json")
local inputs = require("inputs")
for i = 1, %d do
  local key = "row" .. i
  local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
  local v = J.FromJson(T, "ProfileConfig", inputs[key], report)
  if v == nil then
    print(key .. "|refused")
  else
    print(key .. "|ok|" .. string.format("%%d", v.experience) .. "|" .. string.format("%%.17g", v.precision) .. "|" .. report.unknown)
  end
end
`

// TestLuaJsonNumberAnyLength reads, over tables/examples/Tables.schema root
// ProfileConfig, numbers whose tokens run past 511 bytes: each lands its value
// exactly as the engine lands it, a long token under an unknown key is
// skipped and counted, and a long integer whose value is out of range still
// refuses, by its value.
func TestLuaJsonNumberAnyLength(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "examples", "Tables.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Tables.schema: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	rows := jsonNumberLengthRows()
	inputs := map[string][]byte{}
	for i, row := range rows {
		inputs[fmt.Sprintf("row%d", i+1)] = []byte(row.text)
	}
	probe := strings.Replace(fmt.Sprintf(jsonNumberLengthProbe, len(rows)), `require("tables")`, `require("`+ModuleName(u)+`")`, 1)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "|")
		got[k] = v
	}

	m := tabletext.NewModel(u)
	st := m.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("no table ProfileConfig")
	}
	for i, row := range rows {
		lua := got[fmt.Sprintf("row%d", i+1)]
		inst := m.New(st)
		var r tabletext.Report
		engineOK := m.Read(inst, []byte(row.text), &r) && !r.Refused
		if engineOK != row.wantOK {
			t.Fatalf("%s: the engine's verdict is ok=%v, want ok=%v (%+v)", row.name, engineOK, row.wantOK, r)
		}
		if !row.wantOK {
			if lua != "refused" {
				t.Errorf("%s: Lua answered %q, the engine refused", row.name, lua)
			}
			continue
		}
		exp, ok := inst.FieldByKey("experience")
		if !ok {
			t.Fatal("no field experience")
		}
		prec, ok := inst.FieldByKey("precision")
		if !ok {
			t.Fatal("no field precision")
		}
		if exp.Cell.U != row.experience || prec.Cell.F != row.precision || r.Unknown != row.unknown {
			t.Fatalf("%s: the engine landed experience=%d precision=%v unknown=%d, want %d %v %d",
				row.name, exp.Cell.U, prec.Cell.F, r.Unknown, row.experience, row.precision, row.unknown)
		}
		parts := strings.Split(lua, "|")
		if len(parts) != 4 || parts[0] != "ok" {
			t.Errorf("%s: Lua answered %q, the engine read experience=%d precision=%v unknown=%d",
				row.name, lua, exp.Cell.U, prec.Cell.F, r.Unknown)
			continue
		}
		luaExp, err1 := strconv.ParseUint(parts[1], 10, 64)
		luaPrec, err2 := strconv.ParseFloat(parts[2], 64)
		luaUnknown, err3 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil || err3 != nil {
			t.Errorf("%s: Lua printed %q", row.name, lua)
			continue
		}
		if luaExp != exp.Cell.U || luaPrec != prec.Cell.F || luaUnknown != r.Unknown {
			t.Errorf("%s: Lua landed experience=%d precision=%v unknown=%d, the engine %d %v %d",
				row.name, luaExp, luaPrec, luaUnknown, exp.Cell.U, prec.Cell.F, r.Unknown)
		}
	}
}
