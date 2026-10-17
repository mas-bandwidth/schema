--!strict
-- fixed.luau — the Luau table runtime's FIXED FORM (docs/SPEC-TABLES.md §3.4,
-- form byte 3), the one reader and writer for a record. Emitted beside the
-- unit's <Base>Table.luau modules by internal/codegen/luautable and required
-- from them as require("./fixed"). The layout, hash and record size are the
-- emitter's static data (fixedform.go); this file only reads and writes the
-- bytes those facts describe (docs/FIXED-FORM-ALGORITHM.md §1, §3, §4).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")

local F = {}

export type LayoutEntry = { id: integer, kind: number, size: number, children: number }
export type Fixed = { hash: integer, recordBytes: number, layout: { LayoutEntry } }
export type Refusal = { name: string }

local HEADER = 16
local HASH_AT = 8
local ENTRY = 17
local LAYOUT_HEADER = 4
local RECORD_MAX = 65536
local PLAN_MAX = 4096
local COUNT = 4
local PRESENT = 1

local function refuse(name: string): Refusal
	return { name = name }
end

local function signed(kind: number): boolean
	return kind == 2 or kind == 3 or kind == 4 or kind == 5 or
		(kind >= 20 and kind <= 24) or kind == 11
end

-- f32 raw bits <-> a double holding the exact float32 value, by bit surgery so
-- a NaN payload survives a round trip (docs/PORT-LUAU.md, BYTE BUFFERS).
local scratch4 = buffer.create(4)
local function f32ToDouble(bits: number): number
	local exp = bit32.band(bit32.rshift(bits, 23), 0xff)
	if exp == 0xff then
		local sign = bit32.band(bit32.rshift(bits, 31), 1)
		local frac = bit32.band(bits, 0x7fffff)
		local d = buffer.create(8)
		local lo = bit32.lshift(bit32.band(frac, 7), 29)
		local hi2 = bit32.bor(bit32.lshift(sign, 31), 0x7ff00000, bit32.rshift(frac, 3))
		buffer.writeu32(d, 0, lo)
		buffer.writeu32(d, 4, hi2)
		return buffer.readf64(d, 0)
	end
	buffer.writeu32(scratch4, 0, bits)
	return buffer.readf32(scratch4, 0)
end

local function doubleToF32(v: number): number
	local d = buffer.create(8)
	buffer.writef64(d, 0, v)
	local lo = buffer.readu32(d, 0)
	local hi = buffer.readu32(d, 4)
	local sign = bit32.band(bit32.rshift(hi, 31), 1)
	local exp = bit32.band(bit32.rshift(hi, 20), 0x7ff)
	if exp == 0x7ff then
		local frac = bit32.bor(bit32.lshift(bit32.band(hi, 0xfffff), 3), bit32.rshift(lo, 29))
		local f32 = bit32.bor(bit32.lshift(sign, 31), 0x7f800000, frac)
		return f32
	end
	buffer.writef32(scratch4, 0, v)
	return buffer.readu32(scratch4, 0)
end

local function asInt(v: any): integer
	if typeof(v) == "integer" then return v end
	return integer.create(v)
end

-- parse builds an offset tree from the writer's layout. `p` is the pre-order
-- index, and every node records its absolute record offset and child roots.
type Node = {
	id: integer, kind: number, size: number, off: number,
	children: { Node }, index: number,
}

