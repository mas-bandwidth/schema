--!strict
-- Packet end conformance, the moo leg (testdata/wire/packet-end/README.md,
-- SPEC §5 "Read termination"). The shared cases arrive as inputs; the
-- generated public ReadEnd is the one-packet read, so it reads the top-level
-- object and then runs the runtime's end check. A packet with a whole byte
-- left after the object and one with non-zero unused bits in the final byte
-- are both refused; the exact packet is accepted with the value 3.
--
-- Each failed case prints one line, "FAILED: <case>: <detail>", and the run
-- exits nonzero at the end; the negative control greps its exact markers.
local serialize = require("./serialize")
local e = require("./End")
local inputs = require("./inputs") :: any

local failed = 0
-- ran counts the cases; the last stdout line is "packet end moo: ran N,
-- failed M" (tools/mustrun reads it). used holds the vectors this main read:
-- every *.bin the driver carried in from testdata/wire/packet-end must be one
-- of them, and there must be some.
local ran = 0
local used: { [string]: boolean } = {}

local function fail(case: string, detail: string)
	print("FAILED: " .. case .. ": " .. detail)
	failed += 1
end

local function read(name: string): (boolean, any, number)
	ran += 1
	used[name] = true
	local rs = serialize.ReadStream.new(inputs[name])
	local value = e.NewEnd()
	local ok = e.ReadEnd(rs, value)
	return ok, value, rs:bitsProcessed()
end

do -- the exact packet 03: three bits, the value 3, nothing left
	local ok, value, bits = read("exact.bin")
	if not ok then
		fail("exact", "the exact packet was refused")
	elseif value.Value ~= 3 then
		fail("exact", "the value is " .. tostring(value.Value) .. ", want 3")
	elseif bits ~= 3 then
		fail("exact", "the read consumed " .. tostring(bits) .. " bits, want 3")
	end
end

do -- 03 ff: one whole byte is left after the object
	local ok = read("trailing.bin")
	if ok then
		fail("trailing", "a whole byte left after the object was accepted")
	end
end

do -- fb: the final byte's five unused bits are set
	local ok = read("padding.bin")
	if ok then
		fail("padding", "non-zero unused bits in the final byte were accepted")
	end
end

do
	local carried = 0
	for name in inputs do
		if string.sub(name, -4) == ".bin" then
			carried += 1
			if not used[name] then
				fail(name, "a vector in the directory that this main does not read")
			end
		end
	end
	if carried == 0 then
		fail("listing", "the vector directory is empty")
	end
end
print(string.format("packet end moo: ran %d, failed %d", ran, failed))
if failed > 0 or ran == 0 then
	error(string.format("packet end moo: %d case(s) failed", failed), 0)
end
print("packet end moo: the exact packet reads as 3; trailing data and non-zero padding are refused")
