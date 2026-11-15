--!strict
-- schema_blockread.luau — THE BLOCK FORM'S REFLECTIVE READ (docs/SPEC-TABLES.md §19.2,
-- §19.4). Emitted beside the unit's <Base>Table.luau modules by
-- internal/codegen/luautable, which embeds runtime/*.luau by glob, and
-- required as require("./schema_blockread").
--
-- A block is memory another build wrote. BlockOpen checks it ONCE — the magic,
-- the byte order the prologue carries, the build version, each out-of-line
-- array's pitch, count, offset_of and extent, the used extent against the
-- bytes the caller passed, and last the base's alignment — and points at it.
-- A match means the bytes were written by a build with this layout; anything
-- else refuses by the SAME name Go's BlockOpen answers (§19.2, §19.4), and the
-- returned handle points at nothing.
--
-- EVERY NUMBER OUT OF THE INSTANCE IS READ FROM THE INSTANCE. The descriptor's
-- stride is a constant this build asserts against, never what a row is indexed
-- with (§19.2, §19.4). Read walks a projection or a row through the descriptor
-- graph alone: offsets, sizes, kinds and the element column are data (§8.1).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

export type Field = {
	name: string,
	offset: number,
	size: number,
	kind: number,
	outOfLine: boolean?,
	offsetOfOffset: number?,
	countOffset: number?,
	strideOffset: number?,
	stride: number?,
	elemSize: number?,
	elemAlign: number?,
	bound: number?,
	isArray: boolean?,
	arrayBound: number?,
	optional: boolean?,
	presentOffset: number?,
	element: any?,
}

export type Info = {
	name: string,
	buildVersion: integer,
	size: number,
	align: number,
	fields: { Field },
}

export type Block = {
	buffer: buffer,
	base: number,
	bytes: number,
	info: Info,
	-- the first float an accessor met holding a NaN or an infinity, latched
	-- as <Owner>.<field> (docs/SPEC-TABLES.md §3.3, ruling 2 of 2026-10-27)
	nonFiniteFloat: string?,
}

local B = {}

-- §19.1's magic, stored in the producer's NATIVE order, and its byte reversal.
-- The pinned Luau's buffer library reads little-endian words, which is the
-- order this build emits (§20.3: one little, two big).
local MAGIC: integer = 0x4B4C42414D484353i
local MAGIC_REVERSED: integer = 0x5343484D41424C4Bi
local BYTE_ORDER: integer = 1i
local ALIGN = 64

-- BlockOpen(info, buffer, base, extent): the handle, or nil and the first
-- failing clause's name (docs/SPEC-TABLES.md §19.2). `base` is the byte
-- offset of the block inside `buffer`; `extent` is how many bytes from there
-- belong to the caller's block. A nil buffer is a null base, and answers
-- unaligned_base exactly as Go's BlockOpen does.
function B.BlockOpen(info: Info, data: buffer?, base: number?, extent: number?): (Block?, string?)
	if data == nil then
		return nil, "unaligned_base"
	end
	local buf = data :: buffer
	local at = base or 0
	local bytes = extent or (buffer.len(buf) - at)
	if bytes < info.size then
		return nil, "truncated"
	end
	local magic = buffer.readinteger(buf, at)
	if magic ~= MAGIC then
		if magic == MAGIC_REVERSED then
			return nil, "foreign_order"
		end
		return nil, "not_a_cook"
	end
	if buffer.readinteger(buf, at + 16) ~= BYTE_ORDER then
		return nil, "not_a_cook"
	end
	if buffer.readinteger(buf, at + 8) ~= info.buildVersion then
		return nil, "wrong_build_version"
	end
	local used = info.size
	for _, f in info.fields do
		if f.outOfLine then
			local offsetOf = integer.tonumber(buffer.readinteger(buf, at + (f.offsetOfOffset :: number)))
			local count = buffer.readu32(buf, at + (f.countOffset :: number))
			local stride = buffer.readu32(buf, at + (f.strideOffset :: number))
			if offsetOf < 0 or stride ~= f.stride then
				return nil, "bad_layout"
			end
			if count > (f.bound :: number) then
				return nil, "bad_layout"
			end
			local align = f.elemAlign or ALIGN
			if align < ALIGN then
				align = ALIGN
			end
			if offsetOf < info.size or offsetOf % align ~= 0 then
				return nil, "bad_layout"
			end
			if offsetOf > bytes then
				return nil, "bad_layout"
			end
			local rows = count * stride -- both bounded above: this cannot carry
			if rows > bytes - offsetOf then
				return nil, "bad_layout"
			end
			local ending = offsetOf + rows
			if ending > used then
				used = ending
			end
		end
	end
	-- the used extent, rounded to sixty-four WITHOUT the rounding wrapping:
	-- used is already inside the extent, and the padding is paid out of the
	-- slack that is left rather than added and compared after
	local padding = (ALIGN - (used % ALIGN)) % ALIGN
	if padding > bytes - used then
		return nil, "truncated"
	end
	used = used + padding
	-- the base's alignment is LAST: it is the one clause that reads nothing
	-- out of the block (§19.2)
	if at % ALIGN ~= 0 then
		return nil, "unaligned_base"
	end
	return { buffer = buf, base = at, bytes = used, info = info }, nil
end

-- one scalar slot at the width and kind the descriptors carry (§8.1, §19.2).
-- An enum's slot holds the ORDINAL at its own derived storage width, which is
-- why the fallback reads elem_size rather than the kind's wire width.
-- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; the same rule
-- as packets): a NaN or an infinity is not a value of the type.
-- An accessor that meets one never returns it: it latches non_finite_float on
-- the block, naming <Owner>.<field> (the first one wins), and answers +0.
local _block: Block? = nil
local _owner = "?"
local function nonFinite(f: Field): number
	local b = _block
	if b ~= nil and b.nonFiniteFloat == nil then b.nonFiniteFloat = _owner .. "." .. f.name end
	return 0.0
end

local function scalar(buf: buffer, at: number, f: Field): any
	local k = f.kind
	if k == 1 then
		return buffer.readu8(buf, at) ~= 0
	elseif k == 2 then
		return buffer.readi8(buf, at)
	elseif k == 3 then
		return buffer.readi16(buf, at)
	elseif k == 4 then
		return buffer.readi32(buf, at)
	elseif k == 5 then
		return buffer.readinteger(buf, at)
	elseif k == 6 then
		return buffer.readu8(buf, at)
	elseif k == 7 then
		return buffer.readu16(buf, at)
	elseif k == 8 then
		return buffer.readu32(buf, at)
	elseif k == 9 then
		return buffer.readinteger(buf, at)
	elseif k == 10 then
		if bit32.band(buffer.readu32(buf, at), 0x7F800000) == 0x7F800000 then return nonFinite(f) end
		return buffer.readf32(buf, at)
	elseif k == 11 then
		if bit32.band(buffer.readu32(buf, at + 4), 0x7FF00000) == 0x7FF00000 then return nonFinite(f) end
		return buffer.readf64(buf, at)
	end
	local n = f.elemSize or f.size
	if n == 1 then
		return buffer.readu8(buf, at)
	elseif n == 2 then
		return buffer.readu16(buf, at)
	elseif n == 4 then
		return buffer.readu32(buf, at)
	elseif n == 8 then
		return buffer.readinteger(buf, at)
	end
	return buffer.readu8(buf, at)
end

-- one out-of-line array's rows, at the pitch the INSTANCE gives and for the
-- count the INSTANCE gives (§19.2). No call site re-derives the pitch rule.
local readRecord: (Info, buffer, number) -> { [string]: any }

local function readArray(f: Field, buf: buffer, at: number): { any }
	local offsetOf = integer.tonumber(buffer.readinteger(buf, at + (f.offsetOfOffset :: number)))
	local count = buffer.readu32(buf, at + (f.countOffset :: number))
	local stride = buffer.readu32(buf, at + (f.strideOffset :: number))
	local rows: { any } = {}
	for i = 0, count - 1 do
		rows[i + 1] = readRecord(f.element :: Info, buf, at + offsetOf + i * stride)
	end
	return rows
end

readRecord = function(info: Info, buf: buffer, at: number): { [string]: any }
	local out: { [string]: any } = {}
	for _, f in info.fields do
		_owner = info.name
		if f.outOfLine then
			out[f.name] = readArray(f, buf, at)
		elseif f.optional and f.presentOffset ~= nil and buffer.readu8(buf, at + f.presentOffset) == 0 then
			out[f.name] = nil
		elseif f.element ~= nil then
			if f.isArray then
				local n = f.arrayBound or 0
				if f.countOffset ~= nil then
					n = buffer.readu32(buf, at + f.countOffset)
				end
				local elem = f.element :: Info
				local stride = f.elemSize or elem.size
				local rows: { any } = {}
				for i = 0, n - 1 do
					rows[i + 1] = readRecord(elem, buf, at + f.offset + i * stride)
				end
				out[f.name] = rows
			else
				out[f.name] = readRecord(f.element :: Info, buf, at + f.offset)
			end
		elseif f.kind == 12 or f.kind == 14 then
			-- text and bytes: the used length companion is the field's own
			-- count column; hand back the bytes where they lie
			local n = f.countOffset ~= nil and buffer.readu32(buf, at + f.countOffset) or 0
			if n > (f.arrayBound or 0) then
				n = f.arrayBound or 0
			end
			local view = buffer.create(n)
			buffer.copy(view, 0, buf, at + f.offset, n)
			out[f.name] = view
		else
			out[f.name] = scalar(buf, at + f.offset, f)
		end
	end
	return out
end

-- Read(info, block) walks one record — a table's projection, or a row — into
-- a value keyed by field name, using the descriptors alone (docs/SPEC-TABLES.md
-- §19.2). An out-of-line array answers a one-based list of row values.
function B.Read(info: Info, block: Block): { [string]: any }
	_block = block
	local out = readRecord(info, block.buffer, block.base)
	_block = nil
	return out
end

-- NonFiniteFloat(block) is the latched refusal, "non_finite_float: <Owner>.<field>",
-- or nil while every float an accessor read was finite (docs/SPEC-TABLES.md §3.3).
function B.NonFiniteFloat(block: Block): string?
	if block.nonFiniteFloat == nil then return nil end
	return "non_finite_float: " .. block.nonFiniteFloat
end

return table.freeze(B)
