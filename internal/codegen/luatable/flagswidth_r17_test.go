package luatable

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4; R17, Glenn
// 2026-11-22: "The newer reader can read old data, but never the other way").
// W is the flags declaration wire width (the variant count, or K under
// `| max = K`). Every table read refuses a mask with a bit at or above W as
// out_of_range: the variable load, the fixed record and the fixed file, and the
// message read of a newer writer that announces a wider mask. Every table
// writer refuses to write such a mask, in every build (docs/SPEC.md §5.1), so a
// same-build round trip never writes what its own reader refuses.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

const r17Schema = `package rseventeen

flags Perms { %s }
flags Wide | max = 40 { A }

table Doc
{
    lead  uint32
    perms Perms
    mask  Wide
}

fixed table Rec
{
    lead  uint32
    perms Perms
    mask  Wide
    trail uint32
}
`

func r17Unit(t *testing.T, perms string) (map[string][]byte, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "r17.schema"), fmt.Sprintf(r17Schema, perms))
	unit := luarun.LoadUnit(t, filepath.Join(dir, "r17.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	return files, ModuleName(unit)
}

const r17Hex = `local function hex(s)
  return (s:gsub(".", function(c) return string.format("%%02x", string.byte(c)) end))
end
`

func r17Wires(t *testing.T, out string) map[string][]byte {
	t.Helper()
	kv := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = unhexTestBytes(t, v)
		}
	}
	return kv
}

