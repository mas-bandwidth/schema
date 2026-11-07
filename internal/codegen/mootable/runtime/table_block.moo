--!strict
-- table_block.moo — THE BLOCK FORM'S BUILDER (docs/SPEC-TABLES.md §19.1), the
-- moo leg of what Go's generated <Table>BlockBegin and its row writers do.
-- Emitted beside table_blockread.moo when a table of the unit carries the
-- block form (§2.12), against the same <Name>BlockInfo descriptors
-- (internal/codegen/mootable blockform.go, from ir's one layout model, §19.3).
--
-- Begin lays one extent out from its counts: it stamps the prologue (the
-- magic in the producer's NATIVE order, the build version, the byte order),
-- chooses each out-of-line array's start with the layout model's own walk
-- (the projection's size, then each array rounded up to the greater of the
-- block's 64 and its element's alignment, in declaration order), and writes
-- that start with the count and the stride as the array's (offset_of, count,
-- stride) triple in the projection, at the field's own triple offset. It
-- writes NOTHING else — no row, no padding (§19.1) — and every count is
-- checked before one byte of the extent is touched: a count past its declared
-- bound refuses, naming the array, the count and the maximum.
--
-- Write fills the rows of a built block from a value shaped as table_blockread
-- Read answers it (a record keyed by field NAME, an optional's presence at
-- "<name>#present", an out-of-line array a one-based list of rows, a float its
-- bits, an eight- or sixteen-byte integer a serialize.moo wide value). A field
-- the value does not carry is left as it is: zero in a fresh block. A float
-- whose bits are a NaN or an infinity is never written: the write refuses it as
-- non_finite_float, naming the field (§3.3). A refused Write leaves the block
-- partly filled: discard it.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local V = require("./table_values")

local B = {}

local MAGIC_LO, MAGIC_HI = 0x4D484353, 0x4B4C4241
local ALIGN = 64

local function roundUp(n: number, a: number): number
	local r = n % a
	if r == 0 then
		return n
	end
	return n + (a - r)
end

-- Begin(info, counts, refusal): the block and its used extent, or nil and the
-- refusal filled in { name, count, maximum } for the first array whose count is
-- negative or past its declared bound. counts is keyed by array field name; an
-- array it does not name is empty.
function B.Begin(info: any, counts: { [string]: number }, refusal: any?): (any, number?)
	for _, f in info.fields do
		if f.outOfLine then
			local c = counts[f.name] or 0
			if c < 0 or c > f.bound then
				if refusal ~= nil then
					local r: any = refusal
					r.name = f.name
					r.count = c
					r.maximum = f.bound
				end
				return nil, nil
			end
		end
	end
	local offset = info.size
	local starts: { [string]: number } = {}
	for _, f in info.fields do
		if f.outOfLine then
			offset = roundUp(offset, f.startAlign)
			starts[f.name] = offset
			offset += (counts[f.name] or 0) * f.stride
		end
	end
	local bytes = roundUp(offset, ALIGN)
	local buf = buffer.create(bytes)
	buffer.writeu32(buf, 0, MAGIC_LO)
	buffer.writeu32(buf, 4, MAGIC_HI)
	local build = info.buildVersion
	buffer.writeu32(buf, 8, build[1])
	buffer.writeu32(buf, 12, build[2])
	buffer.writeu32(buf, 16, 1)
	for _, f in info.fields do
		if f.outOfLine then
			local start = starts[f.name]
			buffer.writeu32(buf, f.offsetOfOffset, start % V.TWO32)
			buffer.writeu32(buf, f.offsetOfOffset + 4, start // V.TWO32)
			buffer.writeu32(buf, f.countOffset, counts[f.name] or 0)
			buffer.writeu32(buf, f.strideOffset, f.stride)
		end
	end
	return { buffer = buf, base = 0, bytes = bytes, info = info, refusal = nil }, bytes
end

local function refuse(name: string, owner: string, field: string): any
	return { name = name, field = owner .. "." .. field }
end

local writeRecord: any

-- one scalar slot, at the width and kind the descriptors carry; nil, or the
-- refusal
local function scalar(buf: buffer, owner: string, f: any, at: number, v: any): any
	local k = f.kind
	if k == 1 then
		buffer.writeu8(buf, at, if v then 1 else 0)
		return nil
	elseif k == 10 then
		if not V.finite32(v) then
			return refuse("non_finite_float", owner, f.name)
		end
		buffer.writeu32(buf, at, v)
		return nil
	elseif k == 11 then
		if not V.finite64(v[1], v[2]) then
			return refuse("non_finite_float", owner, f.name)
		end
		buffer.writeu32(buf, at, v[1])
		buffer.writeu32(buf, at + 4, v[2])
		return nil
	end
	local n = f.elemSize
	if n == 0 then
		n = f.size
	end
	local signed = (k >= 2 and k <= 5) or k == 18 or (k >= 20 and k <= 24)
	if n == 1 then
		if signed then
			buffer.writei8(buf, at, v)
		else
			buffer.writeu8(buf, at, v)
		end
	elseif n == 2 then
		if signed then
			buffer.writei16(buf, at, v)
		else
			buffer.writeu16(buf, at, v)
		end
	elseif n == 4 then
		if signed then
			buffer.writei32(buf, at, v)
		else
			buffer.writeu32(buf, at, v)
		end
	else
		for i = 1, n // 4 do
			buffer.writeu32(buf, at + (i - 1) * 4, v[i])
		end
	end
	return nil
end

-- one field's value into the record at the base `at`; nil, or the refusal
local function writeField(block: any, info: any, f: any, at: number, v: any): any
	local buf: buffer = block.buffer
	if f.outOfLine then
		local offsetOf, count, stride = B.Triple(block, f)
		if #v ~= count then
			return refuse("row_count", info.name, f.name)
		end
		for i = 0, count - 1 do
			local why = writeRecord(block, f.element, block.base + offsetOf + i * stride, v[i + 1])
			if why ~= nil then
				return why
			end
		end
		return nil
	end
	local slot = at + f.offset
	if f.counted then
		local width = if f.wide then 2 else 1
		local len = if type(v) == "string" then #v else buffer.len(v)
		local used = len // width
		if used > f.arrayBound then
			return refuse("over_bound", info.name, f.name)
		end
		if type(v) == "string" then
			buffer.writestring(buf, slot, v)
		else
			buffer.copy(buf, slot, v, 0, len)
		end
		buffer.writei32(buf, at + f.countOffset, used)
		return nil
	end
	if f.isArray then
		for s = 0, f.arrayBound - 1 do
			local one = slot + s * f.elemSize
			local why
			if f.element ~= nil then
				why = writeRecord(block, f.element, one, v[s + 1])
			else
				why = scalar(buf, info.name, f, one, v[s + 1])
			end
			if why ~= nil then
				return why
			end
		end
		return nil
	end
	if f.element ~= nil then
		return writeRecord(block, f.element, slot, v)
	end
	return scalar(buf, info.name, f, slot, v)
end

writeRecord = function(block: any, info: any, at: number, value: any): any
	for _, f in info.fields do
		local v = value[f.name]
		if v ~= nil then
			local why = writeField(block, info, f, at, v)
			if why ~= nil then
				return why
			end
		end
		if f.optional then
			local present = value[f.name .. "#present"]
			if present ~= nil then
				buffer.writeu8(block.buffer, at + f.presentOffset, if present then 1 else 0)
			end
		end
	end
	return nil
end

-- Triple(block, f): one out-of-line array's (offset_of, count, stride) as
-- Begin wrote them
function B.Triple(block: any, f: any): (number, number, number)
	local buf: buffer = block.buffer
	local at: number = block.base
	return buffer.readu32(buf, at + f.offsetOfOffset) + buffer.readu32(buf, at + f.offsetOfOffset + 4) * V.TWO32, buffer.readu32(buf, at + f.countOffset), buffer.readu32(buf, at + f.strideOffset)
end

-- Write(block, value): the projection's inline fields and every out-of-line
-- array's rows, from a value shaped as Read answers it. nil, or the refusal
-- { name, field }.
function B.Write(block: any, value: any): any
	return writeRecord(block, block.info, block.base, value)
end

return table.freeze(B)
