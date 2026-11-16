--!strict
-- The Luau driver of the packet Write text cases (test/packet-write-text,
-- SPEC §5.1 contract row 8): the generated WriteTextPacket refuses, in every
-- build, a string(8) whose used bytes carry a zero byte or ill-formed UTF-8,
-- and writes each well-formed case as its canonical packet, read back byte
-- exact. cases.luau carries cases.txt in as source (the interpreter has no io
-- library).
local serialize = require("./serialize")
local Text = require("./Text")
local casesText: string = require("./cases") :: any

local function unhex(s: string): buffer
	if s == "-" then
		return buffer.create(0)
	end
	local digits = string.gsub(s, "%s", "")
	local n = #digits // 2
	local b = buffer.create(n)
	for i = 0, n - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(digits, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
end

local function trim(s: string): string
	return (string.gsub(s, "^%s*(.-)%s*$", "%1"))
end

local failed = false
local function check(ok: boolean, what: string)
	if not ok then
		failed = true
		print("FAILED: " .. what)
	end
end

local refused, written = 0, 0
for line in string.gmatch(casesText, "[^\n]+") do
	if string.sub(line, 1, 1) ~= "#" and trim(line) ~= "" then
		local parts = string.split(line, "|")
		check(#parts == 3, "packet-write-text case line has three fields: " .. line)
		local name, used, verdict = trim(parts[1]), unhex(trim(parts[2])), trim(parts[3])
		local n = buffer.len(used)
		local value = Text.NewTextPacket()
		buffer.copy(value.Text, 0, used, 0, n)
		value.TextLength = n
		local stream = serialize.WriteStream.new(buffer.create(Text.TextPacketMaxBytes))
		local ok = Text.WriteTextPacket(stream, value)
		if verdict == "refused" then
			refused += 1
			check(not ok, "packet-write-text " .. name .. " refused")
		elseif verdict == "written" then
			written += 1
			check(ok, "packet-write-text " .. name .. " written")
			if ok then
				stream:flush()
				local wire = stream:data()
				-- canonical bytes: the 4-bit length, align, then the used bytes
				local want = buffer.create(1 + n)
				buffer.writeu8(want, 0, n)
				buffer.copy(want, 1, used, 0, n)
				check(buffer.tostring(wire) == buffer.tostring(want), "packet-write-text " .. name .. " canonical bytes")
				local back = Text.NewTextPacket()
				local rs = serialize.ReadStream.new(wire)
				local rok = Text.ReadTextPacket(rs, back)
				check(rok, "packet-write-text " .. name .. " read back")
				check(rok and back.TextLength == n
					and buffer.readstring(back.Text, 0, back.TextLength) == buffer.tostring(used),
					"packet-write-text " .. name .. " read back byte exact")
			end
		else
			check(false, "packet-write-text " .. name .. " has a known verdict")
		end
	end
end
check(refused > 0 and written > 0, "packet-write-text cases: both verdicts present")

if failed then
	error("packet-write-text Luau: FAILED")
end
print(string.format("packet-write-text Luau: %d ill-formed cases refused; %d well-formed cases written and read back byte exact", refused, written))
