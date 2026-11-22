--!strict
-- The moo probe of the write-refusal-packet surface (docs/SPEC.md §5.1;
-- test/conformance/write-refusal-packet/rows.txt). For each row it builds a
-- fresh, lawful PacketWriteRefusal, applies ONE op, runs the generated
-- WritePacketWriteRefusal into a 256-byte buffer and prints "<name> refused"
-- or "<name> wrote". The moo writer's refusal is a false return and never a
-- raise (§5.1, how a refusal surfaces): a write that raises answers nothing
-- and prints a FAILED line, so a crash can never pass as a refusal. The moo
-- CLI has no io: the test hands rows.txt in as inputs.rows.
local serialize = require("./serialize")
local P = require("./PacketWriteRefusal")
local inputs = require("./inputs") :: any

-- fromHex is the bytes a hex argument spells, as a moo string.
local function fromHex(h: string): string
	local parts = {}
	for i = 1, #h, 2 do
		table.insert(parts, string.char(tonumber(string.sub(h, i, i + 1), 16) :: number))
	end
	return table.concat(parts)
end

-- ratioBits is the binary32 pattern a float argument spells: a moo float32
-- field holds its raw bits as a number (SPEC section 4.3), and nan, inf and
-- -inf are spelled out as their patterns, not left to the decimal reader.
local function ratioBits(arg: string): number
	if arg == "nan" then
		return 0x7FC00000
	elseif arg == "inf" then
		return 0x7F800000
	elseif arg == "-inf" then
		return 0xFF800000
	end
	return serialize.Float32.parse(arg).bits
end

-- apply is one op on a fresh value. A string(8) holds its length as the moo
-- string's own length, so a length op is a string of that many lawful bytes.
local function apply(v: any, op: string, arg: string)
	local n = tonumber(arg)
	if op == "scalar" then
		v.Scalar = n
	elseif op == "narrow" then
		v.Narrow = n
	elseif op == "grade" then
		v.Grade = n
	elseif op == "shot_tag" then
		v.Shot.Type = n
	elseif op == "window_count" then
		v.WindowCount = n
	elseif op == "text_length" or op == "text_fill" then
		v.Text = string.rep("a", n :: number)
	elseif op == "text_bytes" then
		v.Text = fromHex(arg)
	elseif op == "fixed_constant" then
		v.FixedConstant = n
	elseif op == "reserved_zero" then
		v.ReservedZero = n
	elseif op == "ratio" then
		v.Ratio = ratioBits(arg)
	else
		error("write-refusal-packet moo: unknown op " .. op)
	end
end

local rows = 0
for line in string.gmatch(buffer.tostring(inputs.rows), "[^\n]+") do
	local f = {}
	for word in string.gmatch(line, "%S+") do
		table.insert(f, word)
	end
	if #f == 4 and string.sub(f[1], 1, 1) ~= "#" then
		rows += 1
		local name = f[1]
		local v: any = P.NewPacketWriteRefusal()
		apply(v, f[3], f[4])
		local ws = serialize.WriteStream.new(buffer.create(256))
		local ran, ok = pcall(P.WritePacketWriteRefusal, ws, v)
		if not ran then
			print("FAILED: " .. name .. ": the write raised (" .. tostring(ok) .. ")")
		elseif ok == false then
			print(name .. " refused")
		elseif ok == true then
			print(name .. " wrote")
		else
			print("FAILED: " .. name .. ": the write returned " .. tostring(ok) .. ", not a boolean")
		end
	end
end
if rows == 0 then
	error("write-refusal-packet moo: rows.txt held no rows")
end
