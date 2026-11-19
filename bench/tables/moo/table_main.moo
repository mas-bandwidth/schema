--!strict
-- the tables bench — the MOO Form-1 gate runner (bench/tables/README.md).
-- Shape-blind: names the generated root once (`rootName`) and nothing else.
local _T: any = require("./BenchTableTable")
local Tables = require("./schema_tables")
local L = require("./table_load")
local S = require("./table_save")
local inputs: any = require("./inputs")

local NUM_VARIANTS = 64
local rootName = "TableMixed"
local form1Root = rootName .. "Form1"

local function cloneDesc(value: any): any
	if type(value) ~= "table" or getmetatable(value) ~= nil then
		return value
	end
	local copy: any = {}
	for key, field in value do
		copy[key] = cloneDesc(field)
	end
	return copy
end

local desc: any = cloneDesc(Tables.lookup(rootName))
desc.name = form1Root
desc.fixed = false
Tables.table(desc)
desc = Tables.lookup(form1Root)

local goldenStr = buffer.tostring(inputs.golden)
local packedStr = buffer.tostring(inputs.variants)
if #packedStr == 0 or #packedStr % NUM_VARIANTS ~= 0 then
	error("FAILED: bench_table: invalid variant corpus size", 0)
end
local record = math.floor(#packedStr / NUM_VARIANTS)

local variants: { string } = {}
local bufs: { buffer } = {}
for k = 1, NUM_VARIANTS do
	local s = string.sub(packedStr, (k - 1) * record + 1, k * record)
	variants[k] = s
	bufs[k] = buffer.fromstring(s)
end

if variants[1] ~= goldenStr then
	error("WIRE GOLDEN MISMATCH: bench_table", 0)
end

for k = 1, NUM_VARIANTS do
	local rep = L.newReport()
	local val = L.load(form1Root, bufs[k], rep)
	if val == nil then
		error("FAILED: bench_table: load of variant " .. tostring(k - 1) .. " failed", 0)
	end
	local out = S.save(desc, val)
	if out == nil or buffer.tostring(out) ~= variants[k] then
		error("FAILED: bench_table: variant " .. tostring(k - 1) .. " round-trip bytes differ", 0)
	end
end

for step = 0, 2 * NUM_VARIANTS - 1 do
	local k = (step % NUM_VARIANTS) + 1
	local rep = L.newReport()
	local val = L.load(form1Root, bufs[k], rep)
	local out = if val ~= nil then S.save(desc, val) else nil
	if out == nil or buffer.tostring(out) ~= variants[k] then
		error("FAILED: bench_table: repeated load round-trip bytes differ", 0)
	end
end

print("OK (moo tables bench gate: 64 variants + repeated round-trips verified)")
