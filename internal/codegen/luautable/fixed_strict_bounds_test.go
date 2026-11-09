package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedStrictSchema lays every value the bounds pass holds beside a marker, so
// each forge is one byte at an offset the layout fixes: a bool, a ranged
// int32, a bits(3), an optional's present byte and an enum ordinal
// (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6).
const fixedStrictSchema = `package probe

enum Color
{
    Red
    Green
    Blue
}

fixed table S
{
    m1 uint32
    b bool
    m2 uint32
    r int32 | min = 0, max = 10
    m3 uint32
    k bits(3)
    m4 uint32
    o ?int32
    m5 uint32
    e Color
    m6 uint32
}
`

// The record body's byte offsets, the layout's declaration order (4 + 1 + 4 +
// 4 + 4 + 4 + 4 + 5 + 4 + 1 + 4 = 39 bytes); the probe checks the record size
// first, so a layout that moves makes the test fail rather than forge the
// wrong byte.
const fixedStrictProbe = `--!strict
local T = require("./ProbeTable")
local F = require("./schema_fixed")
local fixed = { hash = T.SFixedHash, recordBytes = T.SFixedRecordBytes, layout = T.SFixedLayout,
	lineage = T.SFixedLineage, floor = T.SFixedFloor }
local fails = 0
local function check(ok: boolean, what: string)
	if not ok then fails += 1; print("FAIL: " .. what) end
end
check(T.SFixedRecordBytes == 39, "the record body moved: " .. tostring(T.SFixedRecordBytes))
local s = T.NewS()
s.M1, s.M2, s.M3, s.M4, s.M5, s.M6 = 0x11111111, 0x22222222, 0x33333333, 0x44444444, 0x55555555, 0x66666666
s.B, s.R, s.K, s.O, s.OPresent, s.E = true, 7, 5, -3, true, 2
local good = F.save(T.SDescriptor, fixed, { s })
local rec = 16 + 4 + buffer.readu32(good, 16) + 8
local function forge(at: number, v: number): buffer
	local b = buffer.create(buffer.len(good))
	buffer.copy(b, 0, good, 0, buffer.len(good))
	buffer.writeu8(b, rec + at, v)
	return b
end
local function forge32(at: number, v: number): buffer
	local b = forge(at, 0)
	buffer.writei32(b, rec + at, v)
	return b
end
local r: any = {}
local back = F.read(T.SDescriptor, fixed, good, r)
check(back ~= nil and #back == 1, "the clean file reads: " .. tostring(r.reason))
if back ~= nil then
	local v = back[1]
	check(v.B == true and v.R == 7 and v.K == 5 and v.OPresent == true and v.O == -3 and v.E == 2 and v.M6 == 0x66666666,
		"the clean file's values moved")
end
local cases = {
	{ "bool 2", forge(4, 2), "wire_damaged" },
	{ "bool 0xff", forge(4, 0xff), "wire_damaged" },
	{ "ranged past max", forge32(9, 11), "out_of_range" },
	{ "ranged below min", forge32(9, -1), "out_of_range" },
	{ "bits past 2^3-1", forge32(17, 8), "out_of_range" },
	{ "present byte 2", forge(25, 2), "wire_damaged" },
	{ "ordinal past the writer's set", forge(34, 4), "out_of_range" },
}
for _, c in cases do
	local rep: any = {}
	local out = F.read(T.SDescriptor, fixed, c[2], rep)
	check(out == nil, c[1] .. ": read, not refused")
	check(rep.reason == c[3], c[1] .. ": refused " .. tostring(rep.reason) .. ", want " .. c[3])
	check((rep.widened or 0) == 0 and (rep.unknown or 0) == 0, c[1] .. ": a refusal moved a counter")
	local ok = pcall(F.load, T.SDescriptor, fixed, c[2])
	check(not ok, c[1] .. ": F.load did not raise")
end
-- THE SLACK RULE (§3.4): an absent optional's payload is slack, never read
local absent = forge(25, 0)
buffer.writei32(absent, rec + 26, 0x7fffffff)
local ra: any = {}
local a = F.read(T.SDescriptor, fixed, absent, ra)
check(a ~= nil and a[1].OPresent == false, "an absent optional over junk slack is refused: " .. tostring(ra.reason))
if fails == 0 then print("ok") end
`

// fixedStrictRun generates the unit, optionally sabotages one line of the
// emitted schema_fixed.luau, and answers the probe's output.
func fixedStrictRun(t *testing.T, from, to string) string {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": fixedStrictSchema})
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
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(fixedStrictProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", nil)))
}

// TestLuauFixedStrictBounds is the strict fixed read's bounds and byte rules on
// the identity plan (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6, §5.8 rows 4, 12
// and 16): a bool or present byte other than 0 or 1 refuses wire_damaged; a
// ranged scalar outside its range, a bits(N) past 2^N - 1 and an ordinal past
// the writer's variants refuse out_of_range; each refusal hands back nothing
// and moves no counter, and an absent optional's slack is never read.
func TestLuauFixedStrictBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	if out := fixedStrictRun(t, "", ""); out != "ok" {
		t.Fatalf("strict bounds:\n%s", out)
	}
}

// TestLuauFixedStrictBoundsNegativeControl plants each rule's defect into the
// emitted runtime, one at a time, and requires the probe to go red.
func TestLuauFixedStrictBoundsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	plants := []struct{ name, from, to string }{
		{"bool-byte", "if b > 1 then error(", "if b > 0xff then error("},
		{"range", "elseif lessThan(v, lo, isSigned) or lessThan(hi, v, isSigned) then\n\t\t\terror(outOfRange(f), 0)",
			"elseif false then\n\t\t\terror(outOfRange(f), 0)"},
		{"bits", "if lessThan(top, v, false) then error(outOfRange(f), 0) end", "if false then error(outOfRange(f), 0) end"},
		{"ordinal", "\t\tif raw > #w.children then\n", "\t\tif raw > 0xff then\n"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			if out := fixedStrictRun(t, p.from, p.to); out == "ok" || !strings.Contains(out, "FAIL:") {
				t.Fatalf("the %s plant left the probe green:\n%s", p.name, out)
			}
		})
	}
}
