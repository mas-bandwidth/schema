package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MESSAGE FORM'S TESTS on the Lua leg (docs/SPEC-TABLES.md §3.3; the
// corpus is testdata/conformance/tables/MANIFEST.txt's message and connection
// rows): for every message row the connection's announcement is read, the
// batch is loaded against it, saved, and byte-compared with the message wire.
// The negative control patches the emitted runtime so the batch count is read
// one short, and requires the comparison to go red.

// manifestConn is one connection row of the manifest: the unit, the build
// version and the announcement wire file.
type manifestConn struct {
	key  string
	unit string
	ann  string
}

// manifestMsg is one message row: the name, the connection key, the root and
// the message-form wire.
type manifestMsg struct {
	name string
	conn string
	root string
	wire string
}

// manifestMessageData reads the manifest for unit, connection and message rows,
// returning the unit key -> schema paths map, the connection rows, and the
// message rows.
func manifestMessageData(t *testing.T, path string) (map[string][]string, map[string]manifestConn, []manifestMsg) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	conns := map[string]manifestConn{}
	var msgs []manifestMsg
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "unit":
			if len(f) >= 3 {
				units[f[1]] = f[2:]
			}
		case "connection":
			if len(f) >= 5 {
				conns[f[1]] = manifestConn{key: f[1], unit: f[2], ann: f[4]}
			}
		case "message":
			if len(f) >= 6 && f[5] != "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
				msgs = append(msgs, manifestMsg{name: f[1], conn: f[2], root: f[3], wire: f[5]})
			}
		}
	}
	return units, conns, msgs
}

// messageProbe is the Lua main for one message row: it reads the connection's
// announcement into a resolved vocabulary, loads the batch against it, saves
// it, and byte-compares with the message wire.
func messageProbe(module, root string) string {
	return `local T = require("` + module + `")
local inputs = require("inputs")
local report = { malformed = false, refused = 0 }
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local ok = T.AnnounceRead(vocab, inputs.announce, report)
assert(ok, "announce refuted or damaged")
local values = T.LoadMessages("` + root + `", vocab, inputs.batch, report)
assert(values ~= nil, "batch refused")
local saved = T.SaveMessages("` + root + `", vocab, values)
assert(saved == inputs.batch, "batch round-trip differs from the message wire")
print("message-ok")
`
}

// TestLuaMessageFormMatchesTheGoldens drives the conformance message corpus:
// for every message row it reads the connection's announcement, loads the
// batch against it, saves, and byte-compares. The count of rows compared is
// printed.
func TestLuaMessageFormMatchesTheGoldens(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units, conns, msgs := manifestMessageData(t, manifest)
	count := 0
	for _, msg := range msgs {
		conn, ok := conns[msg.conn]
		if !ok {
			t.Fatalf("message %s names unknown connection %s", msg.name, msg.conn)
		}
		paths := units[conn.unit]
		if len(paths) == 0 {
			t.Fatalf("connection %s names unknown unit %s", msg.conn, conn.unit)
		}
		schemaPaths := make([]string, len(paths))
		for i, p := range paths {
			schemaPaths[i] = filepath.Join(repo, p)
		}
		unit := luarun.LoadUnit(t, schemaPaths...)
		if ir.VariableTables(unit)[msg.root] {
			continue // a variable table carries no message form (§3.3)
		}
		files, err := Generate(unit)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		announce := readFile(t, filepath.Join(repo, conn.ann))
		batch := readFile(t, filepath.Join(repo, msg.wire))
		luarun.WriteFiles(t, dir, map[string][]byte{"inputs.lua": nil})
		writeFile(t, filepath.Join(dir, "main.lua"), messageProbe(ModuleName(unit), msg.root))
		luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})
		count++
	}
	fmt.Printf("message rows compared: %d\n", count)
	if count == 0 {
		t.Fatal("no message rows found in the manifest")
	}
	assertDamagedBatch(t, repo, units, conns)
}

