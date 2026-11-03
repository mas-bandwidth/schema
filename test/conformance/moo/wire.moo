--!strict
-- THE MOO LEG'S VARIABLE-WIRE SURFACES (test/conformance/README.md, the
-- surfaces; docs/SPEC-TABLES.md §3, §4). main.moo hands this module its context
-- and takes back the surfaces it carries:
--
--   wire    Load the instance's wire, Save, the bytes
--   report  Load the wire tolerantly, the report as `u,k,w,c,d,m,verdict\n`
--   framing the report line as `report` prints it, then the §16 text of the
--           value the read left, unless the row says `no-value` or the read
--           was refused (docs/LAYERS/1-framing.md)
--   strict-reason  the strict read's one reason (§6.5) for every report and
--           framing row's wire, as the refusal-names enum spells it, or ok
--
-- through the emitted table_load.moo and table_save.moo and the descriptors the
-- unit's <Base>Table modules register. ctx is the one block.moo documents, with
-- tables(key) added: it requires every <Base>Table module of a unit, and
-- tableNames(key), which names them.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_load") ~= nil and ctx.module(key, "table_save") ~= nil
	end

	-- the load of one wire at its root: the value, the report, and whether the
	-- load refused (a refused report, or a non-finite float with no value)
	local function loadWire(f: { string }): (any?, any, any, any, boolean)
		ctx.tables(f[3])
		local load: any = ctx.module(f[3], "table_load")
		local save: any = ctx.module(f[3], "table_save")
		local schema: any = ctx.module(f[3], "schema_tables")
		local report = load.newReport()
		local v = load.loadTolerant(f[4], ctx.inputs(f[5]), report)
		local refused = report.refused > 0 or (v == nil and report.nonFinite > 0)
		return v, report, save, schema, refused
	end

	local surfaces: { [string]: Surface } = {}
	surfaces["wire"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("instance") do
				if not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				local v, _, save, schema, refused = loadWire(f)
				if v == nil or refused then
					ctx.say(f[2], "refused")
					continue
				end
				local out, why = save.save(schema.lookup(f[4]), v)
				if out ~= nil then
					ctx.say(f[2], buffer.tostring(out))
				elseif why ~= nil and why.name == "blob_shared" then
					-- the writer refusal every writer owes (docs/SPEC-TABLES.md §2.5)
					ctx.refuse(f[2], why.name)
				else
					ctx.say(f[2], "refused")
				end
			end
		end,
	}
	surfaces["report"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("report") do
				if not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				-- the report line, then the §16 text of the value when the read
				-- reads; a unit with no text module answers the line alone beside
				-- its absence, so the verdict is held and the value is the missing
				-- feature (test/conformance/README.md)
				local v, r, _, schema, refused = loadWire(f)
				local malformed = r.malformed or (v == nil and not refused)
				local text = string.format(
					"%d,%d,%d,%d,%d,%s,%s\n",
					r.unknown,
					r.kindMismatch,
					r.widened,
					r.clamped,
					r.duplicate,
					tostring(malformed),
					if refused then "refused" else "read"
				)
				if not refused and not malformed then
					local J: any = ctx.module(f[3], "json")
					if J == nil then
						ctx.say(f[2], text)
						ctx.say(f[2], nil)
						continue
					end
					text ..= J.toJson(schema.lookup(f[4]), v)
				end
				ctx.say(f[2], text)
			end
		end,
	}
	-- THE FRAMING VECTORS (docs/LAYERS/1-framing.md). A derived line is
	-- `framing <case> <unit> <root> <wire> [no-value]`. A damaged row owes the
	-- refusal alone (D1), so a row with no value owes nothing past the report.
	surfaces["framing"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("framing") do
				if not carried(nil, f[3]) or ctx.module(f[3], "json") == nil then
					ctx.say(f[2], nil)
					continue
				end
				local v, r, _, schema, refused = loadWire(f)
				local malformed = r.malformed or (v == nil and not refused)
				local text = string.format(
					"%d,%d,%d,%d,%d,%s,%s\n",
					r.unknown,
					r.kindMismatch,
					r.widened,
					r.clamped,
					r.duplicate,
					tostring(malformed),
					if refused then "refused" else "read"
				)
				if not refused and f[6] ~= "no-value" then
					if v == nil then
						ctx.say(f[2], text .. "!no value")
						continue
					end
					local J: any = ctx.module(f[3], "json")
					text ..= J.toJson(schema.lookup(f[4]), v)
				end
				ctx.say(f[2], text)
			end
		end,
	}
	-- THE STRICT READ'S ONE REASON (docs/SPEC-TABLES.md §6.5) for every report
	-- and framing row: table_load.moo's ReadStrict, the runtime's one strict
	-- read, its report.reason spelled as the bare refusal-names member (a name
	-- like `missing_required: <field>` answers its name alone), or ok for a
	-- read it keeps. T is the <Base>Table module carrying the root's fixed
	-- form, which a fixed root's form-3 file reads through.
	local function strictCarried(_: any, key: string): boolean
		return ctx.module(key, "table_load") ~= nil
	end
	surfaces["strict-reason"] = {
		carried = strictCarried,
		run = function()
			for _, what in { "report", "framing" } do
				for _, f in ctx.kind(what) do
					if not strictCarried(nil, f[3]) then
						ctx.say(f[2], nil)
						continue
					end
					ctx.tables(f[3])
					local L: any = ctx.module(f[3], "table_load")
					local T: any = nil
					for _, name in ctx.tableNames(f[3]) do
						local m: any = ctx.module(f[3], name)
						if m ~= nil and m[f[4] .. "Fixed"] ~= nil then
							T = m
						end
					end
					local report: any = {}
					local v = L.ReadStrict(f[4], ctx.inputs(f[5]), report, T)
					local reason: string? = report.reason
					if reason ~= nil then
						ctx.say(f[2], (string.match(reason, "^[%w_]+") or reason) .. "\n")
					elseif v ~= nil then
						ctx.say(f[2], "ok\n")
					else
						ctx.refuse(f[2], "no value and no reason named")
					end
				end
			end
		end,
	}
	return surfaces
end
