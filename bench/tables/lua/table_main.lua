-- the tables bench: the Lua runner.
--
-- Measures ONE thing: a representative fixed table written and read on the
-- TOLERANT WIRE (docs/SPEC-TABLES.md §3), through the generated table module
-- and the runtime modules beside it (docs/PORT-LUA.md). It is the C runner's
-- sibling (bench/tables/c/table_main.c) and follows the same contract
-- (BENCH-STANDARD.md): the committed variant corpus drives it, the golden gate
-- runs before the clock, a failing gate emits NO rows, and the report is 1
-- warmup + 7 measured runs with the median beside min/max/spread. What differs
-- is the language: the default iteration count is smaller, because one tolerant
-- round trip through the descriptor walk costs about a millisecond on a quiet
-- machine, and the rate is read as a rate, not as a count.
--
-- THIS FILE IS SHAPE-BLIND. It names the generated table once, in the
-- measured shape below, and nothing else: no field, no pinned value, no wire
-- size. bench/tables/lua/SHAPE-GATE.allow carries the exact count.
--
-- THE CLOCK. os.clock is CPU time, which is what a single-threaded interpreter
-- loop spends; the loop makes no system call. Timing lives here, in the
-- sanctioned bench, and in no unit test.
--
-- Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
-- Usage (from the repository root, the generated unit's directory first):
--   lua bench/tables/lua/table_main.lua <generated dir> [--indexed] [--gate]
--       [--csv] [--round K] [--iterations N] [--wire-dir D] [--variant-dir D]

local MAX_RUNS = 7 -- median of 7 (N >= 5), after 1 warmup run
local NUM_VARIANTS = 64 -- an OP is one record
local DEFAULT_ITERATIONS = 6400 -- a multiple of 64; the C runners' 400000 is hours here

local generated = arg[1]
if generated == nil or generated:sub(1, 2) == "--" then
  io.stderr:write("usage: lua table_main.lua <generated dir> [--indexed] [--gate] [--csv] [--round K] "
    .. "[--iterations N] [--wire-dir D] [--variant-dir D]\n")
  os.exit(1)
end

local csv, gate, indexed = false, false, false
local iterations = 0
local num_runs = MAX_RUNS
local wire_dir, variant_dir = "testdata/wire", "bench/corpus/variants"
local i = 2
while i <= #arg do
  local a = arg[i]
  if a == "--indexed" then indexed = true
  elseif a == "--gate" then gate = true
  elseif a == "--csv" then csv = true
  elseif a == "--wire-dir" and arg[i + 1] then wire_dir = arg[i + 1]; i = i + 1
  elseif a == "--variant-dir" and arg[i + 1] then variant_dir = arg[i + 1]; i = i + 1
  elseif a == "--iterations" and arg[i + 1] then
    local n = math.tointeger(tonumber(arg[i + 1]))
    if n == nil or n <= 0 or n % NUM_VARIANTS ~= 0 then
      io.stderr:write("--iterations requires a positive multiple of 64\n")
      os.exit(1)
    end
    iterations = n
    i = i + 1
  elseif a == "--round" and arg[i + 1] then
    local k = math.tointeger(tonumber(arg[i + 1]))
    if k == nil or k < 0 then
      io.stderr:write("--round takes a non-negative integer, got '" .. arg[i + 1] .. "'\n")
      os.exit(1)
    end
    num_runs = 1 -- §2.4: the driver aggregates across rounds itself
    i = i + 1
  else
    io.stderr:write("unknown argument " .. a .. "\n")
    os.exit(1)
  end
  i = i + 1
end

-- the search path is the generated directory ALONE (docs/PORT-LUA.md, THE
-- RUNTIME LAYOUT): no module outside it can answer a require
package.path = generated .. "/?.lua"
package.cpath = ""
local load_mod = require("load")
local save_mod = require("save")
local tables = require("BenchtableTables")

local failed = false
local goldens = {} -- basename -> bytes, the corpus id's input (§1.6)

local function fail(name, what)
  io.stderr:write("FAILED: " .. name .. ": " .. what .. "\n")
  failed = true
end

local function read_file(dir, basename)
  local f = io.open(dir .. "/" .. basename, "rb")
  if f == nil then
    io.stderr:write("missing corpus " .. dir .. "/" .. basename
      .. " -- run from the schema repo root (or pass --wire-dir/--variant-dir)\n")
    return nil
  end
  local bytes = f:read("a")
  f:close()
  goldens[basename] = bytes
  return bytes
end

-- §1.6: FNV-1a-64 over the goldens THIS RUN LOADED -- for each file in sorted
-- basename order, the basename bytes, a 0x00 byte, then the contents. Native
-- 64-bit integers wrap, which is the arithmetic the hash is.
local FNV_PRIME = 0x100000001b3
local function fnv1a64(h, bytes)
  for k = 1, #bytes do
    h = (h ~ bytes:byte(k)) * FNV_PRIME
  end
  return h
end

local function corpus_id()
  local names = {}
  for name in pairs(goldens) do names[#names + 1] = name end
  table.sort(names)
  local h = 0xcbf29ce484222325
  for _, name in ipairs(names) do
    h = fnv1a64(h, name)
    h = fnv1a64(h, "\0")
    h = fnv1a64(h, goldens[name])
  end
  return string.format("%016x", h)
end

local csv_rows = {}

local function flush_csv()
  if not csv then return end
  if failed then
    io.stderr:write("refusing to emit CSV rows from a failing run\n") -- §1.5
    return
  end
  io.stdout:write("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,"
    .. "median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n")
  local id = corpus_id()
  for _, row in ipairs(csv_rows) do
    -- family `table` (§1.9); linkage `module` (the generated table is a Lua
    -- module over runtime modules beside it); checks `contract` (the reader's
    -- wire-contract validation is unconditional and there is no assert to
    -- compile out); opt `default` (PUC-Rio has no operator-visible level);
    -- inline `unknown` (an interpreter has no §4.1 artifact to disassemble)
    io.stdout:write(row .. "," .. id .. ",table,module,contract,default,unknown\n")
  end
end

local function stats(rates)
  table.sort(rates)
  local n = #rates
  local median = rates[(n // 2) + 1]
  return { median = median, min = rates[1], max = rates[n], spread = (rates[n] - rates[1]) / median * 100.0 }
end

local function report(bench, path, iters, bytes_per_op, s)
  local mbps = s.median * bytes_per_op / (1024.0 * 1024.0)
  io.stderr:write(string.format("%-18s %-11s %10.4f M msg/s %10.2f MB/s   (min %.4f, max %.4f, spread %.1f%%)\n",
    bench, path, s.median / 1e6, mbps, s.min / 1e6, s.max / 1e6, s.spread))
  if csv then
    csv_rows[#csv_rows + 1] = string.format("lua,%s,%s,%d,%.17g,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
      bench, path, iters, bytes_per_op, num_runs, s.median, s.min, s.max, mbps, s.spread)
  end
end

-- the 64 concatenated records, with --indexed their 64 little-endian uint32
-- lengths; the exact mean encoded bytes per record is the second answer
local function load_variants(name)
  local packed = read_file(variant_dir, name .. ".variants.bin")
  if packed == nil then return nil end
  local lengths = {}
  if indexed then
    local index = read_file(variant_dir, name .. ".lengths")
    if index == nil or #index ~= NUM_VARIANTS * 4 then
      fail(name, "the length index is not 64 little-endian uint32 lengths")
      return nil
    end
    for k = 0, NUM_VARIANTS - 1 do
      lengths[k + 1] = string.unpack("<I4", index, k * 4 + 1)
    end
  else
    if #packed == 0 or #packed % NUM_VARIANTS ~= 0 then
      fail(name, "the unindexed corpus is not 64 equal records")
      return nil
    end
    for k = 1, NUM_VARIANTS do lengths[k] = #packed // NUM_VARIANTS end
  end
  local variants, offset = {}, 0
  for k = 1, NUM_VARIANTS do
    if lengths[k] <= 0 or offset + lengths[k] > #packed then
      fail(name, "variant " .. (k - 1) .. "'s length does not fit the corpus")
      return nil
    end
    variants[k] = packed:sub(offset + 1, offset + lengths[k])
    offset = offset + lengths[k]
  end
  if offset ~= #packed then
    fail(name, "the lengths do not sum to the corpus size")
    return nil
  end
  return variants, #packed / NUM_VARIANTS
end

-- ------------------------------------------------------------------------
-- the one measured shape: the generated table, named once. Public Load
-- restores declared defaults before overlaying the wire's fields (§3), so that
-- work stays inside the clock and the runner adds no separate reset.
-- ------------------------------------------------------------------------
local ROOT = "TableMixed"

local function bench_load(bytes)
  -- a report is the caller's zeroed counters, as L.Load zeroes its own
  local report_ = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0,
    duplicate = 0, blobShared = 0, missingRequired = 0, wireDamaged = false }
  local value = load_mod.LoadTolerant(tables, ROOT, bytes, report_)
  if value == nil or report_.malformed then return nil end
  return value
end

local function bench_table(name, golden, base_iters)
  local iters = iterations ~= 0 and iterations or base_iters
  local variants, bytes_per_op = load_variants(name)
  if variants == nil then failed = true; return end

  -- gate 1 (§1.5): variant 0 IS the pinned instance.
  local pinned = read_file(wire_dir, golden .. ".bin")
  if pinned == nil then failed = true; return end
  if pinned ~= variants[1] then
    io.stderr:write("WIRE GOLDEN MISMATCH: " .. golden .. " (" .. #pinned .. " golden vs " .. #variants[1]
      .. " corpus bytes) -- refusing to bench code that does not match the corpus\n")
    failed = true
    return
  end

  -- gate 2: every variant loads, re-saves, and comes back byte-identical
  -- before any clock starts.
  local instances = {}
  for k = 1, NUM_VARIANTS do
    local value = bench_load(variants[k])
    if value == nil then fail(name, "load of a variant failed"); return end
    if save_mod.Save(tables, ROOT, value) ~= variants[k] then
      fail(name, "variant round-trip bytes differ -- refusing to bench a codec that does not reproduce the corpus")
      return
    end
    instances[k] = value
  end

  if gate then return end

  -- WRITE: save the 64 pre-loaded instances round-robin.
  local write_rates = {}
  for run = -1, num_runs - 1 do
    local start = os.clock()
    for n = 0, iters - 1 do
      local k = (n % NUM_VARIANTS) + 1
      if #save_mod.Save(tables, ROOT, instances[k]) ~= #variants[k] then
        fail(name, "save failed in loop")
        return
      end
    end
    local elapsed = os.clock() - start
    if run >= 0 then write_rates[run + 1] = iters / elapsed end
  end

  -- ROUND-TRIP: load a variant buffer, then re-save what came out.
  local trip_rates = {}
  for run = -1, num_runs - 1 do
    local start = os.clock()
    for n = 0, iters - 1 do
      local k = (n % NUM_VARIANTS) + 1
      local value = bench_load(variants[k])
      if value == nil then fail(name, "load failed in loop"); return end
      if #save_mod.Save(tables, ROOT, value) ~= #variants[k] then
        fail(name, "re-save failed in loop")
        return
      end
    end
    local elapsed = os.clock() - start
    if run >= 0 then trip_rates[run + 1] = iters / elapsed end
  end

  local w, rt = stats(write_rates), stats(trip_rates)
  report(name, "write", iters, bytes_per_op, w)
  report(name, "round_trip", iters, bytes_per_op, rt)

  -- READ is DERIVED, never measured: round-trip time minus write time. It
  -- prints for continuity and is NOT a CSV row (§2.9).
  local read_time = 1.0 / rt.median - 1.0 / w.median
  if read_time > 0 then
    io.stderr:write(string.format(
      "%-18s %-11s %10.4f M msg/s   (DERIVED: round-trip minus write, informational -- not a measured row)\n",
      name, "read", 1e-6 / read_time))
  end
end

io.stderr:write("schema tables bench (lua, " .. _VERSION .. ")\n")
bench_table("bench_table", "bench_table", DEFAULT_ITERATIONS)
flush_csv()

local id = corpus_id()
if failed then
  io.stderr:write("TABLES BENCH FAILED (corpus_id " .. id .. ")\n")
  os.exit(1)
end
io.stderr:write("OK (corpus_id " .. id .. ")\n")
