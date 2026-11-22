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

local function fromHex(h: string): buffer
	local b = buffer.create(#h // 2)
	for i = 0, #h // 2 - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(h, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
end

-- real parses a float argument; nan, inf and -inf are spelled out.
local function real(arg: string): number
	if arg == "nan" then
		return 0 / 0
	elseif arg == "inf" then
		return math.huge
	elseif arg == "-inf" then
		return -math.huge
	end
	return assert(tonumber(arg), arg) :: number
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
	elseif op == "ratio" then
		v.Ratio = real(arg)
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
