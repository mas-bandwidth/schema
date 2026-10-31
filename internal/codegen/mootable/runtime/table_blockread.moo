--!strict
-- table_blockread.moo — THE BLOCK FORM'S OPEN AND REFLECTIVE READ
-- (docs/SPEC-TABLES.md §19.1, §19.2, §19.4), the moo leg of what Go's
-- generated <Table>Block.Open and its descriptors answer. Emitted beside the
-- unit's <Base>Table.moo modules when a table of the unit carries the block
-- form (§2.12); each block table's <Name>BlockInfo is its projection as data
-- (internal/codegen/mootable blockform.go, from ir's one layout model, §19.3).
--
-- A block is memory another build wrote. BlockOpen checks it ONCE — the magic,
-- the byte order the prologue carries, the build version, each out-of-line
-- array's pitch, count, offset_of and extent, the used extent against the
-- bytes the caller passed, and last the base's alignment — and answers a
-- handle, or nil and the first failing clause's name, the SAME name Go's
-- Open answers (§19.2). Every number out of the instance is read from the
-- instance; the descriptor's stride is a constant this build asserts against.
--
-- The read is reflective: Read walks a projection, or ReadAt a row, through
-- the descriptors alone, into a value keyed by field NAME (an optional's
-- presence at "<name>#present"), in moo's values (docs/PORT-MOO.md): a float
-- is its bits, an eight- or sixteen-byte integer a serialize.moo wide value.
-- A float whose bits are a NaN or an infinity is never returned: the read
-- refuses it as non_finite_float, naming the field, and the refusal is latched
-- on the handle (§3.3).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local V = require("./table_values")

local B = {}

-- §19.1's magic "SCHMABLK", stored in the producer's NATIVE order, as its two
-- little-endian halves, and the byte reversal a foreign build reads back
local MAGIC_LO, MAGIC_HI = 0x4D484353, 0x4B4C4241
local REVERSED_LO, REVERSED_HI = 0x41424C4B, 0x5343484D
local ALIGN = 64

local function freezeInfo(info: any)
	for _, f in info.fields do
		if f.element ~= nil then
			freezeInfo(f.element)
		end
		table.freeze(f)
	end
	table.freeze(info.fields)
	if info.forms ~= nil then
		table.freeze(info.forms)
	end
	table.freeze(info)
end

-- info freezes one block descriptor graph (a descriptor is a constant).
function B.info(info: any): any
	freezeInfo(info)
	return info
end

-- one u64 word as a number, or V.HUGE when it does not fit one (a forged
-- offset near 2^63 is past every extent, which is all a check needs to know)
local function word(b: buffer, at: number): number
	local lo, hi = buffer.readu32(b, at), buffer.readu32(b, at + 4)
	if hi >= 32768 then
		return V.HUGE
	end
	return hi * V.TWO32 + lo
end

-- BlockOpen(info, buffer, base, extent): the handle, or nil and the first
-- failing clause's name (docs/SPEC-TABLES.md §19.2). base is the block's byte
-- offset inside buffer, extent how many bytes from there the caller claims. A
-- nil buffer is a null base and answers unaligned_base, as Go's Open does. A
-- table whose forms declaration leaves the block form out refuses by name.
function B.BlockOpen(info: any, data: buffer?, base: number?, extent: number?): (any, string?)
	if info.forms ~= nil and info.forms.block ~= true then
		error("schema: table " .. info.name .. " does not carry block (forms declaration)", 2)
	end
	if data == nil then
		return nil, "unaligned_base"
	end
	local buf = data :: buffer
	local at = base or 0
	local bytes = extent or (buffer.len(buf) - at)
	if bytes < info.size or at + bytes > buffer.len(buf) then
		return nil, "truncated"
	end
	local lo, hi = buffer.readu32(buf, at), buffer.readu32(buf, at + 4)
	if lo ~= MAGIC_LO or hi ~= MAGIC_HI then
		if lo == REVERSED_LO and hi == REVERSED_HI then
			return nil, "foreign_order"
		end
		return nil, "not_a_cook"
	end
	if buffer.readu32(buf, at + 16) ~= 1 or buffer.readu32(buf, at + 20) ~= 0 then
		return nil, "not_a_cook"
	end
	local build = info.buildVersion
	if buffer.readu32(buf, at + 8) ~= build[1] or buffer.readu32(buf, at + 12) ~= build[2] then
		return nil, "wrong_build_version"
	end
	local used = info.size
	for _, f in info.fields do
		if f.outOfLine then
			local offsetOf = word(buf, at + f.offsetOfOffset)
			local count = buffer.readu32(buf, at + f.countOffset)
			local stride = buffer.readu32(buf, at + f.strideOffset)
			if stride ~= f.stride then
				return nil, "bad_layout"
			end
			-- past the DECLARED MAXIMUM, whatever the extent
			if count > f.bound then
				return nil, "bad_layout"
			end
			if offsetOf < info.size or offsetOf % f.startAlign ~= 0 then
				return nil, "bad_layout"
			end
			if offsetOf > bytes then
				return nil, "bad_layout"
			end
			local rows = count * stride -- both bounded above: this cannot carry
			if rows > bytes - offsetOf then
				return nil, "bad_layout"
			end
			if offsetOf + rows > used then
				used = offsetOf + rows
			end
		end
	end
	-- the used extent, rounded to sixty-four, the padding paid out of the
	-- slack that is left
	local padding = (ALIGN - (used % ALIGN)) % ALIGN
	if padding > bytes - used then
		return nil, "truncated"
	end
	used += padding
	-- the base's alignment is LAST: it is the one clause that reads nothing
	-- out of the block (§19.2)
	if at % ALIGN ~= 0 then
		return nil, "unaligned_base"
	end
	return { buffer = buf, base = at, bytes = used, info = info, refusal = nil }, nil
end

-- the refusal a read latches on the handle, naming the field (§3.3)
local function refuse(block: any, owner: string, f: any): any
	if block.refusal == nil then
		block.refusal = { name = "non_finite_float", field = owner .. "." .. f.name }
	end
	return nil
end

-- one scalar slot at the width and kind the descriptors carry (§8.1, §19.2):
-- an enum's slot holds the ORDINAL at its own storage width, read unsigned
local function scalar(block: any, owner: string, f: any, at: number): any
	local buf = block.buffer
	local k = f.kind
	if k == 1 then
		return buffer.readu8(buf, at) ~= 0
	elseif k == 10 then
		local bits = buffer.readu32(buf, at)
		if not V.finite32(bits) then
			return refuse(block, owner, f)
		end
		return bits
	elseif k == 11 then
		local lo, hi = buffer.readu32(buf, at), buffer.readu32(buf, at + 4)
		if not V.finite64(lo, hi) then
			return refuse(block, owner, f)
		end
		return V.Uint64.new(lo, hi)
	end
	local n = f.elemSize
	if n == 0 then
		n = f.size
	end
	local signed = (k >= 2 and k <= 5) or k == 18 or (k >= 20 and k <= 24)
	if n == 1 then
		return if signed then buffer.readi8(buf, at) else buffer.readu8(buf, at)
	elseif n == 2 then
		return if signed then buffer.readi16(buf, at) else buffer.readu16(buf, at)
	elseif n == 4 then
		return if signed then buffer.readi32(buf, at) else buffer.readu32(buf, at)
	elseif n == 8 then
		local C = if signed then V.Int64 else V.Uint64
		return C.new(buffer.readu32(buf, at), buffer.readu32(buf, at + 4))
	end
	local C = if signed then V.Int128 else V.Uint128
	return C.new(buffer.readu32(buf, at), buffer.readu32(buf, at + 4), buffer.readu32(buf, at + 8), buffer.readu32(buf, at + 12))
end

-- Triple(block, f): one out-of-line array's (offset_of, count, stride), from
-- the INSTANCE (§19.2)
function B.Triple(block: any, f: any): (number, number, number)
	local at = block.base
	return word(block.buffer, at + f.offsetOfOffset), buffer.readu32(block.buffer, at + f.countOffset), buffer.readu32(block.buffer, at + f.strideOffset)
end

local readRecord: any

-- one field's value at the record base `at`
local function readField(block: any, info: any, f: any, at: number): any
	local buf = block.buffer
	if f.outOfLine then
		local offsetOf, count, stride = B.Triple(block, f)
		local rows = table.create(count)
		for i = 0, count - 1 do
			rows[i + 1] = readRecord(block, f.element, block.base + offsetOf + i * stride)
		end
		return rows
	end
	local value = at + f.offset
	if f.counted then
		-- text and bytes: the int32 used length is the field's count column,
		-- bounded by the declaration
		local used = buffer.readi32(buf, at + f.countOffset)
		if used < 0 or used > f.arrayBound then
			used = if used < 0 then 0 else f.arrayBound
		end
		if f.kind == 12 then
			return buffer.readstring(buf, value, used)
		end
		local width = if f.wide then 2 else 1
		local out = buffer.create(used * width)
		buffer.copy(out, 0, buf, value, used * width)
		return out
	end
	if f.isArray then
		local slots = table.create(f.arrayBound)
		for s = 0, f.arrayBound - 1 do
			local slot = value + s * f.elemSize
			if f.element ~= nil then
				slots[s + 1] = readRecord(block, f.element, slot)
			else
				slots[s + 1] = scalar(block, info.name, f, slot)
			end
		end
		return slots
	end
	if f.element ~= nil then
		return readRecord(block, f.element, value)
	end
	return scalar(block, info.name, f, value)
end

readRecord = function(block: any, info: any, at: number): { [string]: any }
	local out: { [string]: any } = {}
	for _, f in info.fields do
		out[f.name] = readField(block, info, f, at)
		if f.optional then
			out[f.name .. "#present"] = buffer.readu8(block.buffer, at + f.presentOffset) ~= 0
		end
	end
	return out
end

-- Read(block): the projection's value, out-of-line arrays as one-based lists
-- of rows, or nil and the latched refusal when a float is not finite.
function B.Read(block: any): (any, any)
	if block.refusal ~= nil then
		return nil, block.refusal
	end
	local out = readRecord(block, block.info, block.base)
	if block.refusal ~= nil then
		return nil, block.refusal
	end
	return out, nil
end

-- ReadAt(block, info, at): one record (a row, a nested record) at an absolute
-- offset of the block's buffer, the same rules as Read.
function B.ReadAt(block: any, info: any, at: number): (any, any)
	if block.refusal ~= nil then
		return nil, block.refusal
	end
	local out = readRecord(block, info, at)
	if block.refusal ~= nil then
		return nil, block.refusal
	end
	return out, nil
end

return table.freeze(B)