local function parseNode(layout: { LayoutEntry }, i: number, off: number): (Node, number)
	local e = layout[i]
	local node: Node = { id = e.id, kind = e.kind, size = e.size, off = off, children = {}, index = i }
	i = i + 1
	if e.kind == 13 then
		local cursor = off
		for _ = 1, e.children do
			local c
			c, i = parseNode(layout, i, cursor)
			table.insert(node.children, c)
			cursor = cursor + c.size
		end
	elseif e.kind == 15 then
		local kids = {}
		for _ = 1, e.children do
			local c
			c, i = parseNode(layout, i, off)
			table.insert(kids, c)
		end
		local widest = 0
		for _, c in kids do
			if c.size > widest then widest = c.size end
		end
		local tagw = e.size - widest
		if tagw < 1 then tagw = 1 end
		local function shift(c: Node, d: number)
			c.off = c.off + d
			for _, k in c.children do shift(k, d) end
		end
		for _, c in kids do
			shift(c, tagw)
			table.insert(node.children, c)
		end
	elseif e.kind == 35 then
		local c
		c, i = parseNode(layout, i, off + PRESENT)
		table.insert(node.children, c)
	elseif e.kind == 16 then
		local k
		k, i = parseNode(layout, i, off)
		local c
		c, i = parseNode(layout, i, off)
		table.insert(node.children, k)
		table.insert(node.children, c)
	elseif e.kind == 14 then
		local c
		c, i = parseNode(layout, i, off)
		table.insert(node.children, c)
	elseif e.kind == 30 then
		for _ = 1, e.children do
			local c
			c, i = parseNode(layout, i, off)
			table.insert(node.children, c)
		end
	end
	return node, i
end

local function compile(fixed: Fixed): Node
	local layout = fixed.layout
	if #layout == 0 then error(refuse("layout_malformed"), 0) end
	local root = parseNode(layout, 1, 0)
	if root.size > RECORD_MAX then error(refuse("layout_record_too_large"), 0) end
	if #layout > PLAN_MAX then error(refuse("plan_too_large"), 0) end
	return root
end

local function childById(n: Node, id: integer): Node?
	for _, c in n.children do
		if c.id == id then return c end
	end
	return nil
end

local function childAt(n: Node, k: number): Node?
	return n.children[k]
end

local function shift(n: Node, d: number)
	n.off = n.off + d
	for _, c in n.children do shift(c, d) end
end

local readNode: any

local function readLeaf(f: any, n: Node, buf: buffer, off: number): any
	local k = n.kind
	if f.s == "bool" then
		return buffer.readu8(buf, off) ~= 0
	elseif f.s == "float" then
		if k == 10 then
			return f32ToDouble(buffer.readu32(buf, off))
		end
		return buffer.readf64(buf, off)
	elseif f.s == "enum" then
		local raw = 0
		for b = 0, n.size - 1 do
			raw = raw + buffer.readu8(buf, off + b) * 2 ^ (8 * b)
		end
		return raw
	elseif f.s == "int" then
		local w = n.size
		local v: number
		if w == 1 then
			v = signed(k) and buffer.readi8(buf, off) or buffer.readu8(buf, off)
		elseif w == 2 then
			v = signed(k) and buffer.readi16(buf, off) or buffer.readu16(buf, off)
		elseif w == 4 then
			v = signed(k) and buffer.readi32(buf, off) or buffer.readu32(buf, off)
		elseif w == 8 then
			local raw = signed(k) and buffer.readinteger(buf, off) or buffer.readinteger(buf, off)
			if f.dom == "integer" then return raw end
			return tonumber(raw)
		else
			v = 0
		end
		return v
	end
	return 0
end

