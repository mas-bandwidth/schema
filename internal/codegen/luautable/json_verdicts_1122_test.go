package luautable_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The JSON read's three verdicts the 2026-11-22 review found Luau holding
// differently from Go (docs/SPEC-TABLES.md §2.15, §16.2 enum row, §16.7):
//
//   - a deprecated variant reads as the field's declared default, an element
//     as the element's default, never counted unknown; ToJson refuses a value
//     that holds one (invalid_value), as Save does;
//   - a `&node` label is read EXACTLY (Go reads a uint64): two labels past
//     2^53 are two labels, not one;
//
// (A trailing comma after a reference reads in Go and in Luau alike, §16.2.)
//
// Every read row is compared with the Go engine's own verdict and wire over the
// same unit; each fix has a planted old line that goes red.
const jsonVerdictsSchema = `package p

enum Ship
{
    Fighter
    Bomber | deprecated
    Scout
}

table Node
{
    v int32
}

table Root
{
    ship Ship = Scout
    fleet [..2]Ship
    head *Node
    tail *Node
}
`

var jsonVerdictsTexts = []string{
	// the deprecated variant: field default, element default, no unknown
	`{"ship":"Bomber"}`,
	`{"ship":"Bomber","fleet":["Bomber","Fighter"]}`,
	`{"ship":"Fighter","fleet":["Scout","Bomber"]}`,
	`{"ship":"Nonesuch"}`,
	// the labels: a plain share, then two labels past 2^53 that a double folds
	`{"head":{"&node":1,"v":5},"tail":{"&node":1}}`,
	`{"head":{"&node":9007199254740993,"v":1},"tail":{"&node":9007199254740992,"v":2}}`,
	`{"head":{"&node":9007199254740993,"v":1},"tail":{"&node":9007199254740992}}`,
	`{"head":{"&node":18446744073709551615,"v":1},"tail":{"&node":18446744073709551615}}`,
	// a trailing comma after a reference is read, as Go reads it (§16.2)
	`{"head":{"&node":1,"v":5},"tail":{"&node":1,}}`,
}

const jsonVerdictsProbe = `local json = require("./schema_json")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
for _, text in ipairs(string.split(buffer.tostring(inputs.texts), "\n")) do
	local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson("Root", buffer.fromstring(text), r)
	if v == nil or r.malformed then
		print("refused")
	else
		local b = save.save(St.lookup("Root"), v)
		print(tostring(r.unknown) .. " " .. (if b == nil then "nil" else hex(b)))
	end
end
local d = St.lookup("Root")
for _, ship in { 2, 3 } do
	local v = d.new()
	v.Ship = ship
	local rep: any = {}
	local out = json.ToJson("Root", v, rep)
	print("tojson " .. ship .. " " .. tostring(out ~= nil) .. " " .. tostring(rep.reason))
end
`

func jsonVerdictsRun(t *testing.T, plant func(src string) string) []string {
	t.Helper()
	g := generateFiles(t, map[string]string{"Verdicts.schema": jsonVerdictsSchema})
	if plant != nil {
		path := filepath.Join(g.dir, "schema_json.luau")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(plant(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonModules(g)+jsonVerdictsProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	texts := []byte(strings.Join(jsonVerdictsTexts, "\n"))
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"texts": texts}))
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

func jsonVerdictsOracle(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Verdicts.schema")
	if err := os.WriteFile(path, []byte(jsonVerdictsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	var want []string
	for _, text := range jsonVerdictsTexts {
		inst := model.New(model.Lookup("Root"))
		var rep tabletext.Report
		if !model.Read(inst, []byte(text), &rep) {
			want = append(want, "refused")
			continue
		}
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: the Go reference wire: %v", text, err)
		}
		want = append(want, strconv.Itoa(rep.Unknown)+" "+hexBytes(wire))
	}
	return want
}

// jsonVerdictsRed lists the rows a planted runtime answers differently from
// the Go engine (read rows) or from the spec (the two ToJson rows).
func jsonVerdictsRed(t *testing.T, plant func(string) string) []string {
	t.Helper()
	got := jsonVerdictsRun(t, plant)
	want := jsonVerdictsOracle(t)
	want = append(want, "tojson 2 false invalid_value", "tojson 3 true nil")
	if len(got) != len(want) {
		t.Fatalf("the probe answered %d lines for %d rows: %q", len(got), len(want), got)
	}
	var red []string
	for i := range want {
		if got[i] != want[i] {
			row := want[i]
			if i < len(jsonVerdictsTexts) {
				row = jsonVerdictsTexts[i]
			}
			red = append(red, row+" => "+got[i])
		}
	}
	return red
}

func jsonVerdictsPlant(t *testing.T, anchor, old string) func(string) string {
	return func(src string) string {
		t.Helper()
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the plant's anchor is not once in schema_json.luau: %q", anchor)
		}
		return strings.Replace(src, anchor, old, 1)
	}
}

func TestLuauJsonVerdictsMatchGo(t *testing.T) {
	t.Parallel()
	if red := jsonVerdictsRed(t, nil); len(red) != 0 {
		t.Fatalf("rows that differ from Go: %q", red)
	}
}

// The planted old lines, one per fix: each must turn a row red and the clean
// runtime's other rows stay green, so a row is red for its own line only.
func TestLuauJsonVerdictsPlantedOldLinesAreRed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		plant   func(string) string
		reddens string
	}{
		{"deprecated read", jsonVerdictsPlant(t, "        if v[3] == true then\n          -- A DEPRECATED VARIANT READS", "        if false then\n          -- A DEPRECATED VARIANT READS"), `{"ship":"Bomber"}`},
		{"deprecated write", jsonVerdictsPlant(t, "        if v[3] == true then refuse_reason = \"invalid_value\"; return false end\n", ""), "tojson 2"},
		{"label in a double", jsonVerdictsPlant(t, "  return digits\nend\n\nlocal skip_value", "  local v = 0\n  for i = 1, #digits do v = v * 10 + (digits:byte(i) - 48) end\n  return v\nend\n\nlocal skip_value"), `"&node":9007199254740993`},
	}
	for _, c := range cases {
		red := jsonVerdictsRed(t, c.plant)
		if len(red) == 0 {
			t.Errorf("%s: the planted old line is not red", c.name)
			continue
		}
		if !strings.Contains(strings.Join(red, "\n"), c.reddens) {
			t.Errorf("%s: red on %q, want it to include %s", c.name, red, c.reddens)
		}
	}
}
