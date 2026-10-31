--!strict
-- The Luau driver of the packet UTF-8 sweep (test/packet-text/README.md):
-- one hexadecimal wire per input line ("-" is the empty wire), one verdict
-- per output line, "REFUSE" or "OK <bits> <payload> <bits> <canonical>".
-- stdin.luau is the input, carried in as source by ../packet-text/luau/stdin.sh
-- because the interpreter has no io library. It runs beside the generated
-- Narrow module and the vendored serialize.luau.
local serialize = require("./serialize")
local Narrow = require("./Narrow")
local lines: { string } = require("./stdin") :: any

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

local function hex(b: buffer, n: number): string
	if n == 0 then
		return "-"
	end
	local parts = table.create(n)
	for i = 0, n - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local out = table.create(#lines)
for _, line in lines do
	local wire = unhex(line)
	local value = Narrow.NewNarrow()
	local r = serialize.ReadStream.new(wire)
	if not Narrow.ReadNarrow(r, value) then
		out[#out + 1] = "REFUSE"
		continue
	end
	local w = serialize.WriteStream.new(buffer.create(256))
	assert(Narrow.WriteNarrow(w, value), "the accepted value writes")
	local bits: number = w:bitsProcessed()
	w:flush()
	local data = w:data()
	assert(r:bitsProcessed() == bits, "the read consumed exactly the bits the write wrote")
	local payload: string = hex(value.Text, value.TextLength)
	local canonical: string = hex(data, buffer.len(data))
	out[#out + 1] = string.format("OK %d %s %d %s", bits, payload, bits, canonical)
end
print(table.concat(out, "\n"))
