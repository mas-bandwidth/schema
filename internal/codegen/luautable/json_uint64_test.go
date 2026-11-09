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

// A 64-BIT INTEGER TOKEN LANDS EXACTLY IN ITS DOMAIN (docs/SPEC-TABLES.md
// §16.2: an integral token in range is that integer; a magnitude past the
// domain saturates and counts clamped). The Luau text read holds a uint64 at
// or past 2^63 as a negative 64-bit integer, so every bound it is checked
// against is compared unsigned; a token past the field's domain lands at the
// end its sign names, counted as the engine counts it. Each row's wire and clamped count are the Go engine's
// (internal/tabletext, internal/tablewire) over the same unit. The reversed
// witness puts the signed compare back and holds that it is red on exactly
// the uint64 tokens at and past 2^63.
const jsonInt64Schema = `package p

table Root
{
    u uint64
    s int64
    w uint32
}
`

var jsonInt64Texts = []string{
	`{"u": 9223372036854775807}`,
	`{"u": 9223372036854775808}`,
	`{"u": 18446744073709551615}`,
	`{"u": 18446744073709551616}`,
	`{"u": -1}`,
	`{"s": 9223372036854775807, "w": 4294967295}`,
	`{"s": 9223372036854775808}`,
	`{"s": -9223372036854775808}`,
	`{"s": -9223372036854775809}`,
	`{"s": 99999999999999999999999}`,
	`{"w": 4294967296}`,
	`{"w": 18446744073709551615}`,
	`{"w": -99999999999999999999999}`,
	`{"s": -99999999999999999999999}`,
}

const jsonInt64Probe = `local json = require("./schema_json")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
for _, text in ipairs(string.split(buffer.tostring(inputs.texts), "\n")) do
	local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.fromJsonWalk("Root", buffer.fromstring(text), r)
	local b = save.save(St.lookup("Root"), v)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	print(tostring(r.clamped) .. " " .. table.concat(parts))
end
`

func jsonInt64Run(t *testing.T, g *generatedUnit) []string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonModules(g)+jsonInt64Probe), 0o644); err != nil {
		t.Fatal(err)
	}
	texts := []byte(strings.Join(jsonInt64Texts, "\n"))
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"texts": texts}))
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

func jsonInt64Oracle(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Int64.schema")
	if err := os.WriteFile(path, []byte(jsonInt64Schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	var want []string
	clampedOf := map[string]string{
		`{"u": 18446744073709551616}`:     `{"u": 18446744073709551615}`,
		`{"u": -1}`:                       `{"u": 0}`,
		`{"s": 9223372036854775808}`:      `{"s": 9223372036854775807}`,
		`{"s": -9223372036854775809}`:     `{"s": -9223372036854775808}`,
		`{"s": 99999999999999999999999}`:  `{"s": 9223372036854775807}`,
		`{"w": 4294967296}`:               `{"w": 4294967295}`,
		`{"w": 18446744073709551615}`:     `{"w": 4294967295}`,
		`{"w": -99999999999999999999999}`: `{"w": 0}`,
		`{"s": -99999999999999999999999}`: `{"s": -9223372036854775808}`,
	}
	for _, text := range jsonInt64Texts {
		inst := model.New(model.Lookup("Root"))
		var rep tabletext.Report
		clamped := 0
		if !model.Read(inst, []byte(text), &rep) {
			alt, ok := clampedOf[text]
			if !ok || !strings.HasPrefix(rep.Reason, "out_of_range:") {
				t.Fatalf("%s: the Go reference refused the text (%+v)", text, rep)
			}
			clamped = 1
			if strings.Contains(text, "99999999999999999999999") {
				clamped = 2
			}
			var rep2 tabletext.Report
			if !model.Read(inst, []byte(alt), &rep2) {
				t.Fatalf("%s -> %s: the Go reference refused the clamped text (%+v)", text, alt, rep2)
			}
		}
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: the Go reference wire: %v", text, err)
		}
		want = append(want, strconv.Itoa(clamped)+" "+hexBytes(wire))
	}
	return want
}

func TestLuauJsonInt64DomainIsExact(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"Int64.schema": jsonInt64Schema})
	got := jsonInt64Run(t, g)
	want := jsonInt64Oracle(t)
	if len(got) != len(want) {
		t.Fatalf("the probe answered %d lines for %d texts: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: Luau lands %s, the Go engine %s", jsonInt64Texts[i], got[i], want[i])
		}
	}
}

func TestLuauJsonUint64SignedCompareNegativeControl(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"Int64.schema": jsonInt64Schema})
	path := filepath.Join(g.dir, "schema_json.luau")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "  if moved then report.clamped = report.clamped + 1 end\n"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the plant's anchor is not once in schema_json.luau: %q", anchor)
	}
	planted := strings.Replace(string(src), anchor,
		"  if unsigned and integer.lt(v, 0i) then v = 0i; moved = true end\n"+anchor, 1)
	if err := os.WriteFile(path, []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	got := jsonInt64Run(t, g)
	want := jsonInt64Oracle(t)
	var red []string
	for i := range want {
		if i < len(got) && got[i] != want[i] {
			red = append(red, jsonInt64Texts[i])
		}
	}
	wantRed := []string{jsonInt64Texts[1], jsonInt64Texts[2], jsonInt64Texts[3]}
	if strings.Join(red, " | ") != strings.Join(wantRed, " | ") {
		t.Fatalf("the signed compare is red on %q, want exactly %q", red, wantRed)
	}
}
