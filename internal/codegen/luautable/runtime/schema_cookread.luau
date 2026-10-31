--!strict
-- schema_cookread.luau — THE COOKED FORM's READ (docs/SPEC-TABLES.md §7, §7.1,
-- §7.2, §7.4). Open checks the header in §7.4's order and then reads the
-- region the cook carries — Lock's region written verbatim, the root at its
-- base — into the same value the variable-form load builds (§4). A refusal is
-- (nil, reason), the reason spelled exactly as Go's TableRefuseReason.Error().
--
-- The region is storage, not wire: every scalar sits at its C ABI offset at
-- its storage width, an enum slot holds the ORDINAL, a reference is an int64
-- self-relative delta, a counted array's count is its companion, and a map's
-- entries are by-value records inside the holder's node extent (§7.2).
--
-- The build version is a property of the BUILD, not of a declaration, and the
-- Luau table surface does not yet export it: a consumer passes the unit's
-- BuildVersion as Open's third argument, which is what Go's Table source
-- carries as a constant.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")

local C = {}

local MAGIC: integer = 0x4B4F4F434D484353i
local MAGIC_FOREIGN: integer = 0x5343484D434F4F4Bi
local HEADER_BYTES = 64
local MAX_ALIGN = 64
-- the number of storage slots an array value was read with, holes included;
-- # cannot see a null pointer slot, so putField takes the count from here.
local arrayCounts: { [any]: number } = {}

local function as_number(v: any): number
	if typeof(v) == "integer" then return integer.tonumber(v) end
	return v
end

local function align_up(n: number, a: number): number
	if a <= 1 then return n end
	return math.ceil(n / a) * a
end

-- one unsigned integer at a storage width, little-endian (a cook is this
-- build's order or it refuses foreign_order, §7.1).
local function u_at(b: buffer, at: number, width: number): any
	if width == 1 then return buffer.readu8(b, at) end
	if width == 2 then return buffer.readu16(b, at) end
	if width == 4 then return buffer.readu32(b, at) end
	return buffer.readinteger(b, at)
end

local function i_at(b: buffer, at: number, width: number, signed: boolean): any
	if width == 16 then
		return { lo = buffer.readinteger(b, at), hi = buffer.readinteger(b, at + 8) }
	end
	local raw = u_at(b, at, width)
	if width == 8 or not signed then return raw end
	local n = as_number(raw)
	local bits = width * 8
	local top = 2 ^ (bits - 1)
	if n >= top then n -= 2 ^ bits end
	return n
end

-- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; the same rule
-- as packets): a NaN or an infinity is not a value of the type.
-- Open tests each float's RAW BITS before they become a value and refuses
-- one, non_finite_float naming <Owner>.<field>, before handing back anything.
local f32tmp = buffer.create(4)
local function f_finite(b: buffer, at: number, single: boolean): boolean
	if single then return bit32.band(buffer.readu32(b, at), 0x7F800000) ~= 0x7F800000 end
	return bit32.band(buffer.readu32(b, at + 4), 0x7FF00000) ~= 0x7FF00000
end

local function f_at(b: buffer, at: number, single: boolean): number
	if not single then return buffer.readf64(b, at) end
	buffer.writeu32(f32tmp, 0, buffer.readu32(b, at))
	return buffer.readf32(f32tmp, 0)
end

-- enum_width is the ORDINAL slot's storage width (§4.2, §7.2): the smallest
-- byte width the variant count fits, which is how the cook sizes the slot.
local function enum_width(n: number): number
	if n <= 0xff then return 1 end
	if n <= 0xffff then return 2 end
	if n <= 0xffffffff then return 4 end
	return 8
end

-- ---- the C ABI layout the region is written with (§2.7, §7.2) ----

type Layout = { size: number, align: number }

local recordLayouts: { [string]: Layout } = {}
local unionLayouts: { [string]: { size: number, align: number, tag: number, armOffset: number } } = {}

-- intWidth is an integer-shaped slot's cooked storage width (§7.2): bits(N)
-- is a uint32/uint64 in C, whatever the wire's kind byte says, and every
-- other integer kind carries its width in kw.
local function intWidth(fd: any): number
	if fd.bits ~= nil then
		if fd.bits <= 32 then return 4 end
		return 8
	end
	return fd.kw or 1
end

