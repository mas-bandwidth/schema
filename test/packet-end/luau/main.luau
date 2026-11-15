--!strict
-- The Luau driver of the packet end conformance vectors
-- (testdata/wire/packet-end/README.md): the generated public ReadEnd is the
-- one-packet read, so it holds the accepted value and both refusals. pins.luau
-- carries the shared cases in as source (the interpreter has no io library).
local serialize = require("./serialize")
local End = require("./End")
local pins: { [string]: string } = require("./pins") :: any

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
	local stream = serialize.ReadStream.new(unhex(pins[name]))
	local ok = End.ReadEnd(stream, value)
	return ok, value.Value
end

local failed = false
local function check(ok: boolean, what: string)
	if not ok then
		failed = true
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

if failed then
	error("packet-end Luau: FAILED")
end
print("packet-end Luau: exact accepted as 3; trailing byte and nonzero padding refused")
