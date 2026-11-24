--!strict
-- The Luau probe of the write-refusal-packet surface (docs/SPEC.md §5.1,
-- test/conformance/write-refusal-packet/rows.txt). For every row it builds a
-- fresh lawful PacketWriteRefusal, applies ONE op, writes it with the
-- generated WritePacketWriteRefusal into a 256-byte buffer, and prints
-- "<name> refused" when the write returns false (the leg's own refusal,
-- SPEC §5.1: false, never a raise) and "<name> wrote" when it returns true.
-- The write is NOT wrapped in pcall: a raise is a crash, never a refusal, and
-- ends the run nonzero. rows.luau carries rows.txt in as source (the
-- interpreter has no io library).
local serialize = require("./serialize")
local P = require("./RefusalPacket")
local rows: { { name: string, op: string, arg: string } } = require("./rows") :: any

local function fromHex(h: string): buffer
	local b = buffer.create(#h // 2)
	for i = 0, #h // 2 - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(h, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
end

-- dec128 is a decimal argument as the int128 storage { lo, hi }: the two's
-- complement halves, each a wrapping 64-bit integer. acc = acc * 10 + d runs
-- on 32-bit limbs of lo so the carry into hi is exact.
local function dec128(s: string): any
	local neg, digits = string.match(s, "^(%-?)(%d+)$")
	if digits == nil then
		error("write-refusal-packet: " .. s .. " is not a decimal integer")
	end
	local m32, ten = integer.create(0xffffffff), integer.create(10)
	local lo, hi = integer.create(0), integer.create(0)
	for k = 1, #digits do
		local d = integer.create((string.byte(digits, k) :: number) - 48)
		local p0 = integer.add(integer.mul(integer.band(lo, m32), ten), d)
		local p1 = integer.add(integer.mul(integer.rshift(lo, 32i), ten), integer.rshift(p0, 32i))
		lo = integer.bor(integer.band(p0, m32), integer.lshift(integer.band(p1, m32), 32i))
		hi = integer.add(integer.mul(hi, ten), integer.rshift(p1, 32i))
	end
	if neg == "-" then
		lo = integer.add(integer.bnot(lo), 1i)
		hi = integer.bnot(hi)
		if lo == 0i then
			hi = integer.add(hi, 1i)
		end
	end
	return { lo = lo, hi = hi }
end

local function build(op: string, arg: string): any
	local v: any = P.NewPacketWriteRefusal()
	if op == "scalar" then
		v.Scalar = tonumber(arg)
	elseif op == "narrow" then
		v.Narrow = tonumber(arg)
	elseif op == "grade" then
		v.Grade = tonumber(arg)
	elseif op == "shot_tag" then
		v.Shot.Type = tonumber(arg)
	elseif op == "window_count" then
		v.WindowCount = tonumber(arg)
	elseif op == "text_length" then
		v.TextLength = tonumber(arg)
	elseif op == "text_bytes" then
		local b = fromHex(arg)
		buffer.copy(v.Text, 0, b, 0, buffer.len(b))
		v.TextLength = buffer.len(b)
	elseif op == "text_fill" then
		local n = tonumber(arg) :: number
		buffer.fill(v.Text, 0, string.byte("a"), n)
		v.TextLength = n
	elseif op == "fixed_constant" then
		v.FixedConstant = tonumber(arg)
	elseif op == "reserved_zero" then
		v.ReservedZero = tonumber(arg)
	elseif op == "perks" then
		v.Perks = integer.create(tonumber(arg) :: number) -- a flags mask is integer storage
	elseif op == "trio_len" then
		v.Trio = table.create(tonumber(arg) :: number, 0)
	elseif op == "trio_elem" then
		v.Trio[1] = tonumber(arg)
	elseif op == "ints_elem" then
		v.Ints[1] = tonumber(arg)
		v.IntsCount = 1
	elseif op == "pick_small" then
		v.Pick.Type = P.PickType.Small
		v.Pick.Small.V = tonumber(arg)
	elseif op == "huge" then
		v.Huge = dec128(arg)
	elseif op == "fx" then
		v.Fx = tonumber(arg) -- fixed(16, 16) storage is the raw number
	else
		error("write-refusal-packet: unknown op " .. op)
	end
	return v
end

for _, r in rows do
	local value = build(r.op, r.arg)
	local ws = serialize.WriteStream.new(buffer.create(256))
	local ok = (P :: any).WritePacketWriteRefusal(ws, value)
	print(r.name .. (if ok then " wrote" else " refused"))
end
