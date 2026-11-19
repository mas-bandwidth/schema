--!strict
-- Golden-gated MOO packet bench check over the committed 64-record corpus.
-- Shape-blind: names the generated root once (`rootName`) and nothing else.
local serialize = require("./serialize")
local Bench: any = require("./Bench")
local inputs: any = require("./inputs")

local NUM_VARIANTS = 64
local rootName = "BenchMixed"
local newFn = Bench["New" .. rootName]
local readFn = Bench["Read" .. rootName]
local writeFn = Bench["Write" .. rootName]

local goldenStr = buffer.tostring(inputs.golden)
local packedStr = buffer.tostring(inputs.variants)
if #packedStr == 0 or #packedStr % NUM_VARIANTS ~= 0 then
	error("FAILED: bench_mixed: invalid variant corpus size", 0)
end
local record = math.floor(#packedStr / NUM_VARIANTS)

local variants: { string } = {}
for k = 1, NUM_VARIANTS do
	variants[k] = string.sub(packedStr, (k - 1) * record + 1, k * record)
end

if variants[1] ~= goldenStr then
	error("WIRE GOLDEN MISMATCH: bench_mixed", 0)
end

local function encodeOne(v: any): string?
	local ws = serialize.WriteStream.new(buffer.create(4096))
	if not writeFn(ws, v) then
		return nil
	end
	ws:flush()
	return buffer.tostring(ws:data())
end

for k = 1, NUM_VARIANTS do
	local v = newFn()
	local rs = serialize.ReadStream.new(buffer.fromstring(variants[k]))
	if not readFn(rs, v) then
		error("FAILED: bench_mixed: decode of variant " .. tostring(k - 1) .. " failed", 0)
	end
	local out = encodeOne(v)
	if out ~= variants[k] then
		error("FAILED: bench_mixed: variant " .. tostring(k - 1) .. " round-trip bytes differ", 0)
	end
end

local reused = newFn()
for step = 0, 2 * NUM_VARIANTS - 1 do
	local k = (step % NUM_VARIANTS) + 1
	local rs = serialize.ReadStream.new(buffer.fromstring(variants[k]))
	if not readFn(rs, reused) or encodeOne(reused) ~= variants[k] then
		error("FAILED: bench_mixed: reused round-trip bytes differ", 0)
	end
end

print("OK (moo packet bench gate: 64 variants + reused round-trips verified)")
