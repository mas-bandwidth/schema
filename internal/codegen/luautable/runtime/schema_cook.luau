--!strict
-- schema_cook.luau — the Luau table runtime's COOK WRITE side (docs/SPEC-TABLES.md
-- §7.6, §7.1, §7.2). Cook writes the file internal/tablecook writes for the
-- same value: the 64-byte header, the packed region (the depth-first pre-order
-- numbering of §3.1, every node at align_up(offset, alignof) for its own type)
-- and the attribution part, every byte no field covers zero. CookMeasure is
-- the same walk with the writing half off.
--
-- The layout is read off the descriptors (docs/SPEC-TABLES.md §8.1): the C ABI
-- record model of §20.3, a field's pieces aligned one by one, a string's
-- length/count companion and an optional's presence bool after its value, a
-- counted array's count after its slots, a list's sixteen-byte reference and
-- count at eight, and a blob node's header at eight. Cook refuses by
-- schema-go's names (§7.6): invalid_value for a value the walk cannot lay out
-- (a dead reachable huge count, a union tag past its arms, a reference the
-- numbering does not carry) and data_cycle for a reference to a node whose
-- descent is still open.
--
-- The build version is the unit's own: the generated table module carries it
-- as TableMessageBuildVersion, and the caller hands it here before cooking
-- (C.BuildVersion = module.TableMessageBuildVersion).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local St = require("./schema_tables")

local C = {}

-- the build version the header records (§7.1); the caller sets it from the
-- generated module's TableMessageBuildVersion.
C.BuildVersion = 0i

local HEADER_BYTES = 64
local MAGIC = 0x4B4F4F434D484353i
local NODE_TABLE_ENTRY = 16
local BLOB_HEADER = 8
local BLOB_ALIGN = 8
local FLOOR = 8

local function align_up(n: number, a: number): number
	if a <= 1 then return n end
	return math.ceil(n / a) * a
end

local function max(a: number, b: number): number
	if a > b then return a end
	return b
end

-- ---- THE C ABI LAYOUT (§20.3, internal/ir/blocklayout.go) -----------------
-- The descriptor carries every fact the layout needs; these maps cache the
-- measured records and unions, keyed by the frozen descriptors themselves.

local record_layouts: { [any]: any } = {}
local union_layouts: { [any]: any } = {}
local value_piece: (e: any) -> any = function(_e: any): any return nil end
local field_pieces: (fd: any) -> { any } = function(_fd: any): { any } return {} end
local record_layout: (d: any) -> any = function(_d: any): any return nil end
local union_layout: (u: any) -> any = function(_u: any): any return nil end
local arm_layout: (arm: any) -> any = function(_arm: any): any return nil end

local function storage_width(n: number): number
	if n <= 0xff then return 1 end
	if n <= 0xffff then return 2 end
	if n <= 0xffffffff then return 4 end
	return 8
end

-- an integer's STORAGE width (ir/blocklayout.go's elementPiece): bits(N) is
-- a uint32 up to 32 bits and a uint64 past it, whatever narrower kind it
-- rides the wire in; every other integer is its kind's width.
local function int_width(e: any): number
	if e.bits ~= nil then
		if e.bits <= 32 then return 4 end
		return 8
	end
	return e.kw or 4
end

