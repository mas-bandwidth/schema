package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedByteArraysSchema carries a uint8 array of each shape the fixed form
// lays down: fixed [N], counted [..N] and enum-keyed [E]. Each is a buffer,
// 0-based, in the Luau type mapping (docs/PORT-LUAU.md, THE TYPE MAPPING).
const fixedByteArraysSchema = `package probe

enum Team
{
    Red
    Blue
    Green
}

fixed table B
{
    lead uint32
    a [4]uint8
    c [..4]uint8
    k [Team]uint8
    trail uint32
}
`

// The record body is lead 4, a 4, c's count 4 and its 4 slots, k 3, trail 4:
// 23 bytes, in declaration order. The probe checks the size first, so a layout
// that moves fails rather than checks the wrong bytes.
const fixedByteArraysProbe = `--!strict
local T = require("./ProbeTable")
local F = require("./schema_fixed")
local fixed = { hash = T.BFixedHash, recordBytes = T.BFixedRecordBytes, layout = T.BFixedLayout,
	lineage = T.BFixedLineage, floor = T.BFixedFloor }
local fails = 0
local function check(ok: boolean, what: string)
	if not ok then fails += 1; print("FAIL: " .. what) end
end
check(T.BFixedRecordBytes == 23, "the record body moved: " .. tostring(T.BFixedRecordBytes))
local v = T.NewB()
check(typeof(v.A) == "buffer" and typeof(v.C) == "buffer" and typeof(v.K) == "buffer", "the constructor's byte arrays are not buffers")
v.Lead, v.Trail = 0x11111111, 0x22222222
for i = 0, 3 do buffer.writeu8(v.A, i, 10 + i) end
buffer.writeu8(v.C, 0, 20)
buffer.writeu8(v.C, 1, 21)
v.CCount = 2
for i = 0, 2 do buffer.writeu8(v.K, i, 30 + i) end
local ok, saved = pcall(F.save, T.BDescriptor, fixed, { v })
check(ok, "the save raised: " .. tostring(saved))
if ok then
	local file = saved :: buffer
	local rec = 16 + 4 + buffer.readu32(file, 16) + 8
	local want = { 0x11, 0x11, 0x11, 0x11, 10, 11, 12, 13, 2, 0, 0, 0, 20, 21, 0, 0, 30, 31, 32, 0x22, 0x22, 0x22, 0x22 }
	for i, b in want do
		check(buffer.readu8(file, rec + i - 1) == b, string.format("body byte %d is %d, want %d", i - 1, buffer.readu8(file, rec + i - 1), b))
	end
	local r: any = {}
	local back = F.read(T.BDescriptor, fixed, file, r)
	check(back ~= nil and #back == 1, "the saved file does not read: " .. tostring(r.reason))
	if back ~= nil and #back == 1 then
		local x = back[1]
		check(typeof(x.A) == "buffer" and typeof(x.C) == "buffer" and typeof(x.K) == "buffer", "the read's byte arrays are not buffers")
		if typeof(x.A) == "buffer" and typeof(x.C) == "buffer" and typeof(x.K) == "buffer" then
			check(buffer.len(x.A) == 4 and buffer.len(x.C) == 4 and buffer.len(x.K) == 3, "the read's byte arrays are not their bounds long")
			for i = 0, 3 do check(buffer.readu8(x.A, i) == 10 + i, "a[" .. i .. "] moved") end
			check(x.CCount == 2 and buffer.readu8(x.C, 0) == 20 and buffer.readu8(x.C, 1) == 21 and buffer.readu8(x.C, 2) == 0, "c moved")
			for i = 0, 2 do check(buffer.readu8(x.K, i) == 30 + i, "k[" .. i .. "] moved") end
		end
		check(x.Lead == 0x11111111 and x.Trail == 0x22222222, "the scalars beside the arrays moved")
	end
end
if fails == 0 then print("ok") end
`

// fixedByteArraysRun generates the unit, optionally sabotages one line of the
// emitted schema_fixed.luau, and answers the probe's output.
func fixedByteArraysRun(t *testing.T, from, to string) string {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": fixedByteArraysSchema})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}
	if from != "" {
		path := filepath.Join(g.dir, "schema_fixed.luau")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("the sabotage anchor %q does not occur exactly once: the runtime moved", from)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(fixedByteArraysProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runLuauErr(t, g.dir, "main.luau", nil)
	if err != nil {
		return strings.TrimSpace(string(out)) + "\n" + err.Error()
	}
	return strings.TrimSpace(string(out))
}

// TestLuauFixedByteArrays: a [N]uint8, [..N]uint8 and [E]uint8 in a fixed
// table save from their buffers and read back as buffers of their bounds,
// byte for byte (docs/PORT-LUAU.md, THE TYPE MAPPING; docs/SPEC-TABLES.md
// §7.6's cook reads through this read: port/luau/vt/cook-write).
func TestLuauFixedByteArrays(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	if out := fixedByteArraysRun(t, "", ""); out != "ok" {
		t.Fatalf("fixed byte arrays:\n%s", out)
	}
}

// TestLuauFixedByteArraysNegativeControl reads every array as a 1-based list
// again, the shape the read had before, and requires the row red.
func TestLuauFixedByteArraysNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	if out := fixedByteArraysRun(t, `	if typeof(def) == "buffer" then`, `	if false then`); out == "ok" {
		t.Fatal("the planted defect left the row green")
	}
}
