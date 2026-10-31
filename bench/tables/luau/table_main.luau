--!strict
-- the tables bench — the Luau runner (bench/tables/README.md, "Registering a
-- port"; docs/PORT-LUAU.md, THE HARNESS).
--
-- Measures ONE thing: the representative fixed table written and read on the
-- TOLERANT WIRE (docs/SPEC-TABLES.md §3) through the port's form-1 codec — the
-- one runtime walk over the generated descriptors (schema_load's loadTolerant,
-- schema_save's save) — over the committed 64-record variant corpus. It is a
-- port of bench/tables/cpp/table_main.cpp's bench_table driver and keeps its
-- contract: the golden gate runs before the clock, 1 warmup + 7 measured runs
-- (one under --round K), median beside min/max/spread, rows `write` and
-- `round_trip`, and READ derived on stderr only (§2.9).
--
-- THIS FILE IS SHAPE-BLIND. It names the generated root ONCE, as the string the
-- runtime walk is asked to load at; no field, no pinned value, no wire size.
--
-- WHAT LUAU SPELLS DIFFERENTLY, and the reason at each site:
--
-- - The port's table codec is ONE runtime walk over the generated descriptors,
--   which IS its Load and Save, so it is what is timed; there is no typed
--   per-table codec to measure instead.
-- - The load answers a fresh value built at the declared defaults (there is no
--   load-into-a-reused-target form), so the allocation stays inside the clock,
--   where the reference keeps its default restoration. The report is one table
--   reused across loads, put at rest by the runtime's own resetReport before
--   each (a caller hands the walk its counters).
-- - save answers a new buffer; its length is the write arm's sink and the
--   bytes are compared before the clock. The round trip observes its own load
--   through the re-save, as in every leg.
--
-- Run through bench/tables/luau/leg: the pinned interpreter, native codegen
-- OFF, every row linkage pkg, checks always, opt default.

local Harness = require("./harness")
local inputs = require("./inputs")
local Tables = require("./schema_tables") :: any
local Load = require("./schema_load") :: any
local Save = require("./schema_save") :: any
local resetReport = Load.resetReport
require("./BenchTableTable")

-- the C++ reference's base count for this row is 400,000
-- (bench/tables/cpp/table_main.cpp); the interpreter loads and saves one record
-- in ~0.6 ms and ~0.4 ms (hulk, 2026-10-29), so the count is 6,400 (100
-- rotations of the 64 records: a measured run of ~4 s write, ~7 s round trip,
-- far over §2.1's 200 ms floor), recorded in the iters column as the Elixir
-- leg's reduced count is.
local TABLE_ITERS = 6400

-- CSV v2 (§5.1): family table (§1.9); linkage pkg; checks always (the walks
-- check their contracts in every build); opt default (the interpreter, native
-- codegen off); inline unknown.
local CSV_SUFFIX = "table,pkg,always,default,unknown"

local NUM_VARIANTS = Harness.NUM_VARIANTS
local sink = 0

local opts = Harness.options(...)

-- the root the walk loads at: the generated type, named once (shape-blind)
local root = "TableMixed"

local function runTable()
	local row = "bench_table"
	local golden = Harness.load(inputs.golden, row)
	local packed = Harness.load(inputs.variants, row)
	if golden == nil or packed == nil then
		return
	end
	local v = Harness.splitVariants(packed, row)
	if v == nil then
		return
	end
	local record = v.record
	local descriptor = Tables.lookup(root)
	local report: any = {}

	-- loadOne is the codec's tolerant load of variant k (1-based), accepted
	-- when it answered a value and reported no malformation (the C++ leg's
	-- LoadTolerant && !malformed)
	local function loadOne(k: number): any
		resetReport(report)
		local value = Load.loadTolerant(root, v.bufs[k], report)
		if value == nil or report.malformed or report.refused ~= 0 then
			return nil
		end
		return value
	end

	-- saveOne is the codec's save: the buffer it wrote, or nil on a refusal
	local function saveOne(value: any): buffer?
		local out = Save.save(descriptor, value)
		return out
	end

	-- gate 1 (§1.5): variant 0 IS the pinned instance
	if v.exact[1] ~= golden then
		Harness.fail(row, "variant 0 is not the wire golden " .. row .. ".bin")
		return
	end

	-- gate 2: every variant loads, re-saves, and comes back byte-identical at
	-- the same length, into the instance the write loop rotates over
	local instances: { any } = {}
	for k = 1, NUM_VARIANTS do
		local value = loadOne(k)
		if value == nil then
			Harness.fail(row, "load of variant " .. (k - 1) .. " failed")
			return
		end
		local out = saveOne(value)
		if out == nil or buffer.tostring(out) ~= v.exact[k] then
			Harness.fail(row, "variant " .. (k - 1) .. " round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus")
			return
		end
		instances[k] = value
	end

	-- gate 3: exactly what the timed round trip runs, every variant twice over,
	-- the last-to-first transition included
	for i = 0, 2 * NUM_VARIANTS - 1 do
		local k = i % NUM_VARIANTS + 1
		local value = loadOne(k)
		local out = if value == nil then nil else saveOne(value)
		if out == nil or buffer.tostring(out) ~= v.exact[k] then
			Harness.fail(row, "repeated load round-trip bytes differ")
			return
		end
	end

	if opts.gate then
		return
	end

	local iters = if opts.iterations > 0 then opts.iterations else TABLE_ITERS
	local numRuns = if opts.round then 1 else 7
	local clock = os.clock

	-- WRITE: save the 64 pre-loaded values round-robin (§2.7's variation)
	local writeRates: { number } = {}
	for run = 0, numRuns do
		local t0 = clock()
		local acc = 0
		for i = 0, iters - 1 do
			local out = saveOne(instances[i % NUM_VARIANTS + 1])
			if out == nil or buffer.len(out) ~= record then
				Harness.fail(row, "save failed in loop")
				return
			end
			acc += record
		end
		local elapsed = clock() - t0
		sink += acc
		if run > 0 then
			table.insert(writeRates, iters / elapsed)
		end
	end

	-- ROUND-TRIP: load a variant, then re-save what came out
	local tripRates: { number } = {}
	for run = 0, numRuns do
		local t0 = clock()
		local acc = 0
		for i = 0, iters - 1 do
			local value = loadOne(i % NUM_VARIANTS + 1)
			if value == nil then
				Harness.fail(row, "load failed in loop")
				return
			end
			local out = saveOne(value)
			if out == nil or buffer.len(out) ~= record then
				Harness.fail(row, "re-save failed in loop")
				return
			end
			acc += record
		end
		local elapsed = clock() - t0
		sink += acc
		if run > 0 then
			table.insert(tripRates, iters / elapsed)
		end
	end

	Harness.report("luau", row, "write", iters, record, writeRates)
	Harness.report("luau", row, "round_trip", iters, record, tripRates)
	Harness.derived(row, writeRates, tripRates)
end

if opts.bad ~= "" then
	Harness.log(opts.bad)
	error("bad options", 0)
else
	Harness.log("schema tables bench (luau, interpreter, native codegen off)")
	runTable()
	Harness.finish(opts, CSV_SUFFIX)
	Harness.log("sink: " .. sink)
end
