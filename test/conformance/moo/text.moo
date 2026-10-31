--!strict
-- THE MOO LEG'S TEXT SURFACES (test/conformance/README.md, the surfaces;
-- docs/SPEC-TABLES.md §16). main.moo hands this module its context and takes
-- back the surfaces it carries:
--
--   json-read     FromJson json/<name>.json, Save, the bytes
--   json-write    Load the wire file, ToJson, the text
--   json-hostile  FromJson <tree>/<root>.json, the report as
--                 `u,k,w,c,d,m,verdict\n`, or `refused\n`
--
-- through the emitted json.moo's descriptor-driven walk, the table load and save
-- and the descriptors the unit's <Base>Table modules register. A row marked
-- no-text is carried on the wire alone and is not asked.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "json") ~= nil and ctx.module(key, "table_load") ~= nil
	end

	local function noText(f: { string }): boolean
		return f[6] == "no-text"
	end

	local surfaces: { [string]: Surface } = {}
	surfaces["json-read"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("instance") do
				if noText(f) then
					continue
				elseif not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				ctx.tables(f[3])
				local J: any = ctx.module(f[3], "json")
				local save: any = ctx.module(f[3], "table_save")
				local d: any = (ctx.module(f[3], "schema_tables") :: any).lookup(f[4])
				local rec = d.new()
				local report: any = {}
				if not J.fromJson(d, rec, ctx.inputs("testdata/conformance/tables/json/" .. f[2] .. ".json"), report) then
					ctx.say(f[2], "refused")
					continue
				end
				local out = save.save(d, rec)
				ctx.say(f[2], if out ~= nil then buffer.tostring(out) else "refused")
			end
		end,
	}
	surfaces["json-write"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("instance") do
				if noText(f) then
					continue
				elseif not carried(nil, f[3]) then
					ctx.say(f[2] .. ".json", nil)
					continue
				end
				ctx.tables(f[3])
				local J: any = ctx.module(f[3], "json")
				local load: any = ctx.module(f[3], "table_load")
				local d: any = (ctx.module(f[3], "schema_tables") :: any).lookup(f[4])
				local report = load.newReport()
				local v = load.loadTolerant(f[4], ctx.inputs(f[5]), report)
				local text = if v ~= nil and report.refused == 0 then J.toJson(d, v) else nil
				ctx.say(f[2] .. ".json", text or "refused")
			end
		end,
	}
	surfaces["json-hostile"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("json-hostile") do
				if not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				ctx.tables(f[3])
				local J: any = ctx.module(f[3], "json")
				local d: any = (ctx.module(f[3], "schema_tables") :: any).lookup(f[4])
				local rec = d.new()
				local r: any = {}
				local ok = J.fromJson(d, rec, ctx.inputs(f[5] .. "/" .. f[4] .. ".json"), r)
				if not ok then
					ctx.say(f[2], "refused\n")
				else
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
							"read"
						)
					)
				end
			end
		end,
	}
	return surfaces
end
