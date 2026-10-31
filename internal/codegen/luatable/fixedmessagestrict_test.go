package luatable

// TestLuaFixedAndMessageReadsAreStrict pins docs/SPEC-TABLES.md §6.5's rule
// for the fixed and message forms on the Lua leg: the plain ReadFixed and
// LoadMessages are STRICT and the tolerant twins keep BASE's answers. The
// oracle is the generated Go read (internal/codegen/gotable, whose
// TestGoDefaultReadIsStrict pins the same rows): a clamped record refuses the
// plain name, a damaged one refuses both, and a refused LoadMessages writes no
// record. The message goldens of the conformance manifest read clean under the
// plain name.

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
	"strings"
	"testing"
)

func strictVerdict(err error) string {
	if err == nil { return "clean" }
	s := err.Error()
	if i := strings.IndexByte(s, ':'); i >= 0 { return s[:i] }
	return s
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
		if n := FixFixedLoad([]Fix{got}, data, plan, &r); n < 0 { return strictVerdict(r.Err()) }
		return "clean"
	}
	fmt.Printf("ORACLE\tfixed_clean\t%s\n", fixed(file))
	clamped := append([]byte(nil), file...)
	body := clamped[len(clamped)-FixFixedRecordBytes+8:]
	body[0], body[1], body[2], body[3] = 100, 0, 0, 0
	fmt.Printf("ORACLE\tfixed_clamped\t%s\n", fixed(clamped))
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
		if _, ok := FixLoadMessages(got, &vocab, data, &r); !ok {
			if r.Err() != nil { return strictVerdict(r.Err()) }
			return "refused"
		}
		return "clean"
	}
	fmt.Printf("ORACLE\tmessage_clean\t%s\n", message(msg))
	fmt.Printf("ORACLE\tmessage_clamped\t%s\n", message(forged))
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
local function verdict(err)
  local n = tostring(err)
  local i = n:find(":", 1, true)
  if i then n = n:sub(1, i - 1) end
  return n
end
local function strict(reader, root, data)
  local ok, err = pcall(reader, root, data)
  if ok then return "clean" end
  return verdict(err)
end
local out = {}
local v = T.NewFix(); v.n = 3; v.m = 4
local file = T.WriteFixed("Fix", v)
out.fixed_clean = strict(T.ReadFixed, "Fix", file)
local clamped = file:sub(1, #file - 8) .. string.char(100, 0, 0, 0) .. file:sub(#file - 3)
out.fixed_clamped = strict(T.ReadFixed, "Fix", clamped)
out.fixed_truncated = strict(T.ReadFixed, "Fix", file:sub(1, #file - 1))
-- the tolerant twin keeps BASE's answer: the unchecked value rides
local tv = T.ReadFixedTolerant("Fix", clamped)
out.tolerant_fixed_clamped = tostring(tv.n)

local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local report = {}
assert(T.AnnounceRead(vocab, T.Announcement, report))
local zero = T.NewFix(); zero.n = 0; zero.m = 4
local msg = assert(T.SaveMessages("Fix", vocab, { zero }))
local forged = msg:sub(1, 2) .. string.char((msg:byte(3) & ~0x78) | 0x78) .. msg:sub(4)
local function message(reader, data)
  local r = {}
  local values = reader("Fix", vocab, data, r)
  if values == nil then return "malformed" end
  if (r.clamped or 0) > 0 then return "clamped" end
  if r.malformed then return "malformed" end
  if (r.kindMismatch or 0) > 0 then return "kind_mismatch" end
  if (r.blobShared or 0) > 0 then return "blob_shared" end
  return "clean"
end
out.message_clean = message(T.LoadMessages, msg)
out.message_clamped = message(T.LoadMessages, forged)
out.message_truncated = message(T.LoadMessages, msg:sub(1, 1))
-- the tolerant twin keeps BASE's answer, and a refused strict read wrote nothing
local tr = {}
local kept = T.LoadMessagesTolerant("Fix", vocab, forged, tr)
out.tolerant_message_clamped = tostring(#kept) .. ":" .. tostring(kept[1] and kept[1].n)
local sr = {}
local refused = T.LoadMessages("Fix", vocab, forged, sr)
out.strict_message_refused_count = tostring(#refused)
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
	for _, name := range []string{
		"fixed_clean", "fixed_clamped",
		"message_clean", "message_clamped", "message_truncated",
	} {
		if lua[name] != oracle[name] {
			t.Errorf("%s: Lua says %q, the generated Go strict read says %q", name, lua[name], oracle[name])
		}
	}
	// A damaged fixed file is refused by both names; the Lua leg's fixed
	// runtime names the truncated file `malformed` where Go names it
	// `truncated` (the same refusal, §3.4's own wording on each leg).
	if lua["fixed_truncated"] == "clean" || oracle["fixed_truncated"] == "clean" {
		t.Errorf("fixed_truncated: Lua says %q, the generated Go strict read says %q; both must refuse",
			lua["fixed_truncated"], oracle["fixed_truncated"])
	}
	// THE TOLERANT TWINS KEEP BASE'S ANSWERS (docs/SPEC-TABLES.md §6.5): the
	// fixed read never clamped and the message read never clamped either, so
	// the forged rows keep the value they stated. A strict refusal writes no
	// record.
	if lua["tolerant_fixed_clamped"] != "100" {
		t.Errorf("ReadFixedTolerant answered n=%s for the clamped record, want BASE's 100", lua["tolerant_fixed_clamped"])
	}
	if lua["tolerant_message_clamped"] != "1:-1" {
		t.Errorf("LoadMessagesTolerant answered %q for the clamped batch, want BASE's 1:-1", lua["tolerant_message_clamped"])
	}
	if lua["strict_message_refused_count"] != "0" {
		t.Errorf("a refused LoadMessages left %s record(s) written, want 0", lua["strict_message_refused_count"])
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
assert(not report.malformed, "the plain ReadMessages reported damage on a golden")
print("golden-ok")
`)
		announce := readFile(t, filepath.Join(repo, conn.ann))
		batch := readFile(t, filepath.Join(repo, msg.wire))
		if out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})); !strings.Contains(out, "golden-ok") {
			t.Errorf("message golden %s did not read clean: %q", msg.name, out)
		}
	}
}
