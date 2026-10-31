--!strict
-- THE MOO LEG'S RETAIN SURFACES (test/conformance/README.md, the surfaces;
-- docs/SPEC-TABLES.md §6.6). main.moo hands this module its context and takes
-- back the surfaces it carries:
--
--   retain       LoadRetain the wire at the row's two capacities, MeasureRetain,
--                SaveRetain, the counters `<retained>,<retain_lost>,<unknown> <save lost>\n`
--   retain-save  the same round trip, the SAVED BYTES
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

	local function rows(saves: boolean)
		for _, f in ctx.kind("retain") do
			if saves and f[#f] == "-" then
				continue
			end
			if not carried(nil, f[3]) then
				ctx.say(f[2], nil)
				continue
			end
			local a, why = file(f)
			if a == nil then
				error("retain: " .. (why or f[2]), 0)
			end
			if not saves then
				ctx.say(f[2], a.counters)
			else
				ctx.say(f[2], buffer.tostring(a.saves[1]))
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
