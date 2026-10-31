--!strict
-- Packet wide text, the moo leg's composition contracts (the JavaScript
-- leg's --contracts, test/packet-wide/js/main.mjs): writer length, zero-unit
-- and pairing refusals; reader pairing over crafted bytes; untouched tails
-- past the used length; an unaligned conditional and its zeroing; fixed and
-- counted arrays with a born count; and repeated union selection. Each failed
-- check prints "FAILED: <what>" and the run exits nonzero at the end.
local serialize = require("./serialize")
local w = require("./WideText")
local s = require("./Shapes")

local failed = 0

local function check(ok: boolean, what: string)
	if not ok then
		print("FAILED: " .. what)
		failed += 1
	end
end

local function same(a: any, b: any): boolean
	if type(a) == "buffer" or type(b) == "buffer" then
		return type(a) == "buffer" and type(b) == "buffer" and buffer.tostring(a) == buffer.tostring(b)
	end
	if type(a) == "table" and type(b) == "table" then
		for k, v in a do
			if not same(v, b[k]) then
				return false
			end
		end
		for k in b do
			if a[k] == nil then
				return false
			end
		end
		return true
	end
	return a == b
end

local function encode(mod: any, name: string, value: any): (boolean, buffer, number)
	local stream = serialize.WriteStream.new(buffer.create(256))
	if not mod["Write" .. name](stream, value) then
		return false, buffer.create(0), 0
	end
	local bits: number = stream:bitsProcessed()
	stream:flush()
	return true, stream:data(), bits
end

local function decode(mod: any, name: string, bytes: buffer, value: any): (boolean, number)
	local stream = serialize.ReadStream.new(bytes)
	local ok = mod["Read" .. name](stream, value)
	return ok, stream:bitsProcessed()
end

local function units(b: buffer): { number }
	local out = {}
	for i = 0, buffer.len(b) // 2 - 1 do
		out[i + 1] = buffer.readu16(b, i * 2)
	end
	return out
end

-- THE WRITER refuses, unlatched, a length outside [0, 7], a fraction, a
-- zero unit and an unpaired surrogate.
local v = w.NewWideSeven()
for _, n in { -1, 8, 0.5 } do
	v.TextLength = n
	local stream = serialize.WriteStream.new(buffer.create(256))
	check(not w.WriteWideSeven(stream, v) and stream:refusal() == "none", "the writer refuses length " .. tostring(n))
end
v.TextLength = 1
buffer.writeu16(v.Text, 0, 0)
check(not (encode(w, "WideSeven", v)), "the writer refuses a zero unit")
buffer.writeu16(v.Text, 0, 0xd800)
check(not (encode(w, "WideSeven", v)), "the writer refuses an unpaired high surrogate")
buffer.writeu16(v.Text, 0, 0xdc00)
check(not (encode(w, "WideSeven", v)), "the writer refuses an unpaired low surrogate")

-- THE READER refuses an unpaired surrogate from crafted bytes: length 1 in
-- three bits, then one 32-bit group, no alignment (SPEC §4.12).
local crafted = serialize.WriteStream.new(buffer.create(16))
crafted:serializeBits({ value = 1 }, 3)
crafted:serializeBits({ value = 0xd800 }, 32)
crafted:flush()
check(not (decode(w, "WideSeven", crafted:data(), w.NewWideSeven())), "the reader refuses an unpaired surrogate")

-- A read writes the used units only: the tail past the length is untouched.
buffer.writeu16(v.Text, 0, 0xffff)
local ok, bytes, bits = encode(w, "WideSeven", v)
check(ok, "0xffff is a code unit")
buffer.fill(v.Text, 0, 0x7f)
local readOk, readBits = decode(w, "WideSeven", bytes, v)
check(readOk and readBits == bits, "0xffff reads back at its bits")
check(same(units(v.Text), { 0xffff, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f }), "the reader leaves the tail untouched")

-- An unaligned conditional: the bool, the 3-bit length and two groups.
local c = s.NewConditional()
c.Enabled = true
c.TextLength = 2
buffer.writeu16(c.Text, 0, 0xd800)
buffer.writeu16(c.Text, 2, 0xdc00)
ok, bytes, bits = encode(s, "Conditional", c)
check(ok and bits == 68, "the conditional is 68 bits")
check(bit32.band(buffer.readu8(bytes, 0), 15) == 5, "the length rides at bit 1, unaligned")
local co = s.NewConditional()
check((decode(s, "Conditional", bytes, co)) and same(co, c), "the conditional reads back")
c.Enabled = false
ok, bytes = encode(s, "Conditional", c)
check(ok and (decode(s, "Conditional", bytes, co)) and same(co, s.NewConditional()), "the untaken side reads back to zero")

-- Fixed and counted arrays (the count born at its minimum) and a union arm.
local box = s.NewBox()
check(box.CountedCount == 1, "the counted array is born at its minimum")
box.Items[1].ValueLength = 1
buffer.writeu16(box.Items[1].Value, 0, 0xffff)
box.Counted[1].ValueLength = 1
buffer.writeu16(box.Counted[1].Value, 0, 65)
box.Choice.Type = s.ChoiceType.Text
box.Choice.Text.ValueLength = 1
buffer.writeu16(box.Choice.Text.Value, 0, 66)
ok, bytes, bits = encode(s, "Box", box)
check(ok, "the box writes")
local bo = s.NewBox()
buffer.writeu16(bo.Counted[2].Value, 0, 0x7f7f)
for attempt = 1, 2 do
	buffer.writeu16(bo.Choice.Text.Value, 6, 0x7f7f)
	local boxOk, boxBits = decode(s, "Box", bytes, bo)
	check(boxOk and boxBits == bits, "the box reads back, attempt " .. attempt)
	check(same(bo.Items, box.Items) and same(bo.Counted[1], box.Counted[1]), "the arrays read back, attempt " .. attempt)
	check(bo.CountedCount == 1 and buffer.readu16(bo.Counted[2].Value, 0) == 0x7f7f, "the unused counted element is untouched, attempt " .. attempt)
	check(same(bo.Choice.Text, box.Choice.Text), "the selected arm takes fresh defaults, attempt " .. attempt)
end

if failed > 0 then
	error(string.format("packet wide moo contracts: %d check(s) failed", failed), 0)
end
print("packet wide moo contracts: writer and reader refusals, tails, conditional, arrays and union OK")
