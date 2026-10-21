--!strict
-- THE LUAU CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-LUAU.md,
-- THE HARNESS). The shell driver runs this file once per surface:
--
--   driver <manifest> list
--   driver <manifest> <surface> <outdir>
--
-- A Luau program has no io library, so the driver hands the derived manifest
-- IN AS SOURCE (build/conformance-luau/driver/manifest.luau holds its text as
-- a string literal, which this file requires), and this file writes its
-- answers OUT on stdout, one line per case: the case name, a space, the
-- answer's bytes as lowercase hex, or the word `absent`. The shell driver
-- turns each line into one file per case in <outdir>.
--
-- The fixture files and the generated modules are handed in the same way, as
-- modules the make/luau-conformance.mk writes under build/luau-driver/ and the
-- leg's build writes under build/conformance-luau/ (docs/PORT-LUAU.md, THE
-- HARNESS). The working directory is the repository root, which is the
-- contract: every path in the manifest is repo-relative, so this driver never
-- resolves one.
--
-- Only the surfaces whose runtime has landed are listed; every other surface
-- stays absent so the matrix never claims a leg carries it (docs/PORT-LUAU.md,
-- THE CARD MAP).

local manifest: string = require("../../../build/conformance-luau/driver/manifest")

local args = { ... }
local surface = args[1]

local GENERATED = "build/conformance-luau"

local SURFACES = { wire = true, report = true }

-- `list` names the surfaces and reads no case, so it needs none of the
-- staged inputs (build/luau-driver is made by make/luau-conformance.mk)
if surface == "list" then
	for name in pairs(SURFACES) do
		print(name)
	end
	return
end

local inputs: { [string]: buffer } = require("../../../build/luau-driver/inputs")
local unitModules: { [string]: { string } } = require("../../../build/luau-driver/units")

local function rows(text: string): { { string } }
	local out = {}
	for raw in string.gmatch(text, "[^\n]+") do
		local line = string.gsub(raw, "^%s+", ""):gsub("%s+$", "")
		if #line > 0 and string.sub(line, 1, 1) ~= "#" then
			local f = {}
			for word in string.gmatch(line, "%S+") do
				f[#f + 1] = word
			end
			out[#out + 1] = f
		end
	end
	return out
end

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

-- A unit's table runtime (schema_tables, load, save). A unit the target did
-- not emit is nil, and its cases are answered `absent`.
local units: { [string]: any } = {}
local function unit(name: string): any?
	if units[name] == false then
		return nil
	elseif units[name] ~= nil then
		return units[name]
	end
	local base = "../../../" .. GENERATED .. "/" .. name .. "/"
	local ok, T, loadmod, savemod = pcall(function()
		local T = require(base .. "schema_tables")
		-- every table module registers its descriptors; requiring them is what
		-- makes S.lookup(name) resolve a root. Not every schema file has a
		-- table module, so a missing one is skipped, never fatal.
		for _, module in unitModules[name] or {} do
			pcall(function() require(base .. module) end)
		end
		return T, require(base .. "load"), require(base .. "save")
	end)
	if not ok then
		units[name] = false
		return nil
	end
	local u = { S = T, load = loadmod, save = savemod }
	units[name] = u
	return u
end

local function report(): any
	-- docs/SPEC-TABLES.md §4: the six counters and the verdict, as the
	-- harness's Counts (test/conformance/harness/manifest.go) reads them
	return {
		malformed = false,
		refused = 0,
		unknown = 0,
		kindMismatch = 0,
		clamped = 0,
		widened = 0,
		duplicate = 0,
		verdict = 0,
		reason = "",
	}
end

local function say(case: string, b: buffer?)
	if b == nil then
		print(case .. " absent")
	else
		print(case .. " " .. hex(b))
	end
end

-- the WIRE surface loads a form-1 wire and re-saves it (§3). An instance
-- line is `instance <name> <unit> <root> <wire>`; a report line is
-- `report <case> <unit> <root> <wire>` (test/conformance/harness/run.go,
-- deriveManifest).
local function surfaceWire(manifest: { { string } })
	for _, f in rows(manifest) do
		if f[1] == "instance" then
			local u = unit(f[3])
			local wire = inputs[f[5]]
			if u == nil or wire == nil then
				say(f[2], nil)
			else
				-- the TOLERANT read, as the reference leg's driver reads (§6.5)
				local r = report()
				local v = u.load.loadTolerant(f[4], wire, r)
				if v == nil then
					error("load refused " .. f[5])
				end
				local saved, why = u.save.save(u.S.lookup(f[4]), v)
				if saved == nil and why ~= nil and why.name == "blob_shared" then
					-- every writer refuses one blob named from two slots
					-- (docs/SPEC-TABLES.md §2.5): the case is a named absence
					say(f[2], nil)
				elseif saved == nil then
					error("save refused " .. f[5])
				else
					say(f[2], saved)
				end
			end
		end
	end
end

-- the REPORT surface is the six counters and the verdict (§4)
local function surfaceReport(manifest: { { string } })
	for _, f in rows(manifest) do
		if f[1] == "report" then
			local u = unit(f[3])
			if u == nil or inputs[f[5]] == nil then
				say(f[2], nil)
			else
				local r = report()
				u.load.loadTolerant(f[4], inputs[f[5]], r)
				local verdict = r.refused > 0 and "refused" or "read"
				local text = string.format("%d,%d,%d,%d,%d,%s,%s\n",
					r.unknown, r.kindMismatch, r.widened, r.clamped,
					r.duplicate, tostring(r.malformed), verdict)
				local out = buffer.fromstring(text)
				say(f[2], out)
			end
		end
	end
end

if surface == nil then
	error("usage: driver <manifest> surface")
end

if not SURFACES[surface] then
	error("unknown surface " .. surface)
end

if surface == "wire" then
	surfaceWire(manifest)
elseif surface == "report" then
	surfaceReport(manifest)
end
