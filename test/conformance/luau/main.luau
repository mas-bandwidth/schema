--!strict
-- THE LUAU CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-LUAU.md,
-- THE HARNESS). The shell driver runs this file once per surface:
--
--   driver <manifest> list
--   driver <manifest> <surface> <outdir>
--
-- A Luau program has no io library, so the driver hands the derived manifest
-- IN AS SOURCE (build/conformance-luau/driver/manifest.luau holds its text as
-- a string literal, which this file requires), and every file the manifest's
-- lines name the same way (build/conformance-luau/driver/files.luau, keyed by
-- repo-relative path). This file writes its answers OUT on stdout, one line
-- per case: the case name, a space, the answer's bytes as lowercase hex, or
-- the word `absent` (and, for a named writer refusal, the name after it). The
-- shell driver turns each line into one file per case in <outdir>.
--
-- The generated modules are the leg's build, build/conformance-luau/<unit>/
-- (make/luau.mk), and the per-unit module lists are build/luau-driver/
-- units.luau (make/luau-conformance.mk). The working directory is the
-- repository root, which is the contract: every path in the manifest is
-- repo-relative, so this driver never resolves one.
--
-- Every surface is answered through the generated unit and the table runtime
-- emitted beside it; nothing here knows a field. A case the leg cannot answer
-- is `absent` beside where the answer would go, and the matrix counts it.

local manifest: string = require("../../../build/conformance-luau/driver/manifest")

local args = { ... }
local surface = args[1]

local GENERATED = "build/conformance-luau"

local SURFACES = {
	"wire", "report", "retain", "retain-save", "json-read", "json-write", "json-hostile",
	"cook-write",
}
local listed: { [string]: boolean } = {}
for _, s in SURFACES do
	listed[s] = true
end

-- `list` names the surfaces and reads no case, so it needs none of the
-- staged inputs
if surface == "list" then
	for _, name in SURFACES do
		print(name)
	end
	return
end

local files: { [string]: buffer } = require("../../../build/conformance-luau/driver/files")
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

local function kind(lines: { { string } }, what: string): { { string } }
	local out = {}
	for _, f in lines do
		if f[1] == what then
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

local function file(path: string): buffer
	local b = files[path]
	if b == nil then
		error("driver: the shim staged no file " .. path)
	end
	return b
end

-- A unit's table runtime: the registry (schema_tables), the load, the save,
-- the text form and retention. A unit the target did not emit is nil, and its
-- cases are answered `absent`.
local units: { [string]: any } = {}
local function unit(name: string): any?
	if units[name] == false then
		return nil
	elseif units[name] ~= nil then
		return units[name]
	end
	local base = "../../../" .. GENERATED .. "/" .. name .. "/"
	local ok, u = pcall(function()
		local T = require(base .. "schema_tables")
		-- every table module registers its descriptors; requiring them is what
		-- makes S.lookup(name) resolve a root. Not every schema file has a
		-- table module, so a missing one is skipped, never fatal.
		local modules = {}
		for _, module in unitModules[name] or {} do
			local okm, m = pcall(function() return require(base .. module) end)
			if okm then
				modules[#modules + 1] = m
			end
		end
		return {
			S = T,
			modules = modules,
			load = require(base .. "load"),
			save = require(base .. "save"),
			json = require(base .. "json"),
			retain = require(base .. "retain"),
			cook = require(base .. "cook"),
		}
	end)
	if not ok then
		units[name] = false
		return nil
	end
	units[name] = u
	return u
end

-- the root's descriptor in its unit, or nil when the unit does not carry it
local function rootOf(u: any, root: string): any?
	local ok, d = pcall(function() return u.S.lookup(root) end)
	if ok then
		return d
	end
	return nil
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
		retained = 0,
		retainLost = 0,
	}
end

local function say(case: string, b: buffer?)
	if b == nil then
		print(case .. " absent")
	else
		print(case .. " " .. hex(b))
	end
end

local function sayText(case: string, s: string)
	say(case, buffer.fromstring(s))
end

local function noText(f: { string }): boolean
	return f[6] == "no-text"
end

