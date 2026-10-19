--!strict
-- wirefuzz.luau — THE LUAU LEG's WIRE-FUZZ DRIVER (test/conformance/README.md,
-- "The wire-fuzz driver"; docs/PORT-LUAU.md, THE HARNESS; docs/SPEC-TABLES.md
-- §4.2, the read is the verifier).
--
-- The pinned Luau has no io library (docs/PORT-LUAU.md, THE HARNESS), so this
-- file runs in one of two shapes, chosen by the host it finds:
--
-- THE PIPE. Under the native host tools/luaufuzzhost (its __luaufuzz.read and
-- __luaufuzz.write are stdin and stdout), started by the executable driver
-- test/conformance/luau/wirefuzz-driver from the repository root, this file
-- IS the harness's one process on a pipe: it reads the roster, answers one
-- byte per entry (1 for a FORM-1, non-retaining root a unit under
-- build/conformance-luau registers, 0 for the rest), then reads mutants until
-- EOF and writes one little-endian reply per mutant, flushed before the next
-- is read. Each unit is required from its own directory, so every unit keeps
-- its own registry.
--
-- THE BATCH. Under the plain interpreter (the in-package differential,
-- internal/codegen/luautable/fuzz_test.go) the working directory is one unit
-- directory, every module is required relative to it, and inputs.luau is:
--
--   return table.freeze({
--     root = "Root",
--     modules = { "ProbeTable" },          -- every <Base>Table module
--     wire = buffer.fromstring("\x.."),   -- one mutant, or
--     wires = { buffer.fromstring(".."), ... }, -- a batch (the unit tests)
--   })
--
-- and each reply is printed as one hex line (an ERR line for a raise).
--
-- The reply is the harness's 47-byte header
--   u8 loaded; i32 unknown, kind_mismatch, widened, clamped, duplicate;
--   u8 malformed; u8 refused; i64 measure; i32 retained, retain_lost;
--   i64 saved, then that many bytes
-- with the runtime's counters and saved bytes. measure is -1: a Luau load
-- has no caller region to measure (docs/PORT-LUAU.md), so a VARIABLE root
-- whose load returns no root answers loaded 0 and measure -1, the harness's
-- LoadMeasure-refusal branch, and the differential compares the counters,
-- the verdict and the saved bytes.

type Unit = { S: any, load: any, save: any }

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

