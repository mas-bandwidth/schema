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
local P = require("./PacketWriteRefusal")
local rows: { { name: string, op: string, arg: string } } = require("./rows") :: any
local Rt = require("./schema_runtime")

-- int128 is an Rt.int128(lo, hi) pair of 64-bit integer lanes and a ufixed(32, 32)
-- raw value a uint64 bit pattern in an integer; the probe spells each decimal
-- rows.txt names, and an unlisted one is an error, never a guess.
local wide128: { [string]: any } = {
	["100000000000000000001"] = Rt.int128(0x6bc75e2d63100001i, 0x5i),
	["100000000000000000000"] = Rt.int128(0x6bc75e2d63100000i, 0x5i),
	["-11"] = Rt.int128(0xfffffffffffffff5i, 0xffffffffffffffffi),
	["-10"] = Rt.int128(0xfffffffffffffff6i, 0xffffffffffffffffi),
}
local raw64: { [string]: any } = {
	["17179869184000000001"] = 0xee6b280000000001i,
	["17179869184000000000"] = 0xee6b280000000000i,
	["4294967295"] = 0xffffffffi,
	["4294967296"] = 0x100000000i,
}

local function spelled(t: { [string]: any }, arg: string): any
	local v = t[arg]
	if v == nil then
		error("write-refusal-packet: no spelling for " .. arg)
	end
	return v
end

local function fromHex(h: string): buffer
	local b = buffer.create(#h // 2)
	for i = 0, #h // 2 - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(h, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
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
	elseif op == "wide" then
		v.Wide = spelled(wide128, arg)
	elseif op == "fx" then
		v.Fx = tonumber(arg)
	elseif op == "ufx" then
		v.Ufx = spelled(raw64, arg)
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
