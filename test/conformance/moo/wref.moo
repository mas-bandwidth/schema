--!strict
-- THE WRITE CONTRACT (docs/SPEC.md §5.1): the four write-refusal surfaces for moo.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_load") ~= nil and ctx.module(key, "table_save") ~= nil
	end

	local function deepCopy(x: any): any
		if type(x) == "buffer" then
			local nb = buffer.create(buffer.len(x))
			buffer.copy(nb, 0, x, 0, buffer.len(x))
			return nb
		elseif type(x) ~= "table" then
			return x
		end
		local out = {}
		for k, e in x do
			out[k] = deepCopy(e)
		end
		return out
	end

	local function hexBytes(text: string): string
		if #text % 2 ~= 0 then
			error("driver: hex " .. text .. " is not whole bytes", 0)
		end
		local out = table.create(math.floor(#text / 2))
		for i = 1, #text, 2 do
			out[#out + 1] = string.char(tonumber(string.sub(text, i, i + 1), 16) :: number)
		end
		return table.concat(out)
	end

	local function wrefWalk(St: any, root: string, v: any, path: string): (any, any, number?)
		local segments = {}
		for seg in string.gmatch(path, "[^.]+") do
			segments[#segments + 1] = seg
		end
		local tname: string = root
		local owner: any = v
		for i, seg in segments do
			local name, idxStr = string.match(seg, "^([%a_][%w_]*)%[(%d+)%]$")
			if name == nil then
				name = string.match(seg, "^([%a_][%w_]*)$")
				if name == nil then
					error("driver: " .. path .. ": " .. seg .. " is not a path segment", 0)
				end
			end
			local td = St.lookup(tname)
			if td == nil then error("driver: no table " .. tname, 0) end
			local fd = nil
			for _, f in td.fields do
				if f.name == name then fd = f; break end
			end
			if fd == nil then error("driver: no field " .. (name :: string) .. " in " .. tname, 0) end
			local idx: number? = if idxStr ~= nil then (tonumber(idxStr) :: number) + 1 else nil
			if i == #segments then
				return owner, fd, idx
			end
			local child = owner[fd.slot]
			-- a union's arm, held by the field itself or by one element of an
			-- array of unions (bounded_steps[3].label): the element IS the union
			-- value, never a table to look up by the union's name
			local unionType: string? = nil
			if fd.s == "union" and idx == nil then unionType = fd.t end
			if fd.s == "arr" and idx ~= nil and fd.e ~= nil and fd.e.s == "union" then unionType = fd.e.t end
			if unionType ~= nil and i == #segments - 1 then
				if idx ~= nil then child = child[idx] end
				if child == nil then error("driver: " .. path .. ": " .. seg .. " holds no union", 0) end
				local ud = St.lookup(unionType)
				local armName = segments[#segments]
				for _, arm in ud.arms do
					if arm.name == armName then
						return child, arm, nil
					end
				end
				error("driver: no arm " .. armName .. " in union " .. unionType, 0)
			end
			if idx ~= nil then
				child = child[idx]
			end
			local nextType = if idx ~= nil then (if fd.s == "map" then fd.t else fd.e.t) else fd.t
			owner, tname = child, nextType
		end
		error("driver: empty path", 0)
	end

		-- THE ONE CHECKED CONVERSION for an ordinal, a tag, a count and a length:
		-- every number here is a double, which holds an integer exactly only to
		-- 2^53. tonumber rounds a token past that (18446744073709551616 to 2^64),
		-- and the writer then narrowed the rounded tag into a lawful one and the
		-- row answered `wrote`. A token the double does not hold exactly is
		-- unrepresentable: wrefSet answers true and sets nothing.
		local function wrefWhole(text: string): number?
			local n = tonumber(text)
			if n == nil or n ~= math.floor(n) or math.abs(n) > 2 ^ 53 then return nil end
			return n
		end

	local function wrefSet(unitKey: string, St: any, root: string, v: any, path: string, what: string, text: string)
		local owner, fd, idx = wrefWalk(St, root, v, path)
		local holder: any = owner
		local key: any = fd.slot
		if idx ~= nil then
			holder = owner[fd.slot]
			key = idx
		end
		local d: any = if idx ~= nil and fd.s == "arr" then fd.e else fd
		if what == "ordinal" then
			local n = wrefWhole(text)
			if n == nil then return true end
			holder[key] = n
		elseif what == "int" then
			local num = tonumber(text) :: number
			holder[key] = if d.cls == "fx" then num / 65536 else num
		elseif what == "float" then
			if d.f32 then
				if text == "nan" then holder[key] = 0x7fc00000
				elseif text == "inf" then holder[key] = 0x7f800000
				elseif text == "-inf" then holder[key] = 0xff800000
				else
					local n = tonumber(text) :: number
					local b = buffer.create(4)
					buffer.writef32(b, 0, n)
					holder[key] = buffer.readu32(b, 0)
				end
			else
				local ser: any = ctx.module(unitKey, "serialize")
				if text == "nan" then holder[key] = ser.Uint64.new(0, 0x7ff80000)
				elseif text == "inf" then holder[key] = ser.Uint64.new(0, 0x7ff00000)
				elseif text == "-inf" then holder[key] = ser.Uint64.new(0, 0xfff00000)
				else
					local n = tonumber(text) :: number
					local b = buffer.create(8)
					buffer.writef64(b, 0, n)
					holder[key] = ser.Uint64.new(buffer.readu32(b, 0), buffer.readu32(b, 4))
				end
			end
		elseif what == "count" then
			local n = wrefWhole(text)
			if n == nil then return true end
			local arr = holder[key]
			if owner[fd.slot .. "Count"] ~= nil then
				owner[fd.slot .. "Count"] = n
				for i = #arr + 1, n do
					arr[i] = deepCopy(arr[1])
				end
			else
				for i = #arr + 1, n do
					arr[i] = deepCopy(arr[1])
				end
				for i = #arr, n + 1, -1 do
					arr[i] = nil
				end
			end
		elseif what == "length" or what == "bytes" then
			local len = if what == "length" then wrefWhole(text) else 0
			if len == nil then return true end
			local raw = if what == "length" then string.rep("a", len) else hexBytes(text)
			if idx == nil and owner[fd.slot .. "Length"] ~= nil then
				owner[fd.slot] = buffer.fromstring(raw)
				owner[fd.slot .. "Length"] = #raw
			elseif type(holder[key]) == "buffer" then
				holder[key] = buffer.fromstring(raw)
			else
				holder[key] = raw
			end
		elseif what == "tag" then
			local n = wrefWhole(text)
			if n == nil then return true end
			local ud = St.lookup(d.t)
			local x = ud.new()
			x.Type = n
			holder[key] = x
		else
			error("driver: " .. path .. ": " .. what .. " is not a write-refusal kind", 0)
		end
	end

	local function runSurface(kindName: string)
		local connections: { [string]: { string } } = {}
		for _, c in ctx.kind("connection") do
			connections[c[2]] = c
		end
		for _, f in ctx.kind(kindName) do
			local name, key, root, wire, path, what, text = f[2], f[3], f[4], f[5], f[6], f[7], f[8]
			local conn = nil
			if kindName == "write-refusal-message" then
				conn = connections[key]
				if conn == nil then error("driver: " .. name .. ": no connection " .. key, 0) end
				key = conn[3]
			end
			if not carried(nil, key) then
				ctx.say(name, nil)
				continue
			end
			ctx.tables(key)
			local St: any = ctx.module(key, "schema_tables")
			local d: any = St.lookup(root)
			if d == nil then
				ctx.say(name, nil)
				continue
			end
			local v = ctx.read(key, root, ctx.inputs(wire))
			if v == nil then
				error("driver: " .. name .. ": load refused " .. wire, 0)
			end
			if wrefSet(key, St, root, v, path, what, text) then
				ctx.say(name, "unrepresentable\n")
				continue
			end
			local answer = "refused\n"
			if kindName == "write-refusal" or kindName == "write-refusal-fixed" then
				if kindName == "write-refusal-fixed" and not d.fixed then
					error("driver: " .. name .. ": " .. root .. " has no fixed form", 0)
				end
				local ok, out = pcall(ctx.write, key, root, v)
				if ok and out ~= nil then
					answer = "wrote\n"
				end
			elseif kindName == "write-refusal-message" then
				local MS: any = ctx.module(key, "table_message")
				if MS == nil then
					ctx.say(name, nil)
					continue
				end
				local vr: any = {}
				local voc = MS.NewVocabulary()
				if not MS.AnnounceRead(voc, ctx.inputs((conn :: { string })[5]), vr) then
					error("driver: " .. name .. ": announcement refused", 0)
				end
				local mr: any = {}
				local okm, msz = pcall(MS.MeasureMessages, root, { v }, mr)
				if okm and msz >= 0 then
					local mr2: any = {}
					local oks, out, sz = pcall(MS.SaveMessages, root, { v }, mr2)
					if oks and out ~= nil and sz == msz and buffer.len(out) == msz then
						answer = "wrote\n"
					end
				end
			else
				local J: any = ctx.module(key, "json")
				if J == nil then
					ctx.say(name, nil)
					continue
				end
				local okm, msz = pcall(J.toJsonMeasure, d, v)
				if okm and msz >= 0 then
					local oks, out = pcall(J.toJson, d, v)
					if oks and out ~= nil then
						answer = "wrote\n"
						if what == "bytes" and string.find(hexBytes(text), "\0", 1, true) ~= nil and string.find(out, "\\u0000", 1, true) == nil then
							answer = "wrote without \\u0000\n"
						end
					end
				end
			end
			ctx.say(name, answer)
		end
	end

	local surfaces: { [string]: Surface } = {}
	for _, s in { "write-refusal", "write-refusal-fixed", "write-refusal-message", "write-refusal-json" } do
		surfaces[s] = {
			carried = carried,
			run = function()
				runSurface(s)
			end,
		}
	end
	return surfaces
end
