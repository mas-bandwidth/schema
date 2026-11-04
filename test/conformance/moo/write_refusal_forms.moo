--!strict
-- THE MOO LEG'S WRITE CONTRACT OVER THE OTHER WRITERS
-- (testdata/conformance/tables/FORMAT.md, `write-refusal`; docs/SPEC.md §5.1).
-- main.moo hands this module its context and takes back the surfaces it
-- carries. Each loads and sets the row's one field as write_refusal.moo does
-- (ctx.writeRefusalValue), then writes it through one writer:
--
--   write-refusal-fixed    the fixed form's save (table_fixed.moo) as a batch
--                          of one: `refused\n` (nil and its named refusal) or
--                          `wrote\n`
--   write-refusal-message  SaveMessages as a batch of one, after the row's
--                          connection (its <unit> column) is announced into a
--                          vocabulary: `refused\n` (nil and the report's
--                          reason) or `wrote\n`
--   write-refusal-json     ToJson: `wrote\n` when the text spells the value's
--                          zero as \u0000, `refused\n` when the writer refused
--
-- A refusal that names no reason, and text that does not spell the zero,
-- fail the driver.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

local function fail(message: string): never
	error("write-refusal: " .. message, 0)
end

return function(ctx: any): { [string]: Surface }
	local function loads(key: string): boolean
		return ctx.module(key, "table_load") ~= nil and ctx.module(key, "schema_tables") ~= nil
	end

	local function answer(case: string, out: any, why: any)
		if out ~= nil then
			ctx.say(case, "wrote\n")
		elseif why ~= nil and type(why) == "string" or (type(why) == "table" and type(why.name) == "string") then
			ctx.say(case, "refused\n")
		else
			fail(case .. ": the writer refused and named no reason")
		end
	end

	local surfaces: { [string]: Surface } = {}

	local function fixedCarried(_: any, key: string): boolean
		return loads(key) and ctx.module(key, "table_fixed") ~= nil
	end
	surfaces["write-refusal-fixed"] = {
		carried = fixedCarried,
		run = function()
			for _, f in ctx.kind("write-refusal-fixed") do
				local case, key, root = f[2], f[3], f[4]
				if not fixedCarried(nil, key) then
					ctx.say(case, nil)
					continue
				end
				local v = ctx.writeRefusalValue(f, key)
				local spec: any = nil
				for _, m in ctx.tables(key) do
					if type(m) == "table" and m[root .. "Fixed"] ~= nil then
						spec = m[root .. "Fixed"]
					end
				end
				if spec == nil then
					ctx.say(case, nil)
					continue
				end
				local F: any = ctx.module(key, "table_fixed")
				local St: any = ctx.module(key, "schema_tables")
				local out, why = F.save(St.lookup(root), spec, { v })
				answer(case, out, why)
			end
		end,
	}

	local function messageCarried(_: any, key: string): boolean
		return loads(key) and ctx.module(key, "table_message") ~= nil
	end
	surfaces["write-refusal-message"] = {
		carried = messageCarried,
		run = function()
			local connections: { [string]: { string } } = {}
			for _, c in ctx.kind("connection") do
				connections[c[2]] = c
			end
			for _, f in ctx.kind("write-refusal-message") do
				local case, root = f[2], f[4]
				local conn = connections[f[3]] or fail(case .. " names no connection " .. f[3])
				local key = conn[3]
				if not messageCarried(nil, key) then
					ctx.say(case, nil)
					continue
				end
				local v = ctx.writeRefusalValue(f, key)
				local MS: any = ctx.module(key, "table_message")
				local report: any = {}
				local vocabulary = MS.NewVocabulary()
				if not MS.AnnounceRead(vocabulary, ctx.inputs(conn[5]), report) then
					fail(case .. ": the announcement was refused: " .. tostring(report.reason))
				end
				local saved: any = {}
				local out = MS.SaveMessages(root, { v }, saved)
				answer(case, out, if out == nil then saved.reason else nil)
			end
		end,
	}

	local function jsonCarried(_: any, key: string): boolean
		return loads(key) and ctx.module(key, "json") ~= nil
	end
	surfaces["write-refusal-json"] = {
		carried = jsonCarried,
		run = function()
			for _, f in ctx.kind("write-refusal-json") do
				local case, key, root = f[2], f[3], f[4]
				if not jsonCarried(nil, key) then
					ctx.say(case, nil)
					continue
				end
				local v = ctx.writeRefusalValue(f, key)
				local J: any = ctx.module(key, "json")
				local St: any = ctx.module(key, "schema_tables")
				local text, why = J.toJson(St.lookup(root), v)
				if text ~= nil and string.find(text, "\\u0000", 1, true) == nil then
					fail(case .. ": the text writer wrote the zero without \\u0000")
				end
				answer(case, text, why)
			end
		end,
	}
	return surfaces
end
