--!strict
-- schema bench — the Luau runner: a conforming run.sh leg per bench/README.md's
-- runner contract, measuring the GENERATED Luau type-wire codec over
-- bench/corpus/Bench.schema's one measured shape, against the vendored
-- serialize.luau the generated unit carries beside it (docs/PORT-LUAU.md, THE
-- RUNTIME LAYOUT).
--
-- CONTRACT (BENCH-STANDARD.md): the iteration count identical to every other
-- leg's rows (--quick's reduced count is the one deliberate exception, and it
-- is recorded in the iters column); 1 discarded warmup run then 7 measured runs
-- per (bench, path), or exactly one measured run under --round K (§2.4); the 64
-- rotating records of the committed variant corpus on both paths;
-- median/min/max/spread; CSV v2 rows under --csv.
--
-- GOLDEN GATED (§1.5): before any clock, variant 0 is byte-compared against the
-- C++-pinned testdata/wire golden, and every variant is decoded and re-encoded
-- byte-identically. A runner that mismatches emits no rows.
--
-- SHAPE-BLIND: this file names the generated root at its one call site below
-- and nothing else about it: no field, no value, no wire size.
--
-- WHAT LUAU SPELLS DIFFERENTLY. There is no empty-asm clobber: the write
-- stream stores into the caller's buffer in place, and every message's byte
-- count folds into a sink that is printed, so no write can be deleted; the
-- round trip observes its own decode through the re-encode, as in every leg.
-- A stream is reset onto its buffer per message (the per-message state every
-- leg initialises per message), with no allocation in the timed loops. The
-- clock is os.clock().
--
-- Run through bench/luau/run, which runs this file under the pinned
-- interpreter (docs/PORT-LUAU.md, THE PIN) in its release configuration:
-- the stock `luau` CLI, NATIVE CODEGEN OFF (no --codegen), which is what an
-- embedding host gets unless it enables the JIT. Every row says so: linkage
-- pkg, checks always (a generated writer checks its contract in every build;
-- there is no compile-out assert), opt default.

local Harness = require("./harness")
local inputs = require("./inputs")
local serialize = require("./serialize")
local Generated = require("./Bench") :: any

