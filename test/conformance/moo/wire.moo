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
--
-- through the emitted table_load.moo and table_save.moo and the descriptors the
-- unit's <Base>Table modules register. ctx is the one block.moo documents, with
-- tables(key) added: it requires every <Base>Table module of a unit.

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
				local _, r, _, _, refused = loadWire(f)
				ctx.say(
					f[2],
					string.format(
						"%d,%d,%d,%d,%d,%s,%s\n",
						r.unknown,
						r.kindMismatch,
						r.widened,
						r.clamped,
						r.duplicate,
						tostring(r.malformed),
						if refused then "refused" else "read"
					)
				)
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
	return surfaces
end
