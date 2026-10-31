--!strict
-- harness.luau — the BENCH-STANDARD discipline both Luau legs share
-- (bench/README.md "Runner contract"; bench/tables/README.md "The protocol";
-- docs/PORT-LUAU.md, THE HARNESS). Shape-blind: timing, statistics, the
-- corpus_id fold, the variant files and the output lines. Nothing here names a
-- generated type.
--
-- THE OUTPUT GRAMMAR. The `luau` CLI has one output stream, so every line this
-- harness prints carries a tag the leg's shell wrapper (bench/luau/route.sh)
-- routes:
--
--   csv <row>   a CSV v2 row (BENCH-STANDARD §5.1): stdout, under --csv
--   log <text>  the human table and the diagnostics: stderr
--
-- and the wrapper REFUSES any other line, so a stray print fails the leg.
--
-- THE CORPUS COMES IN AS SOURCE. The interpreter has no io (docs/PORT-LUAU.md,
-- THE HARNESS), so the wrapper writes the committed goldens and variant files
-- into a staged module `inputs.luau` (bytes as \x escapes, nothing else) and
-- this harness reads them from it.
--
-- THE CLOCK. os.clock() is the one timing primitive the pinned interpreter
-- carries (a monotonic high-resolution counter, in seconds); it is read only
-- by the runners' timed loops.

local Harness = {}

-- the 64 rotating records of the committed variant corpus (§2.7)
local NUM_VARIANTS = 64
Harness.NUM_VARIANTS = NUM_VARIANTS

-- the ceiling --iterations accepts, every leg's: whole 64-record rotations
-- that also fit a 32-bit long
local MAX_ITERATIONS = 2147483584

export type Options = {
	csv: boolean,
	gate: boolean,
	quick: boolean,
	round: boolean,
	iterations: number,
	bad: string,
}

local failed = false
local rows: { string } = {}
local loadedNames: { string } = {}
local loadedBytes: { [string]: string } = {}

local function log(text: string)
	print("log " .. text)
end
Harness.log = log

function Harness.fail(row: string, what: string)
	log("FAILED: " .. row .. ": " .. what)
	failed = true
end

-- options reads the runner's arguments (everything after `-a`): run.sh's
-- --csv, --round K and --quick, and the paired driver's --gate and
-- --iterations. --wire-dir and --variant-dir are the wrapper's (it reads the
-- corpus), never reaching here.
function Harness.options(...: string): Options
	local args = { ... }
	local o: Options = { csv = false, gate = false, quick = false, round = false, iterations = 0, bad = "" }
	local i = 1
	while i <= #args do
		local a = args[i]
		if a == "--csv" then
			o.csv = true
		elseif a == "--gate" then
			o.gate = true
		elseif a == "--quick" then
			o.quick = true
		elseif a == "--round" and i < #args then
			i += 1
			local k = tonumber(args[i])
			if k == nil or k < 0 or k % 1 ~= 0 then
				o.bad = "--round takes a non-negative integer, got " .. args[i]
			end
			o.round = true
		elseif a == "--iterations" and i < #args then
			i += 1
			local n = tonumber(args[i])
			if n == nil or n <= 0 or n > MAX_ITERATIONS or n % NUM_VARIANTS ~= 0 then
				o.bad = "--iterations requires a positive multiple of 64 up to 2147483584"
			else
				o.iterations = n
			end
		else
			o.bad = "usage: [--csv] [--gate] [--round K] [--quick] [--iterations N] [--wire-dir <dir>] [--variant-dir <dir>] (got " .. a .. ")"
		end
		i += 1
	end
	return o
end

-- load takes one corpus file from the staged inputs and records it for
-- corpus_id (§1.6): its bytes as a string, or nil with the failure named
function Harness.load(entry: any, row: string): string?
	if entry == nil or type(entry.data) ~= "string" or #entry.data == 0 then
		Harness.fail(row, "missing corpus input — run from the schema repo root, or pass --wire-dir/--variant-dir")
		return nil
	end
	table.insert(loadedNames, entry.name)
	loadedBytes[entry.name] = entry.data
	return entry.data
end

-- ---------------------------------------------------------------------------
-- corpus_id (§1.6): FNV-1a-64 over the goldens this run loaded, each as its
-- basename, a 0x00 byte and its contents, in sorted basename order, as 16
-- lowercase hex digits. The 64-bit arithmetic is the interpreter's integer
-- type, which wraps as a uint64 does.
-- ---------------------------------------------------------------------------

