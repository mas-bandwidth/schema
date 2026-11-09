package luatable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

const luaMsgStrictV1Schema = `package msgstrict

fixed table Msg {
    x int32
    oldTag uint16
    score uint16
    label string(16)
    vals [4]uint8
    code uint16
    y int32
}
`

const luaMsgStrictV2Schema = `package msgstrict

fixed table Msg {
    x int32
    score uint16 | min = 0, max = 100
    label string(4)
    vals [2]uint8
    code string(8)
    ratio float32
    y int32
}
`

type luaMsgStrictBundle struct {
	v1Files map[string][]byte
	v2Files map[string][]byte
	mod1    string
	mod2    string
	inputs  map[string][]byte
	probe   string
}

func buildLuaMsgStrictBundle(t *testing.T) luaMsgStrictBundle {
	t.Helper()
	dir1 := t.TempDir()
	p1 := filepath.Join(dir1, "v1.schema")
	if err := os.WriteFile(p1, []byte(luaMsgStrictV1Schema), 0o600); err != nil {
		t.Fatal(err)
	}
	u1 := luarun.LoadUnit(t, p1)
	v1Files, err := Generate(u1)
	if err != nil {
		t.Fatalf("Generate v1: %v", err)
	}

	dir2 := t.TempDir()
	p2 := filepath.Join(dir2, "v2.schema")
	if err := os.WriteFile(p2, []byte(luaMsgStrictV2Schema), 0o600); err != nil {
		t.Fatal(err)
	}
	u2 := luarun.LoadUnit(t, p2)
	v2Files, err := Generate(u2)
	if err != nil {
		t.Fatalf("Generate v2: %v", err)
	}

	mod1 := ModuleName(u1)
	mod2 := ModuleName(u2)

	emitMain := fmt.Sprintf(`local T = require(%q)
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%%02x", string.byte(c)) end))
end
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local r = {}
assert(T.AnnounceRead(vocab, T.Announcement, r), "v1 AnnounceRead")

local clean = T.NewMsg()
clean.x = 42
clean.oldTag = 9
clean.y = -7
local wClean = assert(T.SaveMessages("Msg", vocab, { clean }), "save clean")

local oob = T.NewMsg()
oob.x = 1
oob.score = 250
local wOob = assert(T.SaveMessages("Msg", vocab, { oob }), "save oob")

local longLabel = T.NewMsg()
longLabel.x = 1
longLabel.label = "hello-world"
local wLong = assert(T.SaveMessages("Msg", vocab, { longLabel }), "save longLabel")

local manyVals = T.NewMsg()
manyVals.x = 1
manyVals.vals = { 1, 2, 3, 4 }
local wMany = assert(T.SaveMessages("Msg", vocab, { manyVals }), "save manyVals")

local badKind = T.NewMsg()
badKind.x = 1
badKind.code = 500
local wKind = assert(T.SaveMessages("Msg", vocab, { badKind }), "save badKind")

print("ANNOUNCE=" .. hex(T.Announcement))
print("CLEAN=" .. hex(wClean))
print("OOB=" .. hex(wOob))
print("LONG=" .. hex(wLong))
print("MANY=" .. hex(wMany))
print("KIND=" .. hex(wKind))
`, mod1)

	out1, stderr1, err := try(t, v1Files, emitMain, nil)
	if err != nil {
		t.Fatalf("v1 encoder failed: %v\n%s", err, stderr1)
	}
	kv := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSpace(out1), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			kv[k] = unhexTestBytes(t, v)
		}
	}

	nanEmit := fmt.Sprintf(`local T = require(%q)
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%%02x", string.byte(c)) end))
end
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local r = {}
assert(T.AnnounceRead(vocab, T.Announcement, r), "v2 AnnounceRead")
local m = T.NewMsg()
m.ratio = 1.5
local w = assert(T.SaveMessages("Msg", vocab, { m }), "save ratio")
-- Patch the float32 payload (0x3fc00000 -> 0x7fc00000) in the bit stream.
local bytes = { string.byte(w, 1, #w) }
local found = false
for bitOff = 16, #bytes * 8 - 32 do
  local v = 0
  for b = 0, 31 do
    local bi = ((bitOff + b) >> 3) + 1
    local sh = (bitOff + b) & 7
    if ((bytes[bi] >> sh) & 1) ~= 0 then
      v = v | (1 << b)
    end
  end
  if v == 0x3fc00000 then
    local nan = 0x7fc00000
    for b = 0, 31 do
      local bi = ((bitOff + b) >> 3) + 1
      local sh = (bitOff + b) & 7
      if ((nan >> b) & 1) ~= 0 then
        bytes[bi] = bytes[bi] | (1 << sh)
      else
        bytes[bi] = bytes[bi] & ~(1 << sh)
      end
    end
    found = true
    break
  end
end
assert(found, "did not find float32 1.5 bits in saved message")
local chars = {}
for i = 1, #bytes do chars[i] = string.char(bytes[i]) end
print("ANN2=" .. hex(T.Announcement))
print("NAN=" .. hex(table.concat(chars)))
`, mod2)

	out2, stderr2, err := try(t, v2Files, nanEmit, nil)
	if err != nil {
		t.Fatalf("v2 nan encoder failed: %v\n%s", err, stderr2)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out2), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			kv[k] = unhexTestBytes(t, v)
		}
	}

	inputs := map[string][]byte{
		"announce": kv["ANNOUNCE"],
		"ann2":     kv["ANN2"],
		"clean":    kv["CLEAN"],
		"oob":      kv["OOB"],
		"long":     kv["LONG"],
		"many":     kv["MANY"],
		"kind":     kv["KIND"],
		"nan":      kv["NAN"],
		"damaged":  {2, 0, 0xff},
	}

	probe := fmt.Sprintf(`local T = require(%q)
local M = require("message")
local inputs = require("inputs")

local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local ar = {}
assert(T.AnnounceRead(vocab, inputs.announce, ar), "AnnounceRead v1 on v2 failed: " .. tostring(ar.reason))

-- 1. Clean cross-build message with unknown field skipped.
local rClean = {}
local gotClean = T.LoadMessages("Msg", vocab, inputs.clean, rClean)
assert(gotClean ~= nil and #gotClean == 1, "clean load failed")
assert(not rClean.damaged and rClean.refused == 0 and rClean.reason == "", "clean report dirty")
assert(gotClean[1].x == 42 and gotClean[1].y == -7, "clean values wrong")

-- 2. Out-of-range scalar refused by LoadMessages and M.loadMessages(..., false).
local rOob = {}
local gotOob = T.LoadMessages("Msg", vocab, inputs.oob, rOob)
assert(gotOob ~= nil and #gotOob == 0, "out_of_range must return empty batch")
assert(rOob.refused == 1 and rOob.reason == "out_of_range", "out_of_range reason: " .. tostring(rOob.reason))

local rOobFalse = {}
local gotOobFalse = M.loadMessages(T, "Msg", vocab, inputs.oob, rOobFalse, false)
assert(gotOobFalse ~= nil and #gotOobFalse == 0, "M.loadMessages(..., false) must still refuse out_of_range")
assert(rOobFalse.refused == 1 and rOobFalse.reason == "out_of_range", "M.loadMessages(..., false) reason: " .. tostring(rOobFalse.reason))

-- 3. String length over bound refused.
local rLong = {}
local gotLong = T.LoadMessages("Msg", vocab, inputs.long, rLong)
assert(gotLong ~= nil and #gotLong == 0, "length_over_bound must return empty batch")
assert(rLong.refused == 1 and rLong.reason == "length_over_bound", "length_over_bound reason: " .. tostring(rLong.reason))

-- 4. Array count over bound refused.
local rMany = {}
local gotMany = T.LoadMessages("Msg", vocab, inputs.many, rMany)
assert(gotMany ~= nil and #gotMany == 0, "count_over_bound must return empty batch")
assert(rMany.refused == 1 and rMany.reason == "count_over_bound", "count_over_bound reason: " .. tostring(rMany.reason))

-- 5. Incompatible announced kind refused as kind_mismatch.
local rKind = {}
local gotKind = T.LoadMessages("Msg", vocab, inputs.kind, rKind)
assert(gotKind ~= nil and #gotKind == 0, "kind_mismatch must return empty batch")
assert(rKind.refused == 1 and rKind.reason == "kind_mismatch", "kind_mismatch reason: " .. tostring(rKind.reason))

-- 6. Non-finite float refused by field name.
local vocab2 = {}
for i = 1, T.MessageEntries do vocab2[i] = {} end
assert(T.AnnounceRead(vocab2, inputs.ann2, {}), "AnnounceRead v2 failed")
local rNan = {}
local gotNan = T.LoadMessages("Msg", vocab2, inputs.nan, rNan)
assert(gotNan ~= nil and #gotNan == 0, "non_finite_float must return empty batch")
assert(rNan.refused == 1 and rNan.nonFiniteFloat == "Msg.ratio" and rNan.reason == "non_finite_float: Msg.ratio",
  "non_finite_float reason: " .. tostring(rNan.reason))

-- 7. Damaged wire discards bodies and sets damaged.
local rDam = {}
local gotDam = T.LoadMessages("Msg", vocab2, inputs.damaged, rDam)
assert(gotDam ~= nil and #gotDam == 0 and rDam.damaged == true, "damaged wire must discard bodies")

print("strict-message-ok")
`, mod2)

	return luaMsgStrictBundle{
		v1Files: v1Files,
		v2Files: v2Files,
		mod1:    mod1,
		mod2:    mod2,
		inputs:  inputs,
		probe:   probe,
	}
}

