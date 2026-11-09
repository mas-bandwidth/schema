package luatable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type retainMessageCase struct {
	name, lua, goSetup string
}

const retainMessageNewerSchema = `package probe
fixed table Child { n int32
  k int32 }
fixed table Rec { a int32 }
union Pick { alpha int32
 ping }
fixed table Item { n int32
  y int32 }
table Msg { c Child
  x int32
  extra string(8)
  neg int16
  nums [..4]int32
  rec Rec
  pick Pick
  items [..2]Item }
table SealedMsg | integrity = "crc32c" { x int32
  extra string(8) }
`

const retainMessageOlderSchema = `package probe
fixed table Child { n int32 }
fixed table Item { n int32 }
table Msg { c Child
  x int32 | min = 0, max = 100
  items [..2]Item }
table SealedMsg | integrity = "crc32c" { x int32 }
`

func retainMessageCases() []retainMessageCase {
	return []retainMessageCase{
		{"extra", `v.extra = "hi"`, `v.ExtraLength = 2; copy(v.Extra[:], "hi")`},
		{"neg", `v.neg = -5`, `v.Neg = -5`},
		{"nums", `v.nums = { 1, -2, 3, 4 }`, `v.NumsCount = 4; v.Nums[0] = 1; v.Nums[1] = -2; v.Nums[2] = 3; v.Nums[3] = 4`},
		{"rec", `v.rec.a = 9`, `v.Rec.A = 9`},
		{"pick", `v.pick = { tag = 1, alpha = -7 }`, `v.Pick.Type = PickTypeAlpha; v.Pick.Alpha().Value = -7`},
		{"nested", `v.c.k = 11`, `v.C.K = 11`},
		// an unknown field inside an ELEMENT of an array of tables: the
		// element is addressed by the array's (ordinal, index) step alone, as
		// retain.lua's capture and save address it (§6.6), so the record lands
		// back inside its own element
		{"element", `local it = Tn.NewItem(); it.n = 1; it.y = 5; v.items = { it }`, `v.ItemsCount = 1; v.Items[0].N = 1; v.Items[0].Y = 5`},
	}
}

