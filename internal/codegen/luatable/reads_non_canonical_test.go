package luatable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luaNonCanonicalRow is one row of testdata/wire/non-canonical/MANIFEST.tsv.
type luaNonCanonicalRow struct {
	file     string
	form     string
	spelling string
	verdict  string
}

func luaNonCanonicalRows(t *testing.T, vector string) []luaNonCanonicalRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(vector, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []luaNonCanonicalRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 4", i+1, len(f))
		}
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
			continue
		}
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[3])
		}
		rows = append(rows, luaNonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// luaNonCanonicalExpected is the canonical file a read row re-saves to: the
// vector itself, or the canonical rewrite the manifest's generator pins beside
// it for a lawful spelling no writer produces (docs/SPEC-TABLES.md §3).
func luaNonCanonicalExpected(t *testing.T, vector string, row luaNonCanonicalRow) []byte {
	t.Helper()
	path := filepath.Join(vector, row.file)
	if row.form == "variable" {
		rewrite := filepath.Join(vector, "canonical", filepath.Base(row.file))
		if _, err := os.Stat(rewrite); err == nil {
			path = rewrite
		} else if row.file == "variable-fields-out-of-order.bin" {
			path = filepath.Join(vector, "variable-canonical.bin")
		}
	}
	return readFile(t, path)
}

// luaNonCanonicalOutput parses the probe's `key=value` lines.
func luaNonCanonicalOutput(t *testing.T, files map[string][]byte, probe string, inputs map[string][]byte) map[string]string {
	t.Helper()
	out, stderr, err := try(t, files, probe, inputs)
	if err != nil {
		t.Fatalf("probe failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			got[k] = v
		}
	}
	return got
}

const luaNonCanonicalPrelude = `local T = require(%q)
local inputs = require("inputs")
local function hex(s)
  local out = {}
  for i = 1, #s do out[i] = string.format("%%02x", s:byte(i)) end
  return table.concat(out)
end
`

// TestLuaReadsRefuseNonCanonical holds the Lua variable-form and message-form
// reads to docs/SPEC-TABLES.md §3, §3.1, §3.3, §4 and docs/rulings.json
// reads-refuse-non-canonical over every row of
// testdata/wire/non-canonical/MANIFEST.tsv, through every read entry: the
// plain load, the retaining load, the load measure and the message load. The
// Go engine (internal/tablewire) is the oracle beside the manifest: each
// variable row's plain and retaining verdicts are the engine's, and the
// measure answers what tablewire.FileRegionMeasure answers. A read row re-saves
// through each entry's writer to the canonical bytes; a refused row is refused
// by every entry. A message row is also read under another build version,
// where only the union field written with arm reference 0 stays refused
// (§3.3 (c)); there the read value re-saves to a batch that reads back to the
// same bytes.
func TestLuaReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")

	repo := filepath.Join("..", "..", "..")
	vector := filepath.Join(repo, "testdata", "wire", "non-canonical")
	rows := luaNonCanonicalRows(t, vector)
	ncUnit := luarun.LoadUnit(t, filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema"))
	oneUnit := luarun.LoadUnit(t, filepath.Join(repo, "test", "message-one-encoding", "One.schema"))
	for _, row := range rows {
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()
			data := readFile(t, filepath.Join(vector, row.file))
			unit := ncUnit
			announcement := filepath.Join(vector, "announcement.bin")
			table := "Root"
			if row.form == "message" {
				table = "Message"
			}
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				unit = oneUnit
				announcement = filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin")
				table = "One"
			}
			files, err := Generate(unit)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			switch row.form {
			case "variable":
				if table != "Root" {
					t.Fatalf("%s: a variable row outside NonCanonical.schema", row.file)
				}
				luaNonCanonicalVariable(t, unit, files, data, luaNonCanonicalExpected(t, vector, row), readFile(t, filepath.Join(vector, "variable-canonical.bin")), row.verdict)
			case "message":
				otherBuild := "read"
				if row.spelling == "union-arm-zero" {
					otherBuild = "refused"
				}
				luaNonCanonicalMessage(t, unit, files, table, readFile(t, announcement), data, row.verdict, otherBuild)
			case "announcement":
				if row.verdict != "read" {
					t.Fatalf("%s: an announcement row must read", row.file)
				}
				luaNonCanonicalAnnouncement(t, unit, files, data)
			default:
				t.Fatalf("MANIFEST.tsv: %s: unknown form %q", row.file, row.form)
			}
		})
	}
	t.Run("node table out of its place", testLuaNodeTableOutOfPlace)
	t.Run("union None element", testLuaUnionNoneElementReads)
	t.Run("undeclared field at the reader's build", testLuaUndeclaredFieldSameBuild)
	t.Run("unknown union field written None", testLuaUnknownUnionFieldNone)
}

