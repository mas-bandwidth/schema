package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// verdictsRun generates the unit, lets plant rewrite the emitted runtime
// (nil for the shipped one), and runs the probe.
func verdictsRun(t *testing.T, schema, probe string, plant func(files map[string][]byte)) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	sf := filepath.Join(dir, "v.schema")
	if err := os.WriteFile(sf, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, sf)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if plant != nil {
		plant(files)
	}
	luarun.WriteFiles(t, dir, files)
	main := fmt.Sprintf("local T = require(%q)\n", ModuleName(u)) + probe
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := luarun.Try(t, dir, "main.lua", nil)
	if err != nil {
		stdout += "ERROR " + stderr
	}
	return ModuleName(u), []byte(stdout)
}

// verdictsPlant replaces the one anchor in a runtime file with the old code.
func verdictsPlant(t *testing.T, file, anchor, old string) func(map[string][]byte) {
	return func(files map[string][]byte) {
		t.Helper()
		src, ok := files[file]
		if !ok {
			t.Fatalf("no emitted %s", file)
		}
		if strings.Count(string(src), anchor) != 1 {
			t.Fatalf("anchor %q is not once in %s", anchor, file)
		}
		files[file] = []byte(strings.Replace(string(src), anchor, old, 1))
	}
}

const verdictsPointerSchema = `package probe
table Node
{
    x int32
}
table P
{
    n *Node | required
}
`

const verdictsPointerProbe = `local load = require("load")
local save = require("save")
local rep = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
local w = save.Save(T, "P", T.NewP())
print((w:gsub(".", function(c) return string.format("%02x", c:byte()) end)))
local v = load.Load(T, "P", w, rep)
print(tostring(v ~= nil) .. " " .. tostring(rep.missingRequired or 0) .. " " .. tostring(rep.reason))
`

// A null `*T | required` pointer RIDES as index 0 (docs/SPEC-TABLES.md §3.1,
// gotable's encodeField): the bytes are the Go engine's, and Lua's own read
// takes them back instead of refusing missing_required.
func TestLuaRequiredNullPointerRides(t *testing.T) {
	t.Parallel()
	run := func(plant func(map[string][]byte)) []string {
		_, out := verdictsRun(t, verdictsPointerSchema, verdictsPointerProbe, plant)
		return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	}
	dir := t.TempDir()
	sf := filepath.Join(dir, "v.schema")
	if err := os.WriteFile(sf, []byte(verdictsPointerSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, sf)
	model := tabletext.NewModel(u)
	wire, err := tablewire.Encode(model, model.New(model.Lookup("P")))
	if err != nil {
		t.Fatal(err)
	}
	got := run(nil)
	if got[0] != hexBytes(wire) || got[1] != "true 0 nil" {
		t.Fatalf("a null required pointer: got %q, the Go wire is %s", got, hexBytes(wire))
	}
	// negative control: the old writer skips the null, and Lua's read refuses
	got = run(verdictsPlant(t, "save.lua", "    if (x ~= nil and x ~= false) or fd.required then\n      w_leb(w, id_ref(ids, fd.id))\n      w_u8(w, 17)\n      w_leb(w, nodes.index[x] or 0)\n    end\n  elseif s == \"string_blob\"", "    if x ~= nil and x ~= false then\n      w_leb(w, id_ref(ids, fd.id))\n      w_u8(w, 17)\n      w_leb(w, nodes.index[x])\n    end\n  elseif s == \"string_blob\""))
	if got[0] == hexBytes(wire) || got[1] == "true 0 nil" {
		t.Fatalf("the planted old writer is not red: %q", got)
	}
}

const verdictsFixedSchema = `package probe
fixed table Point
{
    x int32 = 1
    y int32 = 2
}
`

const verdictsFixedProbe = `local r = {}
for i = 1, 4097 do
  local p = T.NewPoint()
  p.x = i
  r[i] = p
end
local wire = T.FixedSave("Point", r)
local back = T.FixedLoad("Point", wire)
print(#back .. " " .. back[4097].x)
local ok, err = pcall(T.FixedLoad, "Point", wire, 4096)
print(tostring(ok) .. " " .. tostring(err))
`

// A fixed load with no capacity reads the file's own records: 4,096 is no
// limit the spec states, and a Go destination slice sets the only bound
// there is. A caller-set capacity below the count still refuses.
func TestLuaFixedLoadWithNoCapacityIsUnbounded(t *testing.T) {
	t.Parallel()
	want := "4097 4097\nfalse batch_too_large"
	_, out := verdictsRun(t, verdictsFixedSchema, verdictsFixedProbe, nil)
	if got := strings.TrimRight(string(out), "\n"); got != want {
		t.Fatalf("no capacity: got %q want %q", got, want)
	}
	// negative control: the old default of 4,096 refuses the file
	_, out = verdictsRun(t, verdictsFixedSchema, verdictsFixedProbe, func(files map[string][]byte) {
		verdictsPlant(t, "fixed.lua", "  cap = cap or nRec\n", "  cap = cap or 4096\n")(files)
	})
	if got := strings.TrimRight(string(out), "\n"); got == want || !strings.Contains(string(out), "batch_too_large") {
		t.Fatalf("the planted 4,096 default is not red: %q", got)
	}
}

const verdictsMessageSchema = `package probe
fixed table Msg
{
    a int32
}
`

const verdictsMessageProbe = `local vocab = {}
local v = T.NewMsg()
v.a = 7
local n = T.MeasureMessages("Msg", vocab, { v })
local out, why = T.SaveMessages("Msg", vocab, { v })
print(tostring(n) .. " " .. tostring(out ~= nil) .. " " .. tostring(why))
print(out and (out:gsub(".", function(c) return string.format("%02x", c:byte()) end)) or "nil")
`

// SaveMessages on a vocabulary nobody announced is the unit's own send, as in
// Go (EncodeMessages takes no vocabulary a peer announced), and
// MeasureMessages agrees: both answer, neither is a silent nil.
func TestLuaSaveMessagesOnUnannouncedVocabulary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sf := filepath.Join(dir, "v.schema")
	if err := os.WriteFile(sf, []byte(verdictsMessageSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, sf)
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Msg"))
	if !model.Read(inst, []byte(`{"a": 7}`), &tabletext.Report{}) {
		t.Fatal("the Go reference does not read the value")
	}
	wire, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	_, out := verdictsRun(t, verdictsMessageSchema, verdictsMessageProbe, nil)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if lines[0] != fmt.Sprintf("%d true nil", len(wire)) || lines[1] != hexBytes(wire) {
		t.Fatalf("an unannounced vocabulary: got %q, the Go batch is %d bytes %s", lines, len(wire), hexBytes(wire))
	}
	// negative control: the old guard answers nil with no reason
	_, out = verdictsRun(t, verdictsMessageSchema, verdictsMessageProbe, verdictsPlant(t, "message.lua", "  local refused = refuse_batch(T, root, values)\n  if refused ~= nil then return nil, refused end\n  local W = new_writer(false)", "  if not vocab.announced then return nil end\n  local refused = refuse_batch(T, root, values)\n  if refused ~= nil then return nil, refused end\n  local W = new_writer(false)"))
	if strings.Contains(string(out), "true nil") {
		t.Fatalf("the planted old guard is not red: %q", out)
	}
}