func retainMessageSetup(t *testing.T) (map[string][]byte, map[string][]byte, string, map[string][]byte) {
	t.Helper()
	cases := retainMessageCases()
	dir := t.TempDir()
	newerPath := filepath.Join(dir, "newer.schema")
	olderPath := filepath.Join(dir, "older.schema")
	if err := os.WriteFile(newerPath, []byte(retainMessageNewerSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(olderPath, []byte(retainMessageOlderSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	newerUnit := luarun.LoadUnit(t, newerPath)
	newerFiles, err := Generate(newerUnit)
	if err != nil {
		t.Fatalf("generate newer Lua: %v", err)
	}
	olderUnit := luarun.LoadUnit(t, olderPath)
	olderFiles, err := Generate(olderUnit)
	if err != nil {
		t.Fatalf("generate older Lua: %v", err)
	}
	// Both units name the same module; rename so one main.lua can require both.
	newerFiles["NewerTables.lua"] = newerFiles["ProbeTables.lua"]
	delete(newerFiles, "ProbeTables.lua")
	olderFiles["OlderTables.lua"] = olderFiles["ProbeTables.lua"]
	delete(olderFiles, "ProbeTables.lua")

	expected := goRetainMessageSaves(t, newerUnit, cases)

	var luaCases strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&luaCases, `{ "%s", function()
  local v = Tn.NewMsg(); v.c.n = 7; v.x = 42
  %s
  return v
end },
`, c.name, c.lua)
	}
	probe := `local Tn = require("NewerTables")
local To = require("OlderTables")
local save = require("save")
local retain = require("retain")
local inputs = require("inputs")
local cases = {
` + luaCases.String() + `}
for _, c in ipairs(cases) do
  local ok, err = pcall(function()
    local v = c[2]()
    local G = save.Save(Tn, "Msg", v)
    if G == nil then print(c[1] .. ":newer_save_refused") return end
    local store = { list = {}, used = 0 }
    local rp = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0, retained = 0, retainLost = 0 }
    local msg = retain.LoadRetain(To, "Msg", G, store, rp)
    if msg == nil or rp.damaged then print(c[1] .. ":load_refused") return end
    local sz = retain.MeasureRetain(To, "Msg", msg, store)
    local srp = { damaged = false, refused = 0, retained = 0, retainLost = 0 }
    local O = retain.SaveRetain(To, "Msg", msg, store, srp)
    if O == nil or srp.damaged or srp.retainLost > 0 or sz ~= #O then print(c[1] .. ":save_refused") return end
    print(c[1] .. ":" .. tostring(O == inputs["go_" .. c[1]]) .. ":" .. rp.retained .. ":" .. rp.retainLost)
  end)
  if not ok then print(c[1] .. ":error:" .. tostring(err)) end
end
do
  local ok, err = pcall(function()
    local v = Tn.NewSealedMsg()
    v.x = 42
    v.extra = "hi"
    local G = save.Save(Tn, "SealedMsg", v)
    local store = { list = {}, used = 0 }
    local rp = {}
    local msg = retain.LoadRetain(To, "SealedMsg", G, store, rp)
    if msg == nil or rp.damaged then print("sealed:load_refused") return end
    local sz = retain.MeasureRetain(To, "SealedMsg", msg, store)
    local srp = { damaged = false, refused = 0, retained = 0, retainLost = 0 }
    local O = retain.SaveRetain(To, "SealedMsg", msg, store, srp)
    if O == nil or srp.retainLost > 0 or sz ~= #O then print("sealed:save_refused") return end
    print("sealed:" .. tostring(O == G) .. ":" .. rp.retained .. ":" .. rp.retainLost)
  end)
  if not ok then print("sealed:error:" .. tostring(err)) end
end
do
  local ok, err = pcall(function()
    local v = Tn.NewMsg()
    v.c.n = 7
    v.x = 500
    v.extra = "hi"
    local G = save.Save(Tn, "Msg", v)
    local store = { list = {}, used = 0 }
    local rp = {}
    local msg = retain.LoadRetain(To, "Msg", G, store, rp)
    local clean = msg == nil and rp.reason == "out_of_range" and #store.list == 0 and store.used == 0 and rp.retained == 0 and rp.retainLost == 0
    print("strict_refusal:" .. tostring(clean) .. ":" .. tostring(rp.retained) .. ":" .. tostring(#store.list))
  end)
  if not ok then print("strict_refusal:error:" .. tostring(err)) end
end
`
	inputs := map[string][]byte{}
	for _, c := range cases {
		inputs["go_"+c.name] = expected[c.name]
	}
	return newerFiles, olderFiles, probe, inputs
}

// TestLuaRetainMessage is the Lua strict-read retaining leg of RETAIN-UNKNOWN
// (docs/SPEC-TABLES.md §6.6): a newer unit writes a variable-form wire whose
// bodies carry fields the older reader cannot name; the older reader loads it
// through the strict read (LoadRetain) and writes the result back through
// SaveRetain. The written-back variable form must equal the Go reference's own
// variable save of the same value byte for byte, and the retained record must
// be placed. A wire that the strict read refuses (such as out_of_range on a
// known field alongside unknown fields) must return nil and leave the retain
// store empty.
func TestLuaRetainMessage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	newerFiles, olderFiles, probe, inputs := retainMessageSetup(t)
	normal := runRetainProbe(t, newerFiles, olderFiles, probe, inputs)
	for _, c := range retainMessageCases() {
		line := normal[c.name]
		if !strings.HasPrefix(line, c.name+":true:") {
			t.Errorf("%s: retain round trip %q, want %s:true", c.name, line, c.name)
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 4 || parts[2] == "0" || parts[3] != "0" {
			t.Errorf("%s: retained,lost = %q, want a placed record and no loss", c.name, line)
		}
	}
	if line := normal["sealed"]; line != "sealed:true:1:0" {
		t.Errorf("sealed: got %q, want sealed:true:1:0", line)
	}
	if line := normal["strict_refusal"]; line != "strict_refusal:true:0:0" {
		t.Errorf("strict_refusal: got %q, want strict_refusal:true:0:0", line)
	}
}

// TestLuaRetainMessageNegativeControl plants one defect at a time in retain.lua
// and verifies that TestLuaRetainMessage's probe turns red on the targeted case.
func TestLuaRetainMessageNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	newerFiles, olderFiles, probe, inputs := retainMessageSetup(t)
	for _, bug := range []struct {
		name, from, to, caught string
	}{
		{
			"element count",
			"    out_leb(o.w, count)\n",
			"    out_leb(o.w, count + 1)\n",
			"nums",
		},
		{
			"framed length",
			"  w_leb(o.w, sub.n)\n  w_raw(o.w, table.concat(sub.parts))\n",
			"  w_leb(o.w, sub.n + 1)\n  w_raw(o.w, table.concat(sub.parts))\n",
			"rec",
		},
		{
			"union arm kind",
			"  elseif kind == 15 then\n    local id = out_ref(o)\n    if id == nil then return false end\n    if id == 0 then return true end\n    if not tr_has(o.r, 1) then return false end\n    local k = tr_u8(o.r)\n    w_u8(o.w, k)\n",
			"  elseif kind == 15 then\n    local id = out_ref(o)\n    if id == nil then return false end\n    if id == 0 then return true end\n    if not tr_has(o.r, 1) then return false end\n    local _ = tr_u8(o.r)\n    w_u8(o.w, 0)\n",
			"pick",
		},
		{
			"nested child path",
			"              capture_body(T, atable, b, er.at, er.at + n, d, path_step(path, ord, 0), report, retain)\n",
			"              capture_body(T, atable, b, er.at, er.at + n, d, path, report, retain)\n",
			"nested",
		},
		{
			"array element path",
			"        capture_value(T, atable, b, body.at, body.at + elen, e, path_step(path, ord, i - 1), ord, report, retain)\n",
			"        capture_value(T, atable, b, body.at, body.at + elen, e, path, ord, report, retain)\n",
			"element",
		},
		{
			"strict refusal guard",
			"  if v == nil and (rep.reason ~= \"non_finite_float\" or rep.nonFiniteFloat ~= nil) then return nil end\n",
			"",
			"strict_refusal",
		},
		{
			"integrity trailer",
			"  if desc ~= nil and desc.integrity ~= nil and #bytes >= 5 then bytes = bytes:sub(1, #bytes - 4) end\n",
			"",
			"sealed",
		},
	} {
		t.Run(bug.name, func(t *testing.T) {
			t.Parallel()
			src := string(olderFiles["retain.lua"])
			if n := strings.Count(src, bug.from); n != 1 {
				t.Fatalf("negative control %s: runtime source matched %d times, want 1: %q", bug.name, n, bug.from)
			}
			patched := strings.Replace(src, bug.from, bug.to, 1)
			patchedOlder := maps.Clone(olderFiles)
			patchedOlder["retain.lua"] = []byte(patched)
			got := runRetainProbe(t, newerFiles, patchedOlder, probe, inputs)
			if strings.HasPrefix(got[bug.caught], bug.caught+":true:") {
				t.Errorf("negative control %s: %s stayed green (%q)", bug.name, bug.caught, got[bug.caught])
			}
		})
	}
}

// runRetainProbe writes one pair of generated units and a probe into a fresh
// directory, runs it under the pinned interpreter, and returns each case's
// answer line by name.
func runRetainProbe(t *testing.T, newerFiles, olderFiles map[string][]byte, probe string, inputs map[string][]byte) map[string]string {
	t.Helper()
	run := t.TempDir()
	for name, data := range newerFiles {
		if err := os.WriteFile(filepath.Join(run, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range olderFiles {
		if err := os.WriteFile(filepath.Join(run, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(run, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, run, "main.lua", inputs)), "\n")
	answers := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if name, _, ok := strings.Cut(line, ":"); ok {
			answers[name] = line
		}
	}
	return answers
}

// goRetainMessageSaves generates the NEWER unit for Go and asks the generated
// variable-form Save for each case's bytes: the oracle is the Go reference's
// own output (internal/codegen/gotable), never a fixture.
func goRetainMessageSaves(t *testing.T, u *ir.Unit, cases []retainMessageCase) map[string][]byte {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var body strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&body, `	{
		var v Msg
		MsgReset(&v)
		v.C.N = 7
		v.X = 42
		%s
		n := MsgMeasure(&v)
		if n < 0 {
			t.Fatalf("measure %s: %%d", n)
		}
		b := make([]byte, n)
		if MsgSave(&v, b) != int64(n) {
			t.Fatalf("save %s")
		}
		if err := os.WriteFile(%q, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
`, c.goSetup, c.name, c.name, filepath.Join(dir, "go_"+c.name+".bin"))
	}
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["oracle_test.go"] = []byte(fmt.Sprintf(`package probe
import ("os"; "testing")
func TestGoRetainMessageOracle(t *testing.T) {
%s}
`, body.String()))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestGoRetainMessageOracle$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go retain message oracle: %v\n%s", err, out)
	}
	saves := map[string][]byte{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(dir, "go_"+c.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		saves[c.name] = data
	}
	return saves
}