func unhexTestBytes(t *testing.T, s string) []byte {
	t.Helper()
	if len(s)%2 != 0 {
		t.Fatalf("odd hex length: %q", s)
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		var b byte
		if _, err := fmt.Sscanf(s[i:i+2], "%02x", &b); err != nil {
			t.Fatalf("bad hex %q: %v", s, err)
		}
		out[i/2] = b
	}
	return out
}

func TestLuaLoadMessagesIsTheStrictRead(t *testing.T) {
	t.Parallel()
	ddSrc, err := os.ReadFile("damage_discard_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ddSrc), "M.loadMessages(") {
		t.Fatal("damage_discard_test.go still calls M.loadMessages directly instead of the plain T.LoadMessages")
	}
	if !strings.Contains(string(ddSrc), `T.LoadMessages("LoginRequest", vocab, inputs.wire, report)`) {
		t.Fatal("damage_discard_test.go does not call T.LoadMessages")
	}

	b := buildLuaMsgStrictBundle(t)
	out, stderr, err := try(t, b.v2Files, b.probe, b.inputs)
	if err != nil || strings.TrimSpace(out) != "strict-message-ok" {
		t.Fatalf("strict message probe failed: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
}

func TestLuaLoadMessagesIsTheStrictReadNegativeControl(t *testing.T) {
	t.Parallel()
	b := buildLuaMsgStrictBundle(t)
	out, stderr, err := try(t, b.v2Files, b.probe, b.inputs)
	if err != nil || strings.TrimSpace(out) != "strict-message-ok" {
		t.Fatalf("baseline failed: %v\nout=%s\nstderr=%s", err, out, stderr)
	}

	for _, tc := range []struct {
		name string
		from string
		to   string
	}{
		{
			name: "out_of_range",
			from: "ctx.reason = \"out_of_range\"\n          return false",
			to:   "holder[key] = v\n          return true",
		},
		{
			name: "length_over_bound",
			from: "if f.bound ~= nil and n > f.bound then\n      ctx.reason = \"length_over_bound\"\n      return false\n    end",
			to:   "",
		},
		{
			name: "count_over_bound",
			from: "if d.bound ~= nil and n > d.bound then\n        ctx.reason = \"count_over_bound\"\n        return false\n      end",
			to:   "",
		},
		{
			name: "kind_mismatch",
			from: "if e.kind ~= f.kind and not widens(e.kind, f.kind) then\n      ctx.reason = \"kind_mismatch\"\n      return false\n    end",
			to:   "",
		},
		{
			name: "non_finite_float",
			from: "if v & D.F32_EXP == D.F32_EXP then return 0.0, false, true end",
			to:   "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := string(b.v2Files["message.lua"])
			if !strings.Contains(src, tc.from) {
				t.Fatalf("missing runtime anchor for %s: %q", tc.name, tc.from)
			}
			broken := maps.Clone(b.v2Files)
			broken["message.lua"] = []byte(strings.Replace(src, tc.from, tc.to, 1))
			out, _, err := try(t, broken, b.probe, b.inputs)
			if err == nil && strings.TrimSpace(out) == "strict-message-ok" {
				t.Fatalf("sabotaging %s in message.lua left TestLuaLoadMessagesIsTheStrictRead green", tc.name)
			}
		})
	}
}
