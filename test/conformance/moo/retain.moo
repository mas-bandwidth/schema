--!strict
-- THE MOO LEG'S RETAIN SURFACES (test/conformance/README.md, the surfaces;
-- docs/SPEC-TABLES.md §6.6). main.moo hands this module its context and takes
-- back the surfaces it carries:
--
--   retain       LoadRetain the wire at the row's two capacities, MeasureRetain,
--                SaveRetain, the counters `<retained>,<retain_lost>,<unknown> <save lost>\n`
--   retain-save  the same round trip, the SAVED BYTES (a form-2 row: one body
--                after another, in body order)
--
-- through the emitted table_retain.moo and the descriptors the unit's
-- <Base>Table modules register. A row's capacities are rules: `full` is a
-- buffer the whole load fits in, `short` is ONE BYTE SHORT OF THE LAST RECORD
-- (load once roomy, read what the store used, load again one byte under it),
-- and the id list's capacity is a COUNT. The pair is run twice and the second
-- save must be the first byte for byte, as the reference's pair is held to.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

-- the roomy capacities the Go driver uses (test/conformance/go/retain.go)
local CAPACITY = 1048576
local IDS = 131072

-- a batch takes one region and one retention buffer a body; no manifest batch
-- carries more bodies than this
local STORES = 8

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_retain") ~= nil
	end

	-- one row's answer: the counters line and the saved bytes, or nil (with a
	-- reason on the case) when this leg cannot write it
	type Answer = { counters: string, saves: { buffer } }

	local function sameBytes(a: buffer, b: buffer): boolean
		return buffer.len(a) == buffer.len(b) and buffer.readstring(a, 0, buffer.len(a)) == buffer.readstring(b, 0, buffer.len(b))
	end

	-- retain <case> <unit> <root> <wire> <capacity> <ids> ...
	local function file(f: { string }): (Answer?, string?)
		local unit, rootName = f[3], f[4]
		ctx.tables(unit)
		local R: any = ctx.module(unit, "table_retain")
		local L: any = ctx.module(unit, "table_load")
		local wire = ctx.inputs(f[5])
		local ids = if f[7] == "full" then IDS else tonumber(f[7]) :: number
		local capacity = CAPACITY
		local store = R.newStore(capacity, ids)
		local report = L.newReport()
		if f[6] == "short" then
			if R.LoadRetain(rootName, wire, report, store) == nil then
				return nil, f[2] .. ": capacity probe refused"
			end
			capacity = math.max(0, R.used(store) - 1)
			store = R.newStore(capacity, ids)
			report = L.newReport()
		end
		local v = R.LoadRetain(rootName, wire, report, store)
		if v == nil or report.malformed then
			return nil, f[2] .. ": retained load refused"
		end
		local retained, lost, unknown = report.retained, report.retainLost, report.unknown
		local size, why = R.MeasureRetain(rootName, v, store)
		if size == nil or size < 0 then
			return nil, f[2] .. ": retaining measure refused " .. tostring(if why ~= nil then why.name else nil)
		end
		local out = R.SaveRetain(rootName, v, store, report)
		if out == nil or buffer.len(out) ~= size then
			return nil, f[2] .. ": retaining save refused"
		end
		local saveLost = report.retainLost - lost
		local second = L.newReport()
		local again = R.SaveRetain(rootName, v, store, second)
		if again == nil or not sameBytes(again, out) or second.retainLost ~= saveLost then
			return nil, f[2] .. ": save is not repeatable"
		end
		return { counters = string.format("%d,%d,%d %d\n", retained, lost, unknown, saveLost), saves = { out } }, nil
	end

	-- retain-message <case> <connection> <unit> <root> <wire> <capacity> <ids> ...
	-- the batch against the connection's announced vocabulary, a store a body,
	-- each body saved as a FILE
	local function message(f: { string }): (Answer?, string?)
		local unit, rootName = f[4], f[5]
		ctx.tables(unit)
		local R: any = ctx.module(unit, "table_retain")
		local MS: any = ctx.module(unit, "table_message")
		local announcement: buffer? = nil
		for _, c in ctx.kind("connection") do
			if c[2] == f[3] then
				announcement = ctx.inputs(c[5])
			end
		end
		if announcement == nil then
			return nil, f[2] .. ": no connection " .. f[3]
		end
		local ids = if f[8] == "full" then IDS else tonumber(f[8]) :: number
		local report: any = {}
		local vocabulary = MS.NewVocabulary()
		if not MS.AnnounceRead(vocabulary, announcement, report) then
			return nil, f[2] .. ": announcement refused " .. tostring(report.reason)
		end
		local stores = {}
		for i = 1, STORES do
			stores[i] = R.newStore(CAPACITY, ids)
		end
		local n, values = R.LoadRetainMessages(vocabulary, rootName, ctx.inputs(f[6]), 256, stores, report)
		if n < 0 or report.malformed then
			return nil, f[2] .. ": retained batch load refused " .. tostring(report.reason)
		end
		local retained, lost, unknown = report.retained or 0, report.retainLost or 0, report.unknown or 0
		local saves: { buffer } = {}
		for i = 1, n do
			local out = R.SaveRetain(rootName, values[i], stores[i], report)
			if out == nil then
				return nil, f[2] .. ": retaining save refused"
			end
			saves[#saves + 1] = out
		end
		local saveLost = (report.retainLost or 0) - lost
		return { counters = string.format("%d,%d,%d %d\n", retained, lost, unknown, saveLost), saves = saves }, nil
	end

	local function rows(saves: boolean)
		for _, kindName in { "retain", "retain-message" } do
			for _, f in ctx.kind(kindName) do
				if saves and f[#f] == "-" then
					continue
				end
				local unit = if kindName == "retain" then f[3] else f[4]
				if not carried(nil, unit) then
					ctx.say(f[2], nil)
					continue
				end
				local a: Answer?, why: string?
				if kindName == "retain" then
					a, why = file(f)
				else
					a, why = message(f)
				end
				if a == nil then
					error("retain: " .. (why or f[2]), 0)
				end
				if not saves then
					ctx.say(f[2], a.counters)
				else
					local parts: { string } = {}
					for _, b in a.saves do
						parts[#parts + 1] = buffer.tostring(b)
					end
					ctx.say(f[2], table.concat(parts))
				end
			end
		end
	end

	return {
		["retain"] = {
			carried = carried,
			run = function()
				rows(false)
			end,
		},
		["retain-save"] = {
			carried = carried,
			run = function()
				rows(true)
			end,
		},
	}
end
