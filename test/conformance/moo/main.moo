--!strict
-- THE MOO CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-MOO.md,
-- THE HARNESS, layer 5). The shell driver beside this file runs it once per
-- surface:
--
--   moo main.moo -a list <generated> <stage>
--   moo main.moo -a <surface> <generated> <stage>
--
-- moo has no io, so everything comes in as SOURCE and goes out on stdout
-- (test/conformance/moo/driver): the derived manifest is the module
-- <stage>/manifest.moo, the fixtures its lines name are <stage>/inputs.moo
-- (buffers by repo-relative path), and the answers are
-- one line per case, `<case> <hex>` or `<case> absent` (per-case absence,
-- docs/PORTING.md I4). <generated> is the repo-relative tree
-- make/moo-conformance.mk writes, one directory per manifest unit; a unit the
-- target refused has no directory and every case naming it is absent.
--
-- `list` names a surface when this file carries it AND some unit's generated
-- tree carries the runtime it calls, so the matrix never claims a surface the
-- leg cannot answer: a surface is listed by what loads, never by hope.

local args = { ... }
local surface: string = args[1] or ""
local generated: string = args[2] or "build/conformance-moo"
-- <stage> is the repo-relative directory the shell driver staged this
-- surface's source into ($MOO_DRIVER_STAGE/<surface>), so a run beside
-- another (a negative control under make -j) reads only its own. It has no
-- default: a run that names none never reads another run's staging.
local staged: string? = args[3]

-- require resolves relative to this file, test/conformance/moo/
local ROOT = "../../../"
local load = require :: any

local function fail(message: string): never
	error("driver: " .. message, 0)
end

-- ---- the manifest, exactly as testdata/conformance/tables/FORMAT.md states it

if staged == nil or staged == "" then
	fail("no stage directory: driver <manifest> <surface> hands main.moo its own")
end
local stage = ROOT .. (staged :: string) .. "/"
local manifestText: string = load(stage .. "manifest")