-- the WIRE surface loads a form-1 wire and re-saves it (§3). An instance
-- line is `instance <name> <unit> <root> <wire>` (test/conformance/harness/
-- run.go, deriveManifest).
local function surfaceWire(lines: { { string } })
	for _, f in kind(lines, "instance") do
		local u = unit(f[3])
		if u == nil or rootOf(u, f[4]) == nil then
			say(f[2], nil)
		else
			-- the TOLERANT read, as the reference leg's driver reads (§6.5)
			local v = u.load.loadTolerant(f[4], file(f[5]), report())
			if v == nil then
				error("load refused " .. f[5])
			end
			local saved, why = u.save.save(u.S.lookup(f[4]), v)
			if saved == nil and why ~= nil and why.name == "blob_shared" then
				-- every writer refuses one blob named from two slots
				-- (docs/SPEC-TABLES.md §2.5): the case is a NAMED refusal, and
				-- the harness holds the name to the manifest's go-refusal
				print(f[2] .. " absent blob_shared")
			elseif saved == nil then
				error("save refused " .. f[5])
			else
				say(f[2], saved)
			end
		end
	end
end

-- the REPORT surface is the six counters and the verdict (§4)
local function surfaceReport(lines: { { string } })
	for _, f in kind(lines, "report") do
		local u = unit(f[3])
		if u == nil or rootOf(u, f[4]) == nil then
			say(f[2], nil)
		else
			local r = report()
			u.load.loadTolerant(f[4], file(f[5]), r)
			local verdict = r.refused > 0 and "refused" or "read"
			sayText(f[2], string.format("%d,%d,%d,%d,%d,%s,%s\n",
				r.unknown, r.kindMismatch, r.widened, r.clamped,
				r.duplicate, tostring(r.malformed), verdict))
		end
	end
end

-- ---- THE TEXT FORM (docs/SPEC-TABLES.md §16, §16.7) ------------------------
--
-- json-read lands the committed text (testdata/conformance/tables/json/
-- <case>.json) and saves the value; json-write loads the wire and writes the
-- text; json-hostile reads one tree's <root>.json and answers the report or
-- `refused`. A `no-text` instance is no text case at all.

local function surfaceJsonRead(lines: { { string } })
	for _, f in kind(lines, "instance") do
		if not noText(f) then
			local u = unit(f[3])
			if u == nil or rootOf(u, f[4]) == nil then
				say(f[2], nil)
			else
				local r = report()
				local v = u.json.FromJson(f[4], file("testdata/conformance/tables/json/" .. f[2] .. ".json"), r)
				if v == nil or r.malformed then
					error(f[2] .. ": FromJson refused the text (" .. tostring(r.reason) .. ")")
				end
				local saved, why = u.save.save(u.S.lookup(f[4]), v)
				if saved == nil then
					error(f[2] .. ": save refused the value (" .. tostring(why and why.name) .. ")")
				end
				say(f[2], saved)
			end
		end
	end
end

local function surfaceJsonWrite(lines: { { string } })
	for _, f in kind(lines, "instance") do
		if not noText(f) then
			local u = unit(f[3])
			if u == nil or rootOf(u, f[4]) == nil then
				say(f[2] .. ".json", nil)
			else
				local v = u.load.loadTolerant(f[4], file(f[5]), report())
				if v == nil then
					error(f[2] .. ": load refused " .. f[5])
				end
				local r = report()
				local text = u.json.ToJson(f[4], v, r)
				if text == nil then
					error(f[2] .. ": ToJson refused the value (" .. tostring(r.reason) .. ")")
				end
				say(f[2] .. ".json", text)
			end
		end
	end
end

local function surfaceJsonHostile(lines: { { string } })
	for _, f in kind(lines, "json-hostile") do
		local u = unit(f[3])
		if u == nil or rootOf(u, f[4]) == nil then
			say(f[2], nil)
		else
			local r = report()
			local v = u.json.FromJsonTolerant(f[4], file(f[5] .. "/" .. f[4] .. ".json"), r)
			if v == nil or r.malformed or (r.refused or 0) > 0 then
				sayText(f[2], "refused\n")
			else
				sayText(f[2], string.format("%d,%d,%d,%d,%d,false,read\n",
					r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate))
			end
		end
	end
end

