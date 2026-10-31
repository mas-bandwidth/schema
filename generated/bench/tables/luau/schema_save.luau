--!strict
-- schema_save.luau — the Luau table runtime's VARIABLE FORM (form 1) SAVE and its
-- MEASURE (docs/SPEC-TABLES.md §3, §3.1, §6.3; docs/PORT-LUAU.md, THE CARD
-- MAP). A port of the Go reference engine (internal/tablewire encode.go,
-- nodes.go, nodetable.go) and of the JavaScript leg's THE SAVE and ONE FIELD
-- (internal/codegen/jstable/variableruntime.go), over the Luau values of
-- docs/PORT-LUAU.md's THE TYPE MAPPING. Emitted beside the unit's
-- <Base>Table.luau modules by internal/codegen/luautable, which embeds
-- runtime/*.luau by glob.
--
-- The save is DRIVEN BY THE DESCRIPTOR (docs/SPEC-TABLES.md §8.1): the form
-- byte, the root's fields in declared order with each elided at its declared
-- default, the node table LAST in the root body for the pointers in
-- first-visit order, the terminator, then the id table in first-use order and
-- its count. MEASURE is the same walk over a writer that counts and stores
-- nothing, so measure and save cannot be two rules.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local St = require("./schema_tables")

local S = {}

-- THE FORM BYTE AND THE RESERVED IDS (docs/SPEC-TABLES.md §3, §3.1, §5)
local Form = 1
local NodeTableId: integer = -1i                  -- 0xffffffffffffffff
local StringBlobId: integer = 0x704be0d8faaffc58i -- FNV-1a 64 of "string"
local BytesBlobId: integer = 0x2f2ec0474f1c4fe4i  -- FNV-1a 64 of "bytes"
local MapKeyId: integer = 0x3dc94a19365b10eci     -- FNV-1a 64 of "key"

-- ---- THE WRITER (docs/SPEC-TABLES.md §3): little-endian, LEB128 lengths --
-- A measuring writer (count = true) stores nothing and only counts.

local function new_writer(count: boolean): any
	if count then return { b = nil, n = 0, count = true } end
	return { b = buffer.create(64), n = 0, count = false }
end

local function reserve(w: any, k: number)
	local need = w.n + k
	local cap = buffer.len(w.b)
	if need <= cap then return end
	while cap < need do cap = cap * 2 end
	local nb = buffer.create(cap)
	buffer.copy(nb, 0, w.b, 0, w.n)
	w.b = nb
end

local function w_u8(w: any, v: number)
	if not w.count then reserve(w, 1); buffer.writeu8(w.b, w.n, v) end
	w.n = w.n + 1
end

-- a 1, 2 or 4 byte integer at its width, two's complement (§3)
local function w_small(w: any, width: number, signed: boolean, v: number)
	if not w.count then
		reserve(w, width)
		if width == 1 then
			if signed then buffer.writei8(w.b, w.n, v) else buffer.writeu8(w.b, w.n, v) end
		elseif width == 2 then
			if signed then buffer.writei16(w.b, w.n, v) else buffer.writeu16(w.b, w.n, v) end
		else
			if signed then buffer.writei32(w.b, w.n, v) else buffer.writeu32(w.b, w.n, v) end
		end
	end
	w.n = w.n + width
end

-- a 64-bit value is the integer's own bit pattern (docs/PORT-LUAU.md, 64-BIT)
local function w_int(w: any, v: integer)
	if not w.count then reserve(w, 8); buffer.writeinteger(w.b, w.n, v) end
	w.n = w.n + 8
end

local function w_f64(w: any, v: number)
	if not w.count then reserve(w, 8); buffer.writef64(w.b, w.n, v) end
	w.n = w.n + 8
end

-- THE ONE CANONICAL SPELLING of a length, count, index or reference (§3)
local function w_leb(w: any, v: number)
	while v >= 128 do
		w_u8(w, (v % 128) + 128)
		v = math.floor(v / 128)
	end
	w_u8(w, v)
end

local function w_raw(w: any, src: buffer, at: number, n: number)
	if not w.count then reserve(w, n); buffer.copy(w.b, w.n, src, at, n) end
	w.n = w.n + n
end

-- a nested writer's bytes, framed by the caller
local function w_sub(w: any, sub: any)
	if not w.count then reserve(w, sub.n); buffer.copy(w.b, w.n, sub.b, 0, sub.n) end
	w.n = w.n + sub.n
end

local function leb_len(v: number): number
	local n = 1
	while v >= 128 do n = n + 1; v = math.floor(v / 128) end
	return n
end

-- ---- THE ID TABLE (§3): ids interned in first-use order, with a mark and a
-- rollback so an elided field costs the id table nothing either

local function new_ids(): any
	return { list = {}, index = {} }
end

local function ref(ids: any, id: integer): number
	local r = ids.index[id]
	if r ~= nil then return r end
	ids.list[#ids.list + 1] = id
	ids.index[id] = #ids.list
	return #ids.list
end

local function mark(ids: any): number
	return #ids.list
end

local function rollback(ids: any, m: number)
	while #ids.list > m do
		local id = ids.list[#ids.list]
		ids.list[#ids.list] = nil
		ids.index[id] = nil
	end
end

-- ---- A FLOAT IS FINITE, AND RIDES AS ITS BITS (§3, §3.3) ----------------

local scratch = buffer.create(8)

-- a float32 field's bits: the double narrowed as Go's float32(v) narrows it
-- (docs/PORT-LUAU.md, BYTE BUFFERS). Every finite float32 is exact in a
-- double, so no bit surgery is owed (docs/SPEC-TABLES.md §3.3).
local function narrow_f32(v: number): number
	buffer.writef32(scratch, 0, v)
	return buffer.readu32(scratch, 0)
end

-- A NaN OR AN INFINITY IS REFUSED BY EVERY WRITER (docs/SPEC-TABLES.md §3.3;
-- the same rule as packets): the save and the measure answer
-- nil and the refusal non_finite_float naming <Owner>.<field>. A float32 field
-- tests its NARROWED bits, so a finite double past the float32 range, which
-- narrows to an infinity, is refused too and never written.
local function refuse_non_finite(c: any, e: any): boolean
	if c.why == nil then
		c.why = { name = "non_finite_float", field = (c.owner or "?") .. "." .. (e.name or c.field or "?") }
	end
	return false
end

-- whether two floats are the SAME BIT PATTERN, which is what a plain float's
-- elision compares (docs/SPEC-TABLES.md §3.3, "A PLAIN FLOAT'S SPECIAL
-- VALUES"): -0 and +0 differ.
local function same_float(a: any, b: any, f32: boolean): boolean
	if typeof(a) == "integer" then a = integer.tonumber(a) end
	if typeof(b) == "integer" then b = integer.tonumber(b) end
	if f32 then return narrow_f32(a) == narrow_f32(b) end
	buffer.writef64(scratch, 0, a)
	local alo, ahi = buffer.readu32(scratch, 0), buffer.readu32(scratch, 4)
	buffer.writef64(scratch, 0, b)
	local blo, bhi = buffer.readu32(scratch, 0), buffer.readu32(scratch, 4)
	return alo == blo and ahi == bhi
end

-- ---- VALUES (docs/PORT-LUAU.md, THE TYPE MAPPING) -------------------------

-- a number or an integer as the other domain, so a default spelled in one
-- compares with a value held in the other
local function as_number(v: any): any
	if typeof(v) == "integer" then return integer.tonumber(v) end
	return v
end

local function same_int(a: any, b: any): boolean
	if typeof(a) == "table" or typeof(b) == "table" then
		local alo = typeof(a) == "table" and a.lo or 0i
		local ahi = typeof(a) == "table" and a.hi or 0i
		local blo = typeof(b) == "table" and b.lo or 0i
		local bhi = typeof(b) == "table" and b.hi or 0i
		return alo == blo and ahi == bhi
	end
	if typeof(a) == typeof(b) then return a == b end
	return as_number(a) == as_number(b)
end

-- the used bytes of a string(N) or bytes(N) value: its buffer and the length
-- beside it, in bytes
local function used(v: any, slot: string): (buffer?, number)
	local b = v[slot]
	if typeof(b) ~= "buffer" then return nil, 0 end
	local n = v[slot .. "Length"]
	if n == nil then n = buffer.len(b) end
	return b, n
end

local function equals_string(b: buffer?, n: number, def: string?): boolean
	local d = def or ""
	if b == nil then return #d == 0 end
	if n ~= #d then return false end
	return buffer.readstring(b, 0, n) == d
end

-- the live count of an array: a list's length, a counted array's count
-- beside it, a fixed array's bound (§2.9)
local function live_count(fd: any, holder: any, val: any): number
	if fd.array == "list" then
		if typeof(val) == "buffer" then return buffer.len(val) end
		-- a list of pointers carries its live length as n, and a null in it
		-- is false (docs/PORT-LUAU.md, THE TYPE MAPPING, Tables)
		if typeof(val.n) == "number" then return val.n end
		return #val
	end
	if fd.array == "counted" then
		local c = holder[fd.slot .. "Count"]
		if c == nil then c = 0 end
		return math.min(c, fd.bound)
	end
	return fd.bound
end

local function elem_at(val: any, i: number): any
	if typeof(val) == "buffer" then return buffer.readu8(val, i - 1) end
	return val[i]
end

-- the bytes one text-list element contributes to its total
-- (docs/SPEC-TABLES.md §2.13): a plain element's buffer length, or a ?element's value
-- when present, zero when absent.
local function text_elem_used(e: any, x: any): number
	if e == nil or x == nil then return 0 end
	if e.s == "str" or e.s == "text" then
		if typeof(x) == "buffer" then return buffer.len(x) end
		return 0
	end
	if e.s == "tab" then
		local vf = St.lookup(e.t).fields[1]
		if vf == nil then return 0 end
		if vf.present ~= nil and not x[vf.present] then return 0 end
		local b = x[vf.slot]
		if typeof(b) == "buffer" then
			local n = x[vf.slot .. "Length"]
			if n == nil then n = buffer.len(b) end
			return n
		end
	end
	return 0
end

-- ---- MAP ORDER (§2.8; internal/tabletext MapKeyOrder): ascending by key, a
-- string key by its bytes, a signed integer key signed, every other unsigned

local function bytes_less(a: buffer, an: number, b: buffer, bn: number): boolean
	local n = math.min(an, bn)
	for i = 0, n - 1 do
		local x = buffer.readu8(a, i)
		local y = buffer.readu8(b, i)
		if x ~= y then return x < y end
	end
	return an < bn
end

local function key_less(keyfd: any, a: any, b: any): boolean
	local ka = a[keyfd.slot]
	local kb = b[keyfd.slot]
	if typeof(ka) == "buffer" then
		local _, an = used(a, keyfd.slot, false)
		local _, bn = used(b, keyfd.slot, false)
		return bytes_less(ka, an, kb, bn)
	end
	if typeof(ka) == "table" then
		if ka.hi ~= kb.hi then
			if keyfd.signed then return integer.lt(ka.hi, kb.hi) end
			return integer.ult(ka.hi, kb.hi)
		end
		return integer.ult(ka.lo, kb.lo)
	end
	if typeof(ka) == "integer" then
		if keyfd.signed then return integer.lt(ka, kb) end
		return integer.ult(ka, kb)
	end
	if typeof(ka) == "boolean" then return (not ka) and kb end
	return ka < kb
end

local function map_order(fd: any, entries: { any }): { any }
	local entry = St.lookup(fd.t)
	local keyfd: any = nil
	for _, f in entry.fields do
		if f.id == MapKeyId then keyfd = f end
	end
	local order = table.clone(entries)
	if keyfd ~= nil then
		table.sort(order, function(a: any, b: any): boolean return key_less(keyfd, a, b) end)
	end
	return order
end

-- ---- THE NUMBERING (docs/SPEC-TABLES.md §3.1; internal/tablewire nodes.go) -
--
-- The first-visit order of a depth-first pre-order walk from the root over
-- POINTER EDGES, descending through every by-value edge to reach them. Node
-- index 1 is the root and record k is index k + 1. A reference to a node whose
-- descent is still open is a DATA CYCLE, and the save refuses it. A field the
-- writer does not write is not an edge: an absent optional, a slot past a
-- counted array's live count, an arm that is not set.

local edges: any

local function arm_edges(fd: any, u: any, out: { any })
	if u == nil or u.Type == nil or u.Type == 0 then return end
	local un = St.lookup(fd.t)
	local arm = un.arms[u.Type]
	if arm == nil or arm.s == "void" then return end
	local val = u[arm.slot]
	if arm.s == "tab" then
		edges(St.lookup(arm.t), val, out)
	elseif arm.s == "sblob" or arm.s == "bblob" then
		if val ~= nil then out[#out + 1] = { blob = arm.s, val = val, field = un.name .. "." .. arm.name } end
	elseif arm.s == "ptr" then
		if val ~= nil then out[#out + 1] = { d = St.lookup(arm.t), val = val } end
	elseif arm.s == "arr" and arm.e ~= nil and arm.e.s == "ptr" then
		local d = St.lookup(arm.e.t)
		for i = 1, live_count(arm, u, val) do
			if val[i] then out[#out + 1] = { d = d, val = val[i] } end
		end
	elseif arm.s == "union" then
		arm_edges(arm, val, out)
	end
end

-- a field's guard, a conjunction of (bool slot, required value) pairs on the
-- descriptor, holds when every pair matches the instance's storage; a field
-- with no guard always holds. A field under a FALSE GUARD is elided too,
-- whatever its storage holds: an if branch that does not run writes none of
-- its fields (docs/SPEC-TABLES.md §3), and a false guard's edges are not
-- visited either (§3.1), so save and measure agree byte for byte.
local function guard_holds(fd: any, v: any): boolean
	if fd.guard == nil then return true end
	for _, pair in fd.guard do
		if v[pair[1]] ~= pair[2] then return false end
	end
	return true
end

edges = function(d: any, v: any, out: { any }): { any }
	for _, fd in d.fields do
		if not guard_holds(fd, v) then continue end
		if fd.present ~= nil and not v[fd.present] then continue end
		local val = v[fd.slot]
		local s = fd.s
		if s == "sblob" or s == "bblob" then
			if val ~= nil then out[#out + 1] = { blob = s, val = val, field = d.name .. "." .. fd.name } end
		elseif s == "ptr" then
			if val ~= nil then out[#out + 1] = { d = St.lookup(fd.t), val = val } end
		elseif s == "union" then
			arm_edges(fd, val, out)
		elseif s == "map" then
			local entry = St.lookup(fd.t)
			for _, ev in map_order(fd, val) do edges(entry, ev, out) end
		elseif s == "tab" then
			edges(St.lookup(fd.t), val, out)
		elseif s == "arr" then
			local e = fd.e
			if e.s == "ptr" then
				local pd = St.lookup(e.t)
				for i = 1, live_count(fd, v, val) do
					if val[i] then out[#out + 1] = { d = pd, val = val[i] } end
				end
			elseif e.s == "union" then
				for i = 1, live_count(fd, v, val) do arm_edges(e, val[i], out) end
			elseif e.s == "tab" then
				local td = St.lookup(e.t)
				for i = 1, live_count(fd, v, val) do
					if val[i] ~= nil then edges(td, val[i], out) end
				end
			end
		end
	end
	return out
end

-- THE WALK HOLDS ITS OWN STACK: a pointer chain is as deep as it is long, and
-- the interpreter's call stack is not the bound. Only one node's by-value
-- closure recurses, and that nesting is the schema's, never the data's.
local function number(d: any, root: any): (any, any?)
	local nodes: any = { list = {}, index = {} }
	nodes.index[root] = 1
	local open: { [any]: boolean } = { [root] = true }
	local stack = { { v = root, edges = edges(d, root, {}), i = 0 } }
	while #stack > 0 do
		local top = stack[#stack]
		if top.i >= #top.edges then
			open[top.v] = nil
			stack[#stack] = nil
		else
			top.i = top.i + 1
			local e = top.edges[top.i]
			if open[e.val] then return nil end
			-- A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): the first
			-- visit numbers it, and a second slot naming the same buffer is
			-- refused by name, blob_shared, as every writer refuses it.
			if e.blob ~= nil and nodes.index[e.val] ~= nil then
				return nil, { name = "blob_shared", field = e.field }
			end
			if nodes.index[e.val] == nil then
				if e.blob ~= nil then
					-- a BYTE BUFFER is numbered as every node is and reaches nothing (§2.5)
					nodes.list[#nodes.list + 1] = { typeId = e.blob == "sblob" and StringBlobId or BytesBlobId, blob = e.val }
					nodes.index[e.val] = #nodes.list + 1
				else
					nodes.list[#nodes.list + 1] = { typeId = e.d.id, d = e.d, v = e.val }
					nodes.index[e.val] = #nodes.list + 1
					open[e.val] = true
					stack[#stack + 1] = { v = e.val, edges = edges(e.d, e.val, {}), i = 0 }
				end
			end
		end
	end
	return nodes
end

-- ---- THE SAVE WALK (docs/SPEC-TABLES.md §3; internal/tablewire encode.go) --

local write_fields: any
local write_value: any
local write_arm_header: any

-- a field header is the id REFERENCE and the kind byte, the whole of it (§3)
local function header(c: any, w: any, id: integer, kind: number)
	w_leb(w, ref(c.ids, id))
	w_u8(w, kind)
end

-- an enum value's reference (§2.15): retired values refuse; the variant name's id, 0 for None, and -1 for a
-- value no variant names, which the save refuses (§5)
local function enum_ref(c: any, e: any, val: any): number
	local n = as_number(val)
	if n == nil or n == 0 then return 0 end
	local pair = e.vs[n]
	if pair == nil or pair[3] == true then return -1 end
	return ref(c.ids, pair[2])
end

-- one integer-shaped value at its kind's width (§2.11, §3): a 128-bit value is
-- its two halves low first, a 64-bit one (int64, uint64, bits(33..64), fixed
-- with I+F > 32, or timestamp kind 36) the integer's 8 little-endian bytes, a
-- narrower one its low bytes
local function write_int(w: any, e: any, val: any)
	local kw = e.kw
	if kw == 16 then
		w_int(w, val.lo)
		w_int(w, val.hi)
	elseif kw == 8 then
		if typeof(val) ~= "integer" then val = integer.create(val) end
		w_int(w, val)
	else
		w_small(w, kw, e.signed == true, as_number(val))
	end
end

-- one body: its fields in declaration order, then the one-byte zero reference
-- that terminates it (docs/SPEC-TABLES.md §3)
local function write_body(c: any, d: any, w: any, v: any): boolean
	if not write_fields(c, d, w, v) then return false end
	w_u8(w, 0)
	return true
end

-- a nested body built before the length that frames it (§3); a measuring
-- save builds a measuring body
local function body_of(c: any, d: any, v: any): any
	local b = new_writer(c.count)
	if not write_body(c, d, b, v) then return nil end
	return b
end

-- one element at its element kind's own framing (§2.9, §3): a table element
-- carries its own length, a pointer its node index (0 is null), an enum its
-- variant reference, a text element its own L, a scalar its width. lens is
-- the Length array beside an array of string(N).
write_value = function(c: any, e: any, w: any, val: any, len: number?): boolean
	local s = e.s
	if s == "bool" then
		w_u8(w, val and 1 or 0)
	elseif s == "int" then
		write_int(w, e, val)
	elseif s == "float" then
		if e.f32 then
			local bits = narrow_f32(val)
			if bit32.band(bits, 0x7F800000) == 0x7F800000 then return refuse_non_finite(c, e) end
			w_small(w, 4, false, bits)
		else
			if val ~= val or val == math.huge or val == -math.huge then return refuse_non_finite(c, e) end
			w_f64(w, val)
		end
	elseif s == "enum" then
		local vref = enum_ref(c, e, val)
		if vref < 0 then return false end
		w_leb(w, vref)
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		w_leb(w, not val and 0 or c.nodes.index[val])
	elseif s == "tab" then
		local b = body_of(c, St.lookup(e.t), val)
		if b == nil then return false end
		w_leb(w, b.n)
		w_sub(w, b)
	elseif s == "union" then
		if val == nil or val.Type == 0 then
			w_leb(w, 0) -- a None element is the single zero byte: position is identity
			return true
		end
		return write_arm_header(c, St.lookup(e.t), w, val)
	elseif s == "str" or s == "text" then
		local n = 0
		if typeof(val) == "buffer" then n = len or buffer.len(val) end
		w_leb(w, n)
		if n > 0 then w_raw(w, val, 0, n) end
	else
		return false
	end
	return true
end

-- a positional array body under kind 14: the element kind, the count, then
-- the elements (docs/SPEC-TABLES.md §2.9, §3)
local function write_array(c: any, fd: any, w: any, holder: any, val: any, count: number): boolean
	header(c, w, fd.id, 14)
	local b = new_writer(c.count)
	w_u8(b, fd.ek)
	w_leb(b, count)
	local lens = holder[fd.slot .. "Length"]
	for i = 1, count do
		local len: number? = nil
		if typeof(lens) == "table" then len = lens[i] end
		if not write_value(c, fd.e, b, elem_at(val, i), len) then return false end
	end
	w_leb(w, b.n)
	w_sub(w, b)
	return true
end

-- the element's default, the value a reader prefills (§3, §5): zero, None,
-- null, empty
local function elem_is_default(e: any, val: any, len: number?): boolean
	local s = e.s
	if s == "bool" then return val == false or val == nil end
	if s == "int" then return same_int(val, 0) end
	if s == "float" then return same_float(val, 0, e.f32) end
	if s == "enum" then return as_number(val) == 0 end
	if s == "ptr" or s == "sblob" or s == "bblob" then return not val end
	if s == "union" then return val == nil or val.Type == 0 end
	if s == "str" or s == "text" then
		if typeof(val) ~= "buffer" then return true end
		return (len or buffer.len(val)) == 0
	end
	return false
end

-- AN ENUM-KEYED ARRAY (docs/SPEC-TABLES.md §3.2): kind 16, then one (key
-- reference, L, element) triple per slot that is not at its default, in
-- variant order. An array with no such slot is not written at all.
local function write_keyed(c: any, fd: any, w: any, val: any): boolean
	local m = mark(c.ids)
	local fref = ref(c.ids, fd.id)
	local body = new_writer(c.count)
	local pairs = 0
	local e = fd.e
	for slot = 1, fd.bound do
		local keyvs = fd.keyvs
		local x = val[slot]
		if keyvs == nil or keyvs[slot] == nil then
			-- a descriptor that names no key ids (luautable.go does not emit
			-- them yet): a slot at its default needs none and elides; any other
			-- has no wire identity, and the save refuses it (§3.2, §5)
			local sm = mark(c.ids)
			local blank: boolean
			if e.s == "tab" then
				local eb = body_of(c, St.lookup(e.t), x)
				rollback(c.ids, sm)
				if eb == nil then return false end
				blank = eb.n <= 1
			else
				blank = elem_is_default(e, x, nil)
			end
			if not blank then return false end
			continue
		end
		local sm = mark(c.ids)
		local kref = ref(c.ids, keyvs[slot][2])
		local eb: any
		if e.s == "tab" then
			eb = body_of(c, St.lookup(e.t), x)
			if eb == nil then return false end
			if eb.n <= 1 then rollback(c.ids, sm); continue end
		else
			if elem_is_default(e, x, nil) then rollback(c.ids, sm); continue end
			eb = new_writer(c.count)
			if not write_value(c, e, eb, x, nil) then return false end
		end
		w_leb(body, kref)
		w_leb(body, eb.n)
		w_sub(body, eb)
		pairs = pairs + 1
	end
	if pairs == 0 then
		rollback(c.ids, m)
		return true
	end
	w_leb(w, fref)
	w_u8(w, 16)
	local inner = new_writer(c.count)
	w_u8(inner, fd.ek)
	w_leb(inner, pairs)
	w_sub(inner, body)
	w_leb(w, inner.n)
	w_sub(w, inner)
	return true
end

-- A MAP (§2.8): kind 14 over element kind 13, its entries in ASCENDING KEY
-- ORDER, each entry's body always riding (identity is the key). An empty map
-- elides, EXCEPT when `required` or a PRESENT optional (both passed as true)
-- forces it: then the header and the two-byte empty array body `0d 00` ride
-- under L = 2 (§2.3, §2.8).
local function write_map(c: any, fd: any, w: any, val: any, required: boolean?): boolean
	if (val == nil or #val == 0) and not required then return true end
	header(c, w, fd.id, 14)
	local entry = St.lookup(fd.t)
	local b = new_writer(c.count)
	w_u8(b, 13)
	w_leb(b, val == nil and 0 or #val)
	for _, ev in map_order(fd, val or {}) do
		local eb = body_of(c, entry, ev)
		if eb == nil then return false end
		w_leb(b, eb.n)
		w_sub(b, eb)
	end
	w_leb(w, b.n)
	w_sub(w, b)
	return true
end

-- ONE ARM's payload, the bytes under its L (§3's arm payload table): a table
-- body, a node index, a string's bytes with no length of their own, the array
-- body for bytes(N) and an array, the union payload in place, or the value at
-- its width.
local function arm_payload(c: any, arm: any, w: any, u: any): boolean
	local s = arm.s
	local val = u[arm.slot]
	if s == "tab" then
		return write_body(c, St.lookup(arm.t), w, val)
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		w_leb(w, not val and 0 or c.nodes.index[val])
	elseif s == "str" then
		local b, n = used(u, arm.slot)
		if b ~= nil and n > 0 then w_raw(w, b, 0, n) end
	elseif s == "text" then
		if typeof(val) == "buffer" then w_raw(w, val, 0, buffer.len(val)) end
	elseif s == "bytes" then
		local b, n = used(u, arm.slot, false)
		w_u8(w, 6) -- bytes ride as an array of u8 (§2.5)
		w_leb(w, n)
		if b ~= nil and n > 0 then w_raw(w, b, 0, n) end
	elseif s == "arr" then
		local count = live_count(arm, u, val)
		w_u8(w, arm.ek)
		w_leb(w, count)
		local lens = u[arm.slot .. "Length"]
		for i = 1, count do
			local len: number? = nil
			if typeof(lens) == "table" then len = lens[i] end
			if not write_value(c, arm.e, w, elem_at(val, i), len) then return false end
		end
	elseif s == "union" then
		if val == nil or val.Type == 0 then
			w_leb(w, 0) -- the inner union's None: L = 1 and that one zero byte (§3)
		else
			return write_arm_header(c, St.lookup(arm.t), w, val)
		end
	else
		return write_value(c, arm, w, val, nil)
	end
	return true
end

-- AN ARM HEADER IS A FIELD HEADER (§3): the arm id reference, the arm's kind
-- byte (32 for a payload-free arm, §2.6), L, then L bytes of payload. A tag no
-- arm names has no wire identity, and the save refuses it (§5).
write_arm_header = function(c: any, un: any, w: any, u: any): boolean
	local arm = un.arms[u.Type]
	if arm == nil then return false end
	c.owner, c.field = un.name, arm.name
	w_leb(w, ref(c.ids, arm.id))
	if arm.s == "void" then
		w_u8(w, 32)
		w_leb(w, 0)
		return true
	end
	w_u8(w, arm.k)
	local b = new_writer(c.count)
	if not arm_payload(c, arm, b, u) then return false end
	w_leb(w, b.n)
	w_sub(w, b)
	return true
end

-- a scalar or an enum at its declared default (§3, internal/tablewire
-- cellIsDefault): a float32 compares as a float32, an integer in either domain
local function at_default(fd: any, val: any): boolean
	local s = fd.s
	if s == "bool" then return (val == true) == (fd.def == true) end
	if s == "float" then return same_float(val, fd.def, fd.f32) end
	if s == "int" then return same_int(val, fd.def) end
	return as_number(val) == as_number(fd.def)
end

-- ONE FIELD, elided at its declared default (§3): a pointer and a blob at
-- null, a scalar at its default, a string at its default bytes, an array when
-- empty (a fixed one when every element is at its default; a fixed array of
-- tables always rides), a map when empty, a union at None, a nested table when
-- its body is its terminator alone, the field's id rolled back with it. A
-- present optional always rides; an absent one never does (§2.3).
local function write_field(c: any, fd: any, w: any, v: any): boolean
	-- a field under a FALSE GUARD is elided too, whatever its storage holds:
	-- an if branch that does not run writes none of its fields
	-- (docs/SPEC-TABLES.md §3)
	if not guard_holds(fd, v) then return true end
	local s = fd.s
	local val = v[fd.slot]
	-- `| required` (docs/SPEC-TABLES.md §2.3): the writer ALWAYS writes the
	-- field when its guard holds — no default elision — so it rides on every
	-- record. A required integer whose declared range excludes its stored
	-- value has no wire value, and the save refuses (nil), as Go's writer does.
	local required = fd.required == true
	if required and s == "int" and fd.lo ~= nil and typeof(fd.lo) ~= "table" and typeof(fd.hi) ~= "table" then
		local x = as_number(val)
		local lo, hi = fd.lo, fd.hi
		if fd.fbits ~= nil and fd.fbits > 0 then lo, hi = lo * (2 ^ fd.fbits), hi * (2 ^ fd.fbits) end
		if x ~= nil and (x < lo or x > hi) then return false end
	end
	-- A TEXT LIST'S TOTAL (docs/SPEC-TABLES.md §2.13): a list whose element
	-- bytes sum past the total is refused, as a count past [..N] is.
	if fd.total ~= nil and s == "arr" then
		local live = live_count(fd, v, val)
		local spent = 0
		for i = 1, live do
			spent = spent + text_elem_used(fd.e, elem_at(val, i))
		end
		if spent > fd.total then return false end
	end

	if s == "ptr" or s == "sblob" or s == "bblob" then
		-- NULL IS INDEX 0 AND NULL IS ELIDED; a non-null pointer always rides (§3.1)
		if val == nil then
			if not required then return true end
			header(c, w, fd.id, 17)
			w_leb(w, 0)
			return true
		end
		header(c, w, fd.id, 17)
		w_leb(w, c.nodes.index[val])
		return true
	end
	if s == "map" then
		-- `?map[K]V`: absent writes nothing; present ALWAYS rides, even empty
		-- (docs/SPEC-TABLES.md §2.3, §2.8)
		if fd.present ~= nil then
			if not v[fd.present] then return true end
			return write_map(c, fd, w, val, true)
		end
		return write_map(c, fd, w, val, required)
	end
	if fd.present ~= nil then
		if not v[fd.present] then return true end
		if s == "arr" then
			return write_array(c, fd, w, v, val, live_count(fd, v, val))
		elseif s == "tab" then
			local fref = ref(c.ids, fd.id)
			local b = body_of(c, St.lookup(fd.t), val)
			if b == nil then return false end
			w_leb(w, fref)
			w_u8(w, 13)
			w_leb(w, b.n)
			w_sub(w, b)
			return true
		end
		header(c, w, fd.id, fd.k)
		if s == "str" then
			local b, n = used(v, fd.slot)
			return write_value(c, { s = "text" }, w, b, n)
		elseif s == "union" then
			if val == nil or val.Type == 0 then w_leb(w, 0); return true end
			return write_arm_header(c, St.lookup(fd.t), w, val)
		end
		return write_value(c, fd, w, val, nil)
	end
	if s == "arr" and fd.array == "keyed" then
		return write_keyed(c, fd, w, val)
	end
	if s == "str" then
		local b, n = used(v, fd.slot)
		if not required then
			if equals_string(b, n, fd.def) then
				return true
			end
		end
		header(c, w, fd.id, fd.k)
		w_leb(w, n)
		if b ~= nil and n > 0 then w_raw(w, b, 0, n) end
		return true
	end
	if s == "text" then
		if not required and (typeof(val) ~= "buffer" or buffer.len(val) == 0) then return true end
		header(c, w, fd.id, 12)
		if typeof(val) == "buffer" then
			w_leb(w, buffer.len(val))
			w_raw(w, val, 0, buffer.len(val))
		else
			w_leb(w, 0)
		end
		return true
	end
	if s == "bytes" then
		local b, n = used(v, fd.slot, false)
		if not required and equals_string(b, n, fd.def) then return true end
		-- L IS THE ARRAY BODY'S LENGTH (§3): the element kind, the count's own
		-- LEB128 bytes, then the bytes
		header(c, w, fd.id, 14)
		w_leb(w, 1 + leb_len(n) + n)
		w_u8(w, 6)
		w_leb(w, n)
		if b ~= nil and n > 0 then w_raw(w, b, 0, n) end
		return true
	end
	if s == "arr" then
		if fd.array == "list" or fd.array == "counted" then
			local n = live_count(fd, v, val)
			if n == 0 and not required then return true end
			return write_array(c, fd, w, v, val, n)
		end
		if fd.e.s == "tab" then
			-- a fixed array of tables always rides: position is identity there
			return write_array(c, fd, w, v, val, fd.bound)
		end
		local lens = v[fd.slot .. "Length"]
		local all = true
		for i = 1, fd.bound do
			local len: number? = nil
			if typeof(lens) == "table" then len = lens[i] end
			if not elem_is_default(fd.e, elem_at(val, i), len) then all = false; break end
		end
		if all and not required then return true end
		return write_array(c, fd, w, v, val, fd.bound)
	end
	if s == "union" then
		if val == nil or val.Type == 0 then
			if not required then return true end -- None elides
			w_leb(w, ref(c.ids, fd.id))
			w_u8(w, 15)
			w_leb(w, 0)
			return true
		end
		local un = St.lookup(fd.t)
		if un.arms[val.Type] == nil then return false end
		w_leb(w, ref(c.ids, fd.id))
		w_u8(w, 15)
		return write_arm_header(c, un, w, val)
	end
	if s == "tab" then
		-- AN ELIDED FIELD COSTS NOTHING IN THE ID TABLE EITHER (§3): intern the
		-- id, build the body that decides, undo both when it is the terminator
		local m = mark(c.ids)
		local fref = ref(c.ids, fd.id)
		local b = body_of(c, St.lookup(fd.t), val)
		if b == nil then return false end
		if b.n <= 1 and not required then
			rollback(c.ids, m)
			return true
		end
		w_leb(w, fref)
		w_u8(w, 13)
		w_leb(w, b.n)
		w_sub(w, b)
		return true
	end
	-- a scalar or an enum, elided at its declared default
	if s ~= "bool" and s ~= "float" and s ~= "int" and s ~= "enum" then return false end
	if not required and at_default(fd, val) then return true end
	header(c, w, fd.id, fd.k)
	return write_value(c, fd, w, val, nil)
end

-- a body's fields in declaration order (§3): first-use order of the id table
-- follows the write order
write_fields = function(c: any, d: any, w: any, v: any): boolean
	for _, fd in d.fields do
		c.owner, c.field = d.name, fd.name
		if not write_field(c, fd, w, v) then return false end
	end
	return true
end

-- CRC-32C, the Castagnoli polynomial, reflected 0x82F63B78, initial and final
-- 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14).
local function crc32c(b: buffer, n: number): number
	local crc = 0xFFFFFFFF
	for i = 0, n - 1 do
		crc = bit32.bxor(crc, buffer.readu8(b, i))
		for _ = 1, 8 do
			if bit32.band(crc, 1) ~= 0 then
				crc = bit32.bxor(bit32.rshift(crc, 1), 0x82F63B78)
			else
				crc = bit32.rshift(crc, 1)
			end
		end
	end
	return bit32.bxor(crc, 0xFFFFFFFF)
end

-- THE SAVE (docs/SPEC-TABLES.md §3, §3.1, §2.14): the form byte, the root's
-- fields, the node table LAST in the root body under the reserved id at kind
-- 12 (a root that reaches no nodes writes none), the terminator, then the id
-- table and its count, and for a root that declares integrity = "crc32c" the
-- four-byte little-endian CRC-32C trailer. A data cycle or a value with no
-- wire identity refuses (nil).
local function walk(d: any, v: any, count: boolean): (any, any?)
	local nodes, why = number(d, v)
	if nodes == nil then return nil, why end
	local c: any = { ids = new_ids(), nodes = nodes, count = count, why = nil }
	local w = new_writer(count)
	w_u8(w, Form)
	if not write_fields(c, d, w, v) then return nil, c.why end
	if #nodes.list > 0 then
		w_leb(w, ref(c.ids, NodeTableId))
		w_u8(w, 12)
		local p = new_writer(count)
		w_leb(p, #nodes.list)
		for _, n in nodes.list do
			w_leb(p, ref(c.ids, n.typeId))
			if n.blob ~= nil then
				-- a BYTE BUFFER's record is the bytes themselves (§2.5)
				w_leb(p, buffer.len(n.blob))
				w_raw(p, n.blob, 0, buffer.len(n.blob))
			else
				local b = body_of(c, n.d, n.v)
				if b == nil then return nil, c.why end
				w_leb(p, b.n)
				w_sub(p, b)
			end
		end
		w_leb(w, p.n)
		w_sub(w, p)
	end
	w_u8(w, 0) -- the root body ends at its own zero reference
	-- THE ID TABLE IS THE LAST THING IN THE FILE: the entries, then the count
	for _, id in c.ids.list do w_int(w, id) end
	w_int(w, integer.create(#c.ids.list))
	-- THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14; docs/PORT-LUAU.md):
	-- four bytes, little-endian, the CRC-32C of every byte before them,
	-- counted by measure and written by save when the root declares integrity.
	if d.integrity ~= nil and d.integrity ~= "" then
		if not w.count then
			local sum = crc32c(w.b, w.n)
			reserve(w, 4)
			buffer.writeu32(w.b, w.n, sum)
		end
		w.n = w.n + 4
	end
	return w
end

-- Save is the root value's form-1 wire as a buffer of exactly its bytes, or
-- nil when the save refuses (a data cycle, a value with no wire identity)
-- (docs/SPEC-TABLES.md §3, §3.1, §5).
-- Validate enum retirement before the walk so every form-1 writer can return
-- the standard named refusal as its second result (§2.15). Shared table
-- identities are visited once; cycles remain the walk's own refusal.
local function invalid_deprecated(d: any, v: any, seen: any): any
	if typeof(v) ~= "table" or seen[v] then return nil end
	seen[v] = true
	for _, fd in d.fields do
		if not guard_holds(fd, v) or (fd.present ~= nil and not v[fd.present]) then continue end
		local val = v[fd.slot]
		if fd.keyvs ~= nil and typeof(val) == "table" then
			for slot, pair in fd.keyvs do
				if pair[3] == true and not elem_is_default(fd.e, val[slot], nil) then
					return { name = "invalid_value", field = d.name .. "." .. fd.name, variant = pair[1] }
				end
			end
		end
		local e = fd
		if fd.s == "arr" then e = fd.e end
		if e ~= nil and e.s == "enum" then
			local limit = if fd.s == "arr" then live_count(fd, v, val) else 1
			for i = 1, limit do
				local ordinal = if fd.s == "arr" then elem_at(val, i) else val
				local pair = if ordinal == nil then nil else e.vs[ordinal]
				if pair ~= nil and pair[3] == true then
					return { name = "invalid_value", field = d.name .. "." .. fd.name, variant = pair[1] }
				end
			end
		elseif fd.s == "tab" then
			local reason = invalid_deprecated(St.lookup(fd.t), val, seen)
			if reason ~= nil then return reason end
		elseif fd.s == "arr" and fd.e ~= nil and fd.e.s == "tab" then
			for i = 1, live_count(fd, v, val) do
				local reason = invalid_deprecated(St.lookup(fd.e.t), elem_at(val, i), seen)
				if reason ~= nil then return reason end
			end
		end
	end
	return nil
end

function S.save(d: any, v: any): (buffer?, any?)
	if v == nil then return nil, { name = "nil_value", field = d.name } end
	local invalid = invalid_deprecated(d, v, {})
	if invalid ~= nil then return nil, invalid end
	local w, why = walk(d, v, false)
	if w == nil then return nil, why end
	local out = buffer.create(w.n)
	buffer.copy(out, 0, w.b, 0, w.n)
	return out, nil
end

-- Measure is the length Save writes, by the same walk storing nothing, or nil
-- when the save refuses (docs/SPEC-TABLES.md §9's measure/save split).
function S.measure(d: any, v: any): (number?, any?)
	local invalid = invalid_deprecated(d, v, {})
	if invalid ~= nil then return nil, invalid end
	local w, why = walk(d, v, true)
	if w == nil then return nil, why end
	return w.n, nil
end

return table.freeze(S)
