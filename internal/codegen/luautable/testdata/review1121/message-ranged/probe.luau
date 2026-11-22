-- A ranged integer in a message, read or skipped, held to the announced kind
-- (TestLuauMessageRangedValueHeldToTheAnnouncedKind). The modules are
-- generated from reader/Probe.schema; inputs.announce is the writer's
-- announcement and inputs.cases is cases.tsv's rows. Each row prints
-- id=verdict reason.
local inputs = require("./inputs")
require("./ProbeTable")
local M = require("./schema_message")
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
local function unhex(s: string): buffer
	local b = buffer.create(#s // 2)
	for i = 0, #s // 2 - 1 do buffer.writeu8(b, i, tonumber(string.sub(s, 2 * i + 1, 2 * i + 2), 16) :: number) end
	return b
end
for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
	local w = string.split(line, "\t")
	local r: any = {}
	local ok, n = pcall(M.LoadMessages, voc, "Msg", unhex(w[4]), 256, r)
	print(w[1] .. "=" .. (if ok and n == 1 then "read" else "refused") .. " " .. tostring(if ok then r.reason else n))
end