-- answer reads one mutant through the unit's tolerant load and writes back what
-- it decoded (test/conformance/README.md, "The wire-fuzz driver"; §4.2).
local function answer(u: Unit, root: string, wire: buffer): buffer
	local S, load, save = u.S, u.load, u.save
	-- docs/SPEC-TABLES.md §4: the six counters and the verdict
	local report = {
		malformed = false,
		refused = 0,
		unknown = 0,
		kindMismatch = 0,
		clamped = 0,
		widened = 0,
		duplicate = 0,
	}
	local desc = S.lookup(root)
	-- THE LEG'S LOAD IS THE TOLERANT READ (test/conformance/README.md, "The
	-- wire-fuzz driver"): it recovers the value and counts the report, and the
	-- plain name (the STRICT read, §6.5) is the caller's and not this pipe's.
	local v = load.loadTolerant(root, wire, report)
	-- A FIXED root always loads (test/conformance/README.md, "The wire-fuzz
	-- driver"): its Load fills a value and reports, so a refused or damaged
	-- wire answers the root at its defaults.
	if v == nil and desc ~= nil and not desc.variable then
		v = desc.new()
	end
	local saved = nil
	if v ~= nil then
		saved = save.save(desc, v)
	end
	local n = 47 + (if saved ~= nil then buffer.len(saved) else 0)
	local b = buffer.create(n)
	buffer.writeu8(b, 0, if v ~= nil then 1 else 0)
	buffer.writei32(b, 1, report.unknown or 0)
	buffer.writei32(b, 5, report.kindMismatch or 0)
	buffer.writei32(b, 9, report.widened or 0)
	buffer.writei32(b, 13, report.clamped or 0)
	buffer.writei32(b, 17, report.duplicate or 0)
	buffer.writeu8(b, 21, if report.malformed then 1 else 0)
	buffer.writeu8(b, 22, if (report.refused or 0) > 0 then 1 else 0)
	buffer.writeinteger(b, 23, -1i)
	buffer.writei32(b, 31, 0)
	buffer.writei32(b, 35, 0)
	if saved == nil then
		buffer.writeinteger(b, 39, -1i)
	else
		buffer.writeinteger(b, 39, integer.create(buffer.len(saved)))
		buffer.copy(b, 47, saved, 0, buffer.len(saved))
	end
	return b
end


-- THE PIPE (test/conformance/README.md, "The wire-fuzz driver": the stream is
-- little-endian throughout, the roster once, then one reply per mutant).
local function pipe(host: any)
	local function readExact(n: number): buffer?
		local b: buffer = host.read(n)
		if buffer.len(b) < n then
			return nil
		end
		return b
	end
	local function u32(): number?
		local b = readExact(4)
		return if b ~= nil then buffer.readu32(b, 0) else nil
	end
	local function str(): string
		local nb = readExact(2)
		assert(nb ~= nil, "wirefuzz: the roster ended inside a name")
		local b = readExact(buffer.readu16(nb, 0))
		assert(b ~= nil, "wirefuzz: the roster ended inside a name")
		return buffer.tostring(b)
	end
	local units: { [string]: any } = require("../../../build/luau-driver/units")
	local loaded: { [string]: Unit | false } = {}
	-- one unit, required from build/conformance-luau/<key>/; a unit the
	-- target refused has no directory, and every root of it answers 0
	local function unit(key: string): Unit?
		local have = loaded[key]
		if have == nil then
			local base = "../../../build/conformance-luau/" .. key .. "/"
			local ok, got = pcall(function(): Unit
				for _, name in units[key] or {} do
					require(base .. name)
				end
				return { S = require(base .. "schema_tables"), load = require(base .. "load"), save = require(base .. "save") }
			end)
			have = if ok then got else false
			loaded[key] = have
		end
		return if have then have else nil
	end
	local count = u32()
	assert(count ~= nil, "wirefuzz: no roster")
	local roster: { { u: Unit, root: string } | false } = {}
	local reply = buffer.create(count)
	for i = 1, count do
		local key = str()
		local root = str()
		local fb = readExact(2)
		assert(fb ~= nil, "wirefuzz: the roster ended inside an entry")
		local form, retain = buffer.readu8(fb, 0), buffer.readu8(fb, 1)
		local entry: { u: Unit, root: string } | false = false
		-- a FORM-2 entry is a batch reader and a RETAIN entry the retention
		-- arm; this leg drives the plain file Load and Save only, so both
		-- answer 0 (test/conformance/README.md, "The wire-fuzz driver")
		if form == 1 and retain == 0 then
			local u = unit(key)
			if u ~= nil and u.S.lookup(root) ~= nil then
				entry = { u = u, root = root }
			end
		end
		roster[i] = entry
		buffer.writeu8(reply, i - 1, if entry then 1 else 0)
	end
	host.write(reply)
	while true do
		local index = u32()
		if index == nil then
			break -- the harness closed the stream
		end
		local n = u32()
		assert(n ~= nil, "wirefuzz: the stream ended inside a mutant header")
		local wire = readExact(n)
		assert(wire ~= nil, "wirefuzz: the stream ended inside a mutant")
		local entry = roster[index + 1]
		assert(entry, "wirefuzz: a mutant for a root this leg answered 0")
		host.write(answer(entry.u, entry.root, wire))
	end
end

local host = (_G :: any).__luaufuzz
if host ~= nil and host.read ~= nil then
	pipe(host)
else
	local inputs = require("./inputs")
	for _, name in inputs.modules or {} do
		require("./" .. name)
	end
	local u: Unit = { S = require("./schema_tables"), load = require("./load"), save = require("./save") }
	local root: string = inputs.root
	if inputs.wires ~= nil then
		-- the unit test's batch: one line per mutant, an ERR line for a raise
		for _, wire in inputs.wires do
			local ok, res = pcall(answer, u, root, wire)
			if ok then
				print(hex(res))
			else
				print("ERR")
			end
		end
	else
		print(hex(answer(u, root, inputs.wire)))
	end
end
