--!strict
-- table_cookwrite.moo — THE COOKED FORM'S WRITE (docs/SPEC-TABLES.md §7.6),
-- the moo leg of Go's generated <Root>CookMeasure and <Root>CookFrom, and of
-- the tool's `schema cook`: a build can write a cook, and THE BYTES ARE THE
-- TOOL'S, in both byte orders, from the same value. Emitted beside
-- table_cookread.moo when a table of the unit carries the cook form (§2.12);
-- it writes through the SAME cooked layouts that file's read walks (every
-- record's and union's storage pieces at their C ABI offsets, from ir's one
-- layout model, §19.3), so the writer and the reader of one build cannot
-- disagree about where a byte is.
--
-- THE REGION IS §7.2'S: the root at zero; every reachable node once, at its
-- OWN type's alignment in the first-visit order table_save.moo numbers a wire
-- with (§3.1; a byte buffer named from two slots is ONE node here, where the
-- wire refuses it); the data length rounded to the region's alignment, the
-- greatest of the nodes' and never below eight; a reference slot the signed
-- self-relative delta from the slot to its node, zero for null; the
-- attribution part one (offset, type id) entry per node.
--
-- A RECORD IS WRITTEN PIECE BY PIECE: the buffer is created zeroed and every
-- byte no field covers stays zero (padding, a string's unused tail, the bytes
-- of a union outside its set arm), and every scalar goes through the TARGET's
-- byte order — a parameter of the write, never the host's — so a big-endian
-- cook is a byte-swap of every scalar, written once. A counted array writes
-- all N slots; a dead slot carries what the value's storage carries (§7.2).
--
-- A list rides in its holder's EXTENT (§2.9, §7.2): the node is its record,
-- rounded to the region's alignment floor, then the arrays of the lists it
-- holds by value, carved by one pre-order walk that measures and writes (the
-- same walk with the writing half switched off, so the two cannot drift).
--
-- A float whose bits are a NaN or an infinity is refused as non_finite_float
-- naming the field (§3.3); a reference the numbering does not carry is
-- refused as unnumbered_reference, a data cycle as cycle, and a map-bearing
-- value as map_in_cook, as the tool refuses it (§7.2).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local V = require("./table_values")
local Save = require("./table_save")
local C = require("./table_cookread")

local W = {}

local TWO32 = V.TWO32
local MAGIC_LO, MAGIC_HI = 0x4D484353, 0x4B4F4F43
local HEADER_BYTES = 64
local REGION_ALIGN_FLOOR = 8
local DIRECTORY_ENTRY = 16
local BYTE_ORDER_LITTLE, BYTE_ORDER_BIG = 1, 2
local RANGE: { number } = { 256, 65536, 0, TWO32 }

local function align_up(n: number, a: number): number
	local r = n % a
	if r == 0 then
		return n
	end
	return n + (a - r)
end

-- ---- the scalar stores, every one through the target's byte order ----