-- §1.2/§2.1: the fixed iteration count. The other legs' is 4,000,000; this
-- interpreter writes a message in ~280 us and round-trips one in ~900 us on the
-- fleet (hulk, 2026-10-29), so 4,000,000 is hours a run. The count is 12,800 (200
-- rotations of the 64 records: a measured run of ~3.6 s write, ~12 s round
-- trip, far over §2.1's 200 ms floor), the way elixir's 400,000 stands for the
-- BEAM (BENCH-STANDARD.md, §2.8 quick-mode paragraph), recorded in the iters
-- column. Rates are per second, so rows stay comparable; the count differs and
-- the tools print it.
local MIXED_ITERS = 12800
-- --quick only (PROPOSED, BENCH-STANDARD quick-mode scaling)
local QUICK_MIXED_ITERS = 1280

-- CSV v2 (§5.1) per-runner constants: family gen (the rows measure generated
-- code); linkage pkg (the generated modules and the vendored runtime are
-- ordinary modules required into one VM, no library boundary); checks always;
-- opt default (the interpreter, native codegen off); inline unknown (no AOT
-- artifact the §4 verdict pass can walk).
local CSV_SUFFIX = "gen,pkg,always,default,unknown"

local NUM_VARIANTS = Harness.NUM_VARIANTS
local sink = 0

local opts = Harness.options(...)

-- the generated root, named once (shape-blind): its constructor, writer and
-- reader are looked up here and nowhere else
local root = "BenchMixed"
local newValue = Generated["New" .. root]
local writeValue = Generated["Write" .. root]
local readValue = Generated["Read" .. root]

local WriteStream = serialize.WriteStream
local ReadStream = serialize.ReadStream

local function runMixed()
	local row = "bench_mixed"
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

	-- the write buffer: whole 64-bit words, the record and one word over
	local cap = (record // 8 + 2) * 8
	local buf = buffer.create(cap)
	local ws = WriteStream.new(buf)
	local rs = ReadStream.new(v.bufs[1])

	-- writeOne encodes value into buf through the reset write stream and
	-- answers the bytes it wrote (-1 when the writer refuses)
	local function writeOne(value: any): number
		ws:reset(buf)
		if not writeValue(ws, value) then
			return -1
		end
		ws:flush()
		return ws:bytesProcessed()
	end

	-- readOne decodes variant k (1-based) into value
	local function readOne(value: any, k: number): boolean
		rs:reset(v.bufs[k])
		return readValue(rs, value)
	end

	-- gate 1 (§1.5): variant 0 IS the pinned instance
	if v.exact[1] ~= golden then
		Harness.fail(row, "variant 0 is not the wire golden " .. row .. ".bin")
		return
	end

	-- gate 2: every variant decodes, re-encodes, and comes back byte-identical
	-- at the same length, into its own instance the write loop rotates over
	local instances: { any } = {}
	for k = 1, NUM_VARIANTS do
		local value = newValue()
		if not readOne(value, k) then
			Harness.fail(row, "decode of variant " .. (k - 1) .. " failed")
			return
		end
		local n = writeOne(value)
		if n ~= record or buffer.readstring(buf, 0, n) ~= v.exact[k] then
			Harness.fail(row, "variant " .. (k - 1) .. " round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus")
			return
		end
		instances[k] = value
	end

	-- gate 3: one reused holder decodes every variant twice over, the
	-- last-to-first transition included, and re-encodes each byte-identically:
	-- the timed round trip reads into exactly this holder
	local out = newValue()
	for i = 0, 2 * NUM_VARIANTS - 1 do
		local k = i % NUM_VARIANTS + 1
		if not readOne(out, k) then
			Harness.fail(row, "decode into the reused holder failed")
			return
		end
		local n = writeOne(out)
		if n ~= record or buffer.readstring(buf, 0, n) ~= v.exact[k] then
			Harness.fail(row, "reused holder round-trip bytes differ")
			return
		end
	end

	if opts.gate then
		return
	end

	local iters = if opts.iterations > 0 then opts.iterations elseif opts.quick then QUICK_MIXED_ITERS else MIXED_ITERS
	local numRuns = if opts.round then 1 elseif opts.quick then 3 else 7
	local clock = os.clock

	-- WRITE: encode the 64 pre-decoded instances round-robin, so the encoder
	-- never sees the same input twice in a row and bytes/op is constant by
	-- construction (§2.7)
	local writeRates: { number } = {}
	for run = 0, numRuns do
		local t0 = clock()
		local acc = 0
		for i = 0, iters - 1 do
			local n = writeOne(instances[i % NUM_VARIANTS + 1])
			if n ~= record then
				Harness.fail(row, "write failed in loop")
				return
			end
			acc += n
		end
		local elapsed = clock() - t0
		sink += acc
		if run > 0 then
			table.insert(writeRates, iters / elapsed)
		end
	end

	-- ROUND-TRIP: decode a variant into the reused holder, then re-encode what
	-- came out; the decode is observed by the encode (§2.7's read-side sink
	-- problem dissolved rather than equalized)
	local tripRates: { number } = {}
	for run = 0, numRuns do
		local t0 = clock()
		local acc = 0
		for i = 0, iters - 1 do
			if not readOne(out, i % NUM_VARIANTS + 1) then
				Harness.fail(row, "decode failed in loop")
				return
			end
			local n = writeOne(out)
			if n ~= record then
				Harness.fail(row, "re-encode failed in loop")
				return
			end
			acc += n
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
	Harness.log("schema bench (luau, generated codec, interpreter, native codegen off" .. (if opts.quick then ", --quick: iteration instrument, not certification" else "") .. ")")
	runMixed()
	Harness.finish(opts, CSV_SUFFIX)
	Harness.log("sink: " .. sink)
end