readNode = function(f: any, n: Node, buf: buffer, off: number): any
	if f.s == "str" then
		local length = buffer.readu32(buf, off)
		local b = buffer.create((f.bound or length) * (f.wide and 2 or 1))
		local unit = f.wide and 2 or 1
		if length > 0 then buffer.copy(b, 0, buf, off + COUNT, length * unit) end
		return b, length
	elseif f.s == "bytes" then
		local length = buffer.readu32(buf, off)
		local b = buffer.create(f.bound or length)
		if length > 0 then buffer.copy(b, 0, buf, off + COUNT, length) end
		return b, length
	elseif f.s == "tab" then
		local d = S.lookup(f.t)
		return readStruct(d, n, buf, n.off)
	elseif f.s == "union" then
		local d = S.lookup(f.t)
		local tagw = n.size
		local widest = 0
		for _, c in n.children do if c.size > widest then widest = c.size end end
		tagw = tagw - widest
		local tag = 0
		for b = 0, tagw - 1 do tag = tag + buffer.readu8(buf, n.off + b) * 2 ^ (8 * b) end
		local v = d.new()
		v.Type = tag
		if tag >= 1 and tag <= #d.arms then
			local arm = d.arms[tag]
			local c = childAt(n, tag)
			if c ~= nil and arm.s ~= "void" then
				local got = readNode(arm, c, buf, c.off)
				v[arm.slot] = got
			end
		end
		return v
	elseif f.s == "arr" then
		local out = {}
		local e = f.e
		if f.array == "counted" then
			local count = buffer.readu32(buf, n.off)
			if count > (f.bound or 0) then error({ name = "count_over_bound" }, 0) end
			local c = childAt(n, 1) :: Node
			shift(c, COUNT)
			for i = 1, count do
				out[i] = readNode(e, c, buf, c.off)
				shift(c, c.size)
			end
			shift(c, -(COUNT + count * c.size))
			return out, count
		elseif f.array == "keyed" then
			local c = childAt(n, 2) :: Node
			for i = 1, f.bound or 0 do
				out[i] = readNode(e, c, buf, c.off)
				shift(c, c.size)
			end
			shift(c, -(f.bound or 0) * c.size)
			return out
		else
			local c = childAt(n, 1) :: Node
			for i = 1, f.bound or 0 do
				out[i] = readNode(e, c, buf, c.off)
				shift(c, c.size)
			end
			shift(c, -(f.bound or 0) * c.size)
			return out
		end
	end
	return readLeaf(f, n, buf, off)
end

function readStruct(d: any, n: Node, buf: buffer, off: number): any
	local v = d.new()
	for _, f in d.fields do
		local c = childById(n, f.id)
		local slot = f.slot
		if c ~= nil then
			if f.present ~= nil then
				local present = buffer.readu8(buf, c.off) ~= 0
				v[f.present] = present
				if present then
					local child = childAt(c, 1)
					if child ~= nil then
						local got, length = readNode(f, child, buf, child.off)
						v[slot] = got
						if f.s == "str" or f.s == "bytes" then
							v[slot .. "Length"] = length
						elseif f.s == "arr" then
							v[slot .. "Count"] = length
						end
					end
				end
			else
				local got, length = readNode(f, c, buf, c.off)
				v[slot] = got
				if f.s == "str" or f.s == "bytes" then
					v[slot .. "Length"] = length
				elseif f.s == "arr" and (f.array == "counted" or f.array == "list") then
					v[slot .. "Count"] = length
				end
			end
		end
	end
	return v
end

local function writeLeaf(f: any, n: Node, v: any, buf: buffer, off: number)
	if f.s == "bool" then
		buffer.writeu8(buf, off, v and 1 or 0)
	elseif f.s == "float" then
		if n.kind == 10 then
			buffer.writeu32(buf, off, doubleToF32(v))
		else
			buffer.writef64(buf, off, v)
		end
	elseif f.s == "enum" then
		local raw = v
		for b = 0, n.size - 1 do
			buffer.writeu8(buf, off + b, raw % 256)
			raw = math.floor(raw / 256)
		end
	elseif f.s == "int" then
		local w = n.size
		if w == 1 then
			buffer.writeu8(buf, off, v % 256)
		elseif w == 2 then
			buffer.writeu16(buf, off, v % 65536)
		elseif w == 4 then
			buffer.writeu32(buf, off, v % 4294967296)
		elseif w == 8 then
			if f.dom == "integer" then
				buffer.writeinteger(buf, off, asInt(v))
			else
				buffer.writeinteger(buf, off, asInt(v))
			end
		end
	end
end

