package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaRetainUnknownMessages is the message-form leg of RETAIN-UNKNOWN
// (docs/SPEC-TABLES.md §6.6, §3.3): a newer unit writes a batch whose bodies
// carry a field the older reader cannot name; the older reader loads it with
// retention against the connection's own (newer) vocabulary and writes the
// result back through the variable form's SaveRetain (§6.6, retention crosses
// from the message form to the variable form). The written-back variable form
// must equal the newer unit's own variable save of the same value byte for
// byte, and the retained record must be placed.
func TestLuaRetainUnknownMessages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	newerPath := filepath.Join(dir, "newer.schema")
	olderPath := filepath.Join(dir, "older.schema")
	newer := `package probe
fixed table Child { n int32 }
fixed table Dummy { tag int32 }
table Msg { c Child
  x int32
  extra string(8) }
`
	older := `package probe
fixed table Child { n int32 }
fixed table Dummy { tag int32 }
table Msg { c Child
  x int32 }
`
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
	probe := `local Tn = require("NewerTables")
local To = require("OlderTables")
-- the newer unit writes the batch and the variable-form expectation
local v = Tn.NewMsg()
v.c = Tn.NewChild(); v.c.n = 7
v.x = 42
v.extra = "hi"
local message = require("message")
local save = require("save")
local vocab_n = {}
for i = 1, Tn.MessageEntries do vocab_n[i] = {} end
local rep = {}
assert(Tn.AnnounceRead(vocab_n, Tn.Announcement, rep))
local G = message.saveMessages(Tn, "Msg", vocab_n, { v })
assert(G ~= nil, "newer message save refused")
local E = save.Save(Tn, "Msg", v)
assert(E ~= nil, "newer variable save refused")
-- the older reader resolves the newer connection's vocabulary
local vocab = {}
for i = 1, Tn.MessageEntries do
  local e = Tn.MessageVocabulary[i]
  vocab[i] = { id = e.id, kind = e.kind, packing = e.packing, bits = e.bits, max = e.max, min = e.min, elem = e.elem }
end
vocab.count = Tn.MessageEntries
vocab.refBits = Tn.MessageRefBits
vocab.announced = true
local retainmsg = require("retainmessage")
local retain = require("retain")
local stores = { { list = {} } }
local rp = { malformed = false, refused = 0, unknown = 0, retained = 0, retainLost = 0 }
local vals = retainmsg.LoadRetainMessages(To, "Msg", vocab, G, stores, rp)
if vals == nil or vals[1] == nil then print("load refused") os.exit(1) end
local O = retain.SaveRetain(To, "Msg", vals[1], stores[1], rp)
if O == nil then print("save refused") os.exit(1) end
print("same:" .. tostring(O == E) .. ":" .. rp.retained .. ":" .. rp.retainLost .. ":" .. #O .. ":" .. #E)
`
	if err := os.WriteFile(filepath.Join(run, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, run, "main.lua", nil)), "\n")
	if !strings.HasPrefix(out, "same:true:1:0:") {
		t.Fatalf("message retain round trip %q, want same:true:1:0", out)
	}
}
