--!strict
-- THE MOO LEG'S TEXT SURFACES (test/conformance/README.md, the surfaces;
-- docs/SPEC-TABLES.md §16). main.moo hands this module its context and takes
-- back the surfaces it carries:
--
--   json-read     FromJson json/<name>.json, Save, the bytes
--   json-write    Load the wire file, ToJson, the text
--                 and, for a json-write-refusal line, the damage put into storage in
--                 code first, then `refused\n` when ToJson refuses
--   json-hostile  FromJson <tree>/<root>.json, the report as
--                 `u,k,w,c,d,m,verdict\n` and the §16 text of the value it
--                 left, or `refused\n`
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

	-- damageText puts into a fixed root's top-level text field what no read
	-- delivers (testdata/conformance/tables/FORMAT.md, `json-write-refusal`): a
	-- narrow field is a moo string, which holds the bytes 61 FF 62 as they are;
	-- a wide field is its buffer of UTF-16LE units, here 0061 D800 0062, and the
	-- slot's Length.
	local function damageText(v: any, damage: string)
		if damage == "none" then
			return
		end
		local form, name = string.match(damage, "^(%a+):(.+)$")
		if form == nil or name == nil then
			error("damage " .. damage .. " is not one this driver knows")
		end
		local slot = string.upper(string.sub(name, 1, 1)) .. string.sub(name, 2)
		if form == "narrow" then
			v[slot] = "a\xffb"
		elseif form == "wide" then
			v[slot] = buffer.fromstring("a\x00\x00\xd8b\x00")
			v[slot .. "Length"] = 3
		else
			error("damage " .. damage .. " is not one this driver knows")
		end
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
				local v = load.load(f[4], ctx.inputs(f[5]), report)
				local text = if v ~= nil and report.refused == 0 then J.toJson(d, v) else nil
				ctx.say(f[2] .. ".json", text or "refused")
			end
			-- THE VALUES THE WRITER REFUSES (docs/SPEC-TABLES.md §16.14, W0): the
			-- wire, then the line's damage put into storage in code, then
			-- ToJsonMeasure and ToJson, which must both refuse
			for _, f in ctx.kind("json-write-refusal") do
				if not carried(nil, f[3]) then
					ctx.say(f[2] .. ".json", nil)
					continue
				end
				ctx.tables(f[3])
				local J: any = ctx.module(f[3], "json")
				local load: any = ctx.module(f[3], "table_load")
				local d: any = (ctx.module(f[3], "schema_tables") :: any).lookup(f[4])
				local v = load.loadTolerant(f[4], ctx.inputs(f[5]), load.newReport())
				if v == nil then
					error(f[2] .. ": load refused " .. f[5])
				end
				damageText(v, f[6])
				if J.toJsonMeasure(d, v) >= 0 then
					ctx.say(f[2] .. ".json", J.toJson(d, v))
				elseif J.toJson(d, v) ~= nil then
					error(f[2] .. ": ToJsonMeasure refused and ToJson wrote")
				else
					ctx.say(f[2] .. ".json", "refused\n")
				end
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
							0,
							r.widened,
							0,
							0,
							tostring(r.damaged),
							"read"
						) .. J.toJson(d, rec)
					)
				end
			end
		end,
	}
	return surfaces
end