function writeOne(f: any, n: Node, value: any, buf: buffer)
	local slot = f.slot
	local own = value
	if slot ~= nil then own = value[slot] end
	if f.s == "str" then
		local length = value[slot .. "Length"] or 0
		buffer.writeu32(buf, n.off, length)
		local b = own
		if b ~= nil and length > 0 then
			buffer.copy(buf, n.off + COUNT, b, 0, length * (f.wide and 2 or 1))
		end
	elseif f.s == "bytes" then
		local length = value[slot .. "Length"] or 0
		buffer.writeu32(buf, n.off, length)
		if length > 0 then buffer.copy(buf, n.off + COUNT, own, 0, length) end
	elseif f.s == "tab" then
		local d = S.lookup(f.t)
		writeStruct(d, n, own, buf)
	elseif f.s == "union" then
		local d = S.lookup(f.t)
		local tag = own.Type or 0
		local widest = 0
		for _, c in n.children do if c.size > widest then widest = c.size end end
		local tagw = n.size - widest
		local raw = tag
		for b = 0, tagw - 1 do
			buffer.writeu8(buf, n.off + b, raw % 256)
			raw = math.floor(raw / 256)
		end
		if tag >= 1 then
			local arm = d.arms[tag]
			local c = childAt(n, tag)
			if arm ~= nil and c ~= nil and arm.s ~= "void" then
				writeOne(arm, c, own, buf)
			end
		end
	elseif f.s == "arr" then
		local e = f.e
		if f.array == "counted" then
			local count = value[slot .. "Count"] or 0
			buffer.writeu32(buf, n.off, count)
			local c = childAt(n, 1) :: Node
			shift(c, COUNT)
			for i = 1, count do
				writeOne(e, c, value[slot][i], buf)
				shift(c, c.size)
			end
			shift(c, -(COUNT + count * c.size))
		elseif f.array == "keyed" then
			local c = childAt(n, 2) :: Node
			for i = 1, f.bound or 0 do
				writeOne(e, c, value[slot][i], buf)
				shift(c, c.size)
			end
			shift(c, -(f.bound or 0) * c.size)
		else
			local c = childAt(n, 1) :: Node
			for i = 1, f.bound or 0 do
				writeOne(e, c, value[slot][i], buf)
				shift(c, c.size)
			end
			shift(c, -(f.bound or 0) * c.size)
		end
	else
		writeLeaf(f, n, own, buf, n.off)
	end
end

function writeStruct(d: any, n: Node, value: any, buf: buffer)
	-- FIXED-WRITE-ANCHOR
	for _, f in d.fields do
		local c = childById(n, f.id)
		if c ~= nil then
			if f.present ~= nil then
				local present = value[f.present]
				buffer.writeu8(buf, c.off, present and 1 or 0)
				if present then
					writeOne(f, childAt(c, 1) :: Node, value, buf)
				end
			else
				writeOne(f, c, value, buf)
			end
		end
	end
end

local function u32(buf: buffer, at: number): number return buffer.readu32(buf, at) end
local function put32(buf: buffer, at: number, v: number) buffer.writeu32(buf, at, v) end
local function put64(buf: buffer, at: number, v: integer) buffer.writeinteger(buf, at, v) end

local function parseLayoutBytes(bytes: buffer, at: number, length: number): { LayoutEntry }
	if length < LAYOUT_HEADER then error(refuse("layout_malformed"), 0) end
	local count = u32(bytes, at)
	if count == 0 or length ~= LAYOUT_HEADER + count * ENTRY then error(refuse("layout_malformed"), 0) end
	if count > PLAN_MAX then error(refuse("plan_too_large"), 0) end
	local out = {}
	local p = at + LAYOUT_HEADER
	for i = 1, count do
		local id = buffer.readinteger(bytes, p)
		local kind = buffer.readu8(bytes, p + 8)
		local size = u32(bytes, p + 9)
		local children = u32(bytes, p + 13)
		if size > RECORD_MAX then error(refuse("layout_record_too_large"), 0) end
		out[i] = { id = id, kind = kind, size = size, children = children }
		p = p + ENTRY
	end
	return out
