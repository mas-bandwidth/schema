package luatable

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luaTimestampSchema is the unit this test drives (docs/SPEC-TABLES.md §2.11,
// §3 kind 36): a VARIABLE table carrying the timestamp kind, and a FIXED table
// that carries it on the fixed form and on the message form.
const luaTimestampSchema = `package stamp

table Event
{
    id        uint32
    opened_at timestamp
    closed_at timestamp
}

fixed table Session
{
    seq     int32
    started timestamp
    ended   timestamp
}
`

// timestampCases is the corpus: both ends of the int64 count, the zero time
// and one nanosecond before the epoch. Every value is a constant; no clock is
// read.
var timestampCases = []struct {
	name string
	n    int64
}{
	{"min", math.MinInt64},
	{"max", math.MaxInt64},
	{"zero", 0},
	{"negone", -1},
}

// TestLuaCarriesTimestamp pins the Lua leg of the timestamp kind on the three
// binary forms Go carries: the variable wire (§3 kind 36), the fixed form
// (§3.4, where the layout names the int64's kind 5 at eight bytes) and the
// message form (§3.3). For each corpus value the generated Go reference
// writes the wire, and the Lua leg must load its bytes to the same values and
// save the same bytes, byte for byte.
//
// The kind's follow-ons — a `[..4]timestamp` array and a `?timestamp` — are
// named follow-ons the checker refuses by name at this standard (§2.11, BARE
// means a plain field), so there is no wire for them to pin; the last part of
// this test pins that refusal rather than inventing a spelling Go does not
// carry.
func TestLuaCarriesTimestamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Stamp.schema")
	if err := os.WriteFile(path, []byte(luaTimestampSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)

	goWires := timestampGoOracle(t, u)

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Lua: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function fresh()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
           clamped = 0, widened = 0, duplicate = 0 }
end
local names = { "min", "max", "zero", "negone" }
for _, name in ipairs(names) do
  local report = fresh()
  local v = load.Load(T, "Event", inputs["var_" .. name], report)
  assert(v ~= nil and not report.malformed, "variable load " .. name)
  local saved = save.Save(T, "Event", v)
  assert(saved ~= nil, "variable save " .. name)
  print("var|" .. name .. "|" .. string.format("%d", v.opened_at) .. "|" ..
    string.format("%d", v.closed_at) .. "|" .. hex(saved))

  local f = T.ReadFixed("Session", inputs["fixed_" .. name])
  local fsaved = T.WriteFixed("Session", f)
  print("fixed|" .. name .. "|" .. string.format("%d", f.started) .. "|" ..
    string.format("%d", f.ended) .. "|" .. hex(fsaved))

  local vocab = {}
  for i = 1, T.MessageEntries do vocab[i] = {} end
  local mreport = fresh()
  assert(T.AnnounceRead(vocab, T.Announcement, mreport), "announce")
  local values = T.LoadMessages("Session", vocab, inputs["msg_" .. name], mreport)
  assert(values ~= nil and #values == 1 and not mreport.malformed, "message load " .. name)
  local msaved = T.SaveMessages("Session", vocab, values)
  assert(msaved ~= nil, "message save " .. name)
  print("msg|" .. name .. "|" .. string.format("%d", values[1].started) .. "|" ..
    string.format("%d", values[1].ended) .. "|" .. hex(msaved))
end
`
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, c := range timestampCases {
		inputs["var_"+c.name] = goWires["var_"+c.name]
		inputs["fixed_"+c.name] = goWires["fixed_"+c.name]
		inputs["msg_"+c.name] = goWires["msg_"+c.name]
	}
	out := strings.TrimRight(string(luarun.Run(t, luaDir, "main.lua", inputs)), "\n")
	var want []string
	for _, c := range timestampCases {
		half := c.n / 2
		want = append(want,
			fmt.Sprintf("var|%s|%d|%d|%x", c.name, c.n, half, goWires["var_"+c.name]),
			fmt.Sprintf("fixed|%s|%d|%d|%x", c.name, c.n, half, goWires["fixed_"+c.name]),
			fmt.Sprintf("msg|%s|%d|%d|%x", c.name, c.n, half, goWires["msg_"+c.name]),
		)
	}
	got := strings.Split(out, "\n")
	if len(got) != len(want) {
		t.Fatalf("Lua printed %d lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}

	// THE FOLLOW-ONS ARE REFUSED BY NAME (docs/SPEC-TABLES.md §2.11): a
	// timestamp is a BARE table field in this major, so an array of one and an
	// optional one never reach a codec. Pin the checker's refusal here so the
	// Lua leg cannot silently grow a wire Go does not have.
	for _, c := range []struct{ field, want string }{
		{"closed_list [..4]timestamp", "an array of timestamp"},
		{"reopened_at ?timestamp", "?timestamp is a named follow-on"},
	} {
		timestampRefusal(t, c.field, c.want)
	}
}

// timestampRefusal pins that a timestamp follow-on field on Event is refused
// by name in the checker (docs/SPEC-TABLES.md §2.11).
func timestampRefusal(t *testing.T, field, want string) {
	t.Helper()
	src := "package stamp\n\ntable Event\n{\n    id uint32\n    opened_at timestamp\n    " + field + "\n}\n"
	ast, perrs := parser.Parse("Stamp.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("%s: parse: %v", field, perrs[0])
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Stamp.schema", Name: "Stamp.schema", Base: "Stamp", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) == 0 {
		t.Fatalf("%s: accepted; a timestamp follow-on is not carried at this standard", field)
	}
	var msg strings.Builder
	for _, err := range cerrs {
		msg.WriteString(err.Error())
		msg.WriteString("\n")
	}
	if !strings.Contains(msg.String(), want) {
		t.Fatalf("%s: the refusal does not name the construct %q:\n%s", field, want, msg.String())
	}
}

// timestampGoOracle generates the Go reference for u and runs one test in a
// t.TempDir(): for every corpus value it writes the variable, fixed and message
// wires the generated Go save emits. Those bytes and their decoded values are
// the expectation, never a fixture (internal/codegen/gotable/timestamp_test.go).
func timestampGoOracle(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	for name, data := range tables {
		files[name] = data
	}
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["oracle_test.go"] = []byte(fmt.Sprintf(`package %s

import ("math";"os";"testing")

var cases = []struct{ name string; n int64 }{
 {"min", math.MinInt64},
 {"max", math.MaxInt64},
 {"zero", 0},
 {"negone", -1},
}

func TestTimestampOracle(t *testing.T) {
 for _, c := range cases {
  func() {
   var source EventBuilder
   if !source.Init() { t.Fatal("builder") }
   defer source.Shutdown()
   root := source.GetRoot()
   root.Id = 7
   root.OpenedAt = c.n
   root.ClosedAt = c.n / 2
   size, err := EventMeasureReason(root, &source.Arena)
   if err != nil { t.Fatal(err) }
   wire := make([]byte, size)
   if EventSave(root, wire, &source.Arena) != size { t.Fatal("variable save") }
   if err := os.WriteFile("var_"+c.name+".bin", wire, 0o644); err != nil { t.Fatal(err) }
  }()
  in := []Session{{Seq: -3, Started: c.n, Ended: c.n / 2}}
  fixed := make([]byte, SessionFixedMeasure(1))
  if SessionFixedSave(in, fixed) != int64(len(fixed)) { t.Fatal("fixed save") }
  if err := os.WriteFile("fixed_"+c.name+".bin", fixed, 0o644); err != nil { t.Fatal(err) }
  values := []*Session{{Seq: -3, Started: c.n, Ended: c.n / 2}}
  n := SessionMeasureMessages(values, nil)
  msg := make([]byte, n)
  var report TableReport
  if SessionSaveMessages(values, msg, &report) != n || report != (TableReport{}) { t.Fatalf("message save %%+v", report) }
  if err := os.WriteFile("msg_"+c.name+".bin", msg, 0o644); err != nil { t.Fatal(err) }
 }
}
`, u.Package))
	dir := t.TempDir()
	for name, data := range files {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestTimestampOracle$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go timestamp oracle: %v\n%s", err, out)
	}
	wires := map[string][]byte{}
	for _, c := range timestampCases {
		for _, form := range []string{"var", "fixed", "msg"} {
			key := form + "_" + c.name
			data, err := os.ReadFile(filepath.Join(dir, key+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			wires[key] = data
		}
	}
	return wires
}