// luaNonCanonicalVariable runs one form-1 file through the plain load, the
// retaining load and the load measure.
func luaNonCanonicalVariable(t *testing.T, unit *ir.Unit, files map[string][]byte, data, expected, warm []byte, verdict string) {
	t.Helper()
	model := tabletext.NewModel(unit)
	engine := map[string]string{}
	for _, retain := range []bool{false, true} {
		inst := model.New(model.Lookup("Root"))
		var ok bool
		var err error
		if retain {
			ok, err = tablewire.DecodeRetain(model, inst, data, &tablewire.Retain{Capacity: 1024, IdCapacity: 64}, &tabletext.Report{})
		} else {
			ok, err = tablewire.Decode(model, inst, data, &tabletext.Report{})
		}
		got := "read"
		if !ok || err != nil {
			got = "refused"
		}
		engine[strconv.FormatBool(retain)] = got
	}
	if engine["false"] != verdict || engine["true"] != verdict {
		t.Fatalf("the engine's verdicts are %v, the manifest's %s", engine, verdict)
	}
	measure := "nil"
	if bytes, whole, refused := tablewire.FileRegionMeasure(unit, unit.Tables["Root"], data); whole && !refused {
		measure = strconv.FormatInt(bytes, 10)
	}

	probe := fmt.Sprintf(luaNonCanonicalPrelude+`local load = require("load")
local save = require("save")
local retain = require("retain")
local function report()
  return { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0, retained = 0, retainLost = 0 }
end
local n = load.LoadMeasure(T, "Root", inputs.wire)
print("measure=" .. (n == nil and "nil" or string.format("%%d", n)))
local v = load.Load(T, "Root", inputs.wire, report())
if v == nil then
  print("load=refused")
else
  print("load=" .. hex(assert(save.Save(T, "Root", v))))
end
local state = load.NewState()
assert(load.Load(T, "Root", inputs.warm, report(), state) ~= nil, "variable-canonical.bin was refused")
local again = load.Load(T, "Root", inputs.wire, report(), state)
if again == nil then
  print("state=refused")
else
  print("state=" .. hex(assert(save.Save(T, "Root", again))))
end
local store = { list = {}, used = 0, capacity = 1 << 20, idCapacity = 131072 }
local r = report()
local k = retain.LoadRetain(T, "Root", inputs.wire, store, r)
if k == nil or r.damaged then
  print("retain=refused")
else
  print("retain=" .. hex(assert(retain.SaveRetain(T, "Root", k, store, report()))))
end
`, ModuleName(unit))
	got := luaNonCanonicalOutput(t, files, probe, map[string][]byte{"wire": data, "warm": warm})
	want := "refused"
	if verdict == "read" {
		want = hex.EncodeToString(expected)
	}
	for _, entry := range []string{"load", "state", "retain"} {
		if got[entry] != want {
			t.Errorf("%s: %s, want %s", entry, got[entry], want)
		}
	}
	if got["measure"] != measure {
		t.Errorf("measure: %s, want the engine's %s", got["measure"], measure)
	}
	if verdict == "read" && got["measure"] == "nil" {
		t.Errorf("measure refused a file that reads")
	}
}

