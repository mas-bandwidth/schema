--!strict
-- THE MOO LEG'S WRITE CONTRACT (testdata/conformance/tables/FORMAT.md,
-- `write-refusal`; docs/SPEC.md §5.1). main.moo hands this module its context
-- and takes back the one surface it carries:
--
--   write-refusal  Load the wire as the wire surface does, set the ONE field
--                  at <path> by <kind>, Save through the same table_save.moo,
--                  and answer `refused\n` (the save's nil and its named
--                  refusal) or `wrote\n`
--
-- A derived line is `write-refusal <case> <unit> <root> <wire> <path> <kind>
-- <value>`. A refusal that names no reason fails the driver. The load and the
-- one set are ctx.writeRefusalValue, which write_refusal_forms.moo uses for
-- the same rows through the fixed, message and text writers.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

local function fail(message: string): never
	error("write-refusal: " .. message, 0)
end

-- the bits of the one float32 each word spells (a binary32 is stored as its
-- bits, docs/PORT-MOO.md, THE VALUE MAPPING)
local F32 = { nan = 0x7FC00000, inf = 0x7F800000, ["-inf"] = 0xFF800000 }

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_load") ~= nil and ctx.module(key, "table_save") ~= nil
	end

	-- the path's segments: { field name, element index (1-based) or nil }
	local function segments(path: string): { { any } }
		local out = {}
		for part in string.gmatch(path, "[^%.]+") do
			local name, index = string.match(part, "^([%w_]+)%[(%d+)%]$")
			if name ~= nil then
				out[#out + 1] = { name, (tonumber(index) :: number) + 1 }
			else
				out[#out + 1] = { part, nil }
			end
		end
		return out
	end

	local function fieldNamed(d: any, name: string): any
		for _, fd in d.fields do
			if fd.name == name then
				return fd
			end
		end
		return fail(d.name .. " has no field " .. name)
	end

	-- walk to the last segment: the instance holding it, its field, its index
	local function resolve(St: any, root: string, v: any, path: string): (any, any, number?)
		local segs = segments(path)
		local d = St.lookup(root)
		local inst = v
		for i = 1, #segs - 1 do
			local fd = fieldNamed(d, segs[i][1])
			local val = inst[fd.slot]
			if segs[i][2] ~= nil then
				val = val[segs[i][2]]
				d = St.lookup(fd.e.t)
			elseif fd.s == "tab" then
				d = St.lookup(fd.t)
			else
				fail(path .. " steps through " .. fd.name .. ", which holds no table")
			end
			inst = val
		end
		local last = segs[#segs]
		return inst, fieldNamed(d, last[1]), last[2]
	end

	-- the bytes a hex word spells
	local function hexBytes(hex: string): string
		return (string.gsub(hex, "%x%x", function(h: string): string
			return string.char(tonumber(h, 16) :: number)
		end))
	end

	-- load the row's wire as the wire surface does and set its ONE field:
	-- the instance, to be written by the caller's writer
	local function setRow(f: { string }, key: string): any
		local case, root, wire, path, how, value = f[2], f[4], f[5], f[6], f[7], f[8]
		ctx.tables(key)
		local load: any = ctx.module(key, "table_load")
		local St: any = ctx.module(key, "schema_tables")
		local function fresh(): any
			local report = load.newReport()
			local v = load.loadTolerant(root, ctx.inputs(wire), report)
			if v == nil or report.refused > 0 then
				fail(case .. ": the load refused " .. wire)
			end
			return v
		end
		local v = fresh()
		local inst, fd, index = resolve(St, root, v, path)
		local function set(x: any)
			if index ~= nil then
				inst[fd.slot][index] = x
			else
				inst[fd.slot] = x
			end
		end
		if how == "int" then
			local e = if index ~= nil then fd.e else fd
			if e.cls ~= "n" then
				fail(case .. ": " .. path .. " is stored as " .. tostring(e.cls) .. ", which this surface does not set")
			end
			set(tonumber(value))
		elseif how == "float" then
			local e = if index ~= nil then fd.e else fd
			if not e.f32 then
				fail(case .. ": " .. path .. " is a float64, which this surface does not set")
			end
			set(F32[value] or fail(case .. ": no float " .. value))
		elseif how == "count" then
			-- the count beside the array, even past its bound; a new
			-- element is element 0 of a fresh load, so no two slots
			-- share one instance
			local n = tonumber(value) :: number
			local arr = inst[fd.slot]
			local have = #arr
			for i = have + 1, math.min(n, fd.bound) do
				local again, againFd = resolve(St, root, fresh(), path)
				arr[i] = again[againFd.slot][1]
			end
			inst[fd.slot .. "Count"] = n
		elseif how == "length" then
			local n = tonumber(value) :: number
			if fd.s == "str" and not fd.wide then
				set(string.rep("a", n))
			elseif fd.s == "bytes" or (fd.s == "str" and fd.wide) then
				local unit = if fd.wide then 2 else 1
				local b = buffer.create(math.max(n, 0) * unit)
				for i = 0, math.max(n, 0) - 1 do
					if fd.wide then
						buffer.writeu16(b, i * 2, 97)
					else
						buffer.writeu8(b, i, 97)
					end
				end
				inst[fd.slot] = b
				inst[fd.slot .. "Length"] = n
			else
				fail(case .. ": " .. path .. " is no text")
			end
		elseif how == "ordinal" then
			-- an enum is stored as its ordinal, 0 for None
			set(tonumber(value))
		elseif how == "tag" then
			local t = tonumber(value) :: number
			local u = inst[fd.slot]
			if index ~= nil then
				u = u[index]
			end
			u.Type = t
			local arm = St.lookup(fd.t).arms[t]
			if arm ~= nil and arm.s == "tab" then
				u[arm.slot] = St.lookup(arm.t).new()
			elseif arm ~= nil and arm.slot ~= nil then
				u[arm.slot] = arm.def
			end
		elseif how == "bytes" then
			-- a narrow text's exact bytes: a string(N) is a moo string,
			-- a bytes(N) its buffer and the Length beside it
			local text = hexBytes(value)
			if fd.s == "str" and not fd.wide then
				set(text)
			elseif fd.s == "bytes" then
				inst[fd.slot] = buffer.fromstring(text)
				inst[fd.slot .. "Length"] = #text
			else
				fail(case .. ": " .. path .. " is no narrow text")
			end
		elseif how == "units" then
			-- a wide text's exact UTF-16 units, the Length in units
			if not (fd.s == "str" and fd.wide) then
				fail(case .. ": " .. path .. " is no wide text")
			end
			local n = #value // 4
			local b = buffer.create(n * 2)
			for i = 0, n - 1 do
				buffer.writeu16(b, i * 2, tonumber(string.sub(value, i * 4 + 1, i * 4 + 4), 16) :: number)
			end
			inst[fd.slot] = b
			inst[fd.slot .. "Length"] = n
		else
			fail(case .. ": no write-refusal kind " .. how)
		end
		return v
	end
	ctx.writeRefusalValue = setRow

	local surfaces: { [string]: Surface } = {}
	surfaces["write-refusal"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("write-refusal") do
				local case, key, root = f[2], f[3], f[4]
				if not carried(nil, key) then
					ctx.say(case, nil)
					continue
				end
				local v = setRow(f, key)
				local save: any = ctx.module(key, "table_save")
				local St: any = ctx.module(key, "schema_tables")
				local out, why = save.save(St.lookup(root), v)
				if out ~= nil then
					ctx.say(case, "wrote\n")
				elseif why ~= nil and type(why.name) == "string" then
					ctx.say(case, "refused\n")
				else
					fail(case .. ": the save refused and named no reason")
				end
			end
		end,
	}
	return surfaces
end
