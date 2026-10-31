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
	"wire", "message", "report", "retain", "retain-save", "json-read", "json-write", "json-hostile",
	"cook", "cook-write", "cook-foreign", "block", "block-foreign", "block-dump", "forgery",
	"cook-forgery", "cook-reason", "block-reason",
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
			load = require(base .. "schema_load"),
			save = require(base .. "schema_save"),
			json = require(base .. "schema_json"),
			retain = require(base .. "schema_retain"),
			cook = require(base .. "schema_cook"),
			base = base,
			optional = {},
		}
	end)
	if not ok then
		units[name] = false
		return nil
	end
	units[name] = u
	return u
end

-- one module of a unit that not every unit emits (the message, cook-read and
-- block surfaces, a <Base>Block.luau), or nil when the target did not emit it
local function optional(u: any, module: string): any?
	local got = u.optional[module]
	if got == false then
		return nil
	elseif got ~= nil then
		return got
	end
	local ok, m = pcall(function() return require(u.base .. module) end)
	if not ok then
		u.optional[module] = false
		return nil
	end
	u.optional[module] = m
	return m
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
-- one.
local function surfaceRetain(lines: { { string } }, saves: boolean)
	local capacityBytes = 1048576
	local fullIds = 131072
	for _, f in lines do
		if f[1] == "retain" then
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

-- ---- the MESSAGE surface (docs/SPEC-TABLES.md §3.3) ------------------------
--
-- `message` reads TWO files (test/conformance/README.md): the CONNECTION's
-- announcement carries a message's id table, AnnounceRead takes it into one
-- direction's vocabulary, and the batch resolves against it. The verbs are
-- PLURAL: LoadMessages the batch, MeasureMessages and SaveMessages the bodies
-- it found, the bytes. A line is `message <name> <connection> <root> <file
-- wire> <message wire>`, a connection `connection <key> <unit> <build> <wire>`.
-- A root that is a variable table carries no message form (§3.3): a named
-- absence.
local function surfaceMessage(lines: { { string } })
	local connections: { [string]: { string } } = {}
	for _, c in kind(lines, "connection") do
		connections[c[2]] = c
	end
	for _, f in kind(lines, "message") do
		local conn = connections[f[3]]
		if conn == nil then
			error("message " .. f[2] .. " names no connection " .. f[3])
		end
		local u = unit(conn[3])
		local M = u and optional(u, "schema_message")
		local d = if u ~= nil then rootOf(u, f[4]) else nil
		if f[6] == "-" then
			-- a named refusal row: the root is a variable table (§3.3)
			print(f[2] .. " absent variable table: the message form is fixed tables only (SPEC-TABLES §3.3)")
		elseif u == nil or M == nil or d == nil then
			say(f[2], nil)
		elseif not (d.table and d.fixed) then
			print(f[2] .. " absent variable table: the message form is fixed tables only (SPEC-TABLES §3.3)")
		else
			local v = M.NewVocabulary()
			local r = report()
			if not M.AnnounceRead(v, file(conn[5]), r) then
				error(f[2] .. ": the announcement was refused (" .. tostring(r.reason) .. ")")
			end
			local count, bodies = M.LoadMessages(v, f[4], file(f[6]), 256, r)
			if count < 1 or r.malformed or r.verdict ~= 0 then
				error(f[2] .. ": message load refused (" .. tostring(r.reason) .. ")")
			end
			local size = M.MeasureMessages(v, f[4], bodies, r)
			if size < 0 then
				error(f[2] .. ": message measure refused (" .. tostring(r.reason) .. ")")
			end
			local out, n = M.SaveMessages(v, f[4], bodies, r)
			if out == nil or n ~= size then
				error(f[2] .. ": message save refused or disagreed with the measure")
			end
			say(f[2], out)
		end
	end
end

