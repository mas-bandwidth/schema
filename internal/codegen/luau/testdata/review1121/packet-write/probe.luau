-- LUAU-4 (review 2026-11-21): the packet writer refuses, never raises.
-- cases.luau is written from cases.tsv by TestLuauPacketWriteRefusesRatherThanRaises:
-- each case takes a fresh Outer and returns the value to write.
local serialize = require("./serialize")
local C = require("./Contract")
local cases = require("./cases")
for _, c in cases do
	local v = c[2](C.NewOuter())
	local w = serialize.WriteStream.new(buffer.create(256))
	local ok, res = pcall(C.WriteOuter, w, v)
	print(c[1] .. " " .. (if not ok then "raised" elseif res then "written" else "refused"))
end
