--!strict
-- THE 2026-11-22 SURFACES for moo (test/conformance/README.md;
-- docs/SPEC-CONFORMANCE.md §2): announce, read-into and json-write-into.
-- Every answer is what the unit's own runtime did; nothing here computes one.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

return function(ctx: any): { [string]: Surface }
	local function carriedMessage(_: any, key: string): boolean
		return ctx.module(key, "table_message") ~= nil
	end
	local function carriedLoad(_: any, key: string): boolean
		return ctx.module(key, "table_load") ~= nil and ctx.module(key, "json") ~= nil
	end

	-- the unit's descriptor registry with every <Base>Table module loaded
	local function registry(key: string): any
		ctx.tables(key)
		return ctx.module(key, "schema_tables")
	end

	-- the first <Base>Table module of a unit carrying `field`, or nil
	local function tableWith(key: string, field: string): any?
		for _, name in ctx.tableList(key) do
			local m: any = ctx.module(key, name)
			if m ~= nil and m[field] ~= nil then
				return m
			end
		end
		return nil
	end

	-- a read refused when the report names a refusal or damage (main.moo's
	-- ctx.read holds the same rule)
	local function refusedBy(report: any): boolean
		return report.damaged == true or report.reason ~= nil
	end

	-- announce <case> <unit> <root> <mode> <input>
	--
	-- `write`: THE MOO LEG HAS NO RUNTIME ANNOUNCEMENT WRITER (no
	-- AnnounceMeasure, no Announce): the emitter writes the unit's announcement
	-- as the table module's constant TableMessageAnnouncement, which is what
	-- this side sends. The answer is those bytes, or `refused` where the unit
	-- carries none. `strict` and `lenient`: NewVocabulary, its `strict` field
	-- set or left at its default, reads the file. `send`: MeasureMessages then
	-- SaveMessages of one value, which take no vocabulary in this leg (the
	-- batch is written under the unit's own).
	local function runAnnounce()
		for _, f in ctx.kind("announce") do
			local name, key, root, mode, input = f[2], f[3], f[4], f[5], f[6]
			local MS: any = ctx.module(key, "table_message")
			if MS == nil then
				ctx.say(name, nil)
				continue
			end
			local St = registry(key)
			if mode == "write" then
				local T: any = tableWith(key, "TableMessageAnnouncement")
				if T == nil then
					ctx.say(name, "refused\n")
				else
					local ann: buffer = T.TableMessageAnnouncement
					ctx.say(name, buffer.tostring(ann))
				end
			elseif mode == "strict" or mode == "lenient" then
				local voc = MS.NewVocabulary()
				if mode == "strict" then voc.strict = true end
				local ok, read = pcall(MS.AnnounceRead, voc, ctx.inputs(input), {})
				ctx.say(name, if not ok then "threw\n" elseif read then "read\n" else "refused\n")
			elseif mode == "send" then
				local d: any = St.lookup(root)
				if d == nil then error("driver: " .. name .. ": no root " .. root, 0) end
				local v: any
				if input == "-" then
					v = d.new()
				else
					v = ctx.read(key, root, ctx.inputs(input))
					if v == nil then error("driver: " .. name .. ": load refused " .. input, 0) end
				end
				local answer = "refused\n"
				local okm, msz = pcall(MS.MeasureMessages, root, { v }, {})
				if okm and msz >= 0 then
					local oks, out, sz = pcall(MS.SaveMessages, root, { v }, {})
					if oks and out ~= nil and sz == msz and buffer.len(out) == msz then
						answer = buffer.tostring(out)
					end
				end
				ctx.say(name, answer)
			else
				error("driver: " .. name .. ": " .. mode .. " is not an announce mode", 0)
			end
		end
	end

	-- read-into <case> <unit> <root> <form> <lawful> <damaged> <count> <announcement>
	--
	-- THE TEXT READ TAKES THE CALLER'S VALUE: json's fromJson(d, rec, text,
	-- report) reads into rec. The variable read (table_refusal's ReadStrict),
	-- the fixed form's load and LoadMessages answer fresh values beside the
	-- caller's report, so for those the driver holds its values, a read that
	-- succeeds replaces them with what it answered, and a refused read answers
	-- none, leaving them as they were.
	local function runReadInto()
		for _, f in ctx.kind("read-into") do
			local name, key, root, form, lawful, damaged, count, announcement =
				f[2], f[3], f[4], f[5], f[6], f[7], f[8], f[9]
			if not carriedLoad(nil, key) then
				ctx.say(name, nil)
				continue
			end
			local St = registry(key)
			local d: any = St.lookup(root)
			if d == nil then error("driver: " .. name .. ": no root " .. root, 0) end
			local J: any = ctx.module(key, "json")
			if form == "fixed-nocap" then
				-- FL-17: no capacity argument at all; the omitted capacity is the file's
				local F: any = ctx.module(key, "table_fixed")
				local T: any = ctx.fixedTable(key, root)
				if F == nil or T == nil then error("driver: " .. name .. ": " .. root .. " has no fixed form here", 0) end
				local r: any = {}
				local ok, out = pcall(F.load, d, T[root .. "Fixed"], ctx.inputs(lawful), r)
				ctx.say(name, if not ok then "threw\n" elseif out == nil then "refused\n" else "read\n" .. tostring(#out) .. "\n")
				continue
			end
			local n = tonumber(count)
			if n == nil or n < 1 then error("driver: " .. name .. ": " .. tostring(count) .. " is not a count", 0) end
			local values: { any } = {}
			for i = 1, n :: number do
				values[i] = d.new()
			end
			local report: any = {}
			local voc: any = nil
			local MS: any = nil
			if form == "message" then
				MS = ctx.module(key, "table_message")
				if MS == nil then error("driver: " .. name .. ": the unit emits no table_message", 0) end
				voc = MS.NewVocabulary()
				if not MS.AnnounceRead(voc, ctx.inputs(announcement), {}) then
					error("driver: " .. name .. ": the announcement was refused: " .. announcement, 0)
				end
			end
			local function readOnce(path: string): string
				local bytes = ctx.inputs(path)
				local ok: boolean, got: any
				if form == "variable" then
					local R: any = ctx.module(key, "table_refusal")
					ok, got = pcall(R.ReadStrict, nil, root, bytes, report)
				elseif form == "json" then
					ok, got = pcall(J.fromJson, d, values[1], bytes, report)
					if ok then got = if got then values[1] else nil end
				elseif form == "fixed" then
					local F: any = ctx.module(key, "table_fixed")
					local T: any = ctx.fixedTable(key, root)
					if F == nil or T == nil then error("driver: " .. name .. ": " .. root .. " has no fixed form here", 0) end
					ok, got = pcall(F.load, d, T[root .. "Fixed"], bytes, report, n)
				elseif form == "message" then
					local c: any, bodies: any
					ok, c, bodies = pcall(MS.LoadMessages, voc, root, bytes, n, report)
					got = if ok and c ~= nil and c >= 1 then bodies else nil
				else
					error("driver: " .. name .. ": " .. form .. " is not a read-into form", 0)
				end
				if not ok then return "threw\n" end
				if got == nil or refusedBy(report) then return "refused\n" end
				if form == "variable" or form == "json" then
					values[1] = got
				else
					values = got
				end
				return "read\n"
			end
			local parts = {}
			parts[#parts + 1] = readOnce(lawful)
			parts[#parts + 1] = readOnce(damaged)
			for i, value in values do
				local text, why = J.toJson(d, value)
				if text == nil then error("driver: " .. name .. ": toJson refused value " .. i .. " (" .. tostring(why) .. ")", 0) end
				parts[#parts + 1] = text
			end
			parts[#parts + 1] = readOnce(lawful)
			ctx.say(name, table.concat(parts))
		end
	end

	-- json-write-into <case> <unit> <root> <wire>
	--
	-- MOO HAS NO PROCESS LOCALE CALL (no os.setlocale, and no float of its
	-- own: a float is its bit pattern, written by json.moo's own formatter),
	-- so the harness's SCHEMA_COMMA_LOCALE cannot be made this process's own;
	-- the rows still run. THE CALLER BUFFER IS REAL: toJsonInto(d, rec, out)
	-- writes into a buffer 64 bytes longer than toJsonMeasure, filled with
	-- 0xA5; the answer is the text it says it wrote, or `tail-written` when a
	-- byte past that length is no longer 0xA5 (FL-15).
	local function runJsonWriteInto()
		for _, f in ctx.kind("json-write-into") do
			local name, key, root, wire = f[2], f[3], f[4], f[5]
			if not carriedLoad(nil, key) then
				ctx.say(name, nil)
				continue
			end
			local St = registry(key)
			local d: any = St.lookup(root)
			if d == nil then error("driver: " .. name .. ": no root " .. root, 0) end
			local v = ctx.read(key, root, ctx.inputs(wire))
			if v == nil then error("driver: " .. name .. ": load refused " .. wire, 0) end
			local J: any = ctx.module(key, "json")
			local size = J.toJsonMeasure(d, v)
			if size < 0 then
				ctx.say(name, "refused\n")
				continue
			end
			local out = buffer.create(size + 64)
			buffer.fill(out, 0, 0xA5)
			local wrote = J.toJsonInto(d, v, out)
			if wrote < 0 then
				ctx.say(name, "refused\n")
				continue
			end
			local tail = false
			for i = wrote, buffer.len(out) - 1 do
				if buffer.readu8(out, i) ~= 0xA5 then
					tail = true
					break
				end
			end
			ctx.say(name, if tail then "tail-written\n" else buffer.readstring(out, 0, wrote))
		end
	end

	return {
		["announce"] = { carried = carriedMessage, run = runAnnounce },
		["read-into"] = { carried = carriedLoad, run = runReadInto },
		["json-write-into"] = { carried = carriedLoad, run = runJsonWriteInto },
	}
end
