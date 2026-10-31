--!strict
-- Packet text, the moo leg (test/packet-text/README.md): the harness's
-- driver protocol. moo has no stdin, so the driver hands the harness's lines
-- in as inputs.stdin; each is one hexadecimal wire ("-" for none). Each line
-- answers "REFUSE", or "OK <read bits> <payload hex> <written bits> <wire
-- hex>" after re-encoding the decoded value (the harness compares both bit
-- counts with C++'s). A string(15) is a moo string:
-- the read validates it (serialize.moo refuses a zero byte, then malformed
-- UTF-8) and no decoder substitutes replacement bytes.
local serialize = require("./serialize")
local n = require("./Narrow")
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

local out = {}
for line in string.gmatch(buffer.tostring(inputs.stdin), "[^\n]+") do
	local wire = unhex(line)
	local value = n.NewNarrow()
	local r = serialize.ReadStream.new(buffer.fromstring(wire))
	if not n.ReadNarrow(r, value) then
		table.insert(out, "REFUSE")
		continue
	end
	-- an accepted value the writer refuses is its own verdict, which the
	-- harness reports as a disagreement with C++ on that line
	local read: number = r:bitsProcessed()
	local w = serialize.WriteStream.new(buffer.create(n.NarrowMaxBytes))
	if not n.WriteNarrow(w, value) then
		table.insert(out, `ACCEPTED-UNWRITABLE {read} {hex(value.Text)}`)
		continue
	end
	local bits = w:bitsProcessed()
	w:flush()
	table.insert(out, `OK {read} {hex(value.Text)} {bits} {hex(buffer.tostring(w:data()))}`)
end
print(table.concat(out, "\n"))
