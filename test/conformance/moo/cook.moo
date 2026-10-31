--!strict
-- THE MOO LEG'S COOK SURFACES (test/conformance/README.md, the surfaces;
-- docs/SPEC-TABLES.md §7). main.moo hands this module its context and takes
-- back the surfaces it carries:
--
--   cook           open each cooked file, the canonical node dump (§7.5)
--   cook-write     Load the instance's wire, Cook it little and big, the bytes,
--                  as `<instance>` and `<instance>-be`
--   cook-foreign   byte-swap the file's magic word, open, `open\n` or `refuse\n`
--   cook-forgery   open the forged file at the claimed extent and base
--   cook-reason    the same, the refusal's name or `ok`
--
-- through the emitted table_cookread.moo (open, layout) and table_cookwrite.moo
-- (cook) and the descriptors the unit's <Base>Table modules register. The dump
-- is the walk this leg makes through its OWN derefs: each record's cooked
-- layout beside its descriptor, a pointer slot an int64 self-relative delta (a
-- delta of zero is null, §6.3), a node visited once, depth first, emitting as
-- it goes, exactly as test/conformance/go/cooks.go spells it.
--
-- The forged placement is block.moo's: a buffer exactly lead + extent long, the
-- base `lead` bytes into it (or no buffer at all), what fits copied, the rest
-- zero. ctx is the one block.moo and wire.moo document.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

local TWO32 = 4294967296

local function fail(message: string): never
	error("cook: " .. message, 0)
end

