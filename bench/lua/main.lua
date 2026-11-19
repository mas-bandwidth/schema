-- Golden-gated Lua packet bench over the committed 64-record corpus.
-- Shape-blind: names the generated root once (`root_name`) and nothing else.
local NUM_VARIANTS = 64
local MAX_RUNS = 7
local DEFAULT_ITERS = 6400
local root_name = "BenchMixed"

local gen_dir = arg[1]
if gen_dir == nil or gen_dir:sub(1, 2) == "--" then
  io.stderr:write("usage: lua main.lua <gen-dir> [--csv] [--gate] [--quick] [--round K] [--iterations N] [--wire-dir D] [--variant-dir D]\n")
  os.exit(1)
end

local csv, gate, quick = false, false, false
local num_runs = MAX_RUNS
local iters_override = 0
local wire_dir, variant_dir = "testdata/wire", "bench/corpus/variants"
local i = 2
while i <= #arg do
  local a = arg[i]
  if a == "--csv" then csv = true
  elseif a == "--gate" then gate = true
  elseif a == "--quick" then quick = true
  elseif a == "--wire-dir" and arg[i + 1] then wire_dir = arg[i + 1]; i = i + 1
  elseif a == "--variant-dir" and arg[i + 1] then variant_dir = arg[i + 1]; i = i + 1
  elseif a == "--round" and arg[i + 1] then
    local k = math.tointeger(tonumber(arg[i + 1]))
    if k == nil or k < 0 then io.stderr:write("--round expects non-negative integer\n"); os.exit(1) end
    num_runs = 1
    i = i + 1
  elseif a == "--iterations" and arg[i + 1] then
    local n = math.tointeger(tonumber(arg[i + 1]))
    if n == nil or n <= 0 or n % NUM_VARIANTS ~= 0 then io.stderr:write("--iterations expects positive multiple of 64\n"); os.exit(1) end
    iters_override = n
    i = i + 1
  else
    io.stderr:write("unknown argument " .. a .. "\n")
    os.exit(1)
  end
  i = i + 1
end
if quick and num_runs == MAX_RUNS then num_runs = 3 end

package.path = gen_dir .. "/?.lua"
package.cpath = ""
local serialize = require("serialize")
local bench = require("Bench")
local new_fn = bench["New" .. root_name]
local read_fn = bench["Read" .. root_name]
local write_fn = bench["Write" .. root_name]

local function read_file(path)
  local f = assert(io.open(path, "rb"))
  local s = f:read("a")
  f:close()
  return s
end

local function fnv_bytes(h, s)
  for idx = 1, #s do
    h = (h ~ s:byte(idx)) * 0x100000001b3
  end
  return h
end

local golden = read_file(wire_dir .. "/bench_mixed.bin")
local packed = read_file(variant_dir .. "/bench_mixed.variants.bin")
assert(#packed > 0 and #packed % NUM_VARIANTS == 0, "invalid bench_mixed.variants.bin size")
local record = #packed // NUM_VARIANTS

local variants = {}
for k = 1, NUM_VARIANTS do
  variants[k] = packed:sub((k - 1) * record + 1, k * record)
end

if variants[1] ~= golden then
  io.stderr:write("WIRE GOLDEN MISMATCH: bench_mixed\n")
  os.exit(1)
end

local function encode_one(v)
  local w = serialize.WriteStream.new(4096)
  if not write_fn(w, v) then return nil end
  w:flush()
  return w:data()
end

local values = {}
for k = 1, NUM_VARIANTS do
  local v = new_fn()
  local r = serialize.ReadStream.new(variants[k])
  if not read_fn(r, v) then
    io.stderr:write("FAILED: bench_mixed: decode of variant " .. (k - 1) .. " failed\n")
    os.exit(1)
  end
  local out = encode_one(v)
  if out ~= variants[k] then
    io.stderr:write("FAILED: bench_mixed: variant " .. (k - 1) .. " round-trip bytes differ\n")
    os.exit(1)
  end
  values[k] = v
end

local reused = new_fn()
for step = 0, 2 * NUM_VARIANTS - 1 do
  local k = (step % NUM_VARIANTS) + 1
  local r = serialize.ReadStream.new(variants[k])
  if not read_fn(r, reused) or encode_one(reused) ~= variants[k] then
    io.stderr:write("FAILED: bench_mixed: reused round-trip bytes differ\n")
    os.exit(1)
  end
end

local h = 0xcbf29ce484222325
h = fnv_bytes(h, "bench_mixed.bin\0" .. golden)
h = fnv_bytes(h, "bench_mixed.variants.bin\0" .. packed)
local cid = string.format("%016x", h)

if gate then
  io.stderr:write("OK (gate only, corpus_id " .. cid .. ")\n")
  os.exit(0)
end

local iters = iters_override > 0 and iters_override or (quick and 640 or DEFAULT_ITERS)
local sink = 0
local rows = {}
for _, path_name in ipairs({ "write", "round_trip" }) do
  local rates = {}
  local holder = new_fn()
  for run = 0, num_runs do
    local t0 = os.clock()
    for idx = 0, iters - 1 do
      local k = (idx % NUM_VARIANTS) + 1
      local v = values[k]
      if path_name == "round_trip" then
        local r = serialize.ReadStream.new(variants[k])
        if not read_fn(r, holder) then os.exit(1) end
        v = holder
      end
      local out = encode_one(v)
      if out == nil or #out ~= record then os.exit(1) end
      sink = sink + #out
    end
    local elapsed = os.clock() - t0
    if run > 0 then rates[#rates + 1] = iters / elapsed end
  end
  table.sort(rates)
  local med = rates[(#rates // 2) + 1]
  local lo, hi = rates[1], rates[#rates]
  rows[#rows + 1] = string.format(
    "lua,bench_mixed,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f,%s,gen,pkg,always,default,unknown",
    path_name, iters, record, num_runs, med, lo, hi, med * record / 1048576.0, (hi - lo) / med * 100.0, cid
  )
end

if csv then
  io.stdout:write("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n")
  io.stdout:write(table.concat(rows, "\n") .. "\n")
else
  io.stderr:write(table.concat(rows, "\n") .. "\n")
end
io.stderr:write("OK (corpus_id " .. cid .. ", sink " .. sink .. ")\n")