// assertDamagedBatch holds §4: damage in body 2 discards the whole batch.
// The pinned input retains its original mutation; no partial body is usable.
func assertDamagedBatch(t *testing.T, repo string, units map[string][]string, conns map[string]manifestConn) {
	t.Helper()
	conn, ok := conns["backend_conn"]
	if !ok {
		t.Fatal("the manifest names no backend_conn connection")
	}
	paths := units[conn.unit]
	if len(paths) == 0 {
		t.Fatalf("connection %s names unknown unit %s", conn.key, conn.unit)
	}
	schemaPaths := make([]string, len(paths))
	for i, p := range paths {
		schemaPaths[i] = filepath.Join(repo, p)
	}
	unit := luarun.LoadUnit(t, schemaPaths...)
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	announce := readFile(t, filepath.Join(repo, conn.ann))
	batch := readFile(t, filepath.Join(repo, "testdata/wire/tables/backend_batch_damaged_second_message.bin"))
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(unit)+`")
local inputs = require("inputs")
local report = { malformed = false, refused = 0 }
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, report))
local values = T.LoadMessages("LoginRequest", vocab, inputs.batch, report)
assert(values ~= nil, "a damaged batch should return an empty result")
assert(#values == 0, "damage in body 2 delivered partial bodies")
assert(report.malformed == true, "damage in body 2 did not report malformed")
print("damage-ok")
`)
	luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})
}

// TestLuaMessageFormNegativeControl patches the emitted runtime in memory (one
// anchor, matched exactly once) so the batch count is read one short, and
// requires the byte comparison to go red.
func TestLuaMessageFormNegativeControl(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units, conns, msgs := manifestMessageData(t, manifest)
	if len(msgs) == 0 {
		t.Fatal("no message rows found in the manifest")
	}
	msg := msgs[0]
	conn := conns[msg.conn]
	paths := units[conn.unit]
	if len(paths) == 0 {
		t.Fatalf("connection %s names unknown unit %s", msg.conn, conn.unit)
	}
	schemaPaths := make([]string, len(paths))
	for i, p := range paths {
		schemaPaths[i] = filepath.Join(repo, p)
	}
	unit := luarun.LoadUnit(t, schemaPaths...)
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	files["message.lua"] = patchMessageRuntime(files["message.lua"])
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	announce := readFile(t, filepath.Join(repo, conn.ann))
	batch := readFile(t, filepath.Join(repo, msg.wire))
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(unit)+`")
local inputs = require("inputs")
local ok2 = pcall(function()
  local report = { malformed = false, refused = 0 }
  local vocab = {}
  for i = 1, T.MessageEntries do vocab[i] = {} end
  local ok = T.AnnounceRead(vocab, inputs.announce, report)
  assert(ok, "announce refused or damaged")
  local values = T.LoadMessages("`+msg.root+`", vocab, inputs.batch, report)
  local saved = T.SaveMessages("`+msg.root+`", vocab, values)
  assert(saved == inputs.batch, "batch round-trip differs from the message wire")
end)
if ok2 then print("green") else print("red") end
`)
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})
	if !strings.Contains(string(out), "red") {
		t.Fatalf("the sabotaged batch count stayed green; the negative control proves nothing:\n%s", out)
	}
}

// patchMessageRuntime sabotages the batch count read so the comparison goes
// red. The anchor is the runtime's body-count read, matched exactly once.
func patchMessageRuntime(runtime []byte) []byte {
	anchor := "local count = string.byte(data, 2) + 1"
	repl := "local count = string.byte(data, 2)"
	s := string(runtime)
	if strings.Count(s, anchor) != 1 {
		return runtime
	}
	return []byte(strings.Replace(s, anchor, repl, 1))
}

