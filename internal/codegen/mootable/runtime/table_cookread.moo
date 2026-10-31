--!strict
-- table_cookread.moo — THE COOKED FORM'S OPEN AND READ (docs/SPEC-TABLES.md §7,
-- §7.1, §7.2, §7.4), the moo leg of Go's generated <Root>Cook.Open and its
-- accessors. Emitted beside the unit's <Base>Table.moo modules when a table of
-- the unit carries the cook form (§2.12); each module registers the cooked
-- layout of every record and union it describes (internal/codegen/mootable
-- cookform.go, from ir's one layout model, §19.3).
--
-- OPEN IS O(1) IN THE FILE'S SIZE: it checks the 64-byte header in §7.4's
-- order — the base, the length, the magic, the byte order, the build version,
-- the reserved words, the alignment, the part lengths, and last the base's
-- alignment — and answers a handle, or nil and the first failing clause's
-- name, the SAME name Go's Open answers. It reads no node: a reference with a
-- forged delta opens, and is refused by the read that follows it.
--
-- READ walks the region from the root into the same moo value the variable
-- load builds (table_load.moo; docs/PORT-MOO.md): every scalar at its C ABI
-- offset at its storage width, an enum slot the ORDINAL, a reference an int64
-- self-relative delta, a counted array's count and a string's used length the
-- int32 companions, a map's and a list's elements by-value records at the
-- delta (§7.2). A float whose bits are a NaN or an infinity is refused as
-- non_finite_float, naming the field (§3.3); a reference that leaves the
-- region is refused as bad_reference, naming the field.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local V = require("./table_values")

local C = {}

local MAGIC_LO, MAGIC_HI = 0x4D484353, 0x4B4F4F43
local FOREIGN_LO, FOREIGN_HI = 0x434F4F4B, 0x5343484D
local HEADER_BYTES = 64
local MAX_ALIGN = 64

local records: { [string]: any } = {}
local unions: { [string]: any } = {}
local build: any = nil

local function freezeFields(list: { any })
	for _, f in list do
		table.freeze(f.p)
		table.freeze(f)
	end
	table.freeze(list)
end

-- unit registers this build's version (§20), the same in every module
function C.unit(version: any)
	build = version
end

-- record and union register one cooked layout by its descriptor's name
function C.record(name: string, layout: any)
	freezeFields(layout.fields)
	records[name] = table.freeze(layout)
end

function C.union(name: string, layout: any)
	freezeFields(layout.arms)
	unions[name] = table.freeze(layout)
end

local function align_up(n: number, a: number): number
	local r = n % a
	if r == 0 then
		return n
	end
	return n + (a - r)
end

-- one u64 header word as a number, V.HUGE when it does not fit one
local function word(b: buffer, at: number): number
	local lo, hi = buffer.readu32(b, at), buffer.readu32(b, at + 4)
	if hi >= 32768 then
		return V.HUGE
	end
	return hi * V.TWO32 + lo
end

-- open(root, buffer, base, extent): the handle, or nil and the first failing
-- clause's name (docs/SPEC-TABLES.md §7.4). base is the file's byte offset in
-- buffer, extent the length the caller claims; a nil buffer is a null base.
function C.open(root: string, data: buffer?, base: number?, extent: number?): (any, string?)
	local d = S.lookup(root)
	if d.table and d.forms ~= nil and d.forms.cook ~= true then
		error("schema: table " .. root .. " does not carry cook (forms declaration)", 2)
	end
	if data == nil then
		return nil, "unaligned_base"
	end
	local b = data :: buffer
	local at = base or 0
	local bytes = extent or (buffer.len(b) - at)
	if bytes < HEADER_BYTES or at + bytes > buffer.len(b) then
		return nil, "truncated"
	end
	local lo, hi = buffer.readu32(b, at), buffer.readu32(b, at + 4)
	if lo ~= MAGIC_LO or hi ~= MAGIC_HI then
		if lo == FOREIGN_LO and hi == FOREIGN_HI then
			return nil, "foreign_order"
		end
		return nil, "not_a_cook"
	end
	if buffer.readu32(b, at + 16) ~= 1 or buffer.readu32(b, at + 20) ~= 0 then
		return nil, "not_a_cook"
	end
	if build == nil or buffer.readu32(b, at + 8) ~= build[1] or buffer.readu32(b, at + 12) ~= build[2] then
		return nil, "wrong_build_version"
	end
	for o = 48, 60, 4 do
		if buffer.readu32(b, at + o) ~= 0 then
			return nil, "reserved_not_zero"
		end
	end
	local layout = records[root]
	local alignment = word(b, at + 40)
	if alignment < 8 or alignment > MAX_ALIGN or bit32.band(alignment, alignment - 1) ~= 0 or alignment % layout.align ~= 0 then
		return nil, "bad_alignment"
	end
	local dataOffset = align_up(HEADER_BYTES, alignment)
	local dataLength = word(b, at + 24)
	local attribution = word(b, at + 32)
	if dataOffset > bytes or dataLength > bytes - dataOffset or attribution ~= bytes - dataOffset - dataLength or dataLength < layout.size then
		return nil, "truncated"
	end
	if at % alignment ~= 0 then
		return nil, "unaligned_base"
	end
	return { buffer = b, region = at + dataOffset, regionLength = dataLength, root = root }, nil
end

-- ---- the read (§7.2) ----

local readRecord: any
local readUnion: any

-- the refusal the read answers, naming the field
local function fail(ctx: any, name: string, owner: string, fd: any): any
	if ctx.why == nil then
		ctx.why = { name = name, field = owner .. "." .. (fd.name or "") }
	end
	return nil
end

-- one integer at a storage width and signedness
local function rawAt(b: buffer, at: number, w: number, signed: boolean): any
	if w == 1 then
		return if signed then buffer.readi8(b, at) else buffer.readu8(b, at)
	elseif w == 2 then
		return if signed then buffer.readi16(b, at) else buffer.readu16(b, at)
	elseif w == 4 then
		return if signed then buffer.readi32(b, at) else buffer.readu32(b, at)
	elseif w == 8 then
		local Cl = if signed then V.Int64 else V.Uint64
		return Cl.new(buffer.readu32(b, at), buffer.readu32(b, at + 4))
	end
	local Cl = if signed then V.Int128 else V.Uint128
	return Cl.new(buffer.readu32(b, at), buffer.readu32(b, at + 4), buffer.readu32(b, at + 8), buffer.readu32(b, at + 12))
end

-- a reference slot's target: nil for null, false for one that leaves the
-- region (the caller refuses it)
local function target(ctx: any, at: number, need: number): any
	local lo, hi = buffer.readu32(ctx.buffer, at), buffer.readu32(ctx.buffer, at + 4)
	if lo == 0 and hi == 0 then
		return nil
	end
	local delta = V.Int64.tryNumber(V.Int64.new(lo, hi))
	if delta == nil then
		return false
	end
	local t = at + delta
	if t < ctx.region or t + need > ctx.region + ctx.regionLength then
		return false
	end
	return t
end

-- a blob record (§2.5): its u32 length, then the bytes eight past its start
local function blobAt(ctx: any, at: number): buffer?
	local t = target(ctx, at, 8)
	if t == nil or t == false then
		return nil
	end
	local n = buffer.readu32(ctx.buffer, t)
	if t + 8 + n > ctx.region + ctx.regionLength then
		return nil
	end
	local out = buffer.create(n)
	buffer.copy(out, 0, ctx.buffer, t + 8, n)
	return out
end

-- one scalar of descriptor e at `at`, its storage width w
local function scalarAt(ctx: any, owner: string, named: any, e: any, at: number, w: number): any
	local b = ctx.buffer
	local s = e.s
	if s == "bool" then
		return buffer.readu8(b, at) ~= 0
	elseif s == "float" then
		if e.f32 then
			local bits = buffer.readu32(b, at)
			if not V.finite32(bits) then
				return fail(ctx, "non_finite_float", owner, named)
			end
			return bits
		end
		local lo, hi = buffer.readu32(b, at), buffer.readu32(b, at + 4)
		if not V.finite64(lo, hi) then
			return fail(ctx, "non_finite_float", owner, named)
		end
		return V.Uint64.new(lo, hi)
	elseif s == "enum" then
		local x = rawAt(b, at, w, false)
		if type(x) ~= "number" then
			return V.Uint64.tryNumber(x) or 0
		end
		return x
	end
	return V.toStorage(e, rawAt(b, at, w, e.signed == true))
end

-- a str element or string(N) value: the buffer, then the int32 used length
-- at the next four-aligned offset past its N + 1 slots
local function textAt(ctx: any, e: any, at: number, lenAt: number?): string
	local units = if e.wide then 2 else 1
	local la = lenAt or align_up(at + ((e.bound or 0) + 1) * units, 4)
	local n = buffer.readi32(ctx.buffer, la)
	if n < 0 then
		n = 0
	elseif n > (e.bound or 0) then
		n = e.bound or 0
	end
	return buffer.readstring(ctx.buffer, at, n)
end

-- one ELEMENT of an array, list or map at `at` (§7.2)
local function readElement(ctx: any, owner: string, fd: any, e: any, at: number, w: number, list: boolean): any
	local s = e.s
	if s == "ptr" then
		local t = target(ctx, at, 1)
		if t == false then
			return fail(ctx, "bad_reference", owner, fd)
		end
		if t == nil then
			return if list then false else nil
		end
		local got = ctx.memo[t]
		if got ~= nil then
			return got
		end
		local d = S.lookup(e.t)
		local inst = d.new()
		ctx.memo[t] = inst
		readRecord(ctx, d, inst, t)
		return inst
	elseif s == "sblob" or s == "bblob" then
		local blob = blobAt(ctx, at)
		if blob == nil then
			return if list then false else nil
		end
		return blob
	elseif s == "tab" then
		local d = S.lookup(e.t)
		local inst = d.new()
		readRecord(ctx, d, inst, at)
		return inst
	elseif s == "union" then
		return readUnion(ctx, e.t, at)
	elseif s == "str" then
		return textAt(ctx, e, at, nil)
	end
	return scalarAt(ctx, owner, fd, e, at, w)
end

-- ONE FIELD of a record (or an arm) whose pieces sit at base + l.p[i]; it
-- lands in v the way table_load.moo lands the same field.
local function readField(ctx: any, owner: string, fd: any, l: any, base: number, v: any): ()
	local b = ctx.buffer
	local at = base + l.p[1]
	local s = fd.s
	local slot = fd.slot
	if fd.present ~= nil then
		local p = buffer.readu8(b, base + l.p[#l.p]) ~= 0
		v[fd.present] = p
		if not p then
			return
		end
	end
	if s == "str" and fd.wide then
		local n = buffer.readi32(b, base + l.p[2])
		if n < 0 then
			n = 0
		elseif n > fd.bound then
			n = fd.bound
		end
		local out = buffer.create(fd.bound * 2)
		buffer.copy(out, 0, b, at, n * 2)
		v[slot] = out
		v[slot .. "Length"] = n
	elseif s == "str" then
		v[slot] = textAt(ctx, fd, at, base + l.p[2])
	elseif s == "bytes" then
		local n = buffer.readi32(b, base + l.p[2])
		if n < 0 then
			n = 0
		elseif n > fd.bound then
			n = fd.bound
		end
		local out = buffer.create(fd.bound)
		buffer.copy(out, 0, b, at, n)
		v[slot] = out
		v[slot .. "Length"] = n
	elseif s == "text" then
		local blob = blobAt(ctx, at)
		v[slot] = if blob == nil then nil else buffer.tostring(blob)
	elseif s == "sblob" or s == "bblob" or s == "ptr" then
		v[slot] = readElement(ctx, owner, fd, fd, at, 8, false)
	elseif s == "tab" then
		local d = S.lookup(fd.t)
		local inst = d.new()
		readRecord(ctx, d, inst, at)
		v[slot] = inst
	elseif s == "union" then
		v[slot] = readUnion(ctx, fd.t, at)
	elseif s == "map" or (s == "arr" and fd.array == "list") then
		local t = target(ctx, at, 0)
		local count = buffer.readi32(b, at + 8)
		local out = {}
		if t == false or (t ~= nil and count > 0 and t + count * l.elem > ctx.region + ctx.regionLength) then
			fail(ctx, "bad_reference", owner, fd)
			return
		end
		if t ~= nil and count > 0 then
			local e = if s == "map" then { s = "tab", t = fd.t } else fd.e
			for i = 0, count - 1 do
				out[i + 1] = readElement(ctx, owner, fd, e, t + i * l.elem, l.elem, true)
			end
		end
		v[slot] = out
	elseif s == "arr" then
		local n = fd.bound
		if fd.array == "counted" then
			n = buffer.readi32(b, base + l.p[2])
			if n < 0 then
				n = 0
			elseif n > fd.bound then
				n = fd.bound
			end
			v[slot .. "Count"] = n
		end
		local dst = v[slot]
		for i = 0, n - 1 do
			local x = readElement(ctx, owner, fd, fd.e, at + i * l.elem, l.elem, false)
			if type(dst) == "buffer" then
				buffer.writeu8(dst, i, x)
			else
				dst[i + 1] = x
			end
		end
	else
		v[slot] = scalarAt(ctx, owner, fd, fd, at, l.w)
	end
end

-- ONE RECORD (§7.2): every field at its offset. Unguarded fields first,
-- because a guard names bool fields of the same record; a field under a false
-- guard was elided by the writer and keeps its constructor default.
readRecord = function(ctx: any, d: any, v: any, base: number)
	local layout = records[d.name]
	for i, fd in d.fields do
		if fd.guard == nil then
			readField(ctx, d.name, fd, layout.fields[i], base, v)
		end
	end
	for i, fd in d.fields do
		if fd.guard ~= nil then
			local live = true
			for _, term in fd.guard do
				if v[term[1]] ~= term[2] then
					live = false
					break
				end
			end
			if live then
				readField(ctx, d.name, fd, layout.fields[i], base, v)
			end
		end
	end
end

-- A UNION is a TAG beside its arms (§2.6, §7.2): only the set arm's bytes are
-- live; a tag naming no arm reads None.
readUnion = function(ctx: any, name: string, at: number): any
	local d = S.lookup(name)
	local u = d.new()
	local layout = unions[name]
	local tag = rawAt(ctx.buffer, at, layout.tag, false)
	if type(tag) ~= "number" or tag == 0 or tag > #d.arms then
		return u
	end
	local arm = d.arms[tag]
	u.Type = arm.tag
	if arm.s ~= "void" then
		readField(ctx, name, arm, layout.arms[tag], at + layout.armOffset, u)
	end
	return u
end

-- read(handle): the root value read from the region, or nil and the refusal
-- { name, field } (non_finite_float, bad_reference).
function C.read(handle: any): (any, any)
	local d = S.lookup(handle.root)
	local ctx = { buffer = handle.buffer, region = handle.region, regionLength = handle.regionLength, memo = {}, why = nil }
	local v = d.new()
	readRecord(ctx, d, v, handle.region)
	if ctx.why ~= nil then
		return nil, ctx.why
	end
	return v, nil
end

-- the cooked layout of a record, for a reflective walker and the write side
function C.layout(name: string): any
	return records[name]
end

-- the cooked layout of a union, and this build's version (§20), for the write
-- side (table_cookwrite.moo)
function C.unionLayout(name: string): any
	return unions[name]
end

function C.version(): any
	return build
end

return table.freeze(C)