// luaNonCanonicalMessage runs one batch through the message load under the
// announcement as written, where the manifest's verdict holds and a read
// re-saves to the same bytes, and under another build version
// (docs/SPEC-TABLES.md §3.3): there a field at a default and fields out of
// order are not judged and read, re-saving to a batch that reads back to the
// same bytes; a union field written with arm reference 0 (§3.3 (c)) is
// refused at every build.
func luaNonCanonicalMessage(t *testing.T, unit *ir.Unit, files map[string][]byte, table string, announcement, data []byte, verdict, otherBuild string) {
	t.Helper()
	probe := fmt.Sprintf(luaNonCanonicalPrelude+`local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, {}), "announcement refused")
local own = vocab.buildVersion
local function read(label, data)
  local values = T.LoadMessages(%q, vocab, data, {})
  if values == nil or #values == 0 then
    print(label .. "=refused")
    return nil
  end
  local out = assert(T.SaveMessages(%q, vocab, values), label .. ": save refused")
  print(label .. "=" .. hex(out))
  return out
end
read("same", inputs.wire)
vocab.buildVersion = own ~ 1
local rewrite = read("other", inputs.wire)
vocab.buildVersion = own
if rewrite ~= nil then read("back", rewrite) end
`, ModuleName(unit), table, table)
	got := luaNonCanonicalOutput(t, files, probe, map[string][]byte{"announce": announcement, "wire": data})
	want := "refused"
	if verdict == "read" {
		want = hex.EncodeToString(data)
	}
	if got["same"] != want {
		t.Errorf("same build: %s, want %s", got["same"], want)
	}
	switch otherBuild {
	case "read":
		if got["other"] == "refused" || got["other"] == "" {
			t.Errorf("other build: %q, want read", got["other"])
		} else if got["back"] != got["other"] {
			t.Errorf("other build: the rewrite %s reads back as %s", got["other"], got["back"])
		}
	default:
		if got["other"] != "refused" {
			t.Errorf("other build: %s, want refused", got["other"])
		}
	}
}

func luaNonCanonicalAnnouncement(t *testing.T, unit *ir.Unit, files map[string][]byte, data []byte) {
	t.Helper()
	probe := fmt.Sprintf(luaNonCanonicalPrelude+`print("announce=" .. hex(T.Announcement))
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
print("read=" .. tostring(T.AnnounceRead(vocab, inputs.announce, {}) and true or false))
`, ModuleName(unit))
	got := luaNonCanonicalOutput(t, files, probe, map[string][]byte{"announce": data})
	if got["announce"] != hex.EncodeToString(data) {
		t.Errorf("Announcement = %s, want %x", got["announce"], data)
	}
	if got["read"] != "true" {
		t.Errorf("AnnounceRead = %s, want true", got["read"])
	}
}

func luaNonCanonicalUnit(t *testing.T, name, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".schema")
	writeFile(t, path, schema)
	return luarun.LoadUnit(t, path)
}

// testLuaNodeTableOutOfPlace holds the misplaced node table (§3.1) on its own,
// as internal/tablewire's TestVariableReadRefusesNodeTableOutOfPlace does: the
// node-table field moved to the front of the root body and the id table
// renumbered to the first-use order of that body, so the table's place is the
// one thing wrong. Every variable-form read entry refuses it, as the engine
// does, and the writer's own bytes, the node table last, read.
func testLuaNodeTableOutOfPlace(t *testing.T) {
	t.Parallel()
	unit := luaNonCanonicalUnit(t, "Probe", `package nodeplace
table Node { v int32 }
table Root
{
    a    int32
    head *Node
}
`)
	m := tabletext.NewModel(unit)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"a":1,"head":{"v":2}}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	canonical, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	count := int(binary.LittleEndian.Uint64(canonical[len(canonical)-8:]))
	first := len(canonical) - count*8 - 8
	body := canonical[1:first]
	ids := make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(canonical[first+i*8:])
	}
	// The writer's body: a (ref 1, i32), head (ref 2, node index 2), then the
	// node table (ref 3, kind 12, L, records), then the terminator.
	if len(ids) != 5 || ids[0] != ir.TableWireId("a") || ids[1] != ir.TableWireId("head") || ids[2] != ir.TableNodeWireId ||
		len(body) < 12 || body[0] != 1 || body[1] != byte(ir.TableKindI32) || body[6] != 2 || body[7] != byte(ir.TableKindPointer) ||
		body[8] != 2 || body[9] != 3 || body[10] != byte(ir.TableKindString) || body[len(body)-1] != 0 {
		t.Fatalf("the writer's layout moved: ids=%x body=%x", ids, body)
	}
	field := func(ref byte, b []byte) []byte { return append([]byte{ref}, b[1:]...) }
	misplacedBody := append(field(1, body[9:len(body)-1]), field(2, body[0:6])...)
	misplacedBody = append(misplacedBody, field(3, body[6:9])...)
	misplacedBody = append(misplacedBody, 0)
	// The records name ids 4 and 5, which keep their slots.
	misplaced := append([]byte{ir.TableWireForm}, misplacedBody...)
	for _, id := range []uint64{ids[2], ids[0], ids[1], ids[3], ids[4]} {
		misplaced = binary.LittleEndian.AppendUint64(misplaced, id)
	}
	misplaced = binary.LittleEndian.AppendUint64(misplaced, uint64(len(ids)))

	files, err := Generate(unit)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	luaNonCanonicalVariable(t, unit, files, misplaced, misplaced, canonical, "refused")
	luaNonCanonicalVariable(t, unit, files, canonical, canonical, canonical, "read")
}