local lines: { { string } } = {}
for raw in string.gmatch(manifestText, "[^\n]+") do
	local text = string.match(raw, "^%s*(.-)%s*$") or ""
	if #text > 0 and string.sub(text, 1, 1) ~= "#" then
		local f = {}
		for word in string.gmatch(text, "%S+") do
			f[#f + 1] = word
		end
		lines[#lines + 1] = f
	end
end

local function kind(what: string): { { string } }
	local out = {}
	for _, f in lines do
		if f[1] == what then
			out[#out + 1] = f
		end
	end
	return out
end

-- ---- the answers

local HEX = table.create(256)
for i = 0, 255 do
	HEX[i + 1] = string.format("%02x", i)
end

local function hex(b: buffer, n: number?): string
	local count = n or buffer.len(b)
	local parts = table.create(count)
	for i = 0, count - 1 do
		parts[i + 1] = HEX[buffer.readu8(b, i) + 1]
	end
	return table.concat(parts)
end

local function say(case: string, b: buffer?, n: number?)
	if b == nil then
		print(case .. " absent")
	else
		print(case .. " " .. hex(b, n))
	end
end

local function sayText(case: string, text: string)
	say(case, buffer.fromstring(text))
end

-- ---- the units: what the generated tree carries, loaded once each

-- The module a unit's tree is entered through, and what each surface needs from
-- it. A unit the target refused (make/moo-conformance.mk records it as
-- <unit>.refused and writes no tree) does not load, and its cases are absent.
type Unit = { [string]: any }
local units: { [string]: Unit | false } = {}

local ENTRY = "schema_tables"

local function unit(key: string): Unit?
	local held = units[key]
	if held == false then
		return nil
	elseif held ~= nil then
		return held
	end
	local ok, mod = pcall(load, ROOT .. generated .. "/" .. key .. "/" .. ENTRY)
	if not ok or type(mod) ~= "table" then
		units[key] = false
		return nil
	end
	units[key] = mod
	return mod
end

-- ---- the surfaces this file carries
--
-- Each is { carried = <does this unit carry it>, run = <answer every case> }.
-- None is carried until the moo table layers land (docs/PORT-MOO.md, THE
-- LAYERS 3 and 4): until then every surface is ABSENT at the surface grain,
-- which is the honest answer for a leg with no table runtime.
type Surface = { carried: (Unit, string) -> boolean, run: () -> () }
local SURFACES: { [string]: Surface } = {}
local ORDER: { string } = {}

-- ---- the surface modules beside this file
--
-- A table form answers its surfaces from its own module, test/conformance/moo/
-- <name>.moo (block.moo, ...): a function of ctx that returns the surfaces it
-- carries, name -> Surface. stage.pl lists the modules that exist (plugins.moo),
-- so a module that exists and fails to load fails the driver, and one that does
-- not exist is simply not there. ctx hands over the manifest's lines, the unit
-- modules, the staged fixtures and the answer line.
local plugins: { string } = load(stage .. "plugins")
local inputs: { [string]: buffer } = {}
if surface ~= "list" then
	inputs = load(stage .. "inputs")
end

local tableModules: { [string]: { string } } = load(stage .. "tables")

-- the <Base>Table module of unit key that carries root's <Root>Fixed data,
-- or nil: a variable root, or a fixed root this build emits no form-3 data for
local function fixedTable(key: string, root: string): any?
	for _, name in tableModules[key] or {} do
		local m: any = load(ROOT .. generated .. "/" .. key .. "/" .. name)
		if type(m) == "table" and m[root .. "Fixed"] ~= nil then
			return m
		end
	end
	return nil
end

local ctx = {
	-- require every <Base>Table module of a unit, which registers its descriptors
	tables = function(key: string)
		for _, name in tableModules[key] or {} do
			load(ROOT .. generated .. "/" .. key .. "/" .. name)
		end
	end,
	kind = kind,
	module = function(key: string, name: string): any?
		local ok, mod = pcall(load, ROOT .. generated .. "/" .. key .. "/" .. name)
		if ok and type(mod) == "table" then
			return mod
		end
		return nil
	end,
	inputs = function(path: string): buffer
		return inputs[path] or fail("no staged fixture " .. path)
	end,
	-- THE ONE READ (docs/SPEC-TABLES.md §6.5): every wire a surface loads goes
	-- through the unit's table_refusal.moo, which answers the root's own form
	-- (form 1 for a variable root, form 3 for a fixed root through its fixed
	-- reader) and refuses every other by name, as the Go oracle does
	-- (internal/tablewire/decode.go). The value, or nil, and the report.
	read = function(key: string, root: string, bytes: buffer): (any?, any)
		local L: any = load(ROOT .. generated .. "/" .. key .. "/table_load")
		local R: any = load(ROOT .. generated .. "/" .. key .. "/table_refusal")
		local report = L.newReport()
		local v = R.ReadStrict(fixedTable(key, root), root, bytes, report)
		-- a read that names a refusal or damage returned no value, whatever
		-- it handed back beside the name (§4: damage discards the whole load)
		if report.damaged or report.reason ~= nil then
			return nil, report
		end
		return v, report
	end,
	-- THE FRAMING ROWS' READ (test/conformance/harness/generate.go,
	-- framingVariableUnit): the framing vectors are form-1 files whose rules
	-- are the variable form's, so a fixed root reads them through the variable
	-- form's strict read, table_load.moo's ReadStrict, with the same fields
	-- and ids; the form byte is still answered first (form 3 and any other
	-- byte newer_form, form 2 message_form_as_file). The value, or nil, and
	-- the report. Every other surface reads through ctx.read.
	readVariable = function(key: string, root: string, bytes: buffer): (any?, any)
		local L: any = load(ROOT .. generated .. "/" .. key .. "/table_load")
		local report = L.newReport()
		local v = L.ReadStrict(root, bytes, report)
		if report.damaged or report.reason ~= nil then
			return nil, report
		end
		return v, report
	end,
	-- THE ONE WRITE: a fixed root's canonical file is form 3, one record,
	-- through table_fixed.moo; any other root's is form 1 through
	-- table_save.moo. The bytes, or nil and the writer's refusal.
	write = function(key: string, root: string, v: any): (buffer?, any?)
		local S: any = load(ROOT .. generated .. "/" .. key .. "/schema_tables")
		local d: any = S.lookup(root)
		local T: any = if d.fixed then fixedTable(key, root) else nil
		if T ~= nil then
			local F: any = load(ROOT .. generated .. "/" .. key .. "/table_fixed")
			return F.save(d, T[root .. "Fixed"], { v })
		end
		local W: any = load(ROOT .. generated .. "/" .. key .. "/table_save")
		return W.save(d, v)
	end,
	-- a named absence: the case the leg cannot write for a reason the corpus names
	refuse = function(case: string, reason: string)
		print(case .. " absent " .. reason)
	end,
	say = function(case: string, text: string?)
		if text == nil then
			print(case .. " absent")
		else
			sayText(case, text)
		end
	end,
}

for _, name in plugins do
	local carried: (any) -> { [string]: Surface } = load("./" .. name)
	for surfaceName, s in carried(ctx) do
		SURFACES[surfaceName] = s
		ORDER[#ORDER + 1] = surfaceName
	end
end

table.sort(ORDER)

if surface == "list" then
	for _, name in ORDER do
		local s = SURFACES[name]
		for _, u in kind("unit") do
			local m = unit(u[2])
			if m ~= nil and s.carried(m, u[2]) then
				print(name)
				break
			end
		end
	end
	return
end

local s = SURFACES[surface]
if s == nil then
	print("!surface-absent")
	return
end

local _ = sayText
local _ = fail
s.run()
