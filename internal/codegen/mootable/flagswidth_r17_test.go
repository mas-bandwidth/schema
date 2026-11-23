package mootable_test

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4; R17, Glenn
// 2026-11-22: "The newer reader can read old data, but never the other way").
// W is the flags declaration wire width (the variant count, or K under
// `| max = K`). Every table read refuses a mask with a bit at or above W as
// out_of_range: the variable load, the fixed load, and the message read of a
// newer writer that announces a wider mask. Every table writer refuses to
// write such a mask, in every build (docs/SPEC.md §5.1): moo has no assert.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const mooR17Schema = `package rseventeen

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

const mooR17Head = `local inputs = require("./inputs")
local T = require("./R17Table")
local St = require("./schema_tables")
local L = require("./table_load")
local S = require("./table_save")
local F = require("./table_fixed")
local MS = require("./table_message")
local serialize = require("./serialize")
local U64 = serialize.Uint64
local function hex(b: buffer): string
	local a = {}
	for i = 0, buffer.len(b) - 1 do a[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(a)
end
local sentinel = U64.new(0x3456789a, 0x12)
`

func mooR17Unit(t *testing.T, perms string) (*generatedUnit, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "R17.schema")
	if err := os.WriteFile(path, fmt.Appendf(nil, mooR17Schema, perms), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return writeUnit(t, u, files), tablewire.Announce(u)
}

func mooR17Wires(t *testing.T, out string) map[string][]byte {
	t.Helper()
	kv := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			b, err := hex.DecodeString(v)
			if err != nil {
				t.Fatalf("%s: %v", k, err)
			}
			kv[k] = b
		}
	}
	return kv
}

func TestMooFlagsMaskHeldToItsWidth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table readers and writers)")
	narrow, _ := mooR17Unit(t, "Read, Write")
	wide, wideAnnounce := mooR17Unit(t, "Read, Write, Exec")

	// The narrow build writes lawful files carrying a sentinel in the mask
	// slot (W = 40), and each of its writers answers a mask past W.
	wrote := narrow.run(t, mooR17Head+`local doc = St.lookup("Doc")
local rec = St.lookup("Rec")
local spec = F.lookup("Rec") :: any
local d = T.NewDoc(); d.Lead = 7; d.Perms = U64.fromNumber(3); d.Mask = sentinel
local r = T.NewRec(); r.Lead = 7; r.Perms = U64.fromNumber(3); r.Mask = sentinel; r.Trail = 9
local db = S.save(doc, d)
assert(db ~= nil, "the lawful Doc was refused")
local rb = F.save(rec, spec, { r })
assert(rb ~= nil, "the lawful Rec was refused")
print("doc=" .. hex(db :: buffer))
print("rec=" .. hex(rb :: buffer))
local function verdict(ok: boolean, b: any): string
	if not ok then return "raised " .. tostring(b) end
	if b == nil then return "refused" end
	return "wrote"
end
for _, c in { { "Perms", U64.fromNumber(4), "perms:4" }, { "Mask", U64.new(0, 0x100), "mask:bit40" }, { "Mask", U64.new(0, 0x80000000), "mask:bit63" } } do
	local d2: any = T.NewDoc(); d2[c[1]] = c[2]
	local r2: any = T.NewRec(); r2[c[1]] = c[2]
	print("Save " .. c[3] .. " " .. verdict(pcall(S.save, doc, d2)))
	print("FixedSave " .. c[3] .. " " .. verdict(pcall(F.save, rec, spec, { r2 })))
	print("SaveMessages " .. c[3] .. " " .. verdict(pcall(MS.SaveMessages, "Rec", { r2 }, {})))
end
`, nil)
	for _, tag := range []string{"perms:4", "mask:bit40", "mask:bit63"} {
		for _, w := range []string{"Save", "FixedSave", "SaveMessages"} {
			line := w + " " + tag + " refused\n"
			if !strings.Contains(wrote+"\n", line) {
				t.Errorf("want %q (a writer writes no mask its reader refuses); the probe printed:\n%s", line, wrote)
			}
		}
	}
	files := mooR17Wires(t, wrote)

	// The newer build (Perms has a third flag) writes a message batch with the
	// new bit set and one without it.
	maps.Copy(files, mooR17Wires(t, wide.run(t, mooR17Head+`local inside = T.NewRec(); inside.Perms = U64.fromNumber(3)
local past = T.NewRec(); past.Perms = U64.fromNumber(5)
local a = MS.SaveMessages("Rec", { inside }, {})
local b = MS.SaveMessages("Rec", { past }, {})
assert(a ~= nil and b ~= nil, "the newer build refused its own batch")
print("msg_inside=" .. hex(a :: buffer))
print("msg_past=" .. hex(b :: buffer))
`, nil)))

	var slot [8]byte
	binary.LittleEndian.PutUint64(slot[:], 0x123456789a)
	inputs := map[string][]byte{"announce": wideAnnounce}
	for _, name := range []string{"doc", "rec"} {
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
	for _, k := range []string{"msg_inside", "msg_past"} {
		if len(files[k]) == 0 {
			t.Fatalf("the newer build printed no %s wire", k)
		}
		inputs[k] = files[k]
	}

	read := narrow.run(t, mooR17Head+`local rec = St.lookup("Rec")
local spec = F.lookup("Rec") :: any
local function same(v: any): string
	return if U64.cmp(v.Mask, sentinel) == 0 then "sentinel" else "other"
end
for _, n in { "inside", "past40", "bit63" } do
	local r: any = {}
	local v = L.load("Doc", (inputs :: any)["doc_" .. n], r)
	print("Load " .. n .. " " .. (if v ~= nil then "read " .. same(v) else "refused " .. tostring(r.reason)))
	local fr: any = {}
	local rows = F.load(rec, spec, (inputs :: any)["rec_" .. n], fr)
	print("FixedLoad " .. n .. " " .. (if rows ~= nil and #rows == 1 then "read " .. same(rows[1]) else "refused " .. tostring(fr.reason)))
end
local voc = MS.NewVocabulary()
assert(MS.AnnounceRead(voc, inputs.announce, {}), "announce")
for _, n in { "msg_inside", "msg_past" } do
	local r: any = {}
	local count = MS.LoadMessages(voc, "Rec", (inputs :: any)[n], 256, r)
	print("LoadMessages " .. n .. " " .. (if count == 1 and not r.refused then "read" else "refused " .. tostring(r.reason)))
end
`, inputs)
	want := []string{
		"Load inside read sentinel",
		"Load past40 refused out_of_range",
		"Load bit63 refused out_of_range",
		"FixedLoad inside read sentinel",
		"FixedLoad past40 refused out_of_range",
		"FixedLoad bit63 refused out_of_range",
		"LoadMessages msg_inside read",
		"LoadMessages msg_past refused out_of_range",
	}
	for _, line := range want {
		if !strings.Contains(read+"\n", line+"\n") {
			t.Errorf("want %q; the probe printed:\n%s", line, read)
		}
	}
}