// TestLuaAnnouncementReadsTheEngineBytes pins the announcement reader to the
// ENGINE's announcement, not the unit's compile-time constant alone
// (docs/SPEC-TABLES.md §3.3): the corpus announcement carries the same
// vocabulary at another build version and must read, and each receiver bound
// with the announcement one past it refuses vocabulary_too_large. The corpus
// is Go's and is never edited; the input is read from it as committed.
func TestLuaAnnouncementReadsTheEngineBytes(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	unit := luarun.LoadUnit(t, filepath.Join(repo, "tables", "examples"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	announce := readFile(t, filepath.Join(repo, "internal", "ci", "hostilecorpus", "corpus", "inputs", "announcement.bin"))
	vocab := ir.TableVocabulary(unit)
	vocabBytes := len(ir.TableVocabularyBytes(vocab))
	entries := len(vocab)
	if vocabBytes == 0 || entries == 0 {
		t.Fatal("the examples unit announces no vocabulary")
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local inputs = require("inputs")
local function report() return { malformed = false, refused = 0 } end

local vocab = {}
local r = report()
assert(T.AnnounceRead(vocab, inputs.announce, r), "the engine announcement was refused")
assert(vocab.announced == true, "the engine announcement set no vocabulary")
assert(vocab.count == %d, "announced entries: " .. tostring(vocab.count))

local atByteBound = {}
local rb = report()
assert(T.AnnounceRead(atByteBound, inputs.announce, rb, %d),
  "the announcement at its byte bound was refused")
local overBytes = {}
local ro = report()
assert(not T.AnnounceRead(overBytes, inputs.announce, ro, %d),
  "the announcement one past the byte bound was accepted")
assert(ro.reason == "vocabulary_too_large", "byte bound reason: " .. tostring(ro.reason))

local atEntryBound = {}
local re = report()
assert(T.AnnounceRead(atEntryBound, inputs.announce, re, nil, %d),
  "the announcement at its entry bound was refused")
local overEntries = {}
local rx = report()
assert(not T.AnnounceRead(overEntries, inputs.announce, rx, nil, %d),
  "the announcement one past the entry bound was accepted")
assert(rx.reason == "vocabulary_too_large", "entry bound reason: " .. tostring(rx.reason))
print("announce-ok")
`, ModuleName(unit), entries, vocabBytes, vocabBytes-1, entries, entries-1))
	luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce})
}

// varMessageSchema is one unit holding both classes: a fixed root, a variable
// table with no pointer, and a variable table pointing at another.
const varMessageSchema = `package probe

fixed table Fixedroot
{
    x int32
}

table Varplain
{
    x int32
}

table Varleaf
{
    x int32
}

table Varroot
{
    leaf *Varleaf
}
`

// TestLuaMessageFormIsFixedTablesOnly holds §3.3's class rule on the Lua leg:
// MessageRoots lists the fixed root and no variable table, and the three batch
// verbs refuse a variable root by name before reading or writing a byte, while
// the fixed root's batch still measures.
func TestLuaMessageFormIsFixedTablesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(varMessageSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	module := string(files[ModuleName(u)+".lua"])
	if !strings.Contains(module, `_M.MessageRoots["Fixedroot"] = true`) {
		t.Error("MessageRoots does not list the fixed root Fixedroot")
	}
	for _, name := range []string{"Varplain", "Varleaf", "Varroot"} {
		if strings.Contains(module, `_M.MessageRoots["`+name+`"]`) {
			t.Errorf("MessageRoots lists the variable table %s", name)
		}
	}
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(u)+`")
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local function refused(name, f)
  local ok, err = pcall(f)
  assert(not ok, name .. " was not refused")
  assert(tostring(err):find("Varplain", 1, true), name .. " was refused without naming the table: " .. tostring(err))
end
refused("LoadMessages", function() return T.LoadMessages("Varplain", vocab, "\2\0", {}) end)
refused("LoadMessagesTolerant", function() return T.LoadMessagesTolerant("Varplain", vocab, "\2\0", {}) end)
refused("MeasureMessages", function() return T.MeasureMessages("Varplain", vocab, { T.NewVarplain() }) end)
refused("SaveMessages", function() return T.SaveMessages("Varplain", vocab, { T.NewVarplain() }) end)
assert(T.MeasureMessages("Fixedroot", vocab, { T.NewFixedroot() }) > 0, "the fixed root's batch did not measure")
print("fixed-only-ok")
`)
	if out := string(luarun.Run(t, dir, "main.lua", nil)); !strings.Contains(out, "fixed-only-ok") {
		t.Fatalf("the probe did not finish: %q", out)
	}
}
