--!strict
-- Packet wide text, the moo leg (test/packet-wide/README.md): the harness's
-- -wide driver protocol. moo has no stdin, so the driver hands the harness's
-- lines in as inputs.stdin; each is "<bound> <hex wire>" with bound 7
-- (WideSeven) or 4 (WideFour). A wstring(N) is a buffer of 2N bytes, UTF-16LE
-- code units, with its used length in units beside it (docs/PORT-MOO.md, THE
-- VALUE MAPPING). The buffer is poisoned with 0x7f7f before each read, so a
-- reader that reached past the used length would show it. Each line answers
-- "REFUSE", or "OK <read bits> <units hex> <written bits> <wire hex>".
local serialize = require("./serialize")
local w = require("./WideText")
local inputs = require("./inputs") :: any

local function hex(s: string): string
	if #s == 0 then
		return "-"
	end
	return (string.gsub(s, ".", function(c: string): string
		return string.format("%02x", string.byte(c))
	end))
end

local function unhex(s: string): string
	if s == "-" then
		return ""
	end
	return (string.gsub(s, "%x%x", function(h: string): string
		return string.char(tonumber(h, 16) :: number)
	end))
end

local function units(b: buffer, n: number): string
	if n == 0 then
		return "-"
	end
	local parts = table.create(n)
	for i = 0, n - 1 do
		parts[i + 1] = string.format("%04x", buffer.readu16(b, i * 2))
	end
	return table.concat(parts)
end

local out = {}
for line in string.gmatch(buffer.tostring(inputs.stdin), "[^\n]+") do
	local bound, raw = string.match(line, "^(%d+) (%S+)$")
	assert(bound == "7" or bound == "4", "a bound of 7 or 4")
	local name = if bound == "7" then "WideSeven" else "WideFour"
	local mod = w :: any
	local value = mod["New" .. name]()
	buffer.fill(value.Text, 0, 0x7f)
	local r = serialize.ReadStream.new(buffer.fromstring(unhex(raw :: string)))
	if not mod["Read" .. name](r, value) then
		table.insert(out, "REFUSE")
		continue
	end
	local read: number = r:bitsProcessed()
	local payload = units(value.Text, value.TextLength)
	-- an accepted value the writer refuses is its own verdict, which the
	-- harness reports as a disagreement with C++ on that line
	local ws = serialize.WriteStream.new(buffer.create(mod[name .. "MaxBytes"]))
	if not mod["Write" .. name](ws, value) then
		table.insert(out, `ACCEPTED-UNWRITABLE {read} {payload}`)
		continue
	end
	local bits: number = ws:bitsProcessed()
	ws:flush()
	table.insert(out, `OK {read} {payload} {bits} {hex(buffer.tostring(ws:data()))}`)
end
print(table.concat(out, "\n"))
