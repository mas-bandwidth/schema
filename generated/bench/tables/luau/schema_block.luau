--!strict
-- schema_block.luau — the block form's builder (docs/SPEC-TABLES.md §19.1), the
-- Luau leg of what blockbuild.go emits on the Go side. It lays one extent out
-- from its counts the way the generated Go builder does: it stamps the
-- prologue, places each out-of-line array's (offset_of, count, stride) triple
-- in the projection at the field's own TripleOffset, and refuses a producer
-- count past its declared bound by name. It writes only the prologue and
-- triples — never rows or padding (docs/SPEC-TABLES.md §19.1).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local BLOCK_ALIGN = 64

local SHIFT32 = 4294967296

-- putU64 writes a uint64 little-endian as two u32 halves: the pinned Luau
-- interpreter's buffer library has no 64-bit integer writers, so the caller
-- supplies the low and high words (docs/SPEC-TABLES.md §19.1's prologue), and
-- an offset word is split with 64-bit-safe float arithmetic below.
local function putU64(buf: buffer, at: number, lo: number, hi: number)
	buffer.writeu32(buf, at, lo)
	buffer.writeu32(buf, at + 4, hi)
end

local function alignUp(n: number, a: number): number
	local r = n % a
	if r == 0 then
		return n
	end
	return n + (a - r)
end

-- elemAlign derives an element's alignment from its stride the way the schema's
-- layout model does: the greatest power of two that divides the stride, bounded
-- by 16 (ir/blocklayout.go's elemAlignOf). The stride is a multiple of it.
local function elemAlign(stride: number): number
	local a = 1
	while a < 16 and stride % (a * 2) == 0 do
		a = a * 2
	end
	return a
end

export type Refusal = {
	name: string,
	count: number,
	maximum: number,
}

export type BlockParams = {
	magicLo: number,
	magicHi: number,
	buildVersionLo: number,
	buildVersionHi: number,
	byteOrder: number,
	size: number,
	arrays: { { name: string, max: number, stride: number, triple: number } },
}

-- Begin lays a block out from its counts (docs/SPEC-TABLES.md §19.1). It
-- returns the buffer and the used extent rounded up to 64; on an over-count or
-- negative count it returns nil and, when refusal is supplied, fills it with
-- the array's name, the count and the maximum.
local function Begin(p: BlockParams, counts: { [string]: number }, refusal: Refusal?): (buffer?, number?)
	for _, a in p.arrays do
		local c = counts[a.name] or 0
		if c < 0 or c > a.max then
			if refusal ~= nil then
				refusal.name = a.name
				refusal.count = c
				refusal.maximum = a.max
			end
			return nil, nil
		end
	end

	local starts: { number } = {}
	local offset = p.size
	for i, a in ipairs(p.arrays) do
		local al = elemAlign(a.stride)
		if al < BLOCK_ALIGN then
			al = BLOCK_ALIGN
		end
		offset = alignUp(offset, al)
		starts[i] = offset
		local c = counts[a.name] or 0
		offset = offset + c * a.stride
	end

	local bytes = alignUp(offset, BLOCK_ALIGN)
	if bytes < alignUp(p.size, BLOCK_ALIGN) then
		bytes = alignUp(p.size, BLOCK_ALIGN)
	end

	local buf = buffer.create(bytes)
	putU64(buf, 0, p.magicLo, p.magicHi)
	putU64(buf, 8, p.buildVersionLo, p.buildVersionHi)
	putU64(buf, 16, p.byteOrder, 0)
	-- The triple lives in the projection, where ir.BlockArray.TripleOffset puts
	-- it; starts[i] is only the triple's OffsetOf value. Writing at starts[i]
	-- would overwrite the first rows' bytes (docs/SPEC-TABLES.md §19.1).
	for i, a in ipairs(p.arrays) do
		local s = starts[i]
		local at = a.triple
		local lo = s % SHIFT32
		local hi = (s - lo) / SHIFT32
		putU64(buf, at, lo, hi)
		local c = counts[a.name] or 0
		buffer.writeu32(buf, at + 8, c)
		buffer.writeu32(buf, at + 12, a.stride)
	end
	return buf, bytes
end

return table.freeze({
	Begin = Begin,
	elemAlign = elemAlign,
	alignUp = alignUp,
	BLOCK_ALIGN = BLOCK_ALIGN,
})
