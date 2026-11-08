package luatable

// TestLuaFixedAndMessageReadsAreStrict pins docs/SPEC-TABLES.md §6.5's rule
// for the fixed and message forms on the Lua leg: ReadFixed and LoadMessages
// are the one read (docs/plans/strict-only.md). The oracle is the generated Go
// read (internal/codegen/gotable): an out-of-range record and a truncated one
// refuse, a clean one reads; only the verdict is compared. The message goldens
// of the conformance manifest read clean.

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

const fixedMessageStrictSchema = `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
`

// fixedMessageStrictGoOracle generates the Go unit and returns case -> the
// strict verdict's name (docs/SPEC-TABLES.md §6.5).
func fixedMessageStrictGoOracle(t *testing.T, src string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schema, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	maps.Copy(goFiles, tables)
	goFiles["go.mod"] = []byte("module strictprobe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.1\n")
	goFiles["strictprobe_test.go"] = []byte(fixedMessageStrictGoProbe)
	probeDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(probeDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestFixedMessageStrictOracle$", "-v", ".")
	cmd.Dir = probeDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOSUMDB=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go strict read: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "ORACLE\t") {
			f := strings.Split(line, "\t")
			if len(f) == 3 {
				got[f[1]] = f[2]
			}
		}
	}
	if len(got) == 0 {
		t.Fatalf("the Go oracle answered no row:\n%s", out)
	}
	return got
}

const fixedMessageStrictGoProbe = `package probe

import (
	"fmt"
	"testing"
)

func strictVerdict(err error) string {
	if err == nil { return "read" }
	return "refused"
}

func TestFixedMessageStrictOracle(t *testing.T) {
	v := Fix{N: 3, M: 4}
	file := make([]byte, FixFixedMeasure(1))
	if FixFixedSave([]Fix{v}, file) != int64(len(file)) { t.Fatal("fixed save") }
	plan := make([]TableFixedEntry, 256)
	fixed := func(data []byte) string {
		var got Fix
		FixReset(&got)
		var r TableReport
		if n := FixFixedLoad([]Fix{got}, data, plan, &r); n < 0 { return "refused" }
		return strictVerdict(r.Err())
	}
	fmt.Printf("ORACLE\tfixed_clean\t%s\n", fixed(file))
	clamped := append([]byte(nil), file...)
	body := clamped[len(clamped)-FixFixedRecordBytes+8:]
	body[0], body[1], body[2], body[3] = 100, 0, 0, 0
	fmt.Printf("ORACLE\tfixed_outOfRange\t%s\n", fixed(clamped))
	fmt.Printf("ORACLE\tfixed_truncated\t%s\n", fixed(file[:len(file)-1]))

	var vocab TableVocabulary
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	announce := make([]byte, AnnounceMeasure())
	Announce(announce)
	var ar TableReport
	if !AnnounceRead(&vocab, announce, &ar) { t.Fatalf("announce: %+v", ar) }
	zero := Fix{N: 0, M: 4}
	msg := make([]byte, FixMeasureMessages([]*Fix{&zero}, nil))
	if FixSaveMessages([]*Fix{&zero}, msg, nil) != int64(len(msg)) { t.Fatal("message save") }
	forged := append([]byte(nil), msg...)
	forged[2] = forged[2]&^0x78 | 0x78
	message := func(data []byte) string {
		got := []Fix{{N: 9, M: 9}}
		var r TableReport
		if _, ok := FixLoadMessages(got, &vocab, data, &r); !ok { return "refused" }
		return strictVerdict(r.Err())
	}
	fmt.Printf("ORACLE\tmessage_clean\t%s\n", message(msg))
	fmt.Printf("ORACLE\tmessage_outOfRange\t%s\n", message(forged))
	fmt.Printf("ORACLE\tmessage_truncated\t%s\n", message(msg[:1]))
}
`