end

local function parseFile(file: buffer): { hash: integer, layout: { LayoutEntry }, at: number }
	if buffer.len(file) < HEADER + LAYOUT_HEADER then error(refuse("layout_malformed"), 0) end
	local form = buffer.readu8(file, 0)
	if form == 1 then error(refuse("previous_form"), 0) end
	if form == 2 then error(refuse("message_form_as_file"), 0) end
	if form ~= 3 then error(refuse("newer_form"), 0) end
	local hash = buffer.readinteger(file, HASH_AT)
	local length = u32(file, HEADER)
	local layout = parseLayoutBytes(file, HEADER + 4, length)
	return { hash = hash, layout = layout, at = HEADER + 4 + length }
end

-- save writes one file: the form header, the writer's layout, and each record
-- behind its own eight-byte hash (docs/FIXED-FORM-ALGORITHM.md §2, §3).
function F.save(d: any, fixed: Fixed, values: { any }): buffer
	local layout = fixed.layout
	local bytes = HEADER + 4 + 4 + #layout * ENTRY
	local out = buffer.create(bytes + #values * (8 + fixed.recordBytes))
	buffer.fill(out, 0, 0, bytes)
	buffer.writeu8(out, 0, 3)
	put64(out, HASH_AT, fixed.hash)
	put32(out, HEADER, #layout * ENTRY + 4)
	local p = HEADER + 4
	put32(out, p, #layout)
	p = p + 4
	for _, e in layout do
		put64(out, p, e.id)
		buffer.writeu8(out, p + 8, e.kind)
		put32(out, p + 9, e.size)
		put32(out, p + 13, e.children)
		p = p + ENTRY
	end
	local root = compile(fixed)
	local rec = buffer.create(fixed.recordBytes)
	for _, v in values do
		buffer.fill(rec, 0, 0, fixed.recordBytes)
		writeStruct(d, root, v, rec)
		put64(out, p, fixed.hash)
		p = p + 8
		buffer.copy(out, p, rec, 0, fixed.recordBytes)
		p = p + fixed.recordBytes
	end
	return out
end

-- load reads every record in a file through a plan compiled from the WRITER's
-- layout; `foreign` is nil for the identity plan (docs/FIXED-FORM-ALGORITHM.md §4).
function F.load(d: any, fixed: Fixed, file: buffer, foreign: Fixed?): { any }
	local parsed = parseFile(file)
	local writer = foreign or fixed
	if foreign == nil and parsed.hash ~= fixed.hash then
		error(refuse("layout_newer"), 0)
	end
	local root = compile(writer)
	local out = {}
	local p = parsed.at
	while p + 8 + writer.recordBytes <= buffer.len(file) do
		local record = buffer.create(writer.recordBytes)
		buffer.copy(record, 0, file, p + 8, writer.recordBytes)
		table.insert(out, readStruct(d, root, record, 0))
		p = p + 8 + writer.recordBytes
	end
	return out
end

-- check validates a layout the way §1.1's seven rules do and names the first
-- refusal; it never parses a record.
function F.check(fixed: Fixed): any
	local ok, err = pcall(function()
		local root = compile(fixed)
		if root.kind ~= 13 then error(refuse("layout_kind_invalid"), 0) end
	end)
	if ok then return nil end
	if type(err) == "table" and (err :: any).name ~= nil then return err end
	return refuse("layout_malformed")
end

-- fixed_layout is the FIXED FORM's layout constructor (docs/SPEC-TABLES.md
-- §3.4): the walk's entries as frozen data a load path reads to build its plan.
-- A layout is a constant, exactly as a descriptor is.
function F.fixed_layout(entries: { any }): { any }
	table.freeze(entries :: any)
	return entries
end

return table.freeze(F)
