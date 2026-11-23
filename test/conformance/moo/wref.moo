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
			if fd.s == "union" and idx == nil and i == #segments - 1 then
				local ud = St.lookup(fd.t)
				local armName = segments[#segments]
				for _, arm in ud.arms do
					if arm.name == armName then
						return child, arm, nil
					end
				end
				error("driver: no arm " .. armName .. " in union " .. fd.t, 0)
			end
			if idx ~= nil then
				child = child[idx]
			end
			local nextType = if idx ~= nil then (if fd.s == "map" then fd.t else fd.e.t) else fd.t
			owner, tname = child, nextType
		end
		error("driver: empty path", 0)
	end

	-- A ROW'S REAL IS SPELLED AT FULL PRECISION: moo has no float, and a
	-- number from tonumber holds a decimal fraction only to 1/65536, so
	-- tonumber(text) through buffer.writef32 turned 0.1 into 0x3dccd000 and
	-- 2.5000002384185791 into 2.5, and the rows that pin a compressed float's
	-- float32 ends (docs/SPEC-TABLES.md §3.3) answered the wrong verdict.
	-- The unit's own JSON parser rounds the token once, to the nearest
	-- binary32 or binary64 pattern, exactly as the Go reader does; a unit
	-- that carries no json.moo has no text form for a real to come from.
	local function floatParser(unitKey: string): any
		local J: any = ctx.module(unitKey, "json")
		if J == nil then
			error("driver: unit " .. unitKey .. " carries no json.moo to spell a real exactly", 0)
		end
		return J
	end

	local function floatBits32(unitKey: string, text: string): number
		return floatParser(unitKey).parseFloat32(text)
	end

	local function floatBits64(unitKey: string, text: string): (number, number)
		local hi, lo = floatParser(unitKey).parseFloat64(text)
		return lo, hi
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
		if what == "int" or what == "ordinal" then
			local num = tonumber(text) :: number
			holder[key] = if d.cls == "fx" then num / 65536 else num
		elseif what == "float" then
			if d.f32 then
				if text == "nan" then holder[key] = 0x7fc00000
				elseif text == "inf" then holder[key] = 0x7f800000
				elseif text == "-inf" then holder[key] = 0xff800000
				else
					holder[key] = floatBits32(unitKey, text)
				end
			else
				local ser: any = ctx.module(unitKey, "serialize")
				if text == "nan" then holder[key] = ser.Uint64.new(0, 0x7ff80000)
				elseif text == "inf" then holder[key] = ser.Uint64.new(0, 0x7ff00000)
				elseif text == "-inf" then holder[key] = ser.Uint64.new(0, 0xfff00000)
				else
					local lo, hi = floatBits64(unitKey, text)
					holder[key] = ser.Uint64.new(lo, hi)
				end
			end
		elseif (what == "count" or what == "elems") and (tonumber(text) :: number) < 0 then
			-- A NEGATIVE COUNT (SF-28): a counted array's count slot holds it;
			-- an array with no count slot is a plain list, and the number
			-- itself, a value that is no list, is handed to the writer
			if owner[fd.slot .. "Count"] ~= nil then
				owner[fd.slot .. "Count"] = tonumber(text) :: number
			else
				holder[key] = tonumber(text) :: number
			end
		elseif what == "elems" then
			-- a fixed [N]T or keyed [E]T is a plain list: more elements are
			-- copies of element 1, fewer drop the tail
			local n = tonumber(text) :: number
			local arr = holder[key]
			if type(arr) ~= "table" then error("driver: " .. path .. ": elems on a field that is no list", 0) end
			for i = #arr + 1, n do
				arr[i] = deepCopy(arr[1])
			end
			for i = #arr, n + 1, -1 do
				arr[i] = nil
			end
		elseif what == "mask" then
			-- a flags value is its raw bits, in the field's own storage class
			local n = tonumber(text) :: number
			if d.cls == "U64" then
				local ser: any = ctx.module(unitKey, "serialize")
				holder[key] = ser.Uint64.fromNumber(n)
			else
				holder[key] = n
			end
		elseif what == "dupkey" then
			-- A MOO MAP IS A LIST OF ENTRIES, so one key held twice is a copy
			-- of the first entry appended; `dupkey 0` leaves the map as loaded
			if fd.s ~= "map" then error("driver: " .. path .. ": dupkey on a field that is no map", 0) end
			if text == "1" then
				local entries = holder[key]
				entries[#entries + 1] = deepCopy(entries[1])
			elseif text ~= "0" then
				error("driver: " .. path .. ": dupkey takes 0 or 1, not " .. text, 0)
			end
		elseif what == "point" then
			-- the pointer at `path` is set to the node the pointer at path
			-- `text` points at: the same table, so a node enclosing `path` is
			-- a cycle
			local towner, tfd, tidx = wrefWalk(St, root, v, text)
			local target = towner[tfd.slot]
			if tidx ~= nil then target = target[tidx] end
			if type(target) ~= "table" then error("driver: " .. path .. ": the pointer at " .. text .. " points at no node", 0) end
			holder[key] = target
		elseif what == "count" then
			local n = tonumber(text) :: number
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
			local raw = if what == "length" then string.rep("a", tonumber(text) :: number) else hexBytes(text)
			if idx == nil and owner[fd.slot .. "Length"] ~= nil then
				owner[fd.slot] = buffer.fromstring(raw)
				owner[fd.slot .. "Length"] = #raw
			elseif type(holder[key]) == "buffer" then
				holder[key] = buffer.fromstring(raw)
			else
				holder[key] = raw
			end
		elseif what == "tag" then
			local n = tonumber(text) :: number
			local ud = St.lookup(d.t)
			local x = ud.new()
			x.Type = n
			holder[key] = x
		else
			error("driver: " .. path .. ": " .. what .. " is not a write-refusal kind", 0)
		end
	end

	-- a table value shaped as the block reader answers it: each field of the
	-- block info by its schema name from the value's slot, a nested record and
	-- every row of a record list likewise; a field with no descriptor mate is
	-- the driver's failure, never the writer's refusal
	local blockShape: any
	blockShape = function(St: any, d: any, info: any, value: any): any
		local out: any = {}
		for _, bf in info.fields do
			local fd: any = nil
			for _, f in d.fields do
				if f.name == bf.name then fd = f; break end
			end
			if fd == nil then error("driver: block field " .. bf.name .. " has no field in " .. d.name, 0) end
			local val = value[fd.slot]
			if bf.element ~= nil and val ~= nil then
				local ed: any = St.lookup(bf.element.name)
				if ed == nil then error("driver: no table " .. bf.element.name, 0) end
				if bf.isArray or bf.outOfLine then
					local rows = {}
					for i, row in val do
						rows[i] = blockShape(St, ed, bf.element, row)
					end
					out[bf.name] = rows
				else
					out[bf.name] = blockShape(St, ed, bf.element, val)
				end
			else
				out[bf.name] = val
			end
		end
		return out
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
			local v: any
			if wire == "-" then
				-- THE ROOT'S FRESH VALUE (SF-05, SF-06): a root with no file form
				v = d.new()
			else
				v = ctx.read(key, root, ctx.inputs(wire))
				if v == nil then
					error("driver: " .. name .. ": load refused " .. wire, 0)
				end
			end
			wrefSet(key, St, root, v, path, what, text)
			local answer = "refused\n"
			if kindName == "write-refusal-cook" then
				-- THE COOK WRITER (SPEC-TABLES §7.6): measure, then cook in
				-- little-endian order into exactly the measure
				local W: any = ctx.module(key, "table_cookwrite")
				if W == nil then
					ctx.say(name, nil)
					continue
				end
				local okm, size = pcall(W.measure, root, v)
				if okm and size ~= nil and size >= 1 then
					local okc, out = pcall(W.cook, root, v, false)
					if okc and out ~= nil then
						answer = if buffer.len(out) == size then "wrote\n" else "wrote " .. buffer.len(out) .. " bytes against a measure of " .. size .. "\n"
					end
				end
			elseif kindName == "write-refusal-block" then
				-- THE BLOCK BUILDER (SPEC-TABLES §19.1): table_block's Begin with
				-- the value's count of every out-of-line array, then Write of the
				-- value shaped as the block reader answers it (each field by its
				-- schema name, a record's fields likewise, a list one row each)
				local B: any = ctx.module(key, "table_block")
				local info: any = nil
				for _, mod in ctx.tableList(key) do
					local m: any = ctx.module(key, mod)
					if m ~= nil and m[root .. "BlockInfo"] ~= nil then
						info = m[root .. "BlockInfo"]
					end
				end
				if B == nil or info == nil then
					ctx.say(name, nil)
					continue
				end
				local shaped = blockShape(St, d, info, v)
				local counts: { [string]: number } = {}
				for _, bf in info.fields do
					if bf.outOfLine then
						local rows = shaped[bf.name]
						counts[bf.name] = if type(rows) == "table" then #rows else rows
					end
				end
				local okb, block = pcall(B.Begin, info, counts, {})
				if okb and block ~= nil then
					local okw, why = pcall(B.Write, block, shaped)
					if okw and why == nil then
						answer = "wrote\n"
					end
				end
			elseif kindName == "write-refusal" or kindName == "write-refusal-fixed" then
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
	for _, s in { "write-refusal", "write-refusal-fixed", "write-refusal-message", "write-refusal-json", "write-refusal-cook", "write-refusal-block" } do
		surfaces[s] = {
			carried = carried,
			run = function()
				runSurface(s)
			end,
		}
	end
	return surfaces
end