func TestLuaFlagsMaskHeldToItsWidth(t *testing.T) {
	t.Parallel()
	narrow, narrowMod := r17Unit(t, "Read, Write")
	wide, wideMod := r17Unit(t, "Read, Write, Exec")

	// The narrow build writes lawful files carrying a sentinel in the mask slot
	// (W = 40), and its four writers each answer a mask past W.
	writeDir := t.TempDir()
	luarun.WriteFiles(t, writeDir, narrow)
	writeFile(t, filepath.Join(writeDir, "main.lua"), fmt.Sprintf(`local T = require(%q)
local save = require("save")
`+r17Hex+`local d = T.NewDoc(); d.lead = 7; d.perms = 3; d.mask = 0x123456789a
local r = T.NewRec(); r.lead = 7; r.perms = 3; r.mask = 0x123456789a; r.trail = 9
print("doc=" .. hex(assert(save.Save(T, "Doc", d))))
print("rec=" .. hex(assert(T.FixedSave("Rec", { r }))))
print("one=" .. hex(assert(T.WriteFixed("Rec", r))))
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, T.Announcement, {}), "AnnounceRead")
local function verdict(b, why)
  if b == nil then return "refused " .. tostring(why) end
  return "wrote"
end
for _, c in ipairs({ { "perms", 4 }, { "mask", 1 << 40 }, { "mask", 1 << 63 } }) do
  local d2 = T.NewDoc(); d2[c[1]] = c[2]
  local r2 = T.NewRec(); r2[c[1]] = c[2]
  local tag = c[1] .. ":" .. string.format("%%x", c[2])
  print("Save " .. tag .. " " .. verdict(save.Save(T, "Doc", d2)))
  print("FixedSave " .. tag .. " " .. verdict(T.FixedSave("Rec", { r2 })))
  print("WriteFixed " .. tag .. " " .. verdict(T.WriteFixed("Rec", r2)))
  print("SaveMessages " .. tag .. " " .. verdict(T.SaveMessages("Rec", vocab, { r2 })))
end
`, narrowMod))
	wrote := string(luarun.Run(t, writeDir, "main.lua", nil))
	for _, tag := range []string{"perms:4", "mask:10000000000", "mask:8000000000000000"} {
		for _, w := range []string{"Save", "FixedSave", "WriteFixed", "SaveMessages"} {
			line := w + " " + tag + " refused "
			if !strings.Contains(wrote, line) {
				t.Errorf("want %q (a writer writes no mask its reader refuses); the probe printed:\n%s", line, wrote)
			}
		}
	}
	files := r17Wires(t, wrote)

	// The newer build (Perms has a third flag) writes a message batch with the
	// new bit set and one without it.
	msgDir := t.TempDir()
	luarun.WriteFiles(t, msgDir, wide)
	writeFile(t, filepath.Join(msgDir, "main.lua"), fmt.Sprintf(`local T = require(%q)
`+r17Hex+`local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, T.Announcement, {}), "AnnounceRead")
local inside = T.NewRec(); inside.perms = 3
local past = T.NewRec(); past.perms = 5
print("announce=" .. hex(T.Announcement))
print("msg_inside=" .. hex(assert(T.SaveMessages("Rec", vocab, { inside }))))
print("msg_past=" .. hex(assert(T.SaveMessages("Rec", vocab, { past }))))
`, wideMod))
	for k, v := range r17Wires(t, string(luarun.Run(t, msgDir, "main.lua", nil))) {
		files[k] = v
	}

	var slot [8]byte
	binary.LittleEndian.PutUint64(slot[:], 0x123456789a)
	inputs := map[string][]byte{}
	for _, name := range []string{"doc", "rec", "one"} {
		b := files[name]
		if n := bytes.Count(b, slot[:]); n != 1 {
			t.Fatalf("%s: the mask slot appears %d times, want 1", name, n)
		}
		at := bytes.Index(b, slot[:])
		for suffix, value := range map[string]uint64{"inside": 0x123456789a, "past40": 0x123456789a | 1<<40, "bit63": 1 << 63} {
			out := append([]byte(nil), b...)
			binary.LittleEndian.PutUint64(out[at:at+8], value)
			inputs[name+"_"+suffix] = out
		}
	}
	for _, k := range []string{"announce", "msg_inside", "msg_past"} {
		if len(files[k]) == 0 {
			t.Fatalf("the newer build printed no %s wire", k)
		}
		inputs[k] = files[k]
	}

	readDir := t.TempDir()
	luarun.WriteFiles(t, readDir, narrow)
	writeFile(t, filepath.Join(readDir, "main.lua"), fmt.Sprintf(`local T = require(%q)
local load = require("load")
local I = require("inputs")
local function var(buf)
  local rep = {}
  local v = load.Load(T, "Doc", buf, rep)
  if v == nil then return "refused " .. tostring(rep.reason) end
  return string.format("read %%x", v.mask)
end
local function one(buf)
  local ok, v = pcall(T.ReadFixed, "Rec", buf)
  if not ok or v == nil then return "refused " .. tostring(v) end
  return string.format("read %%x", v.mask)
end
local function file(buf)
  local ok, rows = pcall(T.FixedLoad, "Rec", buf)
  if not ok or rows == nil then return "refused " .. tostring(rows) end
  return string.format("read %%x", rows[1].mask)
end
for _, n in ipairs({ "inside", "past40", "bit63" }) do
  print("Load " .. n .. " " .. var(I["doc_" .. n]))
  print("ReadFixed " .. n .. " " .. one(I["one_" .. n]))
  print("FixedLoad " .. n .. " " .. file(I["rec_" .. n]))
end
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local ar = {}
assert(T.AnnounceRead(vocab, I.announce, ar), "AnnounceRead: " .. tostring(ar.reason))
for _, n in ipairs({ "msg_inside", "msg_past" }) do
  local rep = {}
  local got = T.LoadMessages("Rec", vocab, I[n], rep)
  if got ~= nil and #got == 1 then
    print("LoadMessages " .. n .. " read " .. got[1].perms)
  else
    print("LoadMessages " .. n .. " refused " .. tostring(rep.reason))
  end
end
`, narrowMod))
	read := string(luarun.Run(t, readDir, "main.lua", inputs))
	want := []string{
		"Load inside read 123456789a",
		"Load past40 refused out_of_range",
		"Load bit63 refused out_of_range",
		"ReadFixed inside read 123456789a",
		"ReadFixed past40 refused out_of_range",
		"ReadFixed bit63 refused out_of_range",
		"FixedLoad inside read 123456789a",
		"FixedLoad past40 refused out_of_range",
		"FixedLoad bit63 refused out_of_range",
		"LoadMessages msg_inside read 3",
		"LoadMessages msg_past refused out_of_range",
	}
	for _, line := range want {
		if !strings.Contains(read, line+"\n") {
			t.Errorf("want %q; the probe printed:\n%s", line, read)
		}
	}
}
