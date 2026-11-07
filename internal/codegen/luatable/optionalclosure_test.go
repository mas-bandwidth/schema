package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuaOptionalVariableClosure holds docs/SPEC-TABLES.md §2.3's rule for
// `?T` over a variable closure in the Lua table runtime: every walk that
// follows pointer edges visits T's edges only when the presence companion is
// set, and an absent value owns no node.
//
// It transcribes the scalar case of gotable's TestGoOptionalVariableClosure-
// Table: the Go tablewire engine supplies the absent and present wires, each
// wire's region measure (FileRegionMeasure), and the JSON text, so no Lua walk
// certifies itself. The unit's Inner holds a pointer (child *Leaf) and both
// blob kinds (text *string, data *bytes); a present value then cleared keeps
// its storage but must save, measure and write JSON exactly as the absent one,
// because save's numbering, the JSON graph and load's resolve gate on presence.
func TestLuaOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	const src = `package probe

table Leaf
{
    n int32
}

table Inner
{
    child *Leaf
    text *string
    data *bytes
    n int32
}

table Outer
{
    inner ?Inner
    tail int32
}
`
	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schemaPath)
	root := u.Tables["Outer"]
	if root == nil {
		t.Fatal("the unit declares no table Outer")
	}
	model := tabletext.NewModel(u)
	outer := model.Lookup("Outer")
	if outer == nil {
		t.Fatal("the Go oracle has no Outer")
	}

	instance := func(name, text string) *tabletext.Instance {
		t.Helper()
		inst := model.New(outer)
		var report tabletext.Report
		if !model.Read(inst, []byte(text), &report) || !report.Silent() {
			t.Fatalf("%s: Go text read: %+v", name, report)
		}
		return inst
	}
	encode := func(name string, inst *tabletext.Instance) []byte {
		t.Helper()
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: Go wire encode: %v", name, err)
		}
		return wire
	}
	jsonText := func(name string, inst *tabletext.Instance) []byte {
		t.Helper()
		text, err := model.Write(inst)
		if err != nil {
			t.Fatalf("%s: Go JSON write: %v", name, err)
		}
		return text
	}

	const absentText = `{"tail":5}`
	const presentText = `{"inner":{"child":{"n":7},"text":"hi","data":"AQI="},"tail":5}`

	absent := instance("absent", absentText)
	present := instance("present", presentText)
	stale := instance("stale", presentText)
	inner, ok := stale.FieldByKey("inner")
	if !ok || !inner.Present || inner.Cell.Tab == nil {
		t.Fatal("the Go oracle lacks the present pointer before clear")
	}
	inner.Present = false

	wantAbsent := encode("absent", absent)
	wantPresent := encode("present", present)
	wantStale := encode("stale", stale)
	if string(wantStale) != string(wantAbsent) {
		t.Fatalf("the Go oracle did not elide a cleared value's stale pointer\n got %x\nwant %x", wantStale, wantAbsent)
	}
	if string(wantPresent) == string(wantAbsent) {
		t.Fatal("the Go oracle did not distinguish absent from present")
	}
	wantAbsentJSON := jsonText("absent", absent)
	wantPresentJSON := jsonText("present", present)
	wantStaleJSON := jsonText("stale", stale)
	if string(wantStaleJSON) != string(wantAbsentJSON) {
		t.Fatalf("the Go oracle wrote a cleared value's stale JSON\n got %s\nwant %s", wantStaleJSON, wantAbsentJSON)
	}

	measure := func(name string, wire []byte) int64 {
		t.Helper()
		n, whole, refused := tablewire.FileRegionMeasure(u, root, wire)
		if !whole || refused {
			t.Fatalf("%s: Go FileRegionMeasure whole=%v refused=%v", name, whole, refused)
		}
		return n
	}
	measureAbsent := measure("absent", wantAbsent)
	measurePresent := measure("present", wantPresent)
	if measurePresent == measureAbsent {
		t.Fatal("the Go oracle's present region measure equals the absent one")
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Lua refused ?T over a variable closure (docs/SPEC-TABLES.md §2.3): %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := fmt.Sprintf(`local T = require(%q)
local load = require("load")
local save = require("save")
local json = require("json")
local inputs = require("inputs")
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function hex(s)
  if s == nil then return "nil" end
  return (s:gsub(".", function(c) return string.format("%%02x", c:byte()) end))
end
local a = load.LoadTolerant(T, "Outer", inputs.absent, report())
print("measure_absent=" .. tostring(load.LoadMeasure(T, "Outer", inputs.absent)))
print("json_absent=" .. hex(json.ToJson(T, "Outer", a)))
local p = load.LoadTolerant(T, "Outer", inputs.present, report())
print("measure_present=" .. tostring(load.LoadMeasure(T, "Outer", inputs.present)))
print("json_present=" .. hex(json.ToJson(T, "Outer", p)))
p.inner_present = false
assert(p.inner ~= nil and p.inner.child ~= nil and p.inner.text ~= nil and p.inner.data ~= nil,
  "the stale pointer and blob edges vanished; the case proves nothing")
print("save_stale=" .. hex(save.Save(T, "Outer", p)))
print("measure_stale=" .. tostring(save.Measure(T, "Outer", p)))
print("json_stale=" .. hex(json.ToJson(T, "Outer", p)))
`, ModuleName(u))
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{
		"absent":  wantAbsent,
		"present": wantPresent,
	}))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	hexOf := hex.EncodeToString
	checks := []struct{ key, want, what string }{
		{"measure_absent", strconv.FormatInt(measureAbsent, 10), "LoadMeasure of the absent wire"},
		{"measure_present", strconv.FormatInt(measurePresent, 10), "LoadMeasure of the present wire"},
		{"json_absent", hexOf(wantAbsentJSON) + "0a", "ToJson of the absent value"},
		{"json_present", hexOf(wantPresentJSON) + "0a", "ToJson of the present value"},
		{"save_stale", hexOf(wantAbsent), "Save of a cleared value with stale pointer and blob edges"},
		{"measure_stale", strconv.Itoa(len(wantAbsent)), "Measure of a cleared value with stale edges"},
		{"json_stale", hexOf(wantAbsentJSON) + "0a", "ToJson of a cleared value with stale edges"},
	}
	for _, c := range checks {
		if got[c.key] != c.want {
			t.Errorf("%s: got %s, want %s", c.what, got[c.key], c.want)
		}
	}
}
