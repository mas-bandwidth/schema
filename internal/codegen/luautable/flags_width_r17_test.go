package luautable_test

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4; R17, Glenn
// 2026-11-22: "The newer reader can read old data, but never the other way").
// W is the flags declaration wire width (the variant count, or K under
// `| max = K`). Every table read refuses a mask with a bit at or above W as
// out_of_range: the variable load, the fixed load, and the message read of a
// newer writer that announces a wider mask. Every table writer refuses to
// write such a mask, in every build (docs/SPEC.md §5.1).

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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const luauR17Schema = `package rseventeen

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

// luauR17Unit generates the unit into a fresh directory and answers it with
// its announcement.
func luauR17Unit(t *testing.T, perms string) (string, []byte) {
	t.Helper()
	schema := filepath.Join(t.TempDir(), "R17.schema")
	if err := os.WriteFile(schema, fmt.Appendf(nil, luauR17Schema, perms), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schema})
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, tablewire.Announce(u)
}

const luauR17Head = `local inputs = require("./inputs")
local P = require("./R17Table")
local St = require("./schema_tables")
local load = require("./schema_load")
local save = require("./schema_save")
local M = require("./schema_message")
local function hex(b: buffer): string
	local a = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do a[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(a)
end
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
`

func luauR17Run(t *testing.T, dir, main string, inputs map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(luauR17Head+main), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(luaurun.Run(t, dir, "main.luau", inputs))
}

func luauR17Wires(t *testing.T, out string) map[string][]byte {
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

func TestLuauFlagsMaskHeldToItsWidth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	narrow, narrowAnnounce := luauR17Unit(t, "Read, Write")
	wide, wideAnnounce := luauR17Unit(t, "Read, Write, Exec")

	// The narrow build writes lawful files carrying a sentinel in the mask
	// slot (W = 40), and each of its writers answers a mask past W.
	wrote := luauR17Run(t, narrow, `local d = P.NewDoc(); d.Lead = 7; d.Perms = 3i; d.Mask = 0x123456789ai
local r = P.NewRec(); r.Lead = 7; r.Perms = 3i; r.Mask = 0x123456789ai; r.Trail = 9
local db, dwhy = save.save(St.lookup("Doc"), d)
assert(db ~= nil, tostring(dwhy))
local rb, rwhy = save.save(St.lookup("Rec"), r)
assert(rb ~= nil, tostring(rwhy))
print("doc=" .. hex(db :: buffer))
print("rec=" .. hex(rb :: buffer))
local function verdict(ok: boolean, b: any): string
	if not ok then return "raised " .. tostring(b) end
	if b == nil then return "refused" end
	return "wrote"
end
for _, c in { { "Perms", 4i, "perms:4" }, { "Mask", integer.lshift(1i, integer.create(40)), "mask:bit40" }, { "Mask", integer.lshift(1i, integer.create(63)), "mask:bit63" } } do
	local d2 = P.NewDoc(); (d2 :: any)[c[1]] = c[2]
	local r2 = P.NewRec(); (r2 :: any)[c[1]] = c[2]
	print("Save " .. c[3] .. " " .. verdict(pcall(save.save, St.lookup("Doc"), d2)))
	print("FixedSave " .. c[3] .. " " .. verdict(pcall(save.save, St.lookup("Rec"), r2)))
	print("SaveMessages " .. c[3] .. " " .. verdict(pcall(M.SaveMessages, voc, "Rec", { r2 }, {})))
end
`, map[string][]byte{"announce": narrowAnnounce})
	for _, tag := range []string{"perms:4", "mask:bit40", "mask:bit63"} {
		for _, w := range []string{"Save", "FixedSave", "SaveMessages"} {
			line := w + " " + tag + " refused\n"
			if !strings.Contains(wrote, line) {
				t.Errorf("want %q (a writer writes no mask its reader refuses); the probe printed:\n%s", line, wrote)
			}
		}
	}
	files := luauR17Wires(t, wrote)

	// The newer build (Perms has a third flag) writes a message batch with the
	// new bit set and one without it.
	maps.Copy(files, luauR17Wires(t, luauR17Run(t, wide, `local inside = P.NewRec(); inside.Perms = 3i
local past = P.NewRec(); past.Perms = 5i
local a = M.SaveMessages(voc, "Rec", { inside }, {})
local b = M.SaveMessages(voc, "Rec", { past }, {})
assert(a ~= nil and b ~= nil, "the newer build refused its own batch")
print("msg_inside=" .. hex(a :: buffer))
print("msg_past=" .. hex(b :: buffer))
`, map[string][]byte{"announce": wideAnnounce})))

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

	read := luauR17Run(t, narrow, `local function rep(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, verdict = 0, reason = "", retained = 0, retainLost = 0 }
end
for _, n in { "inside", "past40", "bit63" } do
	for _, root in { "Doc", "Rec" } do
		local r = rep()
		local ok, v = pcall(load.load, root, (inputs :: any)[string.lower(root) .. "_" .. n], r)
		if ok and v ~= nil then
			print(root .. " " .. n .. " read " .. (if v.Mask == 0x123456789ai then "sentinel" else "other"))
		else
			print(root .. " " .. n .. " refused " .. tostring(if ok then r.reason else v))
		end
	end
end
for _, n in { "msg_inside", "msg_past" } do
	local r = rep()
	local count = M.LoadMessages(voc, "Rec", (inputs :: any)[n], 256, r)
	print("LoadMessages " .. n .. " " .. (if count == 1 then "read" else "refused " .. tostring(r.reason)))
end
`, inputs)
	// the variable load and the message read name out_of_range "clamped", the
	// port's name for it (schema_load.luau REFUSING); the fixed load names it
	// out_of_range
	want := []string{
		"Doc inside read sentinel",
		"Doc past40 refused clamped",
		"Doc bit63 refused clamped",
		"Rec inside read sentinel",
		"Rec past40 refused out_of_range",
		"Rec bit63 refused out_of_range",
		"LoadMessages msg_inside read",
		"LoadMessages msg_past refused clamped",
	}
	for _, line := range want {
		if !strings.Contains(read, line+"\n") {
			t.Errorf("want %q; the probe printed:\n%s", line, read)
		}
	}
}