local function elemLayout(fd: any): Layout
	local s = fd.s
	if s == "bool" then return { size = 1, align = 1 } end
	if s == "int" or s == "flags" then
		local w = if s == "flags" then 8 else intWidth(fd)
		return { size = w, align = w }
	end
	if s == "float" then
		local w = if fd.f32 then 4 else 8
		return { size = w, align = w }
	end
	if s == "enum" then
		local w = enum_width(#fd.vs)
		return { size = w, align = w }
	end
	if s == "tab" then return recordLayout(fd.t) end
	if s == "union" then
		local u = unionLayout(fd.t)
		return { size = u.size, align = u.align }
	end
	if s == "ptr" or s == "sblob" or s == "bblob" or s == "text" then
		return { size = 8, align = 8 }
	end
	if s == "str" then
		local w = if fd.wide then 2 else 1
		return { size = ((fd.bound or 0) + 1) * w, align = w }
	end
	if s == "bytes" then
		return { size = fd.bound or 0, align = 1 }
	end
	return { size = 1, align = 1 }
end

-- layoutField is one field's whole storage at an ABSOLUTE record offset: its
-- pieces laid out the way ir.FieldPieces spells them, presence included. The
-- absolute offset matters because a string's int32 companion pads from where
-- the buffer ends, not from the field's start.
local function layoutField(fd: any, offset: number): { start: number, size: number, align: number, finish: number }
	local cur, start, maxAlign = offset, -1, 1
	local function add(size: number, align: number)
		if align > maxAlign then maxAlign = align end
		cur = align_up(cur, align)
		if start < 0 then start = cur end
		cur += size
	end
	local s = fd.s
	if s == "map" then
		add(16, 8)
	elseif s == "ptr" or s == "sblob" or s == "bblob" or s == "text" then
		add(8, 8)
	elseif s == "str" then
		local w = if fd.wide then 2 else 1
		add(((fd.bound or 0) + 1) * w, w)
		add(4, 4)
	elseif s == "bytes" then
		add(fd.bound or 0, 1)
		add(4, 4)
	elseif s == "arr" then
		if fd.array == "list" then
			add(16, 8)
		else
			local e = elemLayout(fd.e)
			add(e.size * (fd.bound or 0), e.align)
			if fd.array == "counted" then add(4, 4) end
		end
	else
		local e = elemLayout(fd)
		add(e.size, e.align)
	end
	if fd.present ~= nil then add(1, 1) end
	if start < 0 then start = cur end
	return { start = start, size = cur - start, align = maxAlign, finish = cur }
end

function recordLayout(name: string): Layout
	local got = recordLayouts[name]
	if got ~= nil then return got end
	local d = S.lookup(name)
	recordLayouts[name] = { size = 1, align = 1 }
	local offset, maxAlign = 0, 1
	for _, fd in d.fields do
		local fl = layoutField(fd, offset)
		offset = fl.finish
		if fl.align > maxAlign then maxAlign = fl.align end
	end
	local out = { size = align_up(offset, maxAlign), align = maxAlign }
	if out.size == 0 then out.size = 1 end
	recordLayouts[name] = out
	return out
end

function unionLayout(name: string): { size: number, align: number, tag: number, armOffset: number }
	local got = unionLayouts[name]
	if got ~= nil then return got end
	local d = S.lookup(name)
	local tag = enum_width(#d.arms)
	local maxAlign, maxSize = 1, 0
	for _, arm in d.arms do
		if arm.s ~= "void" then
			local fl = layoutField(arm, 0)
			if fl.align > maxAlign then maxAlign = fl.align end
			if fl.size > maxSize then maxSize = fl.size end
		end
	end
	local align = if maxAlign > tag then maxAlign else tag
	local armOffset = align_up(tag, maxAlign)
	local out = { size = align_up(armOffset + maxSize, align), align = align, tag = tag, armOffset = armOffset }
	unionLayouts[name] = out
	return out
end

-- ---- the read (§7.2) ----

type Ctx = {
	b: buffer,
	dataOffset: number,
	dataLength: number,
	memo: { [number]: any },
	owner: string,
	field: string,
	nonFinite: string?,
}

local readRecord: any
local readField: any
local readUnion: any
local putField: any

local function recordOffsets(fields: { any }): { number }
	local out, offset = {}, 0
	for i, fd in fields do
		local fl = layoutField(fd, offset)
		out[i] = fl.start
		offset = fl.finish
	end
	return out
end

local function zeroElement(fd: any, list: boolean): any
	local s = fd.s
	if s == "bool" then return false end
	if s == "int" or s == "flags" or s == "enum" then return 0 end
	if s == "float" then return 0.0 end
	if s == "str" then return "" end
	if s == "bytes" then return buffer.create(0) end
	if s == "ptr" or s == "sblob" or s == "bblob" then return if list then false else nil end
	if s == "tab" then return S.lookup(fd.t).new() end
	if s == "union" then return S.lookup(fd.t).new() end
	return nil
end

local function readElement(ctx: Ctx, fd: any, at: number, list: boolean): any
	local s = fd.s
	local value = readField(ctx, fd, at, false)
	if value == nil and (s == "ptr" or s == "sblob" or s == "bblob" or s == "text") then
		return zeroElement(fd, list)
	end
	return value
end

-- readField reads one value at an ABSOLUTE region offset.
function readField(ctx: Ctx, fd: any, at: number, present: boolean): any
	if fd.present ~= nil then
		local fl = layoutField(fd, at)
		if buffer.readu8(ctx.b, at + fl.size - 1) == 0 then return nil end
	end
	local s = fd.s
	if s == "bool" then return buffer.readu8(ctx.b, at) ~= 0 end
	if s == "int" then return i_at(ctx.b, at, intWidth(fd), fd.signed == true) end
	if s == "flags" then return buffer.readinteger(ctx.b, at) end
	if s == "float" then
		if not f_finite(ctx.b, at, fd.f32 == true) then
			if ctx.nonFinite == nil then ctx.nonFinite = ctx.owner .. "." .. (fd.name or ctx.field) end
			return 0.0
		end
		return f_at(ctx.b, at, fd.f32 == true)
	end
	if s == "enum" then
		local w = enum_width(#fd.vs)
		return as_number(i_at(ctx.b, at, w, false))
	end
	if s == "str" then
		local w = if fd.wide then 2 else 1
		local lenAt = align_up(at + ((fd.bound or 0) + 1) * w, 4)
		local n = buffer.readi32(ctx.b, lenAt)
		if n < 0 then n = 0 end
		if fd.wide then
			local out = buffer.create(n * 2)
			buffer.copy(out, 0, ctx.b, at, n * 2)
			return out
		end
		local out = buffer.create(n)
		buffer.copy(out, 0, ctx.b, at, n)
		return out
	end
	if s == "bytes" then
		local lenAt = align_up(at + (fd.bound or 0), 4)
		local n = buffer.readi32(ctx.b, lenAt)
		if n < 0 then n = 0 end
		local out = buffer.create(n)
		buffer.copy(out, 0, ctx.b, at, n)
		return out
	end
	if s == "sblob" or s == "bblob" then
		local delta = integer.tonumber(buffer.readinteger(ctx.b, at))
		if delta == 0 then return nil end
		local target = at + delta
		local n = buffer.readu32(ctx.b, target)
		local out = buffer.create(n)
		buffer.copy(out, 0, ctx.b, target + 8, n)
		return out
	end
	if s == "ptr" then
		local delta = integer.tonumber(buffer.readinteger(ctx.b, at))
		if delta == 0 then return nil end
		local target = at + delta
		local got = ctx.memo[target]
		if got ~= nil then return got end
		local inst = S.lookup(fd.t).new()
		ctx.memo[target] = inst
		readRecord(ctx, inst, fd.t, target)
		return inst
	end
	if s == "text" then
		local delta = integer.tonumber(buffer.readinteger(ctx.b, at))
		if delta == 0 then return nil end
		local target = at + delta
		local n = buffer.readu32(ctx.b, target)
		local out = buffer.create(n)
		buffer.copy(out, 0, ctx.b, target + 8, n)
		return out
	end
	if s == "tab" then
		local inst = S.lookup(fd.t).new()
		readRecord(ctx, inst, fd.t, at)
		return inst
	end
	if s == "union" then return readUnion(ctx, fd.t, at) end
	if s == "map" then
		local out = {}
		local delta = integer.tonumber(buffer.readinteger(ctx.b, at))
		local count = buffer.readi32(ctx.b, at + 8)
		if delta == 0 or count <= 0 then return out end
		local entry = S.lookup(fd.t)
		local start = at + delta
		local esize = recordLayout(fd.t).size
		for i = 0, count - 1 do
			local inst = entry.new()
			readRecord(ctx, inst, fd.t, start + i * esize)
			out[i + 1] = inst
		end
		return out
	end
	if s == "arr" then
		local e = fd.e
		local out = {}
		if fd.array == "list" then
			local delta = integer.tonumber(buffer.readinteger(ctx.b, at))
			local count = buffer.readi32(ctx.b, at + 8)
			local got = 0
			if delta ~= 0 then
				local el = elemLayout(e)
				local start = at + delta
				for i = 0, count - 1 do
					out[i + 1] = readElement(ctx, e, start + i * el.size, true)
					got = i + 1
				end
			end
			-- a list of pointers may hold null slots, which a Luau length cannot
			-- see: its live length rides as n, the table.pack convention (§2.9),
			-- exactly as schema_load.luau writes it.
			if (e.s == "ptr" or e.s == "sblob" or e.s == "bblob") and got > 0 then out.n = got end
			return out
		end
		local n = if fd.array == "fixed" then (fd.bound or 0) else buffer.readi32(ctx.b, align_up(at + elemLayout(e).size * (fd.bound or 0), 4))
		if n < 0 then n = 0 end
		if fd.array == "keyed" then n = fd.bound or 0 end
		local el = elemLayout(e)
		for i = 0, n - 1 do
			out[i + 1] = readElement(ctx, e, at + i * el.size, false)
		end
		arrayCounts[out] = n
		return out
	end
	return nil
end

-- A UNION is a TAG beside its arms (§2.6, §7.2): only the set arm's bytes are
-- live. A tag naming no arm reads None.
function readUnion(ctx: Ctx, name: string, at: number): any
	local d = S.lookup(name)
	local u = d.new()
	local L = unionLayout(name)
	local tag = as_number(i_at(ctx.b, at, L.tag, false))
	if tag == 0 or tag > #d.arms then return u end
	local arm = d.arms[tag]
	if arm.s == "void" then
		u.Type = arm.tag
		return u
	end
	ctx.owner, ctx.field = name, arm.name
	local value = readField(ctx, arm, at + L.armOffset, true)
	u.Type = arm.tag
	-- the arm lands through the same store as a record field, so a string
	-- keeps its used length and a counted array its Count companion (§7.2).
	putField(u, arm, value)
	return u
end

-- ONE RECORD (§7.2): every field at its offset. Unguarded fields first,
-- because a guard names bool fields of the same record; a field under a false
-- guard is elided by the writer and keeps its constructor default.
function putField(value: any, fd: any, got: any)
	if got == nil or fd.slot == nil or fd.slot == "" then return end
	if fd.s == "arr" then
		if fd.array ~= "list" and typeof(got) == "table" then
			local dst = value[fd.slot]
			if typeof(dst) ~= "table" then
				dst = {}
				value[fd.slot] = dst
			end
			local n = arrayCounts[got] or #got
			for i = 1, n do
				if got[i] ~= nil then dst[i] = got[i] end
			end
			if fd.array == "counted" then value[fd.slot .. "Count"] = n end
			return
		end
		value[fd.slot] = got
		return
	end
	if fd.s == "str" or fd.s == "bytes" then
		local dst = value[fd.slot]
		if typeof(dst) ~= "buffer" or buffer.len(dst) < buffer.len(got) then
			dst = buffer.create(buffer.len(got))
			value[fd.slot] = dst
		end
		buffer.fill(dst, 0, 0)
		buffer.copy(dst, 0, got, 0, buffer.len(got))
		local units = if fd.s == "str" and fd.wide then buffer.len(got) // 2 else buffer.len(got)
		value[fd.slot .. "Length"] = units
		return
	end
	value[fd.slot] = got
end

function readRecord(ctx: Ctx, value: any, name: string, base: number)
	local d = S.lookup(name)
	local offsets = recordOffsets(d.fields)
	for i, fd in d.fields do
		if fd.guard == nil then
			ctx.owner, ctx.field = name, fd.name
			local at = base + offsets[i]
			local got = readField(ctx, fd, at, true)
			putField(value, fd, got)
			if fd.present ~= nil then value[fd.present] = got ~= nil end
		end
	end
	for i, fd in d.fields do
		if fd.guard ~= nil then
			local live = true
			for _, term in fd.guard do
				if value[term[1]] ~= term[2] then live = false; break end
			end
			if live then
				ctx.owner, ctx.field = name, fd.name
				local at = base + offsets[i]
				local got = readField(ctx, fd, at, true)
				putField(value, fd, got)
				if fd.present ~= nil then value[fd.present] = got ~= nil end
			end
		end
	end
end

-- ---- Open: §7.4's check, in order ----

local function refuse(reason: string): (nil, string)
	return nil, reason
end

-- Check is Open's header check alone, in §7.4's order, over a cook that
-- starts `base` bytes into `bytes` and runs `extent` bytes from there (the
-- length the caller claims; the rest of the buffer by default). It answers
-- nil when the header admits the cook, or the first failing clause's name,
-- spelled as Go's TableRefuseReason. Like Go's Open it reads the header and
-- nothing in the region, so a forged reference slot is the deref's business,
-- not Open's; and the base's alignment is LAST, against the alignment the
-- header carries, because it is the one clause that reads nothing out of the
-- cook (§7.4). A nil buffer is a null base: unaligned_base.
function C.Check(root: string, bytes: buffer?, build: integer?, base: number?, extent: number?): string?
	if bytes == nil or typeof(bytes) ~= "buffer" then
		return "unaligned_base"
	end
	local at = base or 0
	local n = extent or (buffer.len(bytes) - at)
	if n < HEADER_BYTES then return "truncated" end
	local magic = buffer.readinteger(bytes, at)
	if magic ~= MAGIC then
		if magic == MAGIC_FOREIGN then return "foreign_order" end
		return "not_a_cook"
	end
	if buffer.readinteger(bytes, at + 16) ~= 1i then return "not_a_cook" end
	if build ~= nil and buffer.readinteger(bytes, at + 8) ~= build then return "wrong_build_version" end
	if buffer.readinteger(bytes, at + 48) ~= 0i or buffer.readinteger(bytes, at + 56) ~= 0i then return "reserved_not_zero" end
	local rootSize = recordLayout(root).size
	local rootAlign = recordLayout(root).align
	-- the header's words are unsigned: a saturated one reads as -1 here and
	-- must refuse as the huge number it is, never pass as a small one
	local alignWord = buffer.readinteger(bytes, at + 40)
	if integer.ult(alignWord, 8i) or integer.ugt(alignWord, integer.create(MAX_ALIGN)) then
		return "bad_alignment"
	end
	local alignment = integer.tonumber(alignWord)
	if bit32.band(alignment, alignment - 1) ~= 0 or alignment % rootAlign ~= 0 then
		return "bad_alignment"
	end
	local dataOffset = align_up(HEADER_BYTES, alignment)
	if dataOffset > n then return "truncated" end
	local room = integer.create(n - dataOffset)
	local dataWord = buffer.readinteger(bytes, at + 24)
	if integer.ugt(dataWord, room) then return "truncated" end
	local dataLength = integer.tonumber(dataWord)
	local attribution = buffer.readinteger(bytes, at + 32)
	if attribution ~= integer.create(n - dataOffset - dataLength) or dataLength < rootSize then
		return "truncated"
	end
	if at % alignment ~= 0 then return "unaligned_base" end
	return nil
end

-- Open checks the header in §7.4's order and answers the root value read from
-- the region, or (nil, reason). The caller passes this build's version, the
-- fact Go's Table source carries as a constant.
function C.open(root: string, bytes: buffer?, build: integer?): (any, string?)
	local why = C.Check(root, bytes, build, 0, nil)
	if why ~= nil then return refuse(why) end
	local b = bytes :: buffer
	local d = S.lookup(root)
	local dataOffset = align_up(HEADER_BYTES, integer.tonumber(buffer.readinteger(b, 40)))
	local dataLength = integer.tonumber(buffer.readinteger(b, 24))
	local ctx: Ctx = { b = b, dataOffset = dataOffset, dataLength = dataLength, memo = {}, owner = root, field = "?", nonFinite = nil }
	local value = d.new()
	readRecord(ctx, value, root, dataOffset)
	if ctx.nonFinite ~= nil then return nil, "non_finite_float: " .. ctx.nonFinite end
	return value, nil
end

-- the region's C ABI layout, as the reader computes it (§2.7, §7.2): a
-- reflective walk of the region's own bytes (the conformance node dump,
-- docs/SPEC-TABLES.md §7.5) reads offsets and widths from here rather than
-- re-deriving them.
C.layout = table.freeze({
	record = recordLayout,
	union = unionLayout,
	field = layoutField,
	elem = elemLayout,
	offsets = recordOffsets,
	enumWidth = enum_width,
	intWidth = intWidth,
	alignUp = align_up,
	headerBytes = HEADER_BYTES,
})

return table.freeze(C)
