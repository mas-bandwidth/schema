--!strict
-- wirefuzz.luau — THE LUAU LEG's WIRE-FUZZ DRIVER (test/conformance/README.md,
-- "The wire-fuzz driver"; docs/PORT-LUAU.md, THE HARNESS; docs/SPEC-TABLES.md
-- §4.2, the read is the verifier).
--
-- The pinned Luau has no io library (docs/PORT-LUAU.md, THE HARNESS), so this
-- file does not speak the harness's little-endian pipe itself. The host shim
-- (make/luau-fuzz.mk) reads the roster and the mutants, writes the mutant into
-- inputs.luau beside this file, runs the pinned interpreter on this file, and
-- frames the one hex line this file prints as the harness's reply. The same
-- shape is what the conformance leg's main.luau reuses: the working directory
-- is one unit directory, and every module is required relative to it.
--
-- inputs.luau is:
--
--   return table.freeze({
--     root = "Root",
--     modules = { "ProbeTable" },          -- every <Base>Table module
--     wire = buffer.fromstring("\x.."),   -- one mutant, or
--     wires = { buffer.fromstring(".."), ... }, -- a batch (the unit tests)
--   })
--
-- The reply is one hex line: the harness's 47-byte header
--   u8 loaded; i32 unknown, kind_mismatch, widened, clamped, duplicate;
--   u8 malformed; u8 refused; i64 measure; i32 retained, retain_lost;
--   i64 saved, then that many bytes
-- with the runtime's counters and saved bytes, and measure -1: this runtime
-- has no caller region to measure. The host shim fills the region measure
-- where the engine has one; the in-package differential compares the counters,
-- the verdict and the saved bytes and leaves measure to the harness.

local S = require("./schema_tables")
local load = require("./load")
local save = require("./save")
local inputs = require("./inputs")

for _, name in inputs.modules or {} do
	require("./" .. name)
end

local root: string = inputs.root

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local function answer(wire: buffer): buffer
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

if inputs.wires ~= nil then
	-- the unit test's batch: one line per mutant, an ERR line for a raise
	for _, wire in inputs.wires do
		local ok, res = pcall(answer, wire)
		if ok then
			print(hex(res))
		else
			print("ERR")
		end
	end
else
	print(hex(answer(inputs.wire)))
end