-- ---- RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6) -----------------------------
--
-- THE TWO RETAIN SURFACES ARE ONE ROUND TRIP AND TWO SHAPES
-- (test/conformance/README.md): `retain` prints the two load counters beside
-- the `unknown` retention never moves and the save's own `retain_lost`;
-- `retain-save` writes the bytes the pair saved. A row's byte capacity is a
-- RULE: `short` is ONE BYTE SHORT OF THE LAST RECORD this port wrote, so the
-- row loads once at a roomy capacity, reads what its own store used, and loads
-- again one byte under that. The id list's capacity is a COUNT and travels as
-- one. A retain-message row's root is a plain `table` (RT1's Node), which
-- carries no message verbs (§3.3), so it is absent and the reason says so.
local function surfaceRetain(lines: { { string } }, saves: boolean)
	local capacityBytes = 1048576
	local fullIds = 131072
	for _, f in lines do
		if f[1] == "retain-message" then
			print(f[2] .. " absent plain table: docs/SPEC-TABLES.md §3.3")
		elseif f[1] == "retain" then
			-- retain <case> <unit> <root> <wire> <capacity> <ids>
			local u = unit(f[3])
			if u == nil or rootOf(u, f[4]) == nil then
				say(f[2], nil)
			else
				local wire = file(f[5])
				local ids = fullIds
				if f[7] ~= "full" then
					ids = tonumber(f[7]) :: number
				end
				local capacity = capacityBytes
				if f[6] == "short" then
					local _, probe = u.retain.LoadRetain(f[4], wire, report(), capacity, ids)
					capacity = math.max(0, probe.used - 1)
				end
				local r = report()
				local root, store = u.retain.LoadRetain(f[4], wire, r, capacity, ids)
				if root == nil or r.malformed then
					error(f[2] .. ": retained load refused (" .. tostring(r.reason) .. ")")
				end
				local retained, lost, unknown = r.retained, r.retainLost, r.unknown
				local out, why = u.retain.SaveRetain(f[4], root, store, r)
				if out == nil then
					error(f[2] .. ": retaining save refused (" .. tostring(why and why.name) .. ")")
				end
				local saveLost = r.retainLost - lost
				-- THE SAVE IS REPEATABLE: a second walk with a fresh report
				-- writes the same bytes and answers the same save-side loss
				local second = report()
				local again = u.retain.SaveRetain(f[4], root, store, second)
				if again == nil or buffer.tostring(again) ~= buffer.tostring(out) or second.retainLost ~= saveLost then
					error(f[2] .. ": save is not repeatable")
				end
				if saves then
					say(f[2], out)
				else
					sayText(f[2], string.format("%d,%d,%d %d\n", retained, lost, unknown, saveLost))
				end
			end
		end
	end
end

-- ---- the COOK WRITE surface (docs/SPEC-TABLES.md §7.6, §19.1) -------------
--
-- Load the instance's wire, measure the cook, then cook it in both byte
-- orders; `<case>` is the little-endian file and `<case>-be` the big. The
-- build version the header records is the unit's own
-- (TableMessageBuildVersion), handed to the cook before it writes.
local function surfaceCookWrite(lines: { { string } })
	local instances: { [string]: { string } } = {}
	for _, f in kind(lines, "instance") do
		instances[f[2]] = f
	end
	for _, f in kind(lines, "cook-write") do
		local inst = instances[f[2]]
		if inst == nil then
			error("cook-write " .. f[2] .. " names no instance")
		end
		local u = unit(inst[3])
		local d = u and rootOf(u, inst[4])
		if u == nil or d == nil or #u.modules == 0 then
			say(f[2], nil)
			say(f[2] .. "-be", nil)
		else
			local v = u.load.loadTolerant(inst[4], file(inst[5]), report())
			if v == nil then
				error(f[2] .. ": cook input did not load")
			end
			u.cook.BuildVersion = u.modules[1].TableMessageBuildVersion
			local size = u.cook.CookMeasure(d, v)
			if size < 1 then
				error(f[2] .. ": cook measure refused")
			end
			for _, order in { 1, 2 } do
				local name = if order == 2 then f[2] .. "-be" else f[2]
				local out = u.cook.Cook(d, v, order)
				if out == nil then
					error(name .. ": cook write refused")
				end
				if buffer.len(out) ~= size then
					error(name .. ": cook wrote " .. buffer.len(out) .. " bytes, measured " .. size)
				end
				say(name, out)
			end
		end
	end
end

if surface == nil then
	error("usage: driver <manifest> surface")
end

if not listed[surface] then
	error("unknown surface " .. surface)
end

local lines = rows(manifest)
if surface == "wire" then
	surfaceWire(lines)
elseif surface == "report" then
	surfaceReport(lines)
elseif surface == "json-read" then
	surfaceJsonRead(lines)
elseif surface == "json-write" then
	surfaceJsonWrite(lines)
elseif surface == "json-hostile" then
	surfaceJsonHostile(lines)
elseif surface == "retain" then
	surfaceRetain(lines, false)
elseif surface == "retain-save" then
	surfaceRetain(lines, true)
elseif surface == "cook-write" then
	surfaceCookWrite(lines)
end