// testLuaUnionNoneElementReads is the control for §3.3 (c): the refusal is of
// a union FIELD written with arm reference 0, never of a None element of an
// array of unions, which is positional. The writer's batch carrying one reads
// under this reader's build version and under another.
func testLuaUnionNoneElementReads(t *testing.T) {
	t.Parallel()
	unit := luaNonCanonicalUnit(t, "Probe", `package messagenone

union Choice
{
    item uint8
}

fixed table Root
{
    tick    uint8
    choice  Choice
    choices [..2]Choice
}
`)
	m := tabletext.NewModel(unit)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	luaNonCanonicalMessage(t, unit, files, "Root", tablewire.Announce(unit), batch, "read", "read")
}

// testLuaUndeclaredFieldSameBuild holds §3.3 (b): under the reader's own build
// version every field the sender's record declares is one this record
// declares, so a field the record does not declare is refused there; under
// another build version it is an unknown field, skipped and counted (§4).
func testLuaUndeclaredFieldSameBuild(t *testing.T) {
	t.Parallel()
	sender := luaNonCanonicalUnit(t, "Sender", `package undeclared
fixed table One
{
    first  uint8
    hidden uint8
    last   uint8
}
`)
	receiver := luaNonCanonicalUnit(t, "Receiver", `package undeclared
fixed table One
{
    first uint8
    last  uint8
}
`)
	m := tabletext.NewModel(sender)
	inst := m.New(m.Lookup("One"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"first": 1, "hidden": 2, "last": 3}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(receiver)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	probe := fmt.Sprintf(luaNonCanonicalPrelude+`local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, {}), "announcement refused")
local function read(label)
  local values = T.LoadMessages("One", vocab, inputs.wire, {})
  if values == nil or #values == 0 then print(label .. "=refused") else print(label .. "=read:" .. values[1].first .. ":" .. values[1].last) end
end
read("other")
vocab.buildVersion = T.BuildVersion
read("same")
`, ModuleName(receiver))
	got := luaNonCanonicalOutput(t, files, probe, map[string][]byte{"announce": tablewire.Announce(sender), "wire": batch})
	if got["other"] != "read:1:3" {
		t.Errorf("other build: %s, want read:1:3", got["other"])
	}
	if got["same"] != "refused" {
		t.Errorf("same build: %s, want refused", got["same"])
	}
}

// testLuaUnknownUnionFieldNone holds §3.3 (c) where the reader cannot name the
// field: the writer elides None at every union FIELD, so an explicit arm
// reference 0 is refused on the skip path too, at another build version, as
// the engine refuses it.
func testLuaUnknownUnionFieldNone(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "message-one-encoding")
	receiver := luaNonCanonicalUnit(t, "Receiver", `package messageone

fixed table One
{
    mode  uint8 = 7
    count uint8
}
`)
	announcement := readFile(t, filepath.Join(vectors, "announcement.bin"))
	for _, file := range []string{"canonical.bin", "union-none.bin"} {
		data := readFile(t, filepath.Join(vectors, file))
		m := tabletext.NewModel(receiver)
		var vocab tablewire.Vocabulary
		if err := vocab.AnnounceRead(announcement, &tabletext.Report{}); err != nil {
			t.Fatal(err)
		}
		ok, err := tablewire.DecodeMessage(m, m.New(m.Lookup("One")), data, &vocab, &tabletext.Report{})
		engine := "read"
		if !ok || err != nil {
			engine = "refused"
		}
		want := "read"
		if file == "union-none.bin" {
			want = "refused"
		}
		if engine != want {
			t.Fatalf("%s: the engine's verdict is %s, want %s", file, engine, want)
		}
		files, err := Generate(receiver)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		probe := fmt.Sprintf(luaNonCanonicalPrelude+`local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, {}), "announcement refused")
local values = T.LoadMessages("One", vocab, inputs.wire, {})
if values == nil or #values == 0 then print("verdict=refused") else print("verdict=read") end
`, ModuleName(receiver))
		got := luaNonCanonicalOutput(t, files, probe, map[string][]byte{"announce": announcement, "wire": data})
		if got["verdict"] != want {
			t.Errorf("%s: %s, want %s", file, got["verdict"], want)
		}
	}
}
