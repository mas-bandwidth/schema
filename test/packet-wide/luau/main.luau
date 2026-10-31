--!strict
-- The Luau driver of the packet wide-text sweep (test/packet-wide/README.md).
-- With no argument it reads one "<bound> <hex wire>" per input line (bound 7
-- or 4, "-" the empty wire) and answers "REFUSE" or "OK <bits> <code units>
-- <bits> <canonical>" per line. stdin.luau is the input, carried in as source
-- by ../packet-text/luau/stdin.sh because the interpreter has no io library.
-- With --contracts it checks the writer contract and the composition rules
-- (SPEC §4.12, §5) on WideText and on shapes/Shapes, and reads no input.
local serialize = require("./serialize")
local W = require("./WideText")

local args = { ... }
local contracts = args[1] == "--contracts"

local function unhex(s: string): buffer
	if s == "-" then
		return buffer.create(0)
	end
	local n = #s // 2
	local b = buffer.create(n)
	for i = 0, n - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(s, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
end

local function hex(b: buffer): string
	local n = buffer.len(b)
	if n == 0 then
		return "-"
	end
	local parts = table.create(n)
	for i = 0, n - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local function encode(mod: any, name: string, v: any): (buffer, number)
	local w = serialize.WriteStream.new(buffer.create(1024))
	assert(mod["Write" .. name](w, v), "write " .. name)
	local bits = w:bitsProcessed()
	w:flush()
	return w:data(), bits
end

local function decode(mod: any, name: string, v: any, bytes: buffer): (boolean, number)
	local r = serialize.ReadStream.new(bytes)
	local ok = mod["Read" .. name](r, v)
	return ok, r:bitsProcessed()
end

-- deepEqual compares two values field by field, buffers by content.
local function deepEqual(a: any, b: any): boolean
	if type(a) ~= type(b) then
		return false
	end
	if type(a) == "buffer" then
		if buffer.len(a) ~= buffer.len(b) then
			return false
		end
		for i = 0, buffer.len(a) - 1 do
			if buffer.readu8(a, i) ~= buffer.readu8(b, i) then
				return false
			end
		end
		return true
	end
	if type(a) ~= "table" then
		return a == b
	end
	for k, x in a do
		if not deepEqual(x, b[k]) then
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

local function fill(b: buffer, unit: number)
	for i = 0, buffer.len(b) // 2 - 1 do
		buffer.writeu16(b, i * 2, unit)
	end
end

if contracts then
	-- the writer contract: a length outside [0, N], a fraction or a NaN, and a
	-- zero code unit among the used units are refused writes (SPEC §4.12, §5)
	local v = W.NewWideSeven()
	for _, n in { -1, 8, 1, 0.5, 0 / 0 } do
		v.TextLength = n
		assert(W.WriteWideSeven(serialize.WriteStream.new(buffer.create(256)), v) == false, "length " .. tostring(n) .. " is refused")
	end
	-- pairing is the reader's: an unpaired surrogate writes, and is refused on read
	v.TextLength = 1
	buffer.writeu16(v.Text, 0, 0xd800)
	local bytes = encode(W, "WideSeven", v)
	assert(not decode(W, "WideSeven", W.NewWideSeven(), bytes), "an unpaired high surrogate is refused on read")
	-- a read writes nothing past the used length (SPEC §5's tail rule)
	buffer.writeu16(v.Text, 0, 0xffff)
	bytes = encode(W, "WideSeven", v)
	fill(v.Text, 0x7f7f)
	assert(decode(W, "WideSeven", v, bytes), "0xffff is a code unit")
	assert(buffer.readu16(v.Text, 0) == 0xffff, "the used unit")
	for i = 1, 6 do
		assert(buffer.readu16(v.Text, i * 2) == 0x7f7f, "the tail is untouched")
	end

	local P = require("./shapes/Shapes")
	-- no alignment before the groups, and an untaken branch zeroed on read
	local c = P.NewConditional()
	c.Enabled = true
	c.TextLength = 2
	buffer.writeu16(c.Text, 0, 0xd800)
	buffer.writeu16(c.Text, 2, 0xdc00)
	local cb, cbits = encode(P, "Conditional", c)
	assert(cbits == 68, "1 + 3 + 2 * 32 bits")
	assert(bit32.band(buffer.readu8(cb, 0), 15) == 5, "the length follows the bool, unaligned")
	local co = P.NewConditional()
	assert(decode(P, "Conditional", co, cb), "the pair reads")
	assert(deepEqual(co, c), "the pair round trips")
	c.Enabled = false
	cb = encode(P, "Conditional", c)
	assert(decode(P, "Conditional", co, cb), "the untaken branch reads")
	assert(deepEqual(co, P.NewConditional()), "the untaken branch is zeroed")

	-- fixed and counted arrays, the born count, and a repeated union tag
	local box = P.NewBox()
	assert(box.CountedCount == 1, "a [1..3] count is born at 1")
	box.Items[1].ValueLength = 1
	buffer.writeu16(box.Items[1].Value, 0, 0xffff)
	box.Counted[1].ValueLength = 1
	buffer.writeu16(box.Counted[1].Value, 0, 65)
	box.Choice.Type = P.ChoiceType.Text
	box.Choice.Text.ValueLength = 1
	buffer.writeu16(box.Choice.Text.Value, 0, 66)
	local bb = encode(P, "Box", box)
	local bo = P.NewBox()
	buffer.writeu16(bo.Counted[2].Value, 0, 0x7f7f)
	local arm = bo.Choice.Text
	for _ = 1, 2 do
		buffer.writeu16(bo.Choice.Text.Value, 6, 0x7f7f)
		assert(decode(P, "Box", bo, bb), "the box reads")
		assert(deepEqual(bo.Items, box.Items), "the fixed array")
		assert(deepEqual(bo.Counted[1], box.Counted[1]), "the counted element")
		assert(bo.CountedCount == 1, "the count")
		assert(buffer.readu16(bo.Counted[2].Value, 0) == 0x7f7f, "an element past the count is untouched")
		assert(deepEqual(bo.Choice.Text, box.Choice.Text), "the selected arm starts at its defaults")
		assert(bo.Choice.Text == arm, "the selected arm is reset in place, never reallocated")
	end
	print("packet wide Luau contracts: OK")
	return
end

local lines: { string } = require("./stdin") :: any
local out = table.create(#lines)
for _, line in lines do
	local bound, raw = string.match(line, "^(%d+) (.*)$")
	local name = if bound == "7" then "WideSeven" elseif bound == "4" then "WideFour" else nil
	assert(name ~= nil and raw ~= nil, "a line is <bound> <wire>")
	local wire = unhex(raw)
	local value = (W :: any)["New" .. name]()
	fill(value.Text, 0x7f7f)
	local ok, consumed = decode(W, name, value, wire)
	if not ok then
		out[#out + 1] = "REFUSE"
		continue
	end
	local bytes, bits = encode(W, name, value)
	assert(consumed == bits, "the read consumed exactly the bits the write wrote")
	local units = table.create(value.TextLength)
	for i = 0, value.TextLength - 1 do
		units[i + 1] = string.format("%04x", buffer.readu16(value.Text, i * 2))
	end
	local payload = if #units == 0 then "-" else table.concat(units)
	out[#out + 1] = string.format("OK %d %s %d %s", bits, payload, bits, hex(bytes))
end
print(table.concat(out, "\n"))