-- ---- placing a fixture the way its line asks for ---------------------------
--
-- EXACTLY THE CLAIM, at a base `lead` bytes into a fresh buffer, with what fits
-- copied and the rest zero (test/conformance/README.md, "A forgery line
-- carries an EXTENT and a POINTER"). A Luau buffer has no address to align;
-- what it has is the offset a reader is handed beside it, which is the one
-- placement fact a consumer can state and the one BlockOpen and Check measure
-- — so a lead of N is a base N bytes into the buffer, and its residue modulo
-- the form's alignment is the residue the C++ leg places by hand.
local function place(source: buffer, claim: number, lead: number): buffer
	local out = buffer.create(lead + claim)
	buffer.copy(out, lead, source, 0, math.min(claim, buffer.len(source)))
	return out
end

-- THE OTHER BYTE ORDER: the same file with its eight-byte MAGIC reversed,
-- which is what that word looks like to a reader of the other order, so every
-- leg on every host must refuse it (§19.1, §7.1).
local function foreign(source: buffer): buffer
	local out = buffer.create(buffer.len(source))
	buffer.copy(out, 0, source)
	if buffer.len(out) >= 8 then
		for i = 0, 3 do
			local a, b = buffer.readu8(out, i), buffer.readu8(out, 7 - i)
			buffer.writeu8(out, i, b)
			buffer.writeu8(out, 7 - i, a)
		end
	end
	return out
end

-- ---- the canonical dumps' spellings (testdata/conformance/tables/FORMAT.md) -

local function join(prefix: string, name: string): string
	if prefix == "" then
		return name
	end
	return prefix .. "." .. name
end

-- an unsigned 64-bit word in decimal: Luau's integer is signed, so the digits
-- come out of an unsigned division
local function u64(v: integer): string
	if not integer.lt(v, 0i) then
		return tostring(v)
	end
	local digits = {}
	local x = v
	while x ~= 0i do
		table.insert(digits, 1, tostring(integer.urem(x, 10i)))
		x = integer.udiv(x, 10i)
	end
	return table.concat(digits)
end

local function hexWord(v: integer, nibbles: number): string
	local out = table.create(nibbles)
	for i = 1, nibbles do
		local shift = (nibbles - i) * 4
		out[i] = string.format("%x", integer.tonumber(integer.band(integer.rshift(v, integer.create(shift)), 15i)))
	end
	return table.concat(out)
end

-- a used length of text or bytes: printable ASCII but the quote and the
-- backslash as itself, every other byte as \xHH
local function dumpText(b: buffer, at: number, used: number): string
	if used < 0 then
		used = 0
	end
	local parts = table.create(used + 2)
	parts[1] = "\""
	for i = 0, used - 1 do
		local c = buffer.readu8(b, at + i)
		if c >= 0x20 and c < 0x7f and c ~= 0x22 and c ~= 0x5c then
			parts[#parts + 1] = string.char(c)
		else
			parts[#parts + 1] = string.format("\\x%02x", c)
		end
	end
	return table.concat(parts) .. "\" len=" .. used
end

local function intAt(b: buffer, at: number, width: number, signed: boolean): string
	if width == 1 then
		return tostring(if signed then buffer.readi8(b, at) else buffer.readu8(b, at))
	elseif width == 2 then
		return tostring(if signed then buffer.readi16(b, at) else buffer.readu16(b, at))
	elseif width == 4 then
		return tostring(if signed then buffer.readi32(b, at) else buffer.readu32(b, at))
	elseif width == 8 then
		local v = buffer.readinteger(b, at)
		return if signed then tostring(v) else u64(v)
	end
	error("driver: no integer of width " .. width)
end

-- ---- the BLOCK surfaces (docs/SPEC-TABLES.md §19.1, §19.2) -----------------
--
-- A block line names an image and its unit; the image is one table's block
-- form, and the table is the manifest's own name for it (the C++ and
-- JavaScript legs bind the same two). Its Info is the generated
-- <Base>Block.luau's <Table>Block, and the reader is schema_blockread's
-- BlockOpen, which refuses by the same names Go's does.
local BLOCK_TABLES: { [string]: string } = {
	block_render = "RenderFrame",
	block_padded = "PaddedFrame",
}

local blockRows: { [string]: { string } } = {}
for _, f in kind(rows(manifest), "block") do
	blockRows[f[2]] = f
end

-- the subject's reader and Info: (blockread, info), or nil when the leg has
-- no block form for it
local function blockOf(subject: string): (any?, any?)
	local row = blockRows[subject]
	local tableName = ""
	for prefix, t in BLOCK_TABLES do
		if string.sub(subject, 1, #prefix) == prefix then
			tableName = t
		end
	end
	if row == nil or tableName == "" then
		error("driver: no block named " .. subject)
	end
	local u = unit(row[3])
	if u == nil then
		return nil, nil
	end
	local B = optional(u, "schema_blockread")
	if B == nil then
		return nil, nil
	end
	for _, module in unitModules[row[3]] or {} do
		local base = string.gsub(module, "Table$", "")
		local m = optional(u, base .. "Block")
		if m ~= nil and m[tableName .. "Block"] ~= nil then
			return B, m[tableName .. "Block"]
		end
	end
	return nil, nil
end

-- open one image at the claim and lead its line gives: the reason, or nil
local function blockReason(B: any, info: any, source: buffer?, extent: number, lead: number): string?
	if source == nil then
		local _, why = B.BlockOpen(info, nil, 0, 0)
		return why
	end
	local claim = if extent < 0 then buffer.len(source) else extent
	local _, why = B.BlockOpen(info, place(source, claim, lead), lead, claim)
	return why
end

local function verdict(why: string?): string
	return if why == nil then "open\n" else "refuse\n"
end

local function surfaceBlock(lines: { { string } }, foreignOrder: boolean)
	for _, f in kind(lines, "block") do
		local B, info = blockOf(f[2])
		if B == nil then
			say(f[2], nil)
		else
			local source = file(f[4])
			if foreignOrder then
				source = foreign(source)
			end
			sayText(f[2], verdict(blockReason(B, info, source, -1, 0)))
		end
	end
end

-- the BLOCK ROW DUMP, written against the descriptors and nothing else: no
-- field is named here, which is the claim §19.2 makes for them. A FLOAT is its
-- IEEE-754 bit pattern, because a block row is a byte-identical projection.
local function blockScalar(b: buffer, at: number, k: number, width: number): string
	if k == 1 then
		return if buffer.readu8(b, at) ~= 0 then "true" else "false"
	elseif k == 10 then
		return "0x" .. string.format("%08x", buffer.readu32(b, at))
	elseif k == 11 then
		return "0x" .. hexWord(buffer.readinteger(b, at), 16)
	end
	return intAt(b, at, width, k >= 2 and k <= 5)
end

local function blockRecord(out: { string }, b: buffer, at: number, info: any, path: string)
	for _, f in info.fields do
		if not f.outOfLine then
			local name = join(path, f.name)
			if f.countOffset ~= nil then
				local used = buffer.readi32(b, at + f.countOffset)
				if used < 0 or used > f.arrayBound then
					error(info.name .. "." .. f.name .. " carries a used length of " .. used .. ", outside [ 0, " .. f.arrayBound .. " ]")
				end
				out[#out + 1] = "  " .. name .. " = " .. dumpText(b, at + f.offset, used)
			else
				local slots = if f.isArray then f.arrayBound else 1
				for s = 0, slots - 1 do
					local where = if f.isArray then name .. "[" .. s .. "]" else name
					local value = at + f.offset + s * f.elemSize
					if f.element ~= nil then
						blockRecord(out, b, value, f.element, where)
					else
						out[#out + 1] = "  " .. where .. " = " .. blockScalar(b, value, f.kind, f.elemSize)
					end
				end
			end
			if f.optional then
				out[#out + 1] = "  " .. name .. "#present = " .. (if buffer.readu8(b, at + f.presentOffset) ~= 0 then "true" else "false")
			end
		end
	end
end

local function blockDump(b: buffer, base: number, info: any): string
	local out: { string } = {}
	out[1] = "projection " .. info.name .. " @0"
	blockRecord(out, b, base, info, "")
	for _, f in info.fields do
		if f.outOfLine then
			local offsetOf = integer.tonumber(buffer.readinteger(b, base + f.offsetOfOffset))
			local count = buffer.readu32(b, base + f.countOffset)
			local stride = buffer.readu32(b, base + f.strideOffset)
			out[#out + 1] = "array " .. f.name .. " " .. f.element.name .. " @" .. offsetOf .. " count=" .. count .. " stride=" .. stride
			for r = 0, count - 1 do
				local at = offsetOf + r * stride
				out[#out + 1] = "row " .. r .. " @" .. at
				blockRecord(out, b, base + at, f.element, "")
			end
		end
	end
	return table.concat(out, "\n") .. "\n"
end

local function surfaceBlockDump(lines: { { string } })
	for _, f in kind(lines, "block") do
		local B, info = blockOf(f[2])
		if B == nil then
			say(f[2], nil)
		else
			local source = file(f[4])
			local b = place(source, buffer.len(source), 0)
			local blk, why = B.BlockOpen(info, b, 0, buffer.len(source))
			if blk == nil then
				error(f[2] .. " does not open: " .. tostring(why))
			end
			sayText(f[2], blockDump(b, 0, info))
		end
	end
end

-- ---- the COOK surfaces (docs/SPEC-TABLES.md §7, §7.4, §7.5) ----------------
--
-- A cook line is `cook <case> <unit> <root> <file>`; a forgery of kind `cook`
-- names its root as the subject, and the unit is the one the cook lines give
-- that root. The reader is schema_cookread: Check is Open's header check at
-- a base and a claimed extent, and the build version it holds a cook to is
-- the unit's own (TableMessageBuildVersion), as cook-write stamps it.
local cookUnits: { [string]: string } = {}
for _, f in kind(rows(manifest), "cook") do
	cookUnits[f[4]] = f[3]
end

-- the root's unit, its cook reader and its build: nil when the leg has none
local function cookOf(root: string): (any?, any?, integer?)
	local name = cookUnits[root]
	if name == nil then
		error("driver: no cook root named " .. root)
	end
	local u = unit(name)
	if u == nil or rootOf(u, root) == nil or #u.modules == 0 then
		return nil, nil, nil
	end
	local C = optional(u, "schema_cookread")
	if C == nil then
		return nil, nil, nil
	end
	return u, C, u.modules[1].TableMessageBuildVersion
end

local function cookReason(C: any, root: string, build: integer, source: buffer?, extent: number, lead: number): string?
	if source == nil then
		return C.Check(root, nil, build, 0, 0)
	end
	local claim = if extent < 0 then buffer.len(source) else extent
	return C.Check(root, place(source, claim, lead), build, lead, claim)
end

-- the COOK's NODE DUMP: every node this side reaches through its own derefs,
-- over the region's bytes and the reader's own layout. A pointer slot is
-- eight bytes holding the SIGNED SELF-RELATIVE delta of §6.3, so a deref is
-- one add and a delta of zero is null. A by-value nesting is storage inside a
-- node, not a node: the walk descends through it to the pointer slots inside.
local function cookDump(C: any, S: any, b: buffer, region: number, length: number, root: string): string
	local L = C.layout
	local out: { string } = {}
	local reached: { { offset: number, name: string } } = {}

	-- the two walks call each other, so they live in one table
	local walk = {}

	local function emit(path: string, value: string)
		out[#out + 1] = "  " .. path .. " = " .. value
	end

	local function scalar(fd: any, at: number): string
		local s = fd.s
		if s == "bool" then
			return if buffer.readu8(b, at) ~= 0 then "true" else "false"
		elseif s == "int" then
			local w = L.intWidth(fd)
			return intAt(b, at, w, fd.signed == true)
		elseif s == "enum" then
			return intAt(b, at, L.enumWidth(#fd.vs), false)
		elseif s == "flags" then
			return intAt(b, at, 8, false)
		end
		-- nothing in the pointered corpus is a float, and a canonical
		-- cross-language spelling of one is a decision this gate does not make
		-- in passing: the day one arrives, the gate says so
		error("the dump met a " .. tostring(s) .. ", whose canonical cross-language spelling this gate does not fix")
	end


	local function element(e: any, at: number, where: string, depth: number)
		if e.s == "tab" then
			walk.storage(at, e.t, where, depth)
		else
			emit(where, scalar(e, at))
		end
	end

	function walk.storage(at: number, typeName: string, path: string, depth: number)
		local d = S.lookup(typeName)
		local offsets = L.offsets(d.fields)
		for i, fd in d.fields do
			local fieldAt = at + offsets[i]
			local name = join(path, fd.name)
			local s = fd.s
			local fl = L.field(fd, fieldAt)
			if s == "ptr" then
				local delta = integer.tonumber(buffer.readinteger(b, fieldAt))
				if delta == 0 then
					emit(name, "null") -- NULL IN A REGION IS A DELTA OF ZERO (§6.3)
				else
					local target = fieldAt + delta
					if target < region or target >= region + length then
						error(d.name .. "." .. fd.name .. " resolves outside the region — a delta of " .. delta)
					end
					emit(name, "-> @" .. (target - region))
					walk.node(target - region, fd.t, depth + 1)
				end
			elseif s == "str" or s == "bytes" then
				local w = if s == "str" and fd.wide then 2 else 1
				local room = if s == "str" then ((fd.bound or 0) + 1) * w else (fd.bound or 0)
				local used = buffer.readi32(b, L.alignUp(fieldAt + room, 4))
				if used < 0 or used > (fd.bound or 0) then
					error(d.name .. "." .. fd.name .. " carries a count companion of " .. used .. ", outside [ 0, " .. (fd.bound or 0) .. " ]")
				end
				emit(name, dumpText(b, fieldAt, used))
			elseif s == "tab" then
				walk.storage(fieldAt, fd.t, name, depth)
			elseif s == "arr" then
				local e = fd.e
				if fd.array == "list" then
					error(d.name .. "." .. fd.name .. ": a list in a cooked node, which this gate does not walk")
				end
				local bound = fd.bound or 0
				local size = L.elem(e).size
				-- a COUNTED array writes all N slots (§7.2), and a slot past the
				-- live count holds the value-initialized element
				for slot = 0, bound - 1 do
					element(e, fieldAt + slot * size, name .. "[" .. slot .. "]", depth)
				end
				if fd.array == "counted" then
					local used = buffer.readi32(b, L.alignUp(fieldAt + size * bound, 4))
					if used < 0 or used > bound then
						error(d.name .. "." .. fd.name .. " carries a count companion of " .. used .. ", outside [ 0, " .. bound .. " ]")
					end
					emit(name .. "#count", tostring(used))
				end
			elseif s == "map" or s == "union" or s == "sblob" or s == "bblob" or s == "text" then
				error(d.name .. "." .. fd.name .. ": a " .. s .. " in a cooked node, which this gate does not walk")
			else
				emit(name, scalar(fd, fieldAt))
			end
			if fd.present ~= nil then
				emit(name .. "#present", if buffer.readu8(b, fieldAt + fl.size - 1) ~= 0 then "true" else "false")
			end
		end
	end

	function walk.node(offset: number, typeName: string, depth: number)
		if depth > 4096 then
			error("the walk nested past any depth a region can hold — a cycle the deref did not close")
		end
		for _, r in reached do
			if r.offset == offset then
				if r.name ~= typeName then
					error("two references name the node at offset " .. offset .. " as two different tables: " .. r.name .. " and " .. typeName)
				end
				-- one node, one visit: sharing and a back-reference are one fact
				return
			end
		end
		local size = L.record(typeName).size
		if offset > length or size > length - offset then
			error("the node at offset " .. offset .. " (" .. typeName .. ", sizeof " .. size .. ") does not fit inside the region's " .. length .. " bytes")
		end
		reached[#reached + 1] = { offset = offset, name = typeName }
		out[#out + 1] = "node " .. (#reached - 1) .. " " .. typeName .. " @" .. offset
		walk.storage(region + offset, typeName, "", depth)
	end

	walk.node(0, root, 0)
	return table.concat(out, "\n") .. "\n"
end

local function surfaceCook(lines: { { string } })
	for _, f in kind(lines, "cook") do
		local u, C, build = cookOf(f[4])
		if u == nil then
			say(f[2], nil)
		else
			local b = file(f[5])
			local why = C.Check(f[4], b, build, 0, buffer.len(b))
			if why ~= nil then
				error(f[2] .. " does not open: " .. why)
			end
			local region = C.layout.alignUp(C.layout.headerBytes, integer.tonumber(buffer.readinteger(b, 40)))
			local length = integer.tonumber(buffer.readinteger(b, 24))
			sayText(f[2], cookDump(C, u.S, b, region, length, f[4]))
		end
	end
end

local function surfaceCookForeign(lines: { { string } })
	for _, f in kind(lines, "cook") do
		local u, C, build = cookOf(f[4])
		if u == nil then
			say(f[2], nil)
		else
			local source = foreign(file(f[5]))
			sayText(f[2], verdict(cookReason(C, f[4], build :: integer, source, -1, 0)))
		end
	end
end

-- ---- the FORGERY batteries and the REASON beside each refusal --------------
--
-- A forgery line is `forgery <name> <kind> <subject> <file> <extent>
-- <pointer>`: the patch is already applied, the extent is the length the
-- caller claims (-1 the file's own) and the pointer the buffer the caller
-- holds (`null` none, else the lead). forgery and cook-forgery answer
-- `open`/`refuse`; block-reason and cook-reason the first failing clause's
-- name, or `ok`. The reason is the reader's own: this driver places bytes and
-- prints what it is told.
local function forgeryReason(f: { string }): (boolean, string?)
	local source: buffer? = nil
	local lead = 0
	if f[7] ~= "null" then
		source = file(f[5])
		lead = tonumber(f[7]) :: number
	end
	local extent = tonumber(f[6]) :: number
	if f[3] == "block" then
		local B, info = blockOf(f[4])
		if B == nil then
			return false, nil
		end
		return true, blockReason(B, info, source, extent, lead)
	end
	local u, C, build = cookOf(f[4])
	if u == nil then
		return false, nil
	end
	return true, cookReason(C, f[4], build :: integer, source, extent, lead)
end

local function surfaceForgery(lines: { { string } }, kindName: string, reasons: boolean)
	for _, f in kind(lines, "forgery") do
		if f[3] == kindName then
			local carried, why = forgeryReason(f)
			if not carried then
				say(f[2], nil)
			elseif reasons then
				sayText(f[2], (why or "ok") .. "\n")
			else
				sayText(f[2], verdict(why))
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
elseif surface == "message" then
	surfaceMessage(lines)
elseif surface == "cook" then
	surfaceCook(lines)
elseif surface == "cook-foreign" then
	surfaceCookForeign(lines)
elseif surface == "block" then
	surfaceBlock(lines, false)
elseif surface == "block-foreign" then
	surfaceBlock(lines, true)
elseif surface == "block-dump" then
	surfaceBlockDump(lines)
elseif surface == "forgery" then
	surfaceForgery(lines, "block", false)
elseif surface == "cook-forgery" then
	surfaceForgery(lines, "cook", false)
elseif surface == "cook-reason" then
	surfaceForgery(lines, "cook", true)
elseif surface == "block-reason" then
	surfaceForgery(lines, "block", true)
end