-- one VALUE's storage (an array element or a scalar), the `elementPiece` of
-- ir/blocklayout.go.
value_piece = function(e: any): any
	local s = e.s
	if s == "ptr" or s == "sblob" or s == "bblob" then return { size = 8, align = 8 } end
	if s == "bool" then return { size = 1, align = 1 } end
	if s == "float" then return e.f32 and { size = 4, align = 4 } or { size = 8, align = 8 } end
	if s == "int" then local w = int_width(e); return { size = w, align = w } end
	if s == "enum" then local w = storage_width(#(e.vs or {})); return { size = w, align = w } end
	if s == "tab" then
		local l = record_layout(St.lookup(e.t))
		return { size = l.size, align = l.align }
	end
	if s == "union" then
		local u = union_layout(St.lookup(e.t))
		return { size = u.size, align = u.align }
	end
	if s == "str" or s == "bytes" or s == "text" or s == "arr" or s == "map" then
		local pieces = field_pieces(e)
		local at, a = 0, 1
		for _, p in pieces do
			if p.align > a then a = p.align end
			at = align_up(at, p.align) + p.size
		end
		return { size = at, align = a }
	end
	return { size = 1, align = 1 }
end

-- one field's contiguous storage pieces, in order (ir.fieldPieces).
field_pieces = function(fd: any): { any }
	local out: { any } = {}
	local s = fd.s
	if s == "map" then
		out[1] = { size = 16, align = 8 }
	elseif s == "ptr" or s == "sblob" or s == "bblob" or s == "text" then
		out[1] = { size = 8, align = 8 }
	elseif s == "str" then
		out[1] = { size = (fd.bound or 0) + 1, align = 1 }
		out[2] = { size = 4, align = 4 }
	elseif s == "bytes" then
		out[1] = { size = fd.bound or 0, align = 1 }
		out[2] = { size = 4, align = 4 }
	elseif fd.array == "keyed" then
		local e = value_piece(fd.e)
		out[1] = { size = e.size * (fd.bound or #(fd.keyvs or {})), align = e.align }
	elseif fd.array == "fixed" or fd.array == "counted" then
		local e = value_piece(fd.e)
		out[1] = { size = e.size * (fd.bound or 0), align = e.align }
		if fd.array == "counted" then out[2] = { size = 4, align = 4 } end
	elseif fd.array == "list" then
		out[1] = { size = 16, align = 8 }
	else
		out[1] = value_piece(fd)
	end
	if fd.present ~= nil then out[#out + 1] = { size = 1, align = 1 } end
	return out
end

-- one record's C layout: each piece at its own alignment, the record rounded
-- to its greatest alignment (an empty record is one byte).
record_layout = function(d: any): any
	local got = record_layouts[d]
	if got ~= nil then return got end
	local offset, maxAlign = 0, 1
	local fields: { any } = {}
	for _, fd in d.fields do
		local pieces = field_pieces(fd)
		local start, falign = -1, 1
		local abs: { any } = {}
		for _, p in pieces do
			if p.align > maxAlign then maxAlign = p.align end
			if p.align > falign then falign = p.align end
			offset = align_up(offset, p.align)
			if start < 0 then start = offset end
			abs[#abs + 1] = { at = offset, size = p.size }
			offset = offset + p.size
		end
		fields[#fields + 1] = { fd = fd, at = start, pieces = abs, size = offset - start, align = falign }
	end
	local size = align_up(offset, maxAlign)
	if size == 0 then size = 1 end
	local l = { size = size, align = maxAlign, fields = fields }
	record_layouts[d] = l
	return l
end

-- one ARM's storage inside the union overlay: its pieces laid out as C lays a
-- struct out (ir.ArmLayout).
arm_layout = function(arm: any): any
	if arm.s == "void" then return { size = 0, align = 1 } end
	local offset, maxAlign = 0, 1
	for _, p in field_pieces(arm) do
		if p.align > maxAlign then maxAlign = p.align end
		offset = align_up(offset, p.align) + p.size
	end
	return { size = align_up(offset, maxAlign), align = maxAlign }
end

-- a union: tag at zero at its own width, arms overlaid at the greatest arm
-- alignment, the whole rounded up (ir.UnionLayout).
union_layout = function(u: any): any
	local got = union_layouts[u]
	if got ~= nil then return got end
	local tag = storage_width(#u.arms)
	local align = tag
	local armAlign, armSize = 1, 0
	for _, arm in u.arms do
		local a = arm_layout(arm)
		if a.align > armAlign then armAlign = a.align end
		if a.size > armSize then armSize = a.size end
	end
	if armAlign > align then align = armAlign end
	local armOffset = align_up(tag, armAlign)
	local size = align_up(armOffset + armSize, align)
	local z = { size = size, align = align, tag = tag, armOffset = armOffset }
	union_layouts[u] = z
	return z
end

-- ---- THE VALUE (docs/PORT-LUAU.md, THE TYPE MAPPING) ----------------------

local function field_value(d: any, fd: any, v: any): any
	local x = v[fd.slot]
	if x ~= nil then return x end
	local s = fd.s
	if s == "bool" then return false end
	if s == "int" or s == "float" then return 0 end
	if s == "enum" then return 0 end
	if s == "tab" then return St.lookup(fd.t).new() end
	if s == "union" then return St.lookup(fd.t).new() end
	if s == "arr" then return {} end
	if s == "map" then return {} end
	return nil
end

local function zero_elem(e: any): any
	local s = e.s
	if s == "bool" then return false end
	if s == "int" or s == "float" or s == "enum" then return 0 end
	if s == "tab" then return St.lookup(e.t).new() end
	if s == "union" then return St.lookup(e.t).new() end
	if s == "arr" then return {} end
	return nil
end

-- a string/bytes value's buffer and used length; `slot` is nil for an array
-- element, whose buffer's own length is the used length.
local function used_bytes(holder: any, fd: any, x: any, slot: string?): (buffer?, number, number)
	if typeof(x) ~= "buffer" then return nil, 0, 0 end
	local n = buffer.len(x)
	if slot ~= nil and holder ~= nil and holder[slot .. "Length"] ~= nil then n = holder[slot .. "Length"] end
	return x, n, n
end

-- the ABSOLUTE offset of one piece of a field that starts at `base`
-- (ir.FieldPieces). Each piece aligns against the address C aligns it
-- against, never against the field's start: a string(32) at an odd offset
-- puts its int32 length at the next multiple of four in the record, which a
-- field-relative walk misplaces whenever the field itself is not aligned.
local function piece_at(fd: any, want: number, base: number): number
	local at = base
	local i = 0
	for _, p in field_pieces(fd) do
		i = i + 1
		at = align_up(at, p.align)
		if i == want then return at end
		at = at + p.size
	end
	return at
end

local function guard_holds(fd: any, v: any): boolean
	if fd.guard == nil then return true end
	for _, pair in fd.guard do
		if v[pair[1]] ~= pair[2] then return false end
	end
	return true
end

local function live_count(fd: any, holder: any, val: any): number?
	if fd.array == "list" then
		if typeof(val) == "buffer" then return buffer.len(val) end
		if typeof(val) == "table" and typeof(val.n) == "number" then return val.n end
		if typeof(val) == "table" then return #val end
		return 0
	end
	if fd.array == "counted" then
		local c = holder[fd.slot .. "Count"]
		if c == nil then c = 0 end
		if c < 0 or c > (fd.bound or 0) then return nil end
		return c
	end
	return fd.bound or 0
end

local BytesBlobTypeId: integer = 0x2f2ec0474f1c4fe4i
local StringBlobTypeId: integer = 0x704be0d8faaffc58i

-- ---- THE NUMBERING (§3.1, internal/tablewire/nodes.go) ---------------------
-- The first-visit order of a depth-first pre-order walk over pointer edges,
-- descending through every by-value edge to reach them. Node one is the root.
-- A pointer to a node whose descent is still open is a DATA CYCLE.

local edges: (d: any, v: any, out: { any }) -> { any } = function(_d: any, _v: any, out: { any }): { any } return out end

local function arm_edges(fd: any, u: any, out: { any })
	if u == nil or u.Type == nil or u.Type == 0 then return end
	local un = St.lookup(fd.t)
	local arm = un.arms[u.Type]
	if arm == nil or arm.s == "void" then return end
	local val = u[arm.slot]
	if arm.s == "tab" then
		edges(St.lookup(arm.t), val, out)
	elseif arm.s == "sblob" or arm.s == "bblob" or arm.s == "text" then
		if val ~= nil then out[#out + 1] = { blob = arm.s, val = val } end
	elseif arm.s == "ptr" then
		if val ~= nil then out[#out + 1] = { d = St.lookup(arm.t), val = val } end
	elseif arm.s == "arr" and arm.e ~= nil and arm.e.s == "ptr" then
		local d = St.lookup(arm.e.t)
		for i = 1, live_count(arm, u, val) or 0 do
			if val[i] then out[#out + 1] = { d = d, val = val[i] } end
		end
	elseif arm.s == "union" then
		arm_edges(arm, val, out)
	end
end

edges = function(d: any, v: any, out: { any }): { any }
	for _, fd in d.fields do
		if not guard_holds(fd, v) then continue end
		if fd.present ~= nil and not v[fd.present] then continue end
		local val = v[fd.slot]
		local s = fd.s
		if s == "sblob" or s == "bblob" or s == "text" then
			if val ~= nil then out[#out + 1] = { blob = s, val = val } end
		elseif s == "ptr" then
			if val ~= nil then out[#out + 1] = { d = St.lookup(fd.t), val = val } end
		elseif s == "union" then
			arm_edges(fd, val, out)
		elseif s == "tab" then
			edges(St.lookup(fd.t), val, out)
		elseif s == "arr" then
			local e = fd.e
			local n = live_count(fd, v, val) or 0
			if e.s == "ptr" then
				local pd = St.lookup(e.t)
				for i = 1, n do
					if val[i] then out[#out + 1] = { d = pd, val = val[i] } end
				end
			elseif e.s == "union" then
				for i = 1, n do arm_edges(e, val[i], out) end
			elseif e.s == "tab" then
				local td = St.lookup(e.t)
				for i = 1, n do
					if val[i] ~= nil then edges(td, val[i], out) end
				end
			end
		end
	end
	return out
end

local function number_nodes(d: any, root: any): any
	if root == nil then return nil end
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
			if nodes.index[e.val] == nil then
				if e.blob ~= nil then
					nodes.list[#nodes.list + 1] = { typeId = e.blob == "bblob" and BytesBlobTypeId or StringBlobTypeId, blob = e.val, string = e.blob ~= "bblob" }
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

-- ---- THE WRITER (§7.2): absolute offsets into one zeroed buffer -----------

local function new_writer(total: number, big: boolean): any
	local b = buffer.create(total)
	buffer.fill(b, 0, 0, total)
	return { b = b, big = big }
end

local function put(w: any, at: number, v: any, width: number)
	local b = w.b
	if b == nil then return end
	if width == 8 then
		if typeof(v) ~= "integer" then v = integer.create(v) end
		buffer.writeinteger(b, at, v)
		if w.big then
			local x0, x1, x2, x3 = buffer.readu8(b, at), buffer.readu8(b, at + 1), buffer.readu8(b, at + 2), buffer.readu8(b, at + 3)
			local x4, x5, x6, x7 = buffer.readu8(b, at + 4), buffer.readu8(b, at + 5), buffer.readu8(b, at + 6), buffer.readu8(b, at + 7)
			buffer.writeu8(b, at, x7); buffer.writeu8(b, at + 1, x6); buffer.writeu8(b, at + 2, x5); buffer.writeu8(b, at + 3, x4)
			buffer.writeu8(b, at + 4, x3); buffer.writeu8(b, at + 5, x2); buffer.writeu8(b, at + 6, x1); buffer.writeu8(b, at + 7, x0)
		end
		return
	end
	if width == 16 then
		if w.big then
			put(w, at, v.hi, 8)
			put(w, at + 8, v.lo, 8)
		else
			put(w, at, v.lo, 8)
			put(w, at + 8, v.hi, 8)
		end
		return
	end
	local mask = width == 1 and 0xff or (width == 2 and 0xffff or 0xffffffff)
	local x = math.floor(v) % (mask + 1)
	if w.big then
		for i = 0, width - 1 do
			buffer.writeu8(b, at + i, math.floor(x / (2 ^ (8 * (width - 1 - i)))) % 256)
		end
	else
		buffer.writeu8(b, at, x % 256)
		if width > 1 then buffer.writeu8(b, at + 1, math.floor(x / 256) % 256) end
		if width > 2 then
			buffer.writeu8(b, at + 2, math.floor(x / 65536) % 256)
			buffer.writeu8(b, at + 3, math.floor(x / 16777216) % 256)
		end
	end
end

-- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; the same rule
-- as packets): a NaN or an infinity is not a value of the type.
-- The cook refuses one, non_finite_float naming <Owner>.<field>: CookMeasure
-- answers -1 and Cook nil, each with the reason beside it. A float32 slot
-- tests its NARROWED bits, so a double past the float32 range is refused too.
local _refusal: string? = nil
local _owner = "?"
local _field = "?"
local finiteScratch = buffer.create(4)
local function finite(v: number, f32: boolean): boolean
	if f32 then
		buffer.writef32(finiteScratch, 0, v)
		return bit32.band(buffer.readu32(finiteScratch, 0), 0x7F800000) ~= 0x7F800000
	end
	return v == v and v ~= math.huge and v ~= -math.huge
end

local function put_float(w: any, at: number, v: number, f32: boolean)
	if w.b == nil then return end
	if f32 then
		buffer.writef32(w.b, at, v)
		if w.big then
			local x = { buffer.readu8(w.b, at), buffer.readu8(w.b, at + 1), buffer.readu8(w.b, at + 2), buffer.readu8(w.b, at + 3) }
			buffer.writeu8(w.b, at, x[4]); buffer.writeu8(w.b, at + 1, x[3]); buffer.writeu8(w.b, at + 2, x[2]); buffer.writeu8(w.b, at + 3, x[1])
		end
	else
		-- a float64 is written at all before its bytes are ordered: the
		-- little-endian write is the value, the big-endian file reverses it
		buffer.writef64(w.b, at, v)
		if not w.big then return end
		local x = {}
		for i = 0, 7 do x[i] = buffer.readu8(w.b, at + i) end
		for i = 0, 7 do buffer.writeu8(w.b, at + i, x[7 - i]) end
	end
end

local function put_copy(w: any, at: number, src: buffer?, n: number)
	if w.b ~= nil and src ~= nil and n > 0 then buffer.copy(w.b, at, src, 0, n) end
end

-- ---- THE EXTENT (§2.9, internal/tablecook/extent.go) ----------------------
-- After a record, each unbounded array's whole body at its element alignment,
-- then element by element the arrays its elements hold by value. The measure
-- and the write are one walk, with `w` nil in the measure.

local record_has_extent: (d: any) -> boolean = function(_d: any): boolean return false end
local union_has_extent: (u: any) -> boolean = function(_u: any): boolean return false end
local field_has_extent: (fd: any) -> boolean = function(_fd: any): boolean return false end
local carve_record: any
local carve_element: any
local carve_field: any

field_has_extent = function(fd: any): boolean
	if fd.s == "map" or (fd.s == "arr" and fd.array == "list") then return true end
	if fd.s == "ptr" or fd.s == "sblob" or fd.s == "bblob" or fd.s == "text" then return false end
	if fd.s == "tab" then return record_has_extent(St.lookup(fd.t)) end
	if fd.s == "union" then return union_has_extent(St.lookup(fd.t)) end
	if fd.s == "arr" then return field_has_extent(fd.e) end
	return false
end

record_has_extent = function(d: any): boolean
	if d == nil then return false end
	for _, fd in d.fields do
		if field_has_extent(fd) then return true end
	end
	return false
end

union_has_extent = function(u: any): boolean
	if u == nil then return false end
	for _, arm in u.arms do
		if arm.s ~= "void" and field_has_extent(arm) then return true end
	end
	return false
end

local write_record: any
local write_value: any
local write_array: any
local write_ref: any
local write_union: any

carve_field = function(fd: any, v: any, x: any, fieldAt: number, extentBase: number, cursor: any, w: any, index: any): boolean
	if fd.s == "map" then return false end
	if fd.s == "arr" and fd.array == "list" then
		local n = live_count(fd, v, x)
		if n == nil then return false end
		local e = fd.e
		local es = value_piece(e)
		local at = fieldAt
		cursor.at = align_up(cursor.at, es.align)
		local start = cursor.at
		cursor.at = start + n * es.size
		local array = extentBase + start
		if w ~= nil then
			put(w, at, n > 0 and (array - at) or 0, 8)
			put(w, at + 8, n, 4)
			for i = 1, n do
				local el = x[i]
				if el == nil then el = zero_elem(e) end
				if not write_value(e, el, array + (i - 1) * es.size, w, index, nil) then return false end
			end
		end
		if field_has_extent(e) then
			for i = 1, n do
				local el = x[i]
				if el == nil then el = zero_elem(e) end
				if not carve_element(e, el, array + (i - 1) * es.size, extentBase, cursor, w, index) then return false end
			end
		end
		return true
	end
	if fd.s == "ptr" or fd.s == "sblob" or fd.s == "bblob" or fd.s == "text" then return true end
	local at = fieldAt
	if fd.s == "arr" then
		x = x or {}
		local n = live_count(fd, v, x)
		if n == nil then return false end
		if fd.array == "counted" then
			-- count is written by write_record; only the live slots have extents
		elseif fd.array == "fixed" or fd.array == "keyed" then
			n = fd.bound or #(fd.keyvs or {})
		end
		local es = value_piece(fd.e)
		for i = 1, n do
			local el = x[i]
			if el == nil then el = zero_elem(fd.e) end
			if not carve_element(fd.e, el, at + (i - 1) * es.size, extentBase, cursor, w, index) then return false end
		end
		return true
	end
	if fd.s == "tab" then
		local d = St.lookup(fd.t)
		if not record_has_extent(d) then return true end
		return carve_record(d, x, at, extentBase, cursor, w, index)
	end
	if fd.s == "union" then
		local u = St.lookup(fd.t)
		if not union_has_extent(u) then return true end
		local tag = (typeof(x) == "table" and x.Type) or 0
		if tag == 0 or tag > #u.arms then return true end
		local arm = u.arms[tag]
		if arm.s == "void" then return true end
		local z = union_layout(u)
		if arm.s == "tab" then
			local d = St.lookup(arm.t)
			return carve_record(d, x[arm.slot], at + z.armOffset, extentBase, cursor, w, index)
		end
		if arm.s == "arr" or arm.s == "str" or arm.s == "bytes" then
			return carve_field(arm, x[arm.slot], x[arm.slot], at + z.armOffset, extentBase, cursor, w, index)
		end
		if arm.s == "union" then
			return carve_field(arm, x[arm.slot], x[arm.slot], at + z.armOffset, extentBase, cursor, w, index)
		end
		return true
	end
	return true
end

carve_element = function(e: any, x: any, at: number, extentBase: number, cursor: any, w: any, index: any): boolean
	if e.s == "tab" then
		local d = St.lookup(e.t)
		if not record_has_extent(d) then return true end
		return carve_record(d, x, at, extentBase, cursor, w, index)
	elseif e.s == "union" then
		local u = St.lookup(e.t)
		if not union_has_extent(u) then return true end
		local tag = (typeof(x) == "table" and x.Type) or 0
		if tag == 0 or tag > #u.arms then return true end
		local arm = u.arms[tag]
		if arm.s == "void" then return true end
		local z = union_layout(u)
		if arm.s == "tab" then
			return carve_record(St.lookup(arm.t), x[arm.slot], at + z.armOffset, extentBase, cursor, w, index)
		end
		return carve_field(arm, x[arm.slot], x[arm.slot], at + z.armOffset, extentBase, cursor, w, index)
	end
	return true
end

carve_record = function(d: any, v: any, recordBase: number, extentBase: number, cursor: any, w: any, index: any): boolean
	if v == nil then return true end
	local l = record_layout(d)
	for _, fl in l.fields do
		local fd = fl.fd
		local x = field_value(d, fd, v)
		if fd.s == "map" then return false end
		if fd.s == "arr" and fd.array == "list" then
			if not carve_field(fd, v, x, recordBase + fl.at, extentBase, cursor, w, index) then return false end
		elseif fd.s == "ptr" or fd.s == "sblob" or fd.s == "bblob" or fd.s == "text" then
			-- a pointee and a blob are nodes, not this extent
		elseif field_has_extent(fd) then
			if not carve_field(fd, v, x, recordBase + fl.at, extentBase, cursor, w, index) then return false end
		end
	end
	return true
end

-- ---- THE WRITE WALK (§7.2) ------------------------------------------------

write_ref = function(fd: any, x: any, at: number, w: any, index: any): boolean
	if x == nil or x == false then
		put(w, at, 0, 8)
		return true
	end
	local e = index[x]
	if e == nil then return false end
	local delta = HEADER_BYTES + e.offset - at
	if delta == 0 then return false end
	put(w, at, delta, 8)
	return true
end

write_array = function(fd: any, holder: any, x: any, at: number, fl: any, w: any, index: any): boolean
	x = x or {}
	local n = fd.bound or 0
	if fd.array == "keyed" then n = fd.bound or #(fd.keyvs or {}) end
	local es = value_piece(fd.e)
	if fd.array == "counted" then
		local used = live_count(fd, holder, x)
		if used == nil then return false end
		put(w, piece_at(fd, 2, at), used, 4)
	end
	for i = 1, n do
		local el = x[i]
		if el == nil then el = zero_elem(fd.e) end
		if not write_value(fd.e, el, at + (i - 1) * es.size, w, index, nil) then return false end
	end
	return true
end

write_union = function(fd: any, x: any, at: number, w: any, index: any): boolean
	local u = St.lookup(fd.t)
	local tag = (typeof(x) == "table" and x.Type) or 0
	local z = union_layout(u)
	if tag < 0 or tag > #u.arms then return false end
	put(w, at, tag, z.tag)
	if tag == 0 then return true end
	local arm = u.arms[tag]
	if arm.s == "void" then return true end
	local armAt = at + z.armOffset
	if arm.s == "tab" then
		local d = St.lookup(arm.t)
		return write_record(d, x[arm.slot], armAt, w, index)
	end
	return write_value(arm, x[arm.slot], armAt, w, index, x)
end

write_value = function(e: any, x: any, at: number, w: any, index: any, holder: any): boolean
	local s = e.s
	if s == "ptr" then return write_ref(e, x, at, w, index) end
	if s == "sblob" or s == "bblob" or s == "text" then return write_ref(e, x, at, w, index) end
	if s == "bool" then put(w, at, x and 1 or 0, 1); return true end
	if s == "int" then
		local kw = int_width(e)
		if kw == 16 then
			put(w, at, x or { lo = 0i, hi = 0i }, 16)
		else
			put(w, at, x or 0, kw)
		end
		return true
	end
	if s == "float" then
		local f = x or 0
		if not finite(f, e.f32 == true) then
			if _refusal == nil then _refusal = _owner .. "." .. (e.name or _field) end
			return false
		end
		put_float(w, at, f, e.f32 == true)
		return true
	end
	if s == "enum" then put(w, at, x or 0, storage_width(#(e.vs or {}))); return true end
	if s == "str" or s == "bytes" then
		local b, nbytes, count = used_bytes(holder, e, x, e.slot)
		put_copy(w, at, b, nbytes)
		if e.slot ~= nil and holder ~= nil then put(w, piece_at(e, 2, at), count, 4) end
		return true
	end
	if s == "tab" then return write_record(St.lookup(e.t), x, at, w, index) end
	if s == "union" then return write_union(e, x, at, w, index) end
	if s == "arr" then
		return write_array(e, holder, x, at, nil, w, index)
	end
	return false
end

write_record = function(d: any, v: any, base: number, w: any, index: any): boolean
	if v == nil then v = d.new() end
	local l = record_layout(d)
	for _, fl in l.fields do
		local fd = fl.fd
		local at = base + fl.at
		_owner, _field = d.name, fd.name
		local x = field_value(d, fd, v)
		if fd.present ~= nil then
			local present = v[fd.present] == true
			put(w, base + fl.pieces[#fl.pieces].at, present and 1 or 0, 1)
		end
		local ok = true
		if fd.s == "map" then
			ok = false
		elseif fd.s == "arr" and fd.array == "list" then
			-- the extent writer fills its slot
		else
			ok = write_value(fd, x, at, w, index, v)
		end
		if not ok then return false end
	end
	return true
end

-- ---- THE FILE (§7.1, §7.2, §7.6) -----------------------------------------

local function layout(d: any, root: any): any
	if d == nil then return nil end
	local nodes = number_nodes(d, root)
	if nodes == nil then return nil end
	local entries: { any } = { { typeId = d.id, d = d, v = root, index = 1 } }
	for _, n in nodes.list do entries[#entries + 1] = n end
	local index: { [any]: any } = {}
	for i, e in entries do
		e.index = i
		local key = e.d ~= nil and e.v or e.blob
		if key ~= nil then index[key] = e end
	end
	local offset, align = 0, FLOOR
	for _, e in entries do
		if e.d ~= nil then
			local l = record_layout(e.d)
			align = max(align, l.align)
			offset = align_up(offset, l.align)
			e.offset = offset
			local record = l.size
			local extent = 0
			if record_has_extent(e.d) then
				record = align_up(record, FLOOR)
				local cursor = { at = 0 }
				if not carve_record(e.d, e.v, 0, 0, cursor, nil, index) then return nil end
				extent = cursor.at
			end
			e.record = record
			e.extent = extent
			offset = offset + record + extent
		else
			offset = align_up(offset, BLOB_ALIGN)
			e.offset = offset
			e.record = BLOB_HEADER + buffer.len(e.blob) + (e.string and 1 or 0)
			e.extent = 0
			offset = offset + e.record
		end
	end
	offset = align_up(offset, align)
	return { entries = entries, index = index, align = align, data = offset, total = HEADER_BYTES + offset + #entries * NODE_TABLE_ENTRY }
end

local function order_of(order: any): boolean?
	if order == nil or order == 1 or order == "little" or order == "LE" then return false end
	if order == 2 or order == "big" or order == "BE" then return true end
	return nil
end

local function refused_layout(d: any, root: any): any
	if d == nil then return nil end
	local l = layout(d, root)
	if l == nil then return nil end
	-- Run the same value walk Cook uses, without allocating or writing the
	-- output buffer. Layout alone cannot validate values held by a numbered
	-- pointee (for example a union tag that names no arm).
	local w = { b = nil, big = false }
	for _, e in l.entries do
		if e.d ~= nil then
			if not write_record(e.d, e.v, HEADER_BYTES + e.offset, w, l.index) then return nil end
			if e.extent > 0 then
				local cursor = { at = 0 }
				local base = HEADER_BYTES + e.offset
				if not carve_record(e.d, e.v, base, base + e.record, cursor, w, l.index) then return nil end
				if cursor.at ~= e.extent then return nil end
			end
		end
	end
	return l
end

-- CookMeasure answers the whole file's length (§7.6), -1 when the cook refuses.
function C.CookMeasure(root: any, value: any): (number, string?)
	_refusal = nil
	local l = refused_layout(root, value)
	if l == nil then
		if _refusal ~= nil then return -1, "non_finite_float: " .. _refusal end
		return -1, nil
	end
	return l.total, nil
end

-- Cook writes the cook (§7.6): header, data part, attribution part, or nil
-- when the value is refused or the order is not little/big.
function C.Cook(root: any, value: any, order: any): (buffer?, string?)
	local big = order_of(order)
	if big == nil then return nil, nil end
	_refusal = nil
	local l = refused_layout(root, value)
	if l == nil then
		if _refusal ~= nil then return nil, "non_finite_float: " .. _refusal end
		return nil, nil
	end
	local w = new_writer(l.total, big)
	for _, e in l.entries do
		local base = HEADER_BYTES + e.offset
		if e.d ~= nil then
			if not write_record(e.d, e.v, base, w, l.index) then return nil, nil end
			if e.extent > 0 then
				local cursor = { at = 0 }
				if not carve_record(e.d, e.v, base, base + e.record, cursor, w, l.index) then return nil, nil end
				if cursor.at ~= e.extent then return nil, nil end
			end
		else
			put(w, base, buffer.len(e.blob), 4)
			buffer.copy(w.b, base + BLOB_HEADER, e.blob, 0, buffer.len(e.blob))
		end
	end
	local build = C.BuildVersion
	if typeof(build) ~= "integer" then build = integer.create(build) end
	put(w, 0, MAGIC, 8)
	put(w, 8, build, 8)
	put(w, 16, big and 2 or 1, 8)
	put(w, 24, l.data, 8)
	put(w, 32, #l.entries * NODE_TABLE_ENTRY, 8)
	put(w, 40, l.align, 8)
	put(w, 48, 0, 8)
	put(w, 56, 0, 8)
	local at = HEADER_BYTES + l.data
	for i, e in l.entries do
		put(w, at + (i - 1) * NODE_TABLE_ENTRY, e.offset, 8)
		put(w, at + (i - 1) * NODE_TABLE_ENTRY + 8, e.typeId, 8)
	end
	return w.b
end

return C