-- n whole bytes of one value in [0, 2^(8w)), w one of 1, 2, 4
local function putUN(ctx: any, at: number, w: number, x: number)
	local b = ctx.b
	if w == 1 then
		buffer.writeu8(b, at, x)
	elseif w == 2 then
		if ctx.big then
			buffer.writeu8(b, at, x // 256)
			buffer.writeu8(b, at + 1, x % 256)
		else
			buffer.writeu16(b, at, x)
		end
	elseif ctx.big then
		buffer.writeu32(b, at, bit32.byteswap(x))
	else
		buffer.writeu32(b, at, x)
	end
end

-- a 64-bit value as its low and high words
local function putU64(ctx: any, at: number, lo: number, hi: number)
	local b = ctx.b
	if ctx.big then
		buffer.writeu32(b, at, bit32.byteswap(hi))
		buffer.writeu32(b, at + 4, bit32.byteswap(lo))
	else
		buffer.writeu32(b, at, lo)
		buffer.writeu32(b, at + 4, hi)
	end
end

-- a moo number (an integer, negative or not, within 2^47) as 64 two's
-- complement bits: a reference's delta, a length
local function putNum64(ctx: any, at: number, n: number)
	putU64(ctx, at, n % TWO32, (n // TWO32) % TWO32)
end

-- n limbs, least significant first, written most significant byte first in a
-- big-endian cook
local function putWords(ctx: any, at: number, words: any, n: number)
	local b = ctx.b
	if ctx.big then
		for i = 1, n do
			buffer.writeu32(b, at + (i - 1) * 4, bit32.byteswap(words[n + 1 - i]))
		end
	else
		for i = 1, n do
			buffer.writeu32(b, at + (i - 1) * 4, words[i])
		end
	end
end

-- a raw integer of a kind at the storage width w: a number written two's
-- complement, a wide limb table extended to w when its kind is narrower
local function putRaw(ctx: any, at: number, w: number, signed: boolean, raw: any)
	if type(raw) == "number" then
		if w <= 4 then
			if raw < 0 then
				raw += RANGE[w]
			end
			putUN(ctx, at, w, raw)
			return
		end
		raw = V.widen(raw, w, signed or raw < 0)
	elseif w == 16 and (V.Int64.is(raw) or V.Uint64.is(raw)) then
		raw = V.widen(raw, 16, V.Int64.is(raw))
	end
	putWords(ctx, at, raw, w // 4)
end

-- ---- the refusal: the first value outside its contract, by name ----

local function fail(ctx: any, name: string, owner: string, fd: any): boolean
	if ctx.why == nil then
		ctx.why = { name = name, field = owner .. "." .. ((fd and fd.name) or "") }
	end
	return false
end

-- ---- what a record holds in its node's extent (§2.9, §7.2) ----

local recExtent: { [string]: boolean } = {}
local unionExtent: { [string]: boolean } = {}

local hasExtentRecord: any
local hasExtentUnion: any

-- a by-value container beneath a field or an element: a nested record or a
-- union whose own storage holds a list
local function fieldExtent(fd: any): boolean
	local e = if fd.s == "arr" then fd.e else fd
	if e == nil then
		return false
	end
	if e.s == "tab" then
		return hasExtentRecord(S.lookup(e.t))
	elseif e.s == "union" then
		return hasExtentUnion(S.lookup(e.t))
	end
	return false
end

local function isList(fd: any): boolean
	return fd.s == "arr" and fd.array == "list"
end

hasExtentRecord = function(d: any): boolean
	local got = recExtent[d.name]
	if got ~= nil then
		return got
	end
	local has = false
	for _, fd in d.fields do
		if fd.s == "map" or isList(fd) or fieldExtent(fd) then
			has = true
			break
		end
	end
	recExtent[d.name] = has
	return has
end

hasExtentUnion = function(u: any): boolean
	local got = unionExtent[u.name]
	if got ~= nil then
		return got
	end
	unionExtent[u.name] = false
	local has = false
	for _, arm in u.arms do
		if arm.s ~= "void" and (arm.s == "map" or isList(arm) or fieldExtent(arm)) then
			has = true
			break
		end
	end
	unionExtent[u.name] = has
	return has
end

-- ---- the record writer (§7.2): the reverse of table_cookread.moo's read ----

local writeRecord: any
local writeUnion: any
local writeField: any
local writeElement: any

-- a reference slot: the self-relative delta to the node, zero for null
local function writeRef(ctx: any, owner: string, fd: any, at: number, target: any)
	if target == nil or target == false then
		putNum64(ctx, at, 0)
		return true
	end
	local to = ctx.offset[target]
	if to == nil then
		return fail(ctx, "unnumbered_reference", owner, fd)
	end
	putNum64(ctx, at, to - at)
	return true
end

-- one value of a scalar shape at the storage width w
local function writeScalar(ctx: any, owner: string, fd: any, e: any, at: number, w: number, x: any): boolean
	local s = e.s
	if s == "bool" then
		buffer.writeu8(ctx.b, at, if x == true then 1 else 0)
		return true
	elseif s == "float" then
		if e.f32 then
			if type(x) ~= "number" or x ~= math.floor(x) or x < 0 or x > 0xFFFFFFFF then
				return fail(ctx, "invalid_value", owner, fd)
			end
			if not V.finite32(x) then
				return fail(ctx, "non_finite_float", owner, fd)
			end
			putUN(ctx, at, 4, x)
			return true
		end
		if not V.Uint64.is(x) then
			return fail(ctx, "invalid_value", owner, fd)
		end
		if not V.finite64(x[1], x[2]) then
			return fail(ctx, "non_finite_float", owner, fd)
		end
		putU64(ctx, at, x[1], x[2])
		return true
	elseif s == "enum" then
		if type(x) ~= "number" or x < 0 or x ~= math.floor(x) then
			return fail(ctx, "invalid_value", owner, fd)
		end
		putUN(ctx, at, w, x)
		return true
	end
	local raw = V.toRaw(e, x)
	if raw == nil then
		return fail(ctx, "invalid_value", owner, fd)
	end
	putRaw(ctx, at, w, e.signed == true, raw)
	return true
end

-- a str element of a text list: its bytes, then the int32 used length at the
-- next four-aligned offset past its N + 1 slots
local function writeText(ctx: any, owner: string, fd: any, e: any, at: number, x: any): boolean
	if type(x) ~= "string" then
		return fail(ctx, "invalid_value", owner, fd)
	end
	local bound = e.bound or 0
	local n = #x
	if n > bound then
		return fail(ctx, "invalid_value", owner, fd)
	end
	buffer.writestring(ctx.b, at, x, n)
	putUN(ctx, align_up(at + (bound + 1), 4), 4, n)
	return true
end

-- ONE ELEMENT of an array or list at `at` (§7.2)
writeElement = function(ctx: any, owner: string, fd: any, e: any, at: number, w: number, x: any): boolean
	local s = e.s
	if s == "ptr" or s == "sblob" or s == "bblob" then
		return writeRef(ctx, owner, fd, at, x)
	elseif s == "tab" then
		local d = S.lookup(e.t)
		return writeRecord(ctx, d, at, x or d.new())
	elseif s == "union" then
		return writeUnion(ctx, e.t, at, x)
	elseif s == "str" then
		return writeText(ctx, owner, fd, e, at, x)
	end
	return writeScalar(ctx, owner, fd, e, at, w, x)
end

-- the live length of a wstring(N) or bytes(N) value, clamped to its bound
local function usedLength(v: any, slot: string, bound: number): number
	local n = v[slot .. "Length"]
	if type(n) ~= "number" or n < 0 then
		return 0
	end
	return math.min(n, bound)
end

-- ONE FIELD of a record (or an arm) whose pieces sit at base + l.p[i], read
-- from v the way table_cookread.moo lands it. A list's slot and the elements
-- behind it are the extent carve's, not this walk's.
writeField = function(ctx: any, owner: string, fd: any, l: any, base: number, v: any): boolean
	local b = ctx.b
	local at = base + l.p[1]
	local s = fd.s
	local slot = fd.slot :: string
	if fd.present ~= nil then
		buffer.writeu8(b, base + l.p[#l.p], if v[fd.present] == true then 1 else 0)
	end
	if s == "str" and fd.wide then
		local src = v[slot]
		local n = usedLength(v, slot, fd.bound)
		if n > 0 and type(src) ~= "buffer" then
			return fail(ctx, "invalid_value", owner, fd)
		end
		if ctx.big then
			for i = 0, n - 1 do
				local u = buffer.readu16(src, i * 2)
				buffer.writeu8(b, at + i * 2, u % 256)
				buffer.writeu8(b, at + i * 2 + 1, u // 256)
			end
		elseif n > 0 then
			buffer.copy(b, at, src, 0, n * 2)
		end
		putUN(ctx, base + l.p[2], 4, n)
	elseif s == "str" then
		local src = v[slot]
		if type(src) ~= "string" or #src > fd.bound then
			return fail(ctx, "invalid_value", owner, fd)
		end
		buffer.writestring(b, at, src, #src)
		putUN(ctx, base + l.p[2], 4, #src)
	elseif s == "bytes" then
		local src = v[slot]
		local n = usedLength(v, slot, fd.bound)
		if n > 0 and type(src) ~= "buffer" then
			return fail(ctx, "invalid_value", owner, fd)
		end
		if n > 0 then
			buffer.copy(b, at, src, 0, n)
		end
		putUN(ctx, base + l.p[2], 4, n)
	elseif s == "text" then
		-- an unbounded text is a blob node no walk numbers (§2.13): only null rides
		if v[slot] ~= nil then
			return fail(ctx, "unnumbered_reference", owner, fd)
		end
		putNum64(ctx, at, 0)
	elseif s == "sblob" or s == "bblob" or s == "ptr" then
		return writeRef(ctx, owner, fd, at, v[slot])
	elseif s == "tab" then
		local d = S.lookup(fd.t)
		return writeRecord(ctx, d, at, v[slot] or d.new())
	elseif s == "union" then
		return writeUnion(ctx, fd.t, at, v[slot])
	elseif s == "map" or isList(fd) then
		return true
	elseif s == "arr" then
		local n = fd.bound
		if fd.array == "counted" then
			local count = v[slot .. "Count"]
			if type(count) ~= "number" or count < 0 or count > n or count ~= math.floor(count) then
				return fail(ctx, "invalid_value", owner, fd)
			end
			putUN(ctx, base + l.p[2], 4, count)
		end
		local dst = v[slot]
		if n > 0 and dst == nil then
			return fail(ctx, "invalid_value", owner, fd)
		end
		local bytes = type(dst) == "buffer"
		for i = 1, n do
			local x = if bytes then buffer.readu8(dst, i - 1) else dst[i]
			if not writeElement(ctx, owner, fd, fd.e, at + (i - 1) * l.elem, l.elem, x) then
				return false
			end
		end
	else
		return writeScalar(ctx, owner, fd, fd, at, l.w, v[slot])
	end
	return true
end

-- ONE RECORD (§7.2): every field at its offset, from the value's storage
writeRecord = function(ctx: any, d: any, base: number, v: any): boolean
	if type(v) ~= "table" then
		return fail(ctx, "invalid_value", d.name, nil)
	end
	local layout = C.layout(d.name)
	for i, fd in d.fields do
		if not writeField(ctx, d.name, fd, layout.fields[i], base, v) then
			return false
		end
	end
	return true
end

-- A UNION is a TAG beside its arms (§2.6, §7.2): the tag, and the set arm's
-- bytes at the arms' shared offset; None, and a payload-free arm, are the tag
-- alone
writeUnion = function(ctx: any, name: string, at: number, u: any): boolean
	local d = S.lookup(name)
	local layout = C.unionLayout(name)
	local tag = if type(u) == "table" then u.Type else 0
	if type(tag) ~= "number" or tag < 0 or tag > #d.arms or tag ~= math.floor(tag) then
		return fail(ctx, "invalid_value", name, nil)
	end
	putUN(ctx, at, layout.tag, tag)
	if tag == 0 then
		return true
	end
	local arm = d.arms[tag]
	if arm.s == "void" then
		return true
	end
	return writeField(ctx, name, arm, layout.arms[tag], at + layout.armOffset, u)
end

-- ---- the extent carve (§2.9, §7.6): one walk, measuring and writing ----

local carveRecord: any
local carveField: any

-- one by-value element or nested value: the lists below it
local function carveElement(c: any, owner: string, e: any, at: number, x: any): boolean
	if e.s == "tab" then
		local d = S.lookup(e.t)
		return carveRecord(c, at, d, x or d.new())
	elseif e.s == "union" then
		local tag = if type(x) == "table" then x.Type else 0
		local d = S.lookup(e.t)
		if type(tag) ~= "number" or tag == 0 or tag > #d.arms then
			return true
		end
		local arm = d.arms[tag]
		if arm.s == "void" then
			return true
		end
		local layout = C.unionLayout(e.t)
		return carveField(c, e.t, arm, layout.arms[tag], at + layout.armOffset, x)
	end
	return true
end

-- one unbounded array: its bytes first, at the element's alignment, the
-- slot's sixteen bytes, then element by element in index order the arrays of
-- any list an element holds by value
local function carveList(c: any, owner: string, fd: any, l: any, base: number, v: any): boolean
	local ctx = c.ctx
	local at = base + l.p[1]
	local val = v[fd.slot]
	local count = 0
	if type(val) == "buffer" then
		count = buffer.len(val)
	elseif type(val) == "table" then
		count = #val
	end
	local size = l.elem
	c.at = align_up(c.at, l.ealign)
	local start = c.at
	c.at += count * size
	if c.write then
		local array = c.extentAt + start
		-- an empty list's reference is null in every encoding (§2.9)
		putNum64(ctx, at, if count > 0 then array - at else 0)
		putUN(ctx, at + 8, 4, count)
		local bytes = type(val) == "buffer"
		for i = 1, count do
			local x = if bytes then buffer.readu8(val, i - 1) else val[i]
			if not writeElement(ctx, owner, fd, fd.e, array + (i - 1) * size, size, x) then
				return false
			end
		end
	end
	if fieldExtent(fd) then
		for i = 1, count do
			if not carveElement(c, owner, fd.e, c.extentAt + start + (i - 1) * size, val[i]) then
				return false
			end
		end
	end
	return true
end

carveField = function(c: any, owner: string, fd: any, l: any, base: number, v: any): boolean
	if fd.s == "map" then
		return fail(c.ctx, "map_in_cook", owner, fd)
	end
	if isList(fd) then
		return carveList(c, owner, fd, l, base, v)
	end
	if not fieldExtent(fd) then
		return true
	end
	local at = base + l.p[1]
	local val = v[fd.slot]
	if fd.s ~= "arr" then
		return carveElement(c, owner, fd, at, val)
	end
	local n = fd.bound
	local live = n
	if fd.array == "counted" then
		live = math.min(v[fd.slot .. "Count"] or 0, n)
	end
	for i = 1, live do
		if not carveElement(c, owner, fd.e, at + (i - 1) * l.elem, val[i]) then
			return false
		end
	end
	-- AN UNREACHED NON-EMPTY EXTENT IS REFUSED (§7.6): a counted array's slots
	-- past its live count are storage the walk does not reach, so a list in
	-- one names bytes the region will not hold
	for i = live + 1, n do
		local probe = { ctx = c.ctx, write = false, extentAt = 0, at = 0 }
		if not carveElement(probe, owner, fd.e, 0, val[i]) then
			return false
		end
		if probe.at ~= 0 then
			return fail(c.ctx, "invalid_value", owner, fd)
		end
	end
	return true
end

carveRecord = function(c: any, at: number, d: any, v: any): boolean
	if v == nil then
		return true
	end
	local layout = C.layout(d.name)
	for i, fd in d.fields do
		if not carveField(c, d.name, fd, layout.fields[i], at, v) then
			return false
		end
	end
	return true
end

-- ---- the layout and the write ----

-- the numbering placed into a region: each node's offset, size and the
-- region's alignment, or nil and the refusal
local function layoutOf(root: string, value: any): (any, any)
	local d = S.lookup(root)
	if d.table and d.forms ~= nil and d.forms.cook ~= true then
		error("schema: table " .. root .. " does not carry cook (forms declaration)", 2)
	end
	if type(value) ~= "table" then
		return nil, { name = "nil_value", field = root }
	end
	local nodes, why = Save.number(d, value, true)
	if nodes == nil then
		return nil, why
	end
	local ctx: any = { b = nil, big = false, why = nil, offset = {} }
	local entries: { any } = { { d = d, v = value } }
	for _, n in nodes.list do
		entries[#entries + 1] = if n.blob ~= nil then { blob = n.blob, str = n.str, typeId = n.typeId } else { d = n.d, v = n.v }
	end
	local align = REGION_ALIGN_FLOOR
	local offset = 0
	for _, n in entries do
		if n.blob ~= nil then
			offset = align_up(offset, 8)
			n.off = offset
			n.size = 8 + buffer.len(n.blob) + (if n.str then 1 else 0)
		else
			local layout = C.layout(n.d.name)
			align = math.max(align, layout.align)
			n.extent = 0
			n.record = layout.size
			if hasExtentRecord(n.d) then
				n.record = align_up(layout.size, REGION_ALIGN_FLOOR)
				local c = { ctx = ctx, write = false, extentAt = 0, at = 0 }
				if not carveRecord(c, 0, n.d, n.v) then
					return nil, ctx.why
				end
				n.extent = c.at
			end
			offset = align_up(offset, layout.align)
			n.off = offset
			n.size = n.record + n.extent
		end
		offset += n.size
	end
	local bytes = align_up(offset, align)
	local dataOffset = align_up(HEADER_BYTES, align)
	return { d = d, entries = entries, align = align, bytes = bytes, dataOffset = dataOffset, total = dataOffset + bytes + #entries * DIRECTORY_ENTRY }, nil
end

-- measure(root, value): the whole file's length — the header, the data part
-- and the attribution part — or nil and the refusal { name, field } (§7.6)
function W.measure(root: string, value: any): (number?, any)
	local plan, why = layoutOf(root, value)
	if plan == nil then
		return nil, why
	end
	return plan.total, nil
end

-- cook(root, value, big): the cooked file of the value as a buffer of exactly
-- its bytes, in the target's byte order — little unless big is true — or nil
-- and the refusal { name, field }: a non-finite float, a data cycle, a
-- reference the numbering does not carry, a value outside its contract
function W.cook(root: string, value: any, big: boolean?): (buffer?, any)
	local plan, why = layoutOf(root, value)
	if plan == nil then
		return nil, why
	end
	local b = buffer.create(plan.total)
	local ctx: any = { b = b, big = big == true, why = nil, offset = {} }
	local region = plan.dataOffset
	for _, n in plan.entries do
		ctx.offset[if n.blob ~= nil then n.blob else n.v] = region + n.off
	end
	local version = C.version()
	putU64(ctx, 0, MAGIC_LO, MAGIC_HI)
	putU64(ctx, 8, version[1], version[2])
	putU64(ctx, 16, if ctx.big then BYTE_ORDER_BIG else BYTE_ORDER_LITTLE, 0)
	putNum64(ctx, 24, plan.bytes)
	putNum64(ctx, 32, #plan.entries * DIRECTORY_ENTRY)
	putNum64(ctx, 40, plan.align)
	local dir = region + plan.bytes
	for i, n in plan.entries do
		local at = region + n.off
		local id: string
		if n.blob ~= nil then
			putUN(ctx, at, 4, buffer.len(n.blob))
			buffer.copy(b, at + 8, n.blob, 0, buffer.len(n.blob))
			id = n.typeId
		else
			if not writeRecord(ctx, n.d, at, n.v) then
				return nil, ctx.why
			end
			if hasExtentRecord(n.d) then
				local c = { ctx = ctx, write = true, extentAt = at + n.record, at = 0 }
				if not carveRecord(c, at, n.d, n.v) then
					return nil, ctx.why
				end
				if c.at ~= n.extent then
					return nil, { name = "invalid_value", field = n.d.name }
				end
			end
			id = n.d.id
		end
		local entry = dir + (i - 1) * DIRECTORY_ENTRY
		local raw = buffer.fromstring(id)
		putNum64(ctx, entry, n.off)
		putU64(ctx, entry + 8, buffer.readu32(raw, 0), buffer.readu32(raw, 4))
	end
	return b, nil
end

return table.freeze(W)
