package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// listTotalCases are the four spellings of compiler/listtotalbytes_refusal_test.go,
// each beside a Wide table that declares no total so an over-total wire can be
// written (docs/SPEC-TABLES.md §2.13).
var listTotalCases = []struct {
	name, field string
	optional    bool
	list        bool
	schema      string
}{
	{"bounded", "names", false, false, `package probe

table L
{
    names [..8]string(8) | total = 12
}

table Wide
{
    names [..8]string(8)
}
`},
	{"bounded ?", "maybe", true, false, `package probe

table L
{
    maybe [..8]?string(8) | total = 12
}

table Wide
{
    maybe [..8]?string(8)
}
`},
	{"unbounded", "repos", false, true, `package probe

table L
{
    repos []string(8) | total = 12
}

table Wide
{
    repos []string(8)
}
`},
	{"unbounded ?", "bases", true, true, `package probe

table L
{
    bases []?string(8) | total = 12
}

table Wide
{
    bases []?string(8)
}
`},
}

// listTotalOracleSchema is the Go engine's oracle unit: all four spellings in
// one L, with a Wide beside them. The field ids depend on the field name
// (docs/SPEC-TABLES.md §5), so a wire from a single-field Luau unit loads here.
const listTotalOracleSchema = `package probe

table L
{
    names [..8]string(8) | total = 12
    maybe [..8]?string(8) | total = 12
    repos []string(8) | total = 12
    bases []?string(8) | total = 12
}

table Wide
{
    names [..8]string(8)
    maybe [..8]?string(8)
    repos []string(8)
    bases []?string(8)
}
`

// listTotalWire writes one of three wires from the Wide table, which declares
// no total: two elements (under 12), three (at 12) and four (over 12).
func listTotalWire(t *testing.T, m *tabletext.Model, field string, elems int) []byte {
	t.Helper()
	parts := make([]string, elems)
	for i := range parts {
		parts[i] = `"abcd"`
	}
	text := fmt.Sprintf(`{"%s":[%s]}`, field, strings.Join(parts, ","))
	inst := m.New(m.Lookup("Wide"))
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) || !report.Silent() {
		t.Fatalf("%s: the oracle text did not place: %+v", text, report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("%s: the oracle refuses the value: %v", text, err)
	}
	return wire
}

// listTotalProbe loads each wire under L, prints the kept count, the kept
// bytes and the clamped counter, then loads the at-total wire, lengthens its
// first element by one byte and saves: an over-total list must refuse.
const listTotalProbe = `local inputs = require("./inputs")
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
require("./ProbeTable")

local FIELD = buffer.tostring(inputs.field)
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local d = St.lookup("L")
local fd
for _, f in d.fields do
	if f.name == FIELD then fd = f end
end
if fd == nil then print("NOFIELD"); return end

local function measure(v)
	local val = v[fd.slot]
	local n = fd.array == "list" and #val or (v[fd.slot .. "Count"] or 0)
	local count, sum = 0, 0
	for i = 1, n do
		local x = val[i]
		local b, used = x, nil
		if fd.e ~= nil and fd.e.s == "tab" then
			local vf = St.lookup(fd.e.t).fields[1]
			if vf.present ~= nil and not x[vf.present] then b = nil else b = x[vf.slot]; used = x[vf.slot .. "Length"] end
		end
		if b ~= nil then count = count + 1; sum = sum + (used or buffer.len(b)) end
	end
	return count, sum
end

for _, name in { "under", "at", "over" } do
	local r = fresh()
	local v = load.loadTolerant("L", inputs[name], r)
	if v == nil or r.refused > 0 then print(name .. " REFUSED"); return end
	local count, sum = measure(v)
	print(string.format("%d %d %d", count, sum, r.clamped))
end

local r = fresh()
local v = load.loadTolerant("L", inputs.at, r)
local val = v[fd.slot]
local x = val[1]
local b = x
local vf = nil
if fd.e ~= nil and fd.e.s == "tab" then
	vf = St.lookup(fd.e.t).fields[1]
	b = x[vf.slot]
end
local nb = buffer.create(buffer.len(b) + 1)
buffer.copy(nb, 0, b, 0, buffer.len(b))
buffer.writeu8(nb, buffer.len(b), 120)
if vf ~= nil then x[vf.slot] = nb; x[vf.slot .. "Length"] = buffer.len(nb); if vf.present ~= nil then x[vf.present] = true end else val[1] = nb end
local out = save.save(d, v)
print(out == nil and "OVERSAVE REFUSED" or "OVERSAVE TAKEN")
`

