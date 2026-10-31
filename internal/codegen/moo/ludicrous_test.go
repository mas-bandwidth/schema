package moo

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooLudicrousWire is test/ludicrous_main.cpp in moo: fixed point,
// ufixed, int128 and uint128 against testdata/wire/ludicrous_state*.bin,
// degenerate_probe.bin and unsigned_probe.bin, the round trips, the §5
// zeroing of an untaken 128-bit field, and the hostile offsets a read must
// REJECT, never clamp. A fixed(16, 16) or fixed(48, 16) field is a moo number
// in whole units (45.5, not its raw 2981888): moo's own Q48.16.
func TestMooLudicrousWire(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, _ := generateInto(t, unitFromDir(t, repo("examples128")))
	writeFile(t, dir, "main.moo", ludicrousProbe)
	in := goldens(t, "ludicrous_state", "ludicrous_state_untargeted", "degenerate_probe", "unsigned_probe")
	out := strings.TrimSpace(string(moorun.Run(t, dir, "main.moo", in)))
	if out != "OK" {
		t.Fatalf("ludicrous probe:\n%s", out)
	}
}

const ludicrousProbe = `--!strict
local serialize = require("./serialize")
local inputs = require("./inputs")
local L = require("./Ludicrous")
local I128, U128 = serialize.Int128, serialize.Uint128

local failed = false
local function check(ok: boolean, what: string)
	if not ok then
		print("FAILED: " .. what)
		failed = true
	end
end
local function same(a: buffer, b: buffer): boolean
	return buffer.len(a) == buffer.len(b) and buffer.tostring(a) == buffer.tostring(b)
end
local function eq(C: any, a: any, b: any): boolean
	return C.cmp(a, b) == 0
end
local function write(fn: (any) -> boolean): buffer
	local ws = serialize.WriteStream.new(buffer.create(256))
	check(fn(ws), "write")
	ws:flush()
	return ws:data()
end
local function setBits(b: buffer, pos: number, n: number)
	for i = pos, pos + n - 1 do
		local at = i // 8
		buffer.writeu8(b, at, bit32.bor(buffer.readu8(b, at), bit32.lshift(1, i % 8)))
	end
end
local function copy(b: buffer): buffer
	local c = buffer.create(buffer.len(b))
	buffer.copy(c, 0, b, 0)
	return c
end

local function makeState(): L.LudicrousState
	local v = L.NewLudicrousState()
	v.Mode = L.DriveMode.Ludicrous
	v.Probe.Angle = 45.5
	v.Probe.Position = -12346.1875
	v.Probe.Reach = I128.sub(I128.mul(65536, 1000000), 1) -- raw_max - 1
	v.Probe.Ticks = 777777
	v.Probe.Samples[1] = -8
	v.Probe.Samples[2] = 8
	v.Wide.EntityId = U128.parse("0x0123456789abcdeffedcba9876543210")
	v.Wide.Energy = I128.parse("4999999999")
	v.Wide.Flux = I128.add(I128.lsh(1, 99), 7)
	v.KeysCount = 2
	v.Keys[1] = U128.fromNumber(1)
	v.Keys[2] = U128.lsh(1, 127)
	v.HasTarget = true
	v.TargetId = U128.fromNumber(42)
	return v
end

check(L.FixedProbeMaxBits == 156, "FixedProbe worst case")
check(L.WideProbeMaxBits == 403, "WideProbe worst case")
check(L.LudicrousStateMaxBits == 1205, "LudicrousState worst case")

do -- construction installs the specified defaults; Zero is the §5 zero form
	local z = L.NewLudicrousState()
	check(z.Mode == L.DriveMode.None, "mode starts None")
	check(eq(I128, z.Wide.Bias, -250), "bias defaults -250")
	check(eq(U128, z.Wide.Seed, U128.lsh(1, 65)), "seed defaults 2^65")
	check(z.KeysCount == 0, "keys start empty")
	L.ZeroLudicrousState(z)
	check(eq(I128, z.Wide.Bias, 0), "the zero form is zero")
	L.InitLudicrousState(z)
	check(eq(I128, z.Wide.Bias, -250), "Init restores the default")
end

local taken
do -- the taken branch: the C++ golden, and the round trip
	local v = makeState()
	taken = write(function(s) return L.WriteLudicrousState(s, v) end)
	check(same(taken, inputs["ludicrous_state"]), "ludicrous_state golden")
	local out = L.NewLudicrousState()
	local rs = serialize.ReadStream.new(taken)
	check(L.ReadLudicrousState(rs, out) and rs:serializeEnd(), "read LudicrousState")
	check(out.Mode == L.DriveMode.Ludicrous, "mode")
	check(out.Probe.Angle == 45.5, "angle is a moo number")
	check(out.Probe.Position == -12346.1875, "position is a moo number")
	check(eq(I128, out.Probe.Reach, v.Probe.Reach), "reach")
	check(out.Probe.Ticks == 777777, "ticks")
	check(out.Probe.Samples[1] == -8 and out.Probe.Samples[2] == 8, "samples")
	check(eq(U128, out.Wide.EntityId, v.Wide.EntityId), "entity_id")
	check(eq(I128, out.Wide.Energy, v.Wide.Energy), "energy")
	check(eq(I128, out.Wide.Flux, v.Wide.Flux), "flux")
	check(eq(I128, out.Wide.Bias, -250), "the bias default rides")
	check(eq(U128, out.Wide.Seed, U128.lsh(1, 65)), "the seed default rides")
	check(out.KeysCount == 2 and eq(U128, out.Keys[2], U128.lsh(1, 127)), "keys")
	check(out.HasTarget and eq(U128, out.TargetId, 42), "the taken branch")
end

do -- the untaken branch reads the 128-bit field back as zero (SPEC §5)
	local v = makeState()
	v.HasTarget = false
	local b = write(function(s) return L.WriteLudicrousState(s, v) end)
	check(same(b, inputs["ludicrous_state_untargeted"]), "ludicrous_state_untargeted golden")
	local out = L.NewLudicrousState()
	out.TargetId = U128.fromNumber(0xdead)
	local rs = serialize.ReadStream.new(b)
	check(L.ReadLudicrousState(rs, out), "read untargeted")
	check(not out.HasTarget and eq(U128, out.TargetId, 0), "the untaken field is zeroed")
end

do -- hostile offsets are REJECTED, never clamped
	local h = copy(taken)
	setBits(h, 2, 25)
	local rs = serialize.ReadStream.new(h)
	check(not L.ReadLudicrousState(rs, L.NewLudicrousState()) and rs:refusal() == "value_out_of_range", "a smuggled fixed offset")
	h = copy(taken)
	setBits(h, 286, 34)
	rs = serialize.ReadStream.new(h)
	check(not L.ReadLudicrousState(rs, L.NewLudicrousState()) and rs:refusal() == "value_out_of_range", "a smuggled int128 offset")
	local cut = buffer.create(4)
	buffer.copy(cut, 0, taken, 0, 4)
	rs = serialize.ReadStream.new(cut)
	check(not L.ReadLudicrousState(rs, L.NewLudicrousState()) and rs:refusal() == "past_end", "truncation")
end

do -- min == max costs zero bits (SPEC §4.6)
	check(L.DegenerateProbeMaxBits == 8, "degenerate fields cost zero bits")
	local v = L.NewDegenerateProbe()
	check(v.LockedFixed == -3 and v.LockedInt == 7, "degenerate defaults")
	v.Tail = 0xa5
	local b = write(function(s) return L.WriteDegenerateProbe(s, v) end)
	check(same(b, inputs["degenerate_probe"]), "degenerate_probe golden")
	local out = L.NewDegenerateProbe()
	L.ZeroDegenerateProbe(out)
	check(L.ReadDegenerateProbe(serialize.ReadStream.new(b), out), "read DegenerateProbe")
	check(out.LockedFixed == -3 and out.LockedInt == 7 and eq(I128, out.LockedWide, I128.parse("-12345678901234")) and out.Tail == 0xa5, "materialized from the range")
end

do -- ufixed: span's raw value fills uint64's high half
	check(L.UnsignedProbeMaxBits == 196, "UnsignedProbe worst case")
	local v = L.NewUnsignedProbe()
	v.Angle = 45.5
	v.Span = serialize.Uint64.parse("0xffffffffffff0000") -- raw_max: ufixed(48, 16) past moo's 2^47 rides raw
	v.Reach = U128.parse("131071999999")
	v.Ticks = 777777
	v.Samples[1] = 0
	v.Samples[2] = 16
	v.Tail = 0xa5
	local b = write(function(s) return L.WriteUnsignedProbe(s, v) end)
	check(same(b, inputs["unsigned_probe"]), "unsigned_probe golden")
	local out = L.NewUnsignedProbe()
	local rs = serialize.ReadStream.new(b)
	check(L.ReadUnsignedProbe(rs, out), "read UnsignedProbe")
	check(out.Angle == 45.5 and serialize.Uint64.cmp(out.Span, v.Span) == 0 and eq(U128, out.Reach, v.Reach)
		and out.Ticks == 777777 and out.Samples[2] == 16 and out.Locked == 3 and out.Tail == 0xa5, "UnsignedProbe round trip")
	local bad = L.NewUnsignedProbe()
	bad.Locked = 3 + 1 / 65536
	local ws = serialize.WriteStream.new(buffer.create(64))
	check(not L.WriteUnsignedProbe(ws, bad), "a wrong degenerate ufixed is refused on write")
	local h = copy(b)
	setBits(h, 25, 64)
	rs = serialize.ReadStream.new(h)
	check(not L.ReadUnsignedProbe(rs, L.NewUnsignedProbe()), "a smuggled ufixed high-half offset")
end

if not failed then
	print("OK")
end
`
