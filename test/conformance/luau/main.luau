--!strict
-- THE LUAU CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-LUAU.md,
-- THE HARNESS). The shell driver runs this file once per surface:
--
--   driver <manifest> list
--   driver <manifest> <surface> <outdir>
--
-- A Luau program has no io library, so the driver hands the derived manifest
-- and every fixture it names IN AS SOURCE (build/conformance-luau/driver/
-- manifest.luau and inputs.luau), and this file writes its answers OUT on
-- stdout, one line per case: the case name, a space, the answer's bytes as
-- lowercase hex, or the word `absent`. The shell driver turns each line into
-- one file per case in <outdir>.
--
-- The working directory is the repository root, which is the contract: every
-- path in the manifest is repo-relative, so this driver never resolves one.
--
-- The generated tree is a PATH for the same reason the JavaScript leg's is
-- (test/conformance/js/main.mjs): the negative control points this same driver
-- at a sabotaged copy of the generated modules and requires the matrix to go
-- red. LUAU_CONFORMANCE_GENERATED names it, relative to the repository root;
-- the default is what the Makefile writes (build/conformance-luau).

local manifest: string = require("../../../build/conformance-luau/driver/manifest")
local inputs: { [string]: buffer } = require("../../../build/conformance-luau/driver/inputs")
local unitModules: { [string]: { string } } = require("../../../build/conformance-luau/driver/units")

local args = { ... }
local GENERATED: string = args[2] or "build/conformance-luau"
local SURFACES = { wire = true, report = true, message = true }

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

-- The unit's table runtime (schema_tables, load, save, message). A unit the
-- target did not emit is nil, and its cases are answered `absent`.
local units: { [string]: any } = {}
local function unit(name: string): any?
	if units[name] ~= nil then
		return units[name]
	end
	local base = "../../../" .. GENERATED .. "/" .. name .. "/"
	local ok, S, load, save, message = pcall(function()
		local T = require(base .. "schema_tables")
		-- every table module registers its descriptors; requiring them is what
		-- makes S.lookup(name) resolve a root
		for _, module in unitModules[name] or {} do
			require(base .. module)
		end
		return T, require(base .. "load"), require(base .. "save"),
			require(base .. "message")
	end)
	if not ok then
		units[name] = false
		return nil
	end
	local u = { S = S, load = load, save = save, message = message }
	units[name] = u
	return u
end

local function report(): any
	-- docs/SPEC-TABLES.md §4: the six counters and the verdict
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

-- the WIRE surface loads a form-1 wire and re-saves it (§3)
local function surfaceWire(manifest: { { string } })
	for _, f in rows(manifest) do
		if f[1] == "instance" then
			local u = unit(f[3])
			local wire = inputs[f[5]]
			if u == nil then
				say(f[2], nil)
			else
				local r = report()
				local v = u.load.load(f[4], wire, r)
				if v == nil then
					error("load refused " .. f[5])
				end
				local saved = u.save.save(u.S.lookup(f[4]), v)
				if saved == nil then
					error("save refused " .. f[5])
				end
				say(f[2], saved)
			end
		end
	end
end

-- the REPORT surface is the six counters and the verdict (§4)
local function surfaceReport(manifest: { { string } })
	for _, f in rows(manifest) do
		if f[1] == "report" then
			local u = unit(f[3])
			if u == nil then
				say(f[2], nil)
			else
				local r = report()
				u.load.load(f[4], inputs[f[5]], r)
				local verdict = (r.refused or 0) > 0 and "refused" or "read"
				local text = string.format("%d,%d,%d,%d,%d,%s,%s\n",
					r.unknown, r.kindMismatch, r.widened, r.clamped,
					r.duplicate, tostring(r.malformed), verdict)
				local out = buffer.fromstring(text)
				say(f[2], out)
			end
		end
	end
end

-- the MESSAGE surface reads the connection's announcement into one
-- direction's VOCABULARY, then the batch against it, and writes it back (§3.3)
local connections: { [string]: { string } } = {}
local function surfaceMessage(manifest: { { string } })
	local all = rows(manifest)
	for _, f in all do
		if f[1] == "connection" then
			connections[f[2]] = f
		end
	end
	for _, f in all do
		if f[1] == "message" then
			local c = connections[f[3]]
			if c == nil then
				error("no connection " .. f[3] .. " for " .. f[2])
			end
			local u = unit(c[3])
			if u == nil then
				say(f[2], nil)
			else
				local r = report()
				local v = u.message.NewVocabulary()
				if not u.message.AnnounceRead(v, inputs[c[5]], r) then
					error("announcement refused " .. c[5])
				end
				local count, bodies = u.message.LoadMessages(v, f[4], inputs[f[6]], 256)
				if count < 0 then
					error("message batch refused " .. f[6])
				end
				local saved = u.message.SaveMessages(v, f[4], bodies, r)
				if saved == nil then
					error("message save refused " .. f[6])
				end
				say(f[2], saved)
			end
		end
	end
end

local function usage()
	error("usage: driver <manifest> surface")
end

local surface = args[1]
if surface == nil then
	usage()
end

if surface == "list" then
	for name in pairs(SURFACES) do
		print(name)
	end
	return
end

if not SURFACES[surface] then
	error("unknown surface " .. surface)
end

if surface == "wire" then
	surfaceWire(manifest)
elseif surface == "report" then
	surfaceReport(manifest)
elseif surface == "message" then
	surfaceMessage(manifest)
end