// goTotalOracle is the expected answer, from schema-go's own generated Go
// engine (internal/codegen/gotable). It loads the three wires under L, prints
// the kept count, kept bytes and clamped counter, then loads the at-total wire,
// lengthens its first element by one byte and measures: an over-total list
// must be refused.
const goTotalOracle = `package probe

import (
	"encoding/hex"
	"fmt"
	"os"
	"testing"
)

func TestOracle(t *testing.T) {
	field := os.Getenv("FIELD")
	load := func(name string) *L {
		raw, err := hex.DecodeString(os.Getenv(name))
		if err != nil {
			t.Fatal(err)
		}
		var r TableReport
		v := LLoadTolerant(make([]byte, 1<<16), raw, &r)
		if v == nil {
			t.Fatalf("%s: load refused %+v", name, r)
		}
		var count int32
		var sum int64
		switch field {
		case "names":
			count = v.NamesCount
			for i := int32(0); i < count; i++ {
				if p, ok := LNamesTextAt(v, i); ok {
					sum += int64(len(p))
				}
			}
		case "maybe":
			count = v.MaybeCount
			for i := int32(0); i < count; i++ {
				if p, ok := LMaybeTextAt(v, i); ok {
					sum += int64(len(p))
				}
			}
		case "repos":
			count = v.Repos.Count
			for i := int32(0); i < count; i++ {
				if p, ok := LReposTextAt(v, i); ok {
					sum += int64(len(p))
				}
			}
		case "bases":
			count = v.Bases.Count
			for i := int32(0); i < count; i++ {
				if p, ok := LBasesTextAt(v, i); ok {
					sum += int64(len(p))
				}
			}
		}
		fmt.Printf("%d %d %d\n", count, sum, r.Clamped)
		return v
	}
	load("UNDER")
	load("AT")
	load("OVER")
	v := load("AT")
	switch field {
	case "names":
		v.Names[0].ValueLength = 5
		copy(v.Names[0].Value[:], "abcde")
	case "maybe":
		v.Maybe[0].ValuePresent = true
		v.Maybe[0].ValueLength = 5
		copy(v.Maybe[0].Value[:], "abcde")
	case "repos":
		p := v.Repos.At(0)
		p.ValueLength = 5
		copy(p.Value[:], "abcde")
	case "bases":
		p := v.Bases.At(0)
		p.ValuePresent = true
		p.ValueLength = 5
		copy(p.Value[:], "abcde")
	}
	if LMeasure(v) < 0 {
		fmt.Println("OVERSAVE REFUSED")
	} else {
		fmt.Println("OVERSAVE TAKEN")
	}
}
`

// goTotalOracleOutput runs the generated Go engine on the three wires and
// returns its six lines.
func goTotalOracleOutput(t *testing.T, field string, wires [3][]byte) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Oracle.schema")
	if err := os.WriteFile(path, []byte(listTotalOracleSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the oracle unit does not load: %v", err)
	}
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", filepath.Join(root, "serialize.go")))
	files["oracle_test.go"] = []byte(goTotalOracle)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-v", "-run", "TestOracle", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "NOVA_TEST_NO_HOST=1",
		"FIELD="+field,
		"UNDER="+hex.EncodeToString(wires[0]),
		"AT="+hex.EncodeToString(wires[1]),
		"OVER="+hex.EncodeToString(wires[2]))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Go oracle failed: %v\n%s", err, out)
	}
	var lines []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "OVERSAVE REFUSED" || line == "OVERSAVE TAKEN" || isThreeNumbers(line) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 5 {
		t.Fatalf("the Go oracle printed %d answer lines, want 5: %q", len(lines), out)
	}
	return lines
}

// isThreeNumbers reports the three decimal fields the oracle prints for one
// wire: kept count, kept bytes and the clamped counter.
func isThreeNumbers(line string) bool {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return false
	}
	for _, f := range fields {
		if f == "" {
			return false
		}
		for _, c := range f {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// runListTotalProbe runs the emitted unit over the three wires and returns the
// three load lines and the over-total save verdict.
func runListTotalProbe(t *testing.T, g *generatedUnit, field string, wires [3][]byte) ([]string, string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(listTotalProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{
		"field": []byte(field),
		"under": wires[0],
		"at":    wires[1],
		"over":  wires[2],
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("unexpected probe output %q", out)
	}
	return lines[:3], lines[3]
}

// TestLuauTextListTotalBytesBound holds docs/SPEC-TABLES.md §2.13's total on a
// text list in the Luau runtime: a list under, at and over the total gives the
// Go load's kept count, kept bytes and clamped counter, and an over-total save
// refuses as the Go writer refuses it. Every expected line comes from
// schema-go's generated Go engine over the same wires, never typed by hand.
func TestLuauTextListTotalBytesBound(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	for _, tc := range listTotalCases {
		t.Run(tc.name, func(t *testing.T) {
			g := generateFiles(t, map[string]string{"Probe.schema": tc.schema})
			path := filepath.Join(t.TempDir(), "Probe.schema")
			if err := os.WriteFile(path, []byte(tc.schema), 0o644); err != nil {
				t.Fatal(err)
			}
			paths, err := compiler.GatherPaths([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load(paths)
			if err != nil {
				t.Fatalf("the unit does not load: %v", err)
			}
			m := tabletext.NewModel(u)
			wires := [3][]byte{
				listTotalWire(t, m, tc.field, 2),
				listTotalWire(t, m, tc.field, 3),
				listTotalWire(t, m, tc.field, 4),
			}
			want := goTotalOracleOutput(t, tc.field, wires)
			got, save := runListTotalProbe(t, g, tc.field, wires)
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("load %d = %q, want Go's %q", i, got[i], want[i])
				}
			}
			if save != want[4] {
				t.Errorf("over-total save = %q, want Go's %q", save, want[4])
			}
			if save != "OVERSAVE REFUSED" {
				t.Errorf("the over-total save was not refused")
			}
		})
	}
}