-- one 64-bit word as decimal, signed by its declared kind
local function dec64(lo: number, hi: number, signed: boolean): string
	local sign = ""
	if signed and hi >= 0x80000000 then
		sign = "-"
		lo = bit32.band(bit32.bnot(lo) + 1, 0xffffffff)
		hi = bit32.band(bit32.bnot(hi) + (if lo == 0 then 1 else 0), 0xffffffff)
	end
	local limbs = { hi // 65536, hi % 65536, lo // 65536, lo % 65536 }
	local digits: { string } = {}
	while limbs[1] ~= 0 or limbs[2] ~= 0 or limbs[3] ~= 0 or limbs[4] ~= 0 do
		local rem = 0
		for i = 1, 4 do
			local cur = rem * 65536 + limbs[i]
			limbs[i] = cur // 10
			rem = cur % 10
		end
		table.insert(digits, 1, tostring(rem))
	end
	if #digits == 0 then
		return "0"
	end
	return sign .. table.concat(digits)
end

-- a string's or a `bytes`' USED bytes, every byte outside printable ASCII
-- (and `"` and `\`) written \xNN
local function cookText(b: buffer, at: number, used: number): string
	local parts = { "\"" }
	for i = 0, used - 1 do
		local c = buffer.readu8(b, at + i)
		if c >= 0x20 and c < 0x7f and c ~= 34 and c ~= 92 then
			parts[#parts + 1] = string.char(c)
		else
			parts[#parts + 1] = string.format("\\x%02x", c)
		end
	end
	parts[#parts + 1] = "\" len=" .. used
	return table.concat(parts)
end

local function scalar(b: buffer, at: number, fd: any, w: number, owner: string): string
	local s = fd.s
	if s == "bool" then
		return if buffer.readu8(b, at) ~= 0 then "true" else "false"
	elseif s ~= "int" and s ~= "enum" then
		fail(owner .. "." .. tostring(fd.name) .. ": the dump meets a " .. tostring(s) .. ", which it does not spell")
	end
	local signed = s == "int" and fd.signed == true
	if w == 1 then
		return tostring(if signed then buffer.readi8(b, at) else buffer.readu8(b, at))
	elseif w == 2 then
		return tostring(if signed then buffer.readi16(b, at) else buffer.readu16(b, at))
	elseif w == 4 then
		return tostring(if signed then buffer.readi32(b, at) else buffer.readu32(b, at))
	end
	return dec64(buffer.readu32(b, at), buffer.readu32(b, at + 4), signed)
end

local function dumpCook(CR: any, S: any, root: string, data: buffer): string
	local handle, why = CR.open(root, data, 0, buffer.len(data))
	if handle == nil then
		fail(root .. ": the cook does not open (" .. tostring(why) .. ")")
	end
	local b: buffer = handle.buffer
	local region: number = handle.region
	local length: number = handle.regionLength
	local out: { string } = {}
	local seen: { [number]: string } = {}
	local count = 0

	local node: (number, string, number) -> ()

	local function storage(base: number, rname: string, path: string, depth: number)
		local layout = CR.layout(rname)
		local d = S.lookup(rname)
		for i, fd in d.fields do
			local l = layout.fields[i]
			local name = if path == "" then fd.name else path .. "." .. fd.name
			local s = fd.s
			-- every COUNT COMPANION, against its declared bound (§7.4's pass two)
			local used = -1
			if s == "str" or s == "bytes" or (s == "arr" and fd.array == "counted") then
				if s ~= "arr" or fd.array == "counted" then
					used = buffer.readi32(b, base + l.p[2])
					if used < 0 or used > fd.bound then
						fail(rname .. "." .. fd.name .. " carries a count companion of " .. used .. ", outside [ 0, " .. fd.bound .. " ]")
					end
				end
			end
			if s == "ptr" then
				local slot = base + l.p[1]
				local delta = buffer.readi32(b, slot + 4) * TWO32 + buffer.readu32(b, slot)
				if delta == 0 then
					out[#out + 1] = "  " .. name .. " = null"
				else
					local at = slot + delta - region
					if at < 0 or at >= length then
						fail(rname .. "." .. fd.name .. " resolves outside the region")
					end
					out[#out + 1] = "  " .. name .. " = -> @" .. at
					node(at, fd.t, depth + 1)
				end
			elseif s == "str" or s == "bytes" then
				out[#out + 1] = "  " .. name .. " = " .. cookText(b, base + l.p[1], used)
			elseif s == "tab" then
				storage(base + l.p[1], fd.t, name, depth)
			elseif s == "arr" then
				if fd.array == "list" or fd.e == nil then
					fail(rname .. "." .. fd.name .. ": the dump does not walk a list")
				end
				for k = 0, fd.bound - 1 do
					local at = base + l.p[1] + k * l.elem
					local element = name .. "[" .. k .. "]"
					if fd.e.s == "tab" then
						storage(at, fd.e.t, element, depth)
					else
						out[#out + 1] = "  " .. element .. " = " .. scalar(b, at, fd.e, l.elem, rname)
					end
				end
			else
				out[#out + 1] = "  " .. name .. " = " .. scalar(b, base + l.p[1], fd, l.w, rname)
			end
			if s == "arr" and fd.array == "counted" then
				out[#out + 1] = "  " .. name .. "#count = " .. used
			end
			if fd.present ~= nil then
				out[#out + 1] = "  " .. name .. "#present = " .. (if buffer.readu8(b, base + l.p[#l.p]) ~= 0 then "true" else "false")
			end
		end
	end

	node = function(offset: number, rname: string, depth: number)
		if depth > 4096 then
			fail("the walk nested past any depth a region can hold")
		end
		local held = seen[offset]
		if held ~= nil then
			if held ~= rname then
				fail("two references name the node at offset " .. offset .. " as " .. held .. " and " .. rname)
			end
			return
		end
		local size = CR.layout(rname).size
		if offset > length or size > length - offset then
			fail("the node at offset " .. offset .. " (" .. rname .. ") does not fit the region")
		end
		seen[offset] = rname
		out[#out + 1] = "node " .. count .. " " .. rname .. " @" .. offset
		count += 1
		storage(region + offset, rname, "", depth)
	end

	node(0, root, 0)
	return table.concat(out, "\n") .. "\n"
end

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_cookread") ~= nil
	end

	-- the unit a root is opened in: the `cook` lines name it
	local function unitOf(root: string): string?
		for _, c in ctx.kind("cook") do
			if c[4] == root then
				return c[3]
			end
		end
		return nil
	end

	-- the same file with its MAGIC word's eight bytes reversed
	local function swapped(data: buffer): buffer
		local out = buffer.create(buffer.len(data))
		buffer.copy(out, 0, data)
		for i = 0, 3 do
			local a, z = buffer.readu8(data, i), buffer.readu8(data, 7 - i)
			buffer.writeu8(out, i, z)
			buffer.writeu8(out, 7 - i, a)
		end
		return out
	end

	-- Open over the placed file: `lead` is the base's offset into a buffer
	-- exactly lead + extent long (nil lead: no buffer at all). The reason is
	-- the first failing clause's name, or `ok`.
	local function open(key: string, root: string, data: buffer, extent: number, lead: number?): string
		ctx.tables(key)
		local CR: any = ctx.module(key, "table_cookread")
		if lead == nil then
			local h, why = CR.open(root, nil, 0, extent)
			return if h ~= nil then "ok" else tostring(why)
		end
		local placed = buffer.create(lead + extent)
		buffer.copy(placed, lead, data, 0, math.min(extent, buffer.len(data)))
		local h, why = CR.open(root, placed, lead, extent)
		return if h ~= nil then "ok" else tostring(why)
	end

	-- every cook forgery: the file is already patched, the extent and the base
	-- are what the caller holds
	local function eachForgery(answer: (string, string, string, buffer, number, number?) -> string, only: { [string]: boolean }?)
		for _, f in ctx.kind("forgery") do
			if f[3] ~= "cook" or (only ~= nil and not only[f[2]]) then
				continue
			end
			local key = unitOf(f[4])
			if key == nil or not carried(nil, key) then
				ctx.say(f[2], nil)
				continue
			end
			local data: buffer = ctx.inputs(f[5])
			local extent = tonumber(f[6]) :: number
			if extent < 0 then
				extent = buffer.len(data)
			end
			local lead: number? = if f[7] == "null" then nil else tonumber(f[7])
			ctx.say(f[2], answer(f[2], key, f[4], data, extent, lead))
		end
	end

	local surfaces: { [string]: Surface } = {}
	surfaces["cook"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("cook") do
				if not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				ctx.tables(f[3])
				ctx.say(f[2], dumpCook(ctx.module(f[3], "table_cookread"), ctx.module(f[3], "schema_tables"), f[4], ctx.inputs(f[5])))
			end
		end,
	}
	surfaces["cook-foreign"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("cook") do
				if not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				local data: buffer = ctx.inputs(f[5])
				local why = open(f[3], f[4], swapped(data), buffer.len(data), 0)
				ctx.say(f[2], if why == "ok" then "open\n" else "refuse\n")
			end
		end,
	}
	surfaces["cook-forgery"] = {
		carried = carried,
		run = function()
			eachForgery(function(_, key, root, data, extent, lead)
				return if open(key, root, data, extent, lead) == "ok" then "open\n" else "refuse\n"
			end)
		end,
	}
	surfaces["cook-reason"] = {
		carried = carried,
		run = function()
			local reasons: { [string]: boolean } = {}
			for _, r in ctx.kind("refusal") do
				if r[3] == "cook" then
					reasons[r[2]] = true
				end
			end
			eachForgery(function(_, key, root, data, extent, lead)
				return open(key, root, data, extent, lead) .. "\n"
			end, reasons)
		end,
	}
	surfaces["cook-write"] = {
		carried = function(_: any, key: string): boolean
			return ctx.module(key, "table_cookwrite") ~= nil and ctx.module(key, "table_load") ~= nil
		end,
		run = function()
			local instances = ctx.kind("instance")
			for _, f in ctx.kind("cook-write") do
				local inst: { string }? = nil
				for _, c in instances do
					if c[2] == f[2] then
						inst = c
						break
					end
				end
				if inst == nil then
					fail("cook-write " .. f[2] .. " names no instance")
				end
				local i = inst :: { string }
				local W: any = ctx.module(i[3], "table_cookwrite")
				local L: any = ctx.module(i[3], "table_load")
				if W == nil or L == nil then
					ctx.say(f[2], nil)
					ctx.say(f[2] .. "-be", nil)
					continue
				end
				ctx.tables(i[3])
				local report = L.newReport()
				local value = L.loadTolerant(i[4], ctx.inputs(i[5]), report)
				if value == nil or report.refused > 0 or report.malformed then
					fail(f[2] .. ": cook input did not load")
				end
				for _, big in { false, true } do
					local name = if big then f[2] .. "-be" else f[2]
					local out, why = W.cook(i[4], value, big)
					if out ~= nil then
						ctx.say(name, buffer.tostring(out))
					elseif why ~= nil and why.name == "blob_shared" then
						ctx.refuse(name, why.name)
					else
						ctx.say(name, nil)
					end
				end
			end
		end,
	}
	return surfaces
end
