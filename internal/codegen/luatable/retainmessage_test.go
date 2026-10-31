package luatable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaRetainUnknownMessages is the message-form leg of RETAIN-UNKNOWN
// (docs/SPEC-TABLES.md §6.6, §3.3): a newer unit writes a batch whose bodies
// carry fields the older reader cannot name; the older reader loads it with
// retention against the connection's own (newer) vocabulary and writes the
// result back through the variable form's SaveRetain (§6.6, retention crosses
// from the message form to the variable form). The written-back variable form
// must equal the newer unit's own variable save of the same value byte for
// byte, and the retained record must be placed.
//
// The fields cover every resolved-walk branch a planted bug can hide in: an
// unknown signed int16 carrying a negative value (the sign-extension
// branch), an unknown counted list (the element count), an
// unknown nested record (the framed body length), an unknown union arm (the
// arm's kind byte), and an unknown field inside a NAMED nested child (a
// non-root path). The Go reference's variable save is the expectation; the
// negative controls at the end prove each planted bug turns this test red.
func TestLuaRetainUnknownMessages(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	newer := `package probe
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
`
	older := `package probe
fixed table Child { n int32 }
fixed table Item { n int32 }
table Msg { c Child
  x int32
  items [..2]Item }
`
	cases := []struct {
		name, lua, goSetup string
	}{
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

	dir := t.TempDir()
	newerPath := filepath.Join(dir, "newer.schema")
	olderPath := filepath.Join(dir, "older.schema")
	if err := os.WriteFile(newerPath, []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(olderPath, []byte(older), 0o644); err != nil {
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
local message = require("message")
local save = require("save")
local retainmsg = require("retainmessage")
local retain = require("retain")
local inputs = require("inputs")
local vocab_n = {}
for i = 1, Tn.MessageEntries do vocab_n[i] = {} end
local rep = {}
assert(Tn.AnnounceRead(vocab_n, Tn.Announcement, rep))
local vocab = {}
for i = 1, Tn.MessageEntries do
  local e = Tn.MessageVocabulary[i]
  vocab[i] = { id = e.id, kind = e.kind, packing = e.packing, bits = e.bits, max = e.max, min = e.min, elem = e.elem }
end
vocab.count = Tn.MessageEntries
vocab.refBits = Tn.MessageRefBits
vocab.announced = true
local cases = {
` + luaCases.String() + `}
for _, c in ipairs(cases) do
  local ok, err = pcall(function()
    local v = c[2]()
    local G = message.saveMessages(Tn, "Msg", vocab_n, { v })
    if G == nil then print(c[1] .. ":newer_save_refused") return end
    local rp = { malformed = false, refused = 0, unknown = 0, retained = 0, retainLost = 0 }
    local stores = { { list = {} } }
    local vals = retainmsg.LoadRetainMessages(To, "Msg", vocab, G, stores, rp)
    if vals == nil or vals[1] == nil then print(c[1] .. ":load_refused") return end
    local O = retain.SaveRetain(To, "Msg", vals[1], stores[1], rp)
    if O == nil then print(c[1] .. ":save_refused") return end
    print(c[1] .. ":" .. tostring(O == inputs["go_" .. c[1]]) .. ":" .. rp.retained .. ":" .. rp.retainLost)
  end)
  if not ok then print(c[1] .. ":error:" .. tostring(err)) end
end
`
	inputs := map[string][]byte{}
	for _, c := range cases {
		inputs["go_"+c.name] = expected[c.name]
	}

	normal := runRetainProbe(t, newerFiles, olderFiles, probe, inputs)
	for _, c := range cases {
		line := normal[c.name]
		if !strings.HasPrefix(line, c.name+":true:") {
			t.Errorf("%s: message retain round trip %q, want %s:true", c.name, line, c.name)
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 4 || parts[2] == "0" || parts[3] != "0" {
			t.Errorf("%s: retained,lost = %q, want a placed record and no loss", c.name, line)
		}
	}

	for _, bug := range []struct {
		name, from, to, caught string
	}{
		{"element count", "  w_u8(self.w, e.elem)\n  w_leb(self.w, n)\n", "  w_u8(self.w, e.elem)\n  w_leb(self.w, n + 1)\n", "nums"},
		{"framed length", "  w_u32(self.w, body.n)\n", "  w_u32(self.w, body.n + 1)\n", "rec"},
		{"scalar sign extension", "    local sign = 1 << (bits - 1)\n    if v & sign ~= 0 then v = v - (1 << bits) end\n", "", "neg"},
		{"union arm kind", "    w_u8(self.w, arm.kind)\n", "    w_u8(self.w, 0)\n", "pick"},
	} {
		patched := strings.Replace(string(olderFiles["retainmessage.lua"]), bug.from, bug.to, 1)
		if patched == string(olderFiles["retainmessage.lua"]) {
			t.Fatalf("negative control %s: runtime source did not carry %q", bug.name, bug.from)
		}
		patchedOlder := map[string][]byte{}
		maps.Copy(patchedOlder, olderFiles)
		patchedOlder["retainmessage.lua"] = []byte(patched)
		got := runRetainProbe(t, newerFiles, patchedOlder, probe, inputs)
		if strings.HasPrefix(got[bug.caught], bug.caught+":true:") {
			t.Errorf("negative control %s: %s stayed green", bug.name, bug.caught)
		}
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
func goRetainMessageSaves(t *testing.T, u *ir.Unit, cases []struct{ name, lua, goSetup string }) map[string][]byte {
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
