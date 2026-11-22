--!strict
-- The Luau driver of the packet end conformance vectors
-- (testdata/wire/packet-end/README.md): the generated public ReadEnd is the
-- one-packet read, so it holds the accepted value and both refusals. pins.luau
-- carries the shared cases in as source (the interpreter has no io library).
local serialize = require("./serialize")
local End = require("./End")
local pins: { [string]: string } = require("./pins") :: any
-- used holds the pins this driver read, missing the ones it asked for that
-- the make recipe did not carry in from testdata/wire/packet-end.
local used: { [string]: boolean } = {}
local missing: { string } = {}

local function unhex(s: string): buffer
	local n = #s // 2
	local b = buffer.create(n)
	for i = 0, n - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(s, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
end

local function read(name: string): (boolean, number)
	local value = End.NewEnd()
	used[name] = true
	if pins[name] == nil then
		table.insert(missing, name)
		return false, -1
	end
	local stream = serialize.ReadStream.new(unhex(pins[name]))
	local ok = End.ReadEnd(stream, value)
	return ok, value.Value
end

-- ran and failures count the checks; the last stdout line is
-- "packet-end Luau: ran N, failed M" (tools/mustrun reads it). Every pin the
-- make recipe carried in must be one this driver read, and there must be some.
local ran, failures = 0, 0
local function check(ok: boolean, what: string)
	ran += 1
	if not ok then
		failures += 1
		print("FAILED: " .. what)
	end
end

-- exact.bin: one byte, the value 3, no other bits
do
	local ok, value = read("exact")
	check(ok, "packet-end exact: accepted")
	check(ok and value == 3, "packet-end exact: value 3")
end

-- trailing.bin: the same packet followed by one whole extra byte
do
	local ok = read("trailing")
	check(not ok, "packet-end trailing byte refused")
end

-- padding.bin: the same value with the final byte's five unused bits set
do
	local ok = read("padding")
	check(not ok, "packet-end padding bits refused")
end

do
	local carried = 0
	for name in pins do
		carried += 1
		check(used[name] == true, "packet-end pin " .. name .. " is in the directory and this driver does not read it")
	end
	check(carried > 0, "packet-end pins directory is not empty")
	check(#missing == 0, "packet-end pins are carried in (missing: " .. table.concat(missing, " ") .. ")")
end
print(string.format("packet-end Luau: ran %d, failed %d", ran, failures))
if failures > 0 or ran == 0 then
	error("packet-end Luau: FAILED")
end
print("packet-end Luau: exact accepted as 3; trailing byte and nonzero padding refused")