func fixedMessageStrictLua(t *testing.T, src string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schema, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Lua: %v", err)
	}
	run := t.TempDir()
	luarun.WriteFiles(t, run, files)
	probe := `local T = require(` + fmt.Sprintf("%q", ModuleName(u)) + `)
local function strict(reader, root, data)
  local ok, v = pcall(reader, root, data)
  if ok and v ~= nil then return "read" end
  return "refused"
end
local out = {}
local v = T.NewFix(); v.n = 3; v.m = 4
local file = T.WriteFixed("Fix", v)
out.fixed_clean = strict(T.ReadFixed, "Fix", file)
local outOfRange = file:sub(1, #file - 8) .. string.char(100, 0, 0, 0) .. file:sub(#file - 3)
out.fixed_outOfRange = strict(T.ReadFixed, "Fix", outOfRange)
out.fixed_truncated = strict(T.ReadFixed, "Fix", file:sub(1, #file - 1))

local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local report = {}
assert(T.AnnounceRead(vocab, T.Announcement, report))
local zero = T.NewFix(); zero.n = 0; zero.m = 4
local msg = assert(T.SaveMessages("Fix", vocab, { zero }))
local forged = msg:sub(1, 2) .. string.char((msg:byte(3) & ~0x78) | 0x78) .. msg:sub(4)
local function message(data)
  local r = {}
  local ok, values = pcall(T.LoadMessages, "Fix", vocab, data, r)
  if not ok or values == nil or values[1] == nil or r.damaged then return "refused" end
  return "read"
end
out.message_clean = message(msg)
out.message_outOfRange = message(forged)
out.message_truncated = message(msg:sub(1, 1))
for k, val in pairs(out) do print("LUA\t" .. k .. "\t" .. tostring(val)) end
`
	if err := os.WriteFile(filepath.Join(run, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, run, "main.lua", nil)
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "LUA\t") {
			f := strings.Split(line, "\t")
			if len(f) == 3 {
				got[f[1]] = f[2]
			}
		}
	}
	return got
}

func TestLuaFixedAndMessageReadsAreStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	oracle := fixedMessageStrictGoOracle(t, fixedMessageStrictSchema)
	lua := fixedMessageStrictLua(t, fixedMessageStrictSchema)
	// One read (docs/plans/strict-only.md): the verdict is the contract, so
	// each row compares read/refused only, never a reason name.
	for _, name := range []string{
		"fixed_clean", "fixed_outOfRange", "fixed_truncated",
		"message_clean", "message_outOfRange", "message_truncated",
	} {
		if lua[name] != oracle[name] {
			t.Errorf("%s: Lua says %q, the generated Go read says %q", name, lua[name], oracle[name])
		}
	}
	for name, want := range map[string]string{
		"fixed_clean": "read", "fixed_outOfRange": "refused", "fixed_truncated": "refused",
		"message_clean": "read", "message_outOfRange": "refused", "message_truncated": "refused",
	} {
		if lua[name] != want {
			t.Errorf("%s: Lua says %q, want %q", name, lua[name], want)
		}
	}

	// THE MESSAGE GOLDENS READ CLEAN UNDER THE PLAIN NAME (§3.3, §6.5).
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units, conns, msgs := manifestMessageData(t, manifest)
	if len(msgs) == 0 {
		t.Fatal("no message rows found in the manifest")
	}
	for _, msg := range msgs {
		conn := conns[msg.conn]
		paths := make([]string, len(units[conn.unit]))
		for i, p := range units[conn.unit] {
			paths[i] = filepath.Join(repo, p)
		}
		u := luarun.LoadUnit(t, paths...)
		if ir.VariableTables(u)[msg.root] {
			continue // a variable table carries no message form (§3.3)
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("unit %s: %v", conn.unit, err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		luarun.WriteFiles(t, dir, map[string][]byte{"inputs.lua": nil})
		writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(u)+`")
local inputs = require("inputs")
local report = {}
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, report))
local values = T.LoadMessages("`+msg.root+`", vocab, inputs.batch, report)
assert(values ~= nil and #values > 0, "the plain ReadMessages refused a golden")
assert(not report.damaged, "the plain ReadMessages reported damage on a golden")
print("golden-ok")
`)
		announce := readFile(t, filepath.Join(repo, conn.ann))
		batch := readFile(t, filepath.Join(repo, msg.wire))
		if out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})); !strings.Contains(out, "golden-ok") {
			t.Errorf("message golden %s did not read clean: %q", msg.name, out)
		}
	}
}