local FNV_PRIME = 0x100000001b3i

local function fnvString(h: integer, s: string): integer
	for i = 1, #s do
		h = integer.mul(integer.bxor(h, integer.create(string.byte(s, i))), FNV_PRIME)
	end
	return h
end

local function hex64(h: integer): string
	local out = {}
	for shift = 60, 0, -4 do
		local nibble = integer.tonumber(integer.band(integer.rshift(h, integer.create(shift)), 15i))
		out[#out + 1] = string.sub("0123456789abcdef", nibble + 1, nibble + 1)
	end
	return table.concat(out)
end

function Harness.corpusId(): string
	local order = table.clone(loadedNames)
	table.sort(order) -- plain byte order: the C++ leg's std::map order
	local h = 0xcbf29ce484222325i
	for _, name in order do
		h = fnvString(h, name)
		h = integer.mul(integer.bxor(h, 0i), FNV_PRIME)
		h = fnvString(h, loadedBytes[name])
	end
	return hex64(h)
end

-- ---------------------------------------------------------------------------
-- the variant corpus: 64 records of one length, cut from the fixed-stride file
-- as exact strings (for the byte compares) and as buffers (what the codec
-- reads; a Luau read stream needs no slack)
-- ---------------------------------------------------------------------------

export type Variants = {
	exact: { string },
	bufs: { buffer },
	record: number,
}

-- splitVariants cuts a fixed-stride file into its 64 records: a file that does
-- not divide evenly is a refusal (the records are one length by construction)
function Harness.splitVariants(packed: string, row: string): Variants?
	if #packed == 0 or #packed % NUM_VARIANTS ~= 0 then
		Harness.fail(row, "variant data is " .. #packed .. " bytes, not a multiple of 64 records — refusing to bench data whose stride is not the record size")
		return nil
	end
	local record = #packed // NUM_VARIANTS
	local v: Variants = { exact = {}, bufs = {}, record = record }
	for k = 0, NUM_VARIANTS - 1 do
		local r = string.sub(packed, k * record + 1, (k + 1) * record)
		v.exact[k + 1] = r
		v.bufs[k + 1] = buffer.fromstring(r)
	end
	return v
end

-- ---------------------------------------------------------------------------
-- statistics and rows
-- ---------------------------------------------------------------------------

local function stats(rates: { number }): (number, number, number, number)
	local s = table.clone(rates)
	table.sort(s)
	local n = #s
	local median = s[n // 2 + 1]
	return median, s[1], s[n], (s[n] - s[1]) / median * 100
end

-- report prints one (bench, path) to the human table and records its row
function Harness.report(lang: string, bench: string, path: string, iters: number, bytesPerOp: number, rates: { number })
	local median, min, max, spread = stats(rates)
	local mbps = median * bytesPerOp / (1024 * 1024)
	log(string.format("%-18s %-11s%10.4f M msg/s %10.2f MB/s   (min %.4f, max %.4f, spread %.1f%%)", bench, path, median / 1e6, mbps, min / 1e6, max / 1e6, spread))
	table.insert(rows, string.format("%s,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f", lang, bench, path, iters, bytesPerOp, #rates, median, min, max, mbps, spread))
end

-- derived prints READ = round-trip minus write: informational, never a row,
-- because a derived number in a CSV is divided as if it had been measured (§2.9)
function Harness.derived(bench: string, writeRates: { number }, tripRates: { number })
	local w = stats(writeRates)
	local rt = stats(tripRates)
	local readTime = 1 / rt - 1 / w
	if readTime > 0 then
		log(string.format("%-18s %-11s%10.4f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)", bench, "read", 0.000001 / readTime))
	end
end

-- finish emits the rows (none from a failing run, §1.5) and raises, which is
-- the interpreter's one way to a non-zero exit, when the run failed
function Harness.finish(o: Options, familySuffix: string)
	local id = Harness.corpusId()
	if failed then
		log("refusing to emit CSV rows from a failing run")
		log("BENCH FAILED (corpus_id " .. id .. ")")
		error("bench failed", 0)
	end
	if o.csv and not o.gate then
		print("csv lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline")
		for _, row in rows do
			print("csv " .. row .. "," .. id .. "," .. familySuffix)
		end
	end
	if o.gate then
		log("OK (gate only, corpus_id " .. id .. ")")
	else
		log("OK (corpus_id " .. id .. ")")
	end
end

return Harness
