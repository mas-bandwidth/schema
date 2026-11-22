--!strict
-- schema_message.luau — the Luau table runtime's MESSAGE FORM (docs/SPEC-TABLES.md
-- §3.3), form byte 2: a batch of bitpacked bodies under one announced
-- vocabulary. The announcement is an ordinary form-1 file whose body carries
-- the build version and the vocabulary; the batch is the form byte, the
-- count, and one continuous bit stream. This file is the one walk both
-- directions share; it reads the announced entries and the descriptors
-- luautable.go emits as data (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
--
-- THE FORM IS FIXED TABLES ONLY (§3.3): a root that is not a fixed table is
-- refused by name before a byte is read or written, and a fixed root's body
-- carries no node table.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")

local M = {}

local BATCH_MAX = 256
-- the receiver's conforming default entry bound (ir.TableVocabularyMaxEntries)
local DEFAULT_MAX_ENTRIES = 4096
local NODE_ID: integer = -1i          -- 0xffffffffffffffff
local VERSION_ID: integer = -2i       -- 0xfffffffffffffffe
local VOCAB_ID: integer = -3i         -- 0xfffffffffffffffd

-- CRC-32C, the Castagnoli polynomial, reflected 0x82F63B78, initial and final
-- 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14). schema_refusal.luau keeps the same routine
-- local to itself; requiring it here would not cycle, but it exports no CRC
-- and this card's PATHS do not include schema_refusal.luau, so the routine is copied
-- rather than moved.
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

-- §2.14's order for a batch: the form byte has been read, so a batch too short
-- to hold the trailer, or whose last four bytes are not the CRC-32C of the
-- bytes before them, is whole-batch damage named integrity_mismatch. The
-- trailer then comes off before any body is decoded.
local function integrityOpen(data: buffer): buffer?
	local n = buffer.len(data)
	if n < 6 or buffer.readu32(data, n - 4) ~= crc32c(data, n - 4) then return nil end
	local out = buffer.create(n - 4)
	buffer.copy(out, 0, data, 0, n - 4)
	return out
end

local function bitsReq(lo: number, hi: number): number
	if hi <= lo then return 0 end
	local n = hi - lo
	local bits = 0
	while n ~= 0 do
		bits = bits + 1
		n = math.floor(n / 2)
	end
	return bits
end

-- ---- the bit stream (docs/SPEC-TABLES.md §3.3: bit i is bit i%8 of byte i/8,
-- low bit first, and every align is relative to the batch buffer's byte 0) ----

local function newReader(b: buffer, off: number, count: number?)
	local n = if count ~= nil then (count :: number) * 8 else buffer.len(b) * 8
	return { b = b, off = off, n = n }
end

local function get(r: any, width: number): (number, boolean)
	if width < 0 or width > 32 then return 0, false end
	if r.off + width > r.n then return 0, false end
	local v = 0
	local at = math.floor(r.off / 8)
	local sh = r.off % 8
	local got = 0
	while got < width do
		local take = math.min(8 - sh, width - got)
		local byte = buffer.readu8(r.b, at)
		local chunk = math.floor(byte / 2 ^ sh) % 2 ^ take
		v = v + chunk * 2 ^ got
		got = got + take
		sh = sh + take
		if sh == 8 then sh = 0; at = at + 1 end
	end
	r.off = r.off + width
	return v, true
end

local function skip(r: any, width: number): boolean
	if width < 0 or r.off + width > r.n then return false end
	r.off = r.off + width
	return true
end

-- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; the same rule
-- as packets): a known float field's raw bits are tested as the walk steps
-- over them, and a NaN or an infinity is recorded, the first one named
-- <Owner>.<field>, for the batch to refuse whole as non_finite_float. A
-- quantized float rides its step index, which cannot spell one. The walk goes
-- on, so the framing is the one a finite batch meets.
local function floatStep(r: any, st: any, kind: number, fd: any): boolean
	local finite = true
	if kind == 10 then
		local bits, ok = get(r, 32)
		if not ok then return false end
		finite = bit32.band(bits, 0x7F800000) ~= 0x7F800000
	else
		local _, lok = get(r, 32)
		local hi, hok = get(r, 32)
		if not lok or not hok then return false end
		finite = bit32.band(hi, 0x7FF00000) ~= 0x7FF00000
	end
	if not finite and st.nonFinite == nil then
		st.nonFinite = (st.owner or "?") .. "." .. (if fd ~= nil then fd.name or "?" else "?")
	end
	return true
end

-- whether a shape is a plain float the walk must test: kind 10 unpacked, or 11
local function plainFloat(shape: any): boolean
	return (shape.kind == 10 and shape.packing == 0) or shape.kind == 11
end

-- A SKIPPED STRING'S BYTES ARE TEXT (docs/SPEC-TABLES.md §3, §4): a string
-- the walk does not keep is held to what a read of it holds — well-formed
-- UTF-8 with no zero byte — exactly as schema_load.luau's utf8_ok holds a
-- named field's bytes.
local function textOk(b: buffer, at: number, n: number): boolean
	local i = at
	local e = at + n
	while i < e do
		local b0 = buffer.readu8(b, i)
		if b0 == 0 then return false end
		if b0 < 0x80 then i = i + 1
		else
			local need, cp
			if b0 >= 0xC2 and b0 <= 0xDF then need, cp = 1, b0 - 0xC0
			elseif b0 >= 0xE0 and b0 <= 0xEF then need, cp = 2, b0 - 0xE0
			elseif b0 >= 0xF0 and b0 <= 0xF4 then need, cp = 3, b0 - 0xF0
			else return false end
			if i + need >= e then return false end
			for k = 1, need do
				local x = buffer.readu8(b, i + k)
				if x < 0x80 or x > 0xBF then return false end
				cp = cp * 64 + (x - 0x80)
			end
			local min = need == 1 and 0x80 or (need == 2 and 0x800 or 0x10000)
			if cp < min or cp > 0x10FFFF or (cp >= 0xD800 and cp <= 0xDFFF) then return false end
			i = i + 1 + need
		end
	end
	return true
end

-- put writes a reference back at a bit offset already read: only a deprecated
-- enum reference is ever rewritten, at the same width (docs/SPEC-TABLES.md §2.15).
local function put(r: any, at: number, width: number, value: number)
	if width <= 0 then return end
	local sh = at % 8
	local byteAt = math.floor(at / 8)
	local got = 0
	while got < width do
		local take = math.min(8 - sh, width - got)
		local byte = buffer.readu8(r.b, byteAt)
		local cur = math.floor(byte / 2 ^ sh) % 2 ^ take
		local chunk = math.floor(value / 2 ^ got) % 2 ^ take
		if chunk ~= cur then
			buffer.writeu8(r.b, byteAt, byte - cur * 2 ^ sh + chunk * 2 ^ sh)
		end
		got = got + take
		sh = sh + take
		if sh == 8 then sh = 0; byteAt = byteAt + 1 end
	end
end

local function align(r: any): boolean
	local width = (8 - r.off % 8) % 8
	if width == 0 then return true end
	local v, ok = get(r, width)
	return ok and v == 0
end

-- ---- the announcement's form-1 framing (docs/SPEC-TABLES.md §3.3) ----

local function rU8(r: any): number
	if r.at >= r.e then r.bad = true; return 0 end
	local v = buffer.readu8(r.b, r.at)
	r.at = r.at + 1
	return v
end

-- rLeb reads one canonical unsigned LEB128 (docs/SPEC-TABLES.md §3): every
-- reference and length in the announcement has one spelling, so a redundant
-- continuation (a last byte 0x00 after one, `0x80 0x00` for zero) and a tenth
-- byte above 1 are malformed, and a failed read sets bad and leaves the cursor
-- where it started.
local function rLeb(r: any): number
	local start = r.at
	local v = 0
	local s = 0
	for i = 0, 9 do
		if r.at >= r.e then r.bad = true; r.at = start; return 0 end
		local x = buffer.readu8(r.b, r.at)
		r.at = r.at + 1
		if i == 9 and x > 1 then r.bad = true; r.at = start; return 0 end
		v = v + (x % 0x80) * 2 ^ s
		if x < 0x80 then
			if i > 0 and x == 0 then r.bad = true; r.at = start; return 0 end
			return v
		end
		s = s + 7
	end
	r.bad = true
	r.at = start
	return 0
end

-- lebBits16 is the exact value of the LEB128 bytes b[from, to) as a 16-byte
-- little-endian buffer, with a zigzag decode when zigzag is set. rLeb answers
-- a double, which cannot hold a 64-bit base exactly (9223372036854775802
-- rounds to 2^63), so the base a ranged sum adds is built from the bytes.
local function lebBits16(b: buffer, from: number, to: number, zigzag: boolean): buffer
	local out = buffer.create(16)
	local bit = 0
	for i = from, to - 1 do
		local x = buffer.readu8(b, i) % 0x80
		for j = 0, 6 do
			if bit < 64 and bit32.band(x, bit32.lshift(1, j)) ~= 0 then
				local bi = bit // 8
				buffer.writeu8(out, bi, bit32.bor(buffer.readu8(out, bi), bit32.lshift(1, bit % 8)))
			end
			bit = bit + 1
		end
	end
	if zigzag then
		local neg = bit32.band(buffer.readu8(out, 0), 1) == 1
		-- the magnitude: the 64 bits shifted right by one
		for bi = 0, 7 do
			local cur = buffer.readu8(out, bi)
			local nxt = if bi < 7 then buffer.readu8(out, bi + 1) else 0
			buffer.writeu8(out, bi, bit32.bor(bit32.rshift(cur, 1), bit32.band(bit32.lshift(nxt, 7), 0xFF)))
		end
		if neg then
			-- -(mag + 1) == ~mag in two's complement
			for bi = 0, 7 do buffer.writeu8(out, bi, bit32.band(bit32.bnot(buffer.readu8(out, bi)), 0xFF)) end
			buffer.fill(out, 8, 0xFF, 8)
		end
	end
	return out
end


local function kindBits(k: number): number
	if k == 1 then return 1 end
	if k == 2 or k == 6 or k == 20 or k == 25 then return 8 end
	if k == 3 or k == 7 or k == 21 or k == 26 then return 16 end
	if k == 4 or k == 8 or k == 10 or k == 22 or k == 27 then return 32 end
	if k == 5 or k == 9 or k == 11 or k == 23 or k == 28 or k == 36 then return 64 end
	if k == 18 or k == 19 or k == 24 or k == 29 then return 128 end
	return 0
end


-- bitsLen32 is bits.Len32: the bits a step count's largest index needs.
local function bitsLen32(n: number): number
	local b = 0
	while n > 0 do
		b = b + 1
		n = math.floor(n / 2)
	end
	return b
end

-- asF32 rounds one arithmetic result to float32, which is the precision the
-- announced quantized triple is derived in (ir.TableMessageQuantization).
local f32scratch = buffer.create(4)
local function asF32(x: number): number
	buffer.writef32(f32scratch, 0, x)
	return buffer.readf32(f32scratch, 0)
end

local function newShape(): any
	return { kind = 0, packing = 0, bits = 0, min = 0, max = 0, elem = 0, inner = nil }
end

local function shapeRead(r: any, k: number): any
	local s = newShape()
	s.kind = k
	s.bits = kindBits(k)
	if (k >= 2 and k <= 10) or (k >= 18 and k <= 29) then
		local packing = rU8(r)
		s.packing = packing
		if packing == 0 then
			return s
		elseif packing == 1 and k ~= 10 then
			local n = rLeb(r)
			if r.bad or n > kindBits(k) then r.bad = true; return s end
			s.bits = n
			local b16 = buffer.create(16)
			if k >= 18 and k <= 29 then
				if r.at + 16 > r.e then r.bad = true; return s end
				buffer.copy(b16, 0, r.b, r.at, 16)
				s.baseLo = buffer.readinteger(r.b, r.at)
				s.baseHi = buffer.readinteger(r.b, r.at + 8)
				r.at = r.at + 16
				if k >= 20 and k <= 23 then
					local loNeg = buffer.readu8(b16, 7) >= 128
					local wantByte = if loNeg then 0xFF else 0x00
					for bi = 8, 15 do
						if buffer.readu8(b16, bi) ~= wantByte then r.bad = true; return s end
					end
					if k < 23 then
						local sb = integer.tonumber(s.baseLo)
						local dom = kindBits(k)
						if sb < -(2 ^ (dom - 1)) or sb > (2 ^ (dom - 1)) - 1 then r.bad = true; return s end
					end
				elseif k >= 25 and k <= 28 then
					for bi = 8, 15 do
						if buffer.readu8(b16, bi) ~= 0 then r.bad = true; return s end
					end
					if k < 28 then
						local dom = kindBits(k)
						if buffer.readu32(b16, 4) ~= 0 or buffer.readu32(b16, 0) > (2 ^ dom) - 1 then r.bad = true; return s end
					end
				end
			else
				local at0 = r.at
				local raw = rLeb(r)
				s.base = raw
				if r.bad then return s end
				local zigzag = (k >= 2 and k <= 5) or k == 36
				if zigzag then
					local mag = math.floor(raw / 2)
					if k < 5 then
						local dom = kindBits(k)
						local lim = 2 ^ (dom - 1)
						if mag >= lim then r.bad = true; return s end
					end
				elseif k < 9 then
					local dom = kindBits(k)
					if raw > (2 ^ dom) - 1 then r.bad = true; return s end
				end
				-- the base exact at every width, from the LEB bytes themselves
				b16 = lebBits16(r.b, at0, r.at, zigzag)
			end
			s.base16 = b16
			return s
		elseif packing == 2 and k == 10 then
			if r.at + 12 > r.e then r.bad = true; return s end
			local qmin = buffer.readf32(r.b, r.at)
			local qmax = buffer.readf32(r.b, r.at + 4)
			local qres = buffer.readf32(r.b, r.at + 8)
			r.at = r.at + 12
			-- the triple's derivation is ir.TableMessageQuantization's, in
			-- float32: the step count and therefore the index width
			if not (qmin < qmax) or not (qres > 0) then r.bad = true; return s end
			local delta = asF32(qmax - qmin)
			local values = asF32(delta / qres)
			if delta - delta ~= 0 or values - values ~= 0 then r.bad = true; return s end
			if not (values >= 1) then values = 1
			elseif values > 4294967040 then values = 4294967040 end
			s.qmin = qmin
			s.qmax = qmax
			s.qres = qres
			s.count = math.ceil(values)
			s.bits = bitsLen32(s.count)
			return s
		else
			r.bad = true
			return s
		end
	end
	if k == 12 then
		s.max = rLeb(r)
		if r.bad or s.max > 0x7FFFFFFF then r.bad = true end
	elseif k == 14 then
		s.min = rLeb(r)
		s.max = rLeb(r)
		if r.bad or s.max > 0xFFFFFFFF then r.bad = true end
		if s.max < s.min then r.bad = true; return s end
	elseif k == 16 then
		s.max = rLeb(r)
		if r.bad or s.max > 0xFFFFFFFF then r.bad = true end
	end
	return s
end

local function entryRead(r: any): any
	local startAt = r.at
	local e: any = { id = 0i, shape = newShape(), element = newShape(), raw = "" }
	if r.at + 9 > r.e then r.bad = true; return e end
	e.id = buffer.readinteger(r.b, r.at)
	r.at = r.at + 8
	if e.id == VERSION_ID or e.id == VOCAB_ID then r.bad = true; return e end
	local kind = rU8(r)
	if kind > 32 and kind ~= 36 then r.bad = true; return e end
	e.shape = shapeRead(r, kind)
	if r.bad then return e end
	if kind == 14 or kind == 16 then
		local elem = rU8(r)
		if (elem > 32 and elem ~= 36) or elem == 14 or elem == 16 then r.bad = true; return e end
		e.shape.elem = elem
		e.element = shapeRead(r, elem)
		e.shape.element = e.element
		if r.bad then return e end
	end
	e.raw = buffer.readstring(r.b, startAt, r.at - startAt)
	return e
end

-- skipStep walks the announcement body's own fields, which is §3's file body.
local function skipStep(r: any, kind: number): boolean
	if kind == 0 or kind == 1 or kind == 32 then return true end
	local w = kindBits(kind)
	if w ~= 0 then
		if r.at + w // 8 > r.e then return false end
		r.at = r.at + w // 8
		return true
	elseif kind == 12 or kind == 13 or kind == 14 or kind == 15 or kind == 16 or kind == 31 then
		local n = rLeb(r)
		if r.bad or r.at + n > r.e then return false end
		r.at = r.at + n
		return true
	elseif kind == 17 or kind == 30 then
		rLeb(r)
		return not r.bad
	end
	return false
end

-- AnnounceRead resolves the announcement once, into the caller's vocabulary
-- (docs/SPEC-TABLES.md §3.3: "The ANNOUNCEMENT READS walkLY, WITH EXACTLY
-- TWO STRICT CHECKS"; a refusal is terminal and a second is
-- `second_announcement`).
function M.NewVocabulary(): any
	return { entries = {}, count = 0, refBits = 1, buildVersion = 0i, announced = false, refused = false, maxBytes = 65536, maxEntries = 4096 }
end

local function refuse(report: any, why: string): boolean
	report.verdict = 1
	report.reason = why
	return false
end

function M.AnnounceRead(v: any, data: buffer, report: any?): boolean
	if report == nil then report = { malformed = false, verdict = 0, reason = "", unknown = 0 } end
	if v.announced or v.refused then
		return refuse(report, "second_announcement")
	end
	local ok = M.announceRead(v, data, report)
	if not ok then v.refused = true end
	return ok
end

function M.announceRead(v: any, data: buffer, report: any): boolean
	local len = buffer.len(data)
	if len < 1 then report.malformed = true; return false end
	local form = buffer.readu8(data, 0)
	if form == 2 then return refuse(report, "message_form_as_file") end
	if form ~= 1 then return refuse(report, "newer_form") end
	if len < 9 then report.malformed = true; return false end
	-- the count is the unsigned u64 of docs/SPEC-TABLES.md §3, not a signed
	-- integer: a high-bit count is huge, never negative, and refuses here
	local count = integer.tonumber(buffer.readinteger(data, len - 8))
	if count < 0 or count > math.floor((len - 9) / 8) then report.malformed = true; return false end
	-- the id table holds at most 4096 entries (docs/SPEC-TABLES.md §3,
	-- ir.TableWireIdTableBound), whatever the buffer would hold
	if count > 4096 then report.malformed = true; return false end
	local start = len - 8 - count * 8
	local ids = {}
	local seen = {}
	for i = 0, count - 1 do
		local id = buffer.readinteger(data, start + i * 8)
		if seen[id] then report.malformed = true; return false end
		seen[id] = true
		ids[i + 1] = id
	end
	local r = { b = data, at = 1, e = start, bad = false }
	local versions = 0
	local vocabs = 0
	local words: buffer = buffer.create(0)
	local seenBody: { [integer]: boolean } = {}
	local nextRef = 1
	while true do
		local ref = rLeb(r)
		if r.bad then report.malformed = true; return false end
		if ref == 0 then break end
		if ref > #ids or ref > nextRef then report.malformed = true; return false end
		if ref == nextRef then nextRef = nextRef + 1 end
		local kind = rU8(r)
		local id = ids[ref]
		if seenBody[id] or id == NODE_ID then report.malformed = true; return false end
		seenBody[id] = true
		if id == VERSION_ID then
			if kind ~= 9 or r.at + 8 > r.e then report.malformed = true; return false end
			v.buildVersion = buffer.readinteger(data, r.at)
			r.at = r.at + 8
			versions = versions + 1
		elseif id == VOCAB_ID then
			if kind ~= 14 then report.malformed = true; return false end
			local n = rLeb(r)
			if r.bad or r.at + n > r.e then report.malformed = true; return false end
			local sub = { b = data, at = r.at, e = r.at + n, bad = false }
			r.at = r.at + n
			if rU8(sub) ~= 6 then report.malformed = true; return false end
			local m = rLeb(sub)
			if sub.bad or m ~= sub.e - sub.at then report.malformed = true; return false end
			if m > v.maxBytes then return refuse(report, "vocabulary_too_large") end
			local wb = buffer.create(m)
			buffer.copy(wb, 0, data, sub.at, m)
			words = wb
			vocabs = vocabs + 1
		else
			report.unknown = (report.unknown or 0) + 1
			if not skipStep(r, kind) then report.malformed = true; return false end
		end
	end
	if r.at ~= start or ((report.unknown or 0) == 0 and nextRef - 1 ~= #ids) then report.malformed = true; return false end
	if versions ~= 1 or vocabs ~= 1 then report.malformed = true; return false end
	local w = { b = words, at = 0, e = buffer.len(words), bad = false }
	local entries = {}
	local seenRaw = {}
	local nodeCount = 0
	local n = 0
	while w.at < w.e do
		local began = w.at
		local e = entryRead(w)
		if w.bad then report.malformed = true; return false end
		if e.id == NODE_ID then
			nodeCount = nodeCount + 1
			if nodeCount > 1 then report.malformed = true; return false end
		end
		local rawKey = buffer.readstring(w.b, began, w.at - began)
		if seenRaw[rawKey] then report.malformed = true; return false end
		seenRaw[rawKey] = true
		n = n + 1
		entries[n] = e
	end
	-- the entry bound is the receiver's second declared bound (§3.3): an
	-- announcement past it is refused by name before the vocabulary is set,
	-- exactly as the byte bound above is. Zero (or no field) is the
	-- conforming default, 4096 entries, as the engine reads it
	-- (internal/tablewire/message.go: ir.TableVocabularyMaxEntries); zero
	-- never means unbounded.
	local maxEntries = v.maxEntries or 0
	if maxEntries <= 0 then maxEntries = DEFAULT_MAX_ENTRIES end
	if n > maxEntries then
		return refuse(report, "vocabulary_too_large")
	end
	v.entries = entries
	v.count = n
	v.refBits = bitsReq(0, n)
	if v.refBits == 0 then v.refBits = 1 end
	v.announced = true
	return true
end

-- ---- the body walk: reference, entry, announced shape (docs/SPEC-TABLES.md
-- §3.3). A skip walks an entry exactly so the next bit is known. ----

local function entryOf(v: any, ref: number): any?
	if ref == 0 or ref > v.count then return nil end
	return v.entries[ref]
end

local function reserved(id: integer): boolean
	return id == NODE_ID or id == VERSION_ID or id == VOCAB_ID
end

local skipShape: any
local skipBody: any

-- fieldById finds one descriptor field under a body's announced field id, so
-- the walk can tell an enum value from any other reference (§2.15).
local fieldMaps = setmetatable({}, { __mode = "k" })
local function fieldById(desc: any, id: integer): any
	if desc == nil or desc.fields == nil then return nil end
	local m = fieldMaps[desc]
	if m == nil then
		m = {}
		for _, f in desc.fields do m[f.id] = f end
		fieldMaps[desc] = m
	end
	return m[id]
end

-- named gives an array's element descriptor its owning field's name, so a
-- refusal names <Table>.<field> even when the deprecated value is an element.
local function named(f: any, name: string): any
	if f == nil or f.name ~= nil then return f end
	return { s = f.s, t = f.t, vs = f.vs, def = f.def, e = f.e, lo = f.lo, hi = f.hi, f32 = f.f32, fbits = f.fbits, ibits = f.ibits, bits = f.bits, signed = f.signed, kw = f.kw, name = name }
end

local function variantIndex(vs: any, id: integer): number
	for i, pair in vs do
		if pair[2] == id then return i end
	end
	return 0
end

local function refOfId(st: any, id: integer): number
	for i = 1, st.v.count do
		local e = st.v.entries[i]
		if e.id == id and e.shape.kind == 0 then return i end
	end
	return 0
end

local function countOf(r: any, s: any): (number, boolean)
	local bits = bitsReq(s.min, s.max)
	local raw, ok = get(r, bits)
	if not ok then return 0, false end
	return raw + s.min, true
end

-- a field's index in its record's declaration order
local fieldIndexMaps = setmetatable({}, { __mode = "k" })
local function fieldIndex(desc: any, id: integer): number?
	local m = fieldIndexMaps[desc]
	if m == nil then
		m = {}
		for i, f in desc.fields do m[f.id] = i end
		fieldIndexMaps[desc] = m
	end
	return m[id]
end

local messageFieldDefault: any
local getOwnVocab: any
local ownSlotEntry: any

local function isSignedKind(k: number): boolean
	return (k >= 2 and k <= 5) or k == 18 or (k >= 20 and k <= 24) or k == 36
end

local function add16(a: buffer, b: buffer): buffer
	local out = buffer.create(16)
	local carry = 0
	for i = 0, 15 do
		local s = buffer.readu8(a, i) + buffer.readu8(b, i) + carry
		buffer.writeu8(out, i, s % 256)
		carry = math.floor(s / 256)
	end
	return out
end

local function sub16(a: buffer, b: buffer): buffer
	local out = buffer.create(16)
	local borrow = 0
	for i = 0, 15 do
		local d = buffer.readu8(a, i) - buffer.readu8(b, i) - borrow
		if d < 0 then
			d = d + 256
			borrow = 1
		else
			borrow = 0
		end
		buffer.writeu8(out, i, d)
	end
	return out
end

local function bit16(b: buffer, idx: number): number
	local byte = buffer.readu8(b, math.floor(idx / 8))
	return bit32.band(bit32.rshift(byte, idx % 8), 1)
end

local function setBit16(b: buffer, idx: number, val: number)
	local bi = math.floor(idx / 8)
	local mask = bit32.lshift(1, idx % 8)
	local cur = buffer.readu8(b, bi)
	if val ~= 0 then
		buffer.writeu8(b, bi, bit32.bor(cur, mask))
	else
		buffer.writeu8(b, bi, bit32.band(cur, bit32.bnot(mask)))
	end
end

local function defTo16(def: any): buffer
	local b = buffer.create(16)
	if typeof(def) == "table" then
		buffer.writeinteger(b, 0, def.lo)
		buffer.writeinteger(b, 8, def.hi)
	elseif typeof(def) == "integer" then
		buffer.writeinteger(b, 0, def)
		if integer.lt(def, 0i) then buffer.fill(b, 8, 0xFF, 8) end
	elseif typeof(def) == "number" then
		local m = math.floor(def)
		buffer.writeu32(b, 0, m % 2 ^ 32)
		buffer.writeu32(b, 4, math.floor(m / 2 ^ 32) % 2 ^ 32)
		if m < 0 then buffer.fill(b, 8, 0xFF, 8) end
	end
	return b
end

local function eq16(a: buffer, b: buffer): boolean
	for i = 0, 15 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end

-- A RANGED VALUE IS HELD TO THE ANNOUNCED KIND, NEVER WRAPPED
-- (docs/SPEC-TABLES.md §3.3): the wire carries value - base, and base plus
-- that offset is the value only where the announced kind can hold the sum.
-- rangedHolds16 answers for the sum readInt16FromWriter built (sum16) from
-- the offset it read (raw16): under a kind narrower than 128 bits the offset
-- stays below 2^width and the sum sign- or zero-extends past the width; at
-- 128 bits an unsigned sum must not wrap below its base, nor a signed one
-- past the top. A sum the kind cannot hold is refused on the named read and
-- is damage on the skip path, one verdict either way.
local function rangedHolds16(shape: any, sum16: buffer, raw16: buffer): boolean
	local w = kindBits(shape.kind)
	if w == 0 then return true end
	local signed = isSignedKind(shape.kind)
	if w < 128 then
		for i = w, 127 do
			if bit16(raw16, i) ~= 0 then return false end
		end
		local top = if signed then bit16(sum16, w - 1) else 0
		for i = w, 127 do
			if bit16(sum16, i) ~= top then return false end
		end
		return true
	end
	local base = shape.base16
	local hs, hb = bit16(sum16, 127), bit16(base, 127)
	if signed and hs ~= hb then
		-- a negative sum below a non-negative base wrapped past the top
		return hs == 0
	end
	for bi = 15, 0, -1 do
		local a, b = buffer.readu8(sum16, bi), buffer.readu8(base, bi)
		if a ~= b then return a > b end
	end
	return true
end

local function readInt16FromWriter(r: any, shape: any): (buffer, boolean, buffer?)
	local raw = buffer.create(16)
	local bits = shape.bits or 0
	if (shape.packing or 0) == 0 and bits == 0 then
		bits = kindBits(shape.kind)
	end
	for i = 0, bits - 1 do
		local bit, ok = get(r, 1)
		if not ok then return raw, false, nil end
		if bit ~= 0 then setBit16(raw, i, 1) end
	end
	if (shape.packing or 0) == 1 then
		return add16(raw, shape.base16), true, raw
	end
	if isSignedKind(shape.kind) and bits > 0 and bit16(raw, bits - 1) == 1 then
		for i = bits, 127 do
			setBit16(raw, i, 1)
		end
	end
	return raw, true, nil
end

skipBody = function(r: any, st: any, desc: any, dep: any): boolean
	local seen: { [integer]: boolean } = {}
	local ordered = st.same == true and desc ~= nil and desc.fields ~= nil
	local last = 0
	while true do
		local ref, ok = get(r, st.refBits)
		if not ok then return false end
		if ref == 0 then return true end
		local e = entryOf(st.v, ref)
		if e == nil or reserved(e.id) or seen[e.id] then return false end
		seen[e.id] = true
		if dep ~= nil and desc ~= nil then dep.table = desc.name end
		if desc ~= nil then st.owner = desc.name end
		local fd = fieldById(desc, e.id)
		local exact = false
		if ordered then
			local i = fieldIndex(desc, e.id)
			if i == nil then return false end
			exact = true
			if st.ov ~= nil and st.rootName ~= nil then
				local _, oe = ownSlotEntry(st.ov, st.rootName, desc.name, i, e.id)
				exact = oe ~= nil and e.raw == oe.raw
			end
			if exact then
				if i <= last then return false end
				last = i
				local at = r.off
				local def = messageFieldDefault(fd, r, e, st)
				r.off = at
				if def then return false end
			end
		end
		if not skipShape(r, st, e, fd, dep, true, exact) then return false end
	end
end

local defScratch = buffer.create(16)

-- messageFieldDefault is the message writer's elision rule read off the bits
-- (docs/SPEC-TABLES.md §3.3 (a), tablewire's messageElides): whether a field,
-- neither required nor optional, holds what its writer leaves out. A string or
-- bytes at the default's own bytes; a counted array or an enum-keyed array of
-- no element; a nested body that is its terminator alone; a bool, an unpacked
-- integer of any width, or a fixed-point raw at the default's own bits; a
-- plain float at the default's own bit pattern; an enum naming the default
-- variant, or None where the default is None. A union's None is refused where
-- it is read (skipShape). A packed integer, a compressed float and a fixed
-- array are not judged here and read as before.
messageFieldDefault = function(fd: any, r: any, e: any, st: any): boolean
	if fd == nil or fd.required == true or fd.present ~= nil then return false end
	local k = e.shape.kind
	if k == 12 then
		local def = fd.def or ""
		if typeof(def) ~= "string" then return false end
		local n, ok = countOf(r, e.shape)
		if not ok or n ~= #def then return false end
		if n == 0 then return true end
		if not align(r) or r.off + n * 8 > r.n then return false end
		return buffer.readstring(r.b, math.floor(r.off / 8), n) == def
	end
	if k == 13 then
		if fd.s ~= "tab" then return false end
		local ref, ok = get(r, st.refBits)
		return ok and ref == 0
	end
	if k == 14 and fd.s == "bytes" then
		local def = fd.def or ""
		if typeof(def) ~= "string" then return false end
		local n, ok = countOf(r, e.shape)
		if not ok or n ~= #def then return false end
		if n == 0 then return true end
		if not align(r) or r.off + n * 8 > r.n then return false end
		return buffer.readstring(r.b, math.floor(r.off / 8), n) == def
	end
	if k == 14 or k == 16 then
		local n, ok = countOf(r, e.shape)
		if not ok then return false end
		if n == 0 then return true end
		if k == 16 then return false end
		local born = if e.shape.min == e.shape.max then e.shape.min else (fd.min or 0)
		if born <= 0 or n ~= born then return false end
		if e.shape.elem == 13 and e.shape.min == e.shape.max then return false end
		local element = if e.element ~= nil then e.element else newShape()
		if (element.packing or 0) == 2 then return false end
		if e.shape.elem == 6 and not align(r) then return false end
		local ef = if fd.e ~= nil then named(fd.e, fd.name) else nil
		if ef == nil then return false end
		for _ = 1, n do
			if element.kind == 15 then
				local uref, uok = get(r, st.refBits)
				if not uok or uref ~= 0 then return false end
			elseif element.kind == 30 then
				local eref, eok = get(r, st.refBits)
				if not eok or eref ~= 0 then return false end
			elseif element.kind == 13 then
				local bref, bok = get(r, st.refBits)
				if not bok or bref ~= 0 then return false end
			else
				local before = r.off
				if not messageFieldDefault(ef, r, { shape = element }, st) then return false end
				r.off = before
				if not skipShape(r, st, { shape = element }, ef, nil) then return false end
			end
		end
		return true
	end
	if k == 30 then
		if fd.s ~= "enum" then return false end
		local ref, ok = get(r, st.refBits)
		if not ok then return false end
		local def = fd.def or 0
		if typeof(def) == "integer" then def = integer.tonumber(def) end
		if ref == 0 then return def == 0 end
		if def == 0 or fd.vs == nil or fd.vs[def] == nil then return false end
		local en = entryOf(st.v, ref)
		return en ~= nil and en.id == fd.vs[def][2]
	end
	if (e.shape.packing or 0) == 2 then return false end
	if k == 10 or k == 11 then
		if fd.s ~= "float" then return false end
		local def = fd.def or 0
		if typeof(def) == "integer" then def = integer.tonumber(def) end
		if typeof(def) ~= "number" then return false end
		if k == 10 then
			buffer.writef32(defScratch, 0, def)
			local bits, ok = get(r, 32)
			return ok and bits == buffer.readu32(defScratch, 0)
		end
		buffer.writef64(defScratch, 0, def)
		local lo, lok = get(r, 32)
		local hi, hok = get(r, 32)
		return lok and hok and lo == buffer.readu32(defScratch, 0) and hi == buffer.readu32(defScratch, 4)
	end
	if k == 1 then
		local raw, ok = get(r, 1)
		return ok and (raw ~= 0) == (fd.def == true)
	end
	if not ((k >= 2 and k <= 9) or (k >= 18 and k <= 29) or k == 36) then return false end
	local v16, vok = readInt16FromWriter(r, e.shape)
	return vok and eq16(v16, defTo16(fd.def or 0))
end

skipShape = function(r: any, st: any, e: any, fd: any, dep: any, fieldEntry: boolean?, exactField: boolean?): boolean
	local k = e.shape.kind
	if k == 0 or k == 32 then return true end
	if plainFloat(e.shape) then return floatStep(r, st, k, fd) end
	if k == 10 and e.shape.packing == 2 then
		-- A QUANTIZED FLOAT RIDES ITS STEP INDEX, and an index above the
		-- announced count is damage (docs/SPEC-TABLES.md §3.3).
		local idx, ok = get(r, e.shape.bits)
		if not ok then return false end
		return idx <= (e.shape.count or 0)
	end
	if k == 30 then
		local at = r.off
		local ref, ok = get(r, st.refBits)
		if not ok then return false end
		if ref == 0 then return true end
		local en = entryOf(st.v, ref)
		if en == nil or reserved(en.id) or en.shape.kind ~= 0 then return false end
		-- A DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT (the
		-- ELEMENT's default for an element) and never counts unknown
		-- (docs/SPEC-TABLES.md §2.15); a strict write refuses it by name.
		if fd ~= nil and fd.vs ~= nil and dep ~= nil then
			local ord = variantIndex(fd.vs, en.id)
			if ord > 0 and fd.vs[ord][3] == true then
				dep.found = true
				if dep.field == nil then dep.field = (dep.table or "?") .. "." .. (fd.name or "?") end
				if dep.write then
					local def = fd.def or 0
					local newref = 0
					if def ~= 0 then
						local p = fd.vs[def]
						if p ~= nil then newref = refOfId(st, p[2]) end
					end
					put(r, at, st.refBits, newref)
				end
			end
		end
		return true
	elseif k == 17 then
		return false -- a fixed root numbers no node, so no index can be read (§3.1, §3.3)
	elseif k == 13 then
		local sub = nil
		if fd ~= nil and fd.s == "tab" then sub = S.lookup(fd.t) end
		return skipBody(r, st, sub, dep)
	elseif k == 15 then
		local ref, ok = get(r, st.refBits)
		if not ok then return false end
		if ref == 0 then
			-- A UNION FIELD WRITTEN WITH ARM REFERENCE 0 IS REFUSED at every
			-- build version (§3.3 (c)): a conforming writer elides a None
			-- field, one this reader cannot name included. An array element's
			-- or a nested arm's None is positional and stays lawful.
			if fieldEntry == true and not (fd ~= nil and fd.required == true) then return false end
			return true
		end
		local arm = entryOf(st.v, ref)
		if arm == nil or reserved(arm.id) or arm.shape.kind == 0 then return false end
		local armFd = nil
		if fd ~= nil and fd.s == "union" then
			for _, a in S.lookup(fd.t).arms do
				if a.id == arm.id then armFd = a; break end
			end
		end
		if armFd ~= nil then st.owner = fd.t end
		return skipShape(r, st, arm, armFd, dep)
	elseif k == 12 then
		local n, ok = countOf(r, e.shape)
		if not ok or not align(r) then return false end
		if r.off + n * 8 > r.n then return false end
		-- SKIPPED IS NOT UNREAD: a string's bytes are held to what a read of
		-- them holds (docs/SPEC-TABLES.md §3, §4).
		if not textOk(r.b, math.floor(r.off / 8), n) then return false end
		return skip(r, n * 8)
	elseif k == 14 or k == 16 then
		local n, ok = countOf(r, e.shape)
		if not ok then return false end
		-- THE COUNT'S BITS CAN SPELL MORE THAN max - min, so a count above
		-- the announced maximum is damage here as it is at a named field.
		if n > e.shape.max then return false end
		local ef = nil
		if fd ~= nil then
			if fd.e ~= nil then ef = named(fd.e, fd.name)
			elseif fd.s == "map" then ef = { s = "tab", t = fd.t, name = fd.name } end
		end
		-- tableMessageEntryRead reads an inner kind and keeps no inner shape.
		-- A missing element is that zero shape (kind 0, width 0), not a raised index.
		local element = if e.element ~= nil then e.element else newShape()
		local ik = element.kind
		local variable = ik == 12 or ik == 13 or ik == 14 or ik == 15 or ik == 16 or ik == 17 or ik == 30 or ik == 31
		-- A walked element that consumes no bits refuses the read
		-- (docs/SPEC-TABLES.md §3.3; tableMessageSkip, r.Offset==before).
		local function walkOne(): boolean
			local before = r.off
			if not skipShape(r, st, { shape = element }, ef, dep) then return false end
			return r.off ~= before
		end
		if k == 16 then
			local seen: { [integer]: boolean } = {}
			local kvs = if fd ~= nil then (fd.keyvs or fd.vs) else nil
			local checkKeyed = exactField == true and kvs ~= nil
			local lastKord = 0
			for _ = 1, n do
				local key, kok = get(r, st.refBits)
				if not kok then return false end
				local ken = entryOf(st.v, key)
				if ken == nil or reserved(ken.id) or ken.shape.kind ~= 0 then return false end
				if seen[ken.id] then return false end
				seen[ken.id] = true
				local kord = if kvs ~= nil then variantIndex(kvs, ken.id) else 0
				if checkKeyed and kord > 0 then
					if kord <= lastKord then return false end
					lastKord = kord
					if ef ~= nil and (element.packing or 0) ~= 2 then
						local peekOff = r.off
						local slotDef = false
						if element.kind == 15 then
							local uref, uok = get(r, st.refBits)
							slotDef = uok and uref == 0
						else
							slotDef = messageFieldDefault(ef, r, { shape = element }, st)
						end
						r.off = peekOff
						if slotDef then return false end
					end
				end
				if variable then
					if not walkOne() then return false end
				elseif not skipShape(r, st, { shape = element }, ef, dep) then
					return false
				end
			end
			return true
		end
		if e.shape.elem == 6 and not align(r) then return false end
		if plainFloat(element) then
			for _ = 1, n do
				if not floatStep(r, st, ik, ef) then return false end
			end
			return true
		end
		-- kind 10 is also the quantized float, whose index is checked, so it
		-- is walked element by element rather than skipped as a run.
		-- A ranged element carries a value condition (the announced kind holds
		-- base plus offset), so it is walked too, unless it rides no bits.
		local ranged = (element.packing or 0) == 1 and (element.bits or 0) > 0
		if ik ~= 10 and not variable and not ranged then
			return skip(r, n * element.bits)
		end
		if ranged then
			for _ = 1, n do
				if not skipShape(r, st, { shape = element }, ef, dep) then return false end
			end
			return true
		end
		for _ = 1, n do
			if not walkOne() then return false end
		end
		return true
	elseif k == 31 then
		if not align(r) then return false end
		local n, ok = get(r, 32)
		return ok and skip(r, n * 8)
	end
	-- A SKIPPED RANGED VALUE IS HELD TO THE ANNOUNCED KIND as the named read
	-- holds it (§3.3, SKIPPED IS NOT UNREAD): a sum the kind cannot hold is
	-- damage here.
	if (e.shape.packing or 0) == 1 and (e.shape.bits or 0) > 0 and ((k >= 2 and k <= 9) or (k >= 18 and k <= 29) or k == 36) then
		local sum16, ok, raw16 = readInt16FromWriter(r, e.shape)
		return ok and raw16 ~= nil and rangedHolds16(e.shape, sum16, raw16)
	end
	return skip(r, e.shape.bits)
end

-- messageRoot is root's descriptor when root is a fixed table, and a refusal
-- by name otherwise: the message form is fixed tables only (§3.3)
local function messageRoot(root: string): any
	local desc = S.lookup(root)
	if not (desc.table and desc.fixed) then
		error("schema: " .. root .. " is not a fixed table: the message form is fixed tables only (docs/SPEC-TABLES.md §3.3)")
	end
	return desc
end

-- ---- the surface (docs/SPEC-TABLES.md §3.3, "THE BATCH SURFACE'S FIVE
-- ANSWERS"): LoadMessages fills the caller's storage, SaveMessages writes one
-- buffer, MeasureMessages sizes it. The bodies' bits are the batch's own, so
-- a loaded body re-saves byte for byte. ----

local function reset(report: any)
	report.malformed = false
	report.verdict = 0
	report.reason = ""
	report.unknown = 0
	report.kindMismatch = 0
	report.clamped = 0
	report.widened = 0
end

-- walkBodies finds the M bodies, and -- when write is set -- rewrites each
-- deprecated enum reference to the field or element default as it goes
-- (docs/SPEC-TABLES.md §2.15). The caller only asks for the rewriting pass on
-- a copy, so a clean batch allocates nothing and the caller's buffer is never
-- changed.
local function walkBodies(v: any, data: buffer, desc: any, count: number, write: boolean): (number, any, boolean, any, string?)
	-- same: the sender announced this reader's own build version, the one
	-- place its declaration is the sender's (§3.3 (a), (b))
	local own = S.buildOf(desc.name)
	local st: any = { v = v, refBits = v.refBits, owner = nil, nonFinite = nil, same = own ~= nil and own == v.buildVersion, rootName = desc.name, ov = if own ~= nil then getOwnVocab(desc.name) else nil }
	local r = newReader(data, 16)
	local dep = { write = write, found = false, field = nil, table = nil }
	local bodies = {}
	local at = 0
	for i = 1, count do
		local from = r.off
		if not skipBody(r, st, desc, dep) then
			return i - 1, bodies, true, dep, st.nonFinite
		end
		at = at + 1
		bodies[at] = { buffer = data, from = from, to = r.off }
	end
	if not align(r) or r.off ~= r.n then
		return count, bodies, true, dep, st.nonFinite
	end
	return count, bodies, false, dep, st.nonFinite
end

-- A NaN OR AN INFINITY REFUSES THE BATCH WHOLE (docs/SPEC-TABLES.md §3.3):
-- every read, walk and strict alike, and every writer, the field named.
local function refuseNonFinite(report: any, field: string, read: boolean)
	report.verdict = 1
	report.reason = if read then "non_finite_float: " .. field else "non_finite_float"
	report.nonFiniteFloat = field
	report.field = field
end


-- ---------------------------------------------------------------------------
-- Cross-build message transcoding (SPEC-TABLES §3.3, §4): when a batch arrives
-- under another build's vocabulary (v.buildVersion ~= own), LoadMessages decodes
-- its bodies into this unit's declared model (widening scalars, dropping unknown
-- fields and unknown union arms, substituting defaults for unknown/deprecated
-- enum variants, checking narrowing bounds) and MeasureMessages / SaveMessages
-- write them in this unit's own vocabulary in continuous batch bit order.
-- ---------------------------------------------------------------------------

local ownVocabCache: { [integer]: any } = {}
getOwnVocab = function(tableName: string): any?
	local bv = S.buildOf(tableName)
	if bv == nil then return nil end
	local cached = ownVocabCache[bv]
	if cached ~= nil then return cached end
	local ann = S.messageAnnouncementOf(tableName)
	if ann == nil then return nil end
	local ov = M.NewVocabulary()
	if not M.AnnounceRead(ov, ann, {}) then return nil end
	ownVocabCache[bv] = ov
	return ov
end

-- 128-bit integer helpers declared above skipBody

local function checkAndPackInt16ForOwn(val16: buffer, shape: any): (buffer?, boolean)
	local bits = shape.bits or 0
	if (shape.packing or 0) == 1 then
		local diff = sub16(val16, shape.base16)
		for i = bits, 127 do
			if bit16(diff, i) ~= 0 then return nil, false end
		end
		return diff, true
	end
	if isSignedKind(shape.kind) then
		local sign = if bits > 0 then bit16(val16, bits - 1) else 0
		for i = bits, 127 do
			if bit16(val16, i) ~= sign then return nil, false end
		end
	else
		for i = bits, 127 do
			if bit16(val16, i) ~= 0 then return nil, false end
		end
	end
	return val16, true
end

local function quantizeF32(shape: any, v: number): (number, boolean)
	local v32 = asF32(v)
	if v32 < shape.qmin or v32 > shape.qmax then return 0, false end
	local delta = asF32(shape.qmax - shape.qmin)
	local count32 = asF32(shape.count)
	local norm = asF32(asF32(v32 - shape.qmin) / delta)
	if norm < 0 then norm = 0 end
	if norm > 1 then norm = 1 end
	local idx = math.floor(asF32(asF32(norm * count32) + 0.5))
	if idx > shape.count then idx = shape.count end
	return idx, true
end

local function dequantizeF32(shape: any, idx: number): number
	local delta = asF32(shape.qmax - shape.qmin)
	local t = asF32(idx / asF32(shape.count))
	return asF32(shape.qmin + asF32(t * delta))
end

local function ownNameEntryById(ov: any, id: integer): (number, any)
	for i = 1, ov.count do
		local e = ov.entries[i]
		if e ~= nil and e.id == id and e.shape.kind == 0 then
			return i, e
		end
	end
	return 0, nil
end

ownSlotEntry = function(ov: any, rootName: string, ownerName: string, idx1: number, fallbackId: integer): (number, any)
	local slot = S.messageSlotOf(rootName, ownerName, idx1)
	if slot > 0 and ov.entries[slot] ~= nil then
		return slot, ov.entries[slot]
	end
	for i = 1, ov.count do
		local e = ov.entries[i]
		if e ~= nil and e.id == fallbackId and e.shape.kind ~= 0 then
			return i, e
		end
	end
	return 0, nil
end

local transcodeBody: any
local ownFieldElides: any
local emptyOwnBody: any
local bornOwnElem: any
local transcodeValue: any

transcodeValue = function(r: any, pv: any, ov: any, rootName: string, pe: any, oe: any, fd: any, rep: any): (any, boolean)
	local pk = pe.shape.kind
	local okind = oe.shape.kind
	if okind == 0 or okind == 32 then
		return { kind = okind }, true
	end
	if okind == 1 then
		if pk ~= 1 then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		local bit, ok = get(r, 1)
		if not ok then return nil, false end
		return { kind = 1, bit = bit }, true
	end
	if okind == 10 or okind == 11 then
		if pk ~= 10 and pk ~= 11 then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		if pk == 11 and okind == 10 then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		local fval = 0
		local raw32 = 0
		local rawLo, rawHi = 0, 0
		local tmp8 = buffer.create(8)
		if pk == 10 then
			if pe.shape.packing == 2 then
				local idx, ok = get(r, pe.shape.bits)
				if not ok or idx > pe.shape.count then return nil, false end
				fval = dequantizeF32(pe.shape, idx)
				buffer.writef32(tmp8, 0, fval)
				raw32 = buffer.readu32(tmp8, 0)
			else
				local bits, ok = get(r, 32)
				if not ok then return nil, false end
				raw32 = bits
				buffer.writeu32(tmp8, 0, bits)
				fval = buffer.readf32(tmp8, 0)
			end
			buffer.writef64(tmp8, 0, fval)
			rawLo = buffer.readu32(tmp8, 0)
			rawHi = buffer.readu32(tmp8, 4)
		else
			local lo, lok = get(r, 32)
			local hi, hok = get(r, 32)
			if not lok or not hok then return nil, false end
			rawLo, rawHi = lo, hi
			buffer.writeu32(tmp8, 0, lo)
			buffer.writeu32(tmp8, 4, hi)
			fval = buffer.readf64(tmp8, 0)
		end
		if pk == 10 and okind == 11 then rep.widened = (rep.widened or 0) + 1 end
		if fd ~= nil and fd.lo ~= nil and fd.hi ~= nil then
			if okind == 10 then
				local fv32 = asF32(fval)
				if fv32 < asF32(fd.lo) or fv32 > asF32(fd.hi) then rep.clamped = (rep.clamped or 0) + 1; return nil, false end
			else
				if fval < fd.lo or fval > fd.hi then rep.clamped = (rep.clamped or 0) + 1; return nil, false end
			end
		end
		if okind == 10 then
			if oe.shape.packing == 2 then
				local qidx, qok = quantizeF32(oe.shape, fval)
				if not qok then rep.clamped = (rep.clamped or 0) + 1; return nil, false end
				return { kind = 10, packing = 2, bits = oe.shape.bits, idx = qidx }, true
			end
			return { kind = 10, packing = 0, raw32 = raw32 }, true
		end
		return { kind = 11, lo = rawLo, hi = rawHi }, true
	end
	if okind == 12 then
		if pk ~= 12 then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		local n, ok = countOf(r, pe.shape)
		if not ok or not align(r) or r.off + n * 8 > r.n then return nil, false end
		local str = buffer.readstring(r.b, math.floor(r.off / 8), n)
		r.off = r.off + n * 8
		if n > oe.shape.max then rep.clamped = (rep.clamped or 0) + 1; return nil, false end
		return { kind = 12, max = oe.shape.max, str = str }, true
	end
	if okind == 13 then
		if pk ~= 13 then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		local subDesc = if fd ~= nil and fd.t ~= nil then S.lookup(fd.t) else nil
		if subDesc == nil then return nil, false end
		local subFields, ok = transcodeBody(r, pv, ov, rootName, subDesc, rep)
		if not ok or subFields == nil then return nil, false end
		return { kind = 13, fields = subFields }, true
	end
	if okind == 15 then
		if pk ~= 15 then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		local pref, ok = get(r, pv.refBits)
		if not ok then return nil, false end
		if pref == 0 then
			return { kind = 15, armRef = 0, payload = nil }, true
		end
		local pa = entryOf(pv, pref)
		if pa == nil then return nil, false end
		local udesc: any = if fd ~= nil and fd.t ~= nil then S.lookup(fd.t) else nil
		local armFd = nil
		local armIdx = 0
		if udesc ~= nil and udesc.arms ~= nil then
			for idx, a in udesc.arms do
				if a.id == pa.id then armFd = a; armIdx = idx; break end
			end
		end
		local oaref, oa = 0, nil
		if armFd ~= nil and udesc ~= nil then
			oaref, oa = ownSlotEntry(ov, rootName, udesc.name, armIdx, pa.id)
		end
		if armFd == nil or oaref == 0 or oa == nil then
			-- Unknown union arm in writer: skip payload and fold to None (§4).
			local st: any = { v = pv, refBits = pv.refBits, owner = nil, nonFinite = nil }
			if not skipShape(r, st, pa, nil, nil) then return nil, false end
			rep.unknown = (rep.unknown or 0) + 1
			return { kind = 15, armRef = 0, payload = nil, unknownArm = true }, true
		end
		local armVal, aok = transcodeValue(r, pv, ov, rootName, pa, oa, armFd, rep)
		if not aok then return nil, false end
		return { kind = 15, armRef = oaref, payload = armVal }, true
	end
	if okind == 30 then
		if pk ~= 30 then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		local pref, ok = get(r, pv.refBits)
		if not ok then return nil, false end
		local oref = 0
		if pref ~= 0 then
			local pen = entryOf(pv, pref)
			if pen == nil then return nil, false end
			local ord = if fd ~= nil and fd.vs ~= nil then variantIndex(fd.vs, pen.id) else 0
			if ord > 0 and not fd.vs[ord][3] then
				oref = select(1, ownNameEntryById(ov, pen.id))
			else
				if ord == 0 then
					rep.unknown = (rep.unknown or 0) + 1
				else
					local def = if fd ~= nil then (fd.def or 0) else 0
					if typeof(def) == "integer" then def = integer.tonumber(def) end
					if def ~= 0 and fd ~= nil and fd.vs ~= nil and fd.vs[def] ~= nil then
						oref = select(1, ownNameEntryById(ov, fd.vs[def][2]))
					end
				end
			end
		end
		return { kind = 30, ref = oref }, true
	end
	if okind == 14 or okind == 16 then
		if pk ~= okind then rep.kindMismatch = (rep.kindMismatch or 0) + 1; return nil, false end
		local n, ok = countOf(r, pe.shape)
		if not ok then return nil, false end
		local ef = if fd ~= nil and fd.e ~= nil then named(fd.e, fd.name) else fd
		local peElem = { shape = pe.element or newShape(), element = if pe.element ~= nil then pe.element.element else nil }
		local oeElem = { shape = oe.element or newShape(), element = if oe.element ~= nil then oe.element.element else nil }
		if okind == 16 then
			local pairs = {}
			for _ = 1, n do
				local kref, kok = get(r, pv.refBits)
				if not kok then return nil, false end
				local ken = entryOf(pv, kref)
				if ken == nil then return nil, false end
				local kvs = if fd ~= nil then (fd.keyvs or fd.vs) else nil
				local ord = if kvs ~= nil then variantIndex(kvs, ken.id) else 1
				local okref = select(1, ownNameEntryById(ov, ken.id))
				if ord == 0 or okref == 0 then
					local st: any = { v = pv, refBits = pv.refBits, owner = nil, nonFinite = nil }
					if not skipShape(r, st, peElem, nil, nil) then return nil, false end
					rep.unknown = (rep.unknown or 0) + 1
				else
					local ev, eok = transcodeValue(r, pv, ov, rootName, peElem, oeElem, ef, rep)
					if not eok then return nil, false end
					if not ownFieldElides(ov, ef, oeElem, ev) then
						pairs[#pairs + 1] = { ord = ord, keyRef = okref, val = ev }
					end
				end
			end
			if #pairs > oe.shape.max then rep.clamped = (rep.clamped or 0) + 1; return nil, false end
			table.sort(pairs, function(a: any, b: any) return (a.ord :: number) < (b.ord :: number) end)
			return { kind = 16, max = oe.shape.max, pairs = pairs }, true
		end
		-- A GROWN FIXED BOUND (docs/SPEC-TABLES.md §21.1): an own fixed array
		-- (min == max) read from a sender that announced a smaller bound holds
		-- the n elements the sender wrote and one born element per new slot.
		-- A counted array below its own min is still out of range.
		local grown = oe.shape.min == oe.shape.max and n < oe.shape.min
		if (n < oe.shape.min and not grown) or n > oe.shape.max then rep.clamped = (rep.clamped or 0) + 1; return nil, false end
		if pe.shape.elem == 6 and not align(r) then return nil, false end
		local elems = {}
		for i = 1, n do
			local ev, eok = transcodeValue(r, pv, ov, rootName, peElem, oeElem, ef, rep)
			if not eok then return nil, false end
			elems[i] = ev
		end
		if grown then
			for i = n + 1, oe.shape.min do
				elems[i] = bornOwnElem(ov, rootName, oeElem, ef)
			end
		end
		return { kind = 14, min = oe.shape.min, max = oe.shape.max, elemKind = oe.shape.elem, elems = elems }, true
	end
	-- Integer / flags / fixed-point / timestamp scalar kinds
	local widens = false
	if pk ~= okind then
		if okind >= 3 and okind <= 5 then widens = pk >= 2 and pk < okind
		elseif okind == 18 then widens = pk >= 2 and pk <= 5
		elseif okind >= 7 and okind <= 9 then widens = pk >= 6 and pk < okind
		elseif okind == 19 then widens = pk >= 6 and pk <= 9
		end
		if not widens then
			rep.kindMismatch = (rep.kindMismatch or 0) + 1
			return nil, false
		end
	end
	local oSigned = isSignedKind(okind)
	local val16, ok, raw16 = readInt16FromWriter(r, pe.shape)
	if not ok then return nil, false end
	-- the announced kind holds base plus offset (rangedHolds16) before the
	-- reader's own range and domain are asked
	if raw16 ~= nil and not rangedHolds16(pe.shape, val16, raw16) then
		rep.clamped = (rep.clamped or 0) + 1
		return nil, false
	end
	if widens then rep.widened = (rep.widened or 0) + 1 end
	if fd ~= nil then
		if fd.lo ~= nil and fd.hi ~= nil then
			local fshift = fd.fbits or 0
			local function shl16(src: buffer, sh: number): buffer
				if sh <= 0 then return src end
				local res = buffer.create(16)
				for bi = sh, 127 do
					if bit16(src, bi - sh) ~= 0 then setBit16(res, bi, 1) end
				end
				return res
			end
			local function cmp16(a: buffer, b: buffer, sgn: boolean): number
				if sgn then
					local sa, sb = bit16(a, 127), bit16(b, 127)
					if sa ~= sb then return if sa ~= 0 then -1 else 1 end
				end
				for bi = 15, 0, -1 do
					local ba, bb = buffer.readu8(a, bi), buffer.readu8(b, bi)
					if ba ~= bb then return if ba < bb then -1 else 1 end
				end
				return 0
			end
			local lo16 = shl16(defTo16(fd.lo), fshift)
			local hi16 = shl16(defTo16(fd.hi), fshift)
			if cmp16(val16, lo16, oSigned) < 0 or cmp16(val16, hi16, oSigned) > 0 then
				rep.clamped = (rep.clamped or 0) + 1
				return nil, false
			end
			elseif fd.bits ~= nil and fd.bits > 0 and fd.bits < 128 then
			for bi = fd.bits, 127 do
				if bit16(val16, bi) ~= 0 then
					rep.clamped = (rep.clamped or 0) + 1
					return nil, false
				end
			end
		end
	end
	local packed16, pok = checkAndPackInt16ForOwn(val16, oe.shape)
	if not pok or packed16 == nil then
		rep.clamped = (rep.clamped or 0) + 1
		return nil, false
	end
	return { kind = okind, packing = oe.shape.packing or 0, bits = oe.shape.bits or kindBits(okind), raw16 = packed16, val16 = val16 }, true
end

ownFieldElides = function(ov: any, fd: any, oe: any, node: any): boolean
	if fd == nil or fd.required == true or fd.present ~= nil then return false end
	local k = node.kind
	if k == 15 then return node.armRef == 0 end
	if k == 12 then return node.str == (fd.def or "") end
	if k == 13 then return #node.fields == 0 end
	if k == 14 then
		if fd.s == "bytes" then
			local def = fd.def or ""
			if typeof(def) ~= "string" or #node.elems ~= #def then return false end
			for idx = 1, #def do
				local el = node.elems[idx]
				if el == nil or el.val16 == nil or buffer.readu8(el.val16, 0) ~= string.byte(def, idx) then return false end
			end
			return true
		end
		if oe.shape.min == oe.shape.max then
			if oe.shape.elem == 13 then return false end
			local ef = if fd.e ~= nil then named(fd.e, fd.name) else fd
			local oeElem = { shape = oe.element or newShape(), element = if oe.element ~= nil then oe.element.element else nil }
			for _, el in node.elems do
				if not ownFieldElides(ov, ef, oeElem, el) then return false end
			end
			return true
		end
		return #node.elems == 0
	end
	if k == 16 then return #node.pairs == 0 end
	if k == 30 then
		local def = fd.def or 0
		if typeof(def) == "integer" then def = integer.tonumber(def) end
		if def == 0 or fd.vs == nil or fd.vs[def] == nil then return node.ref == 0 end
		return node.ref == select(1, ownNameEntryById(ov, fd.vs[def][2]))
	end
	if k == 1 then return (node.bit ~= 0) == (fd.def == true) end
	if k == 10 then
		local def = fd.def or 0
		if typeof(def) == "integer" then def = integer.tonumber(def) end
		if (oe.shape.packing or 0) == 2 then
			local dq = dequantizeF32(oe.shape, node.idx)
			return dq == asF32(def)
		end
		buffer.writef32(f32scratch, 0, def)
		return node.raw32 == buffer.readu32(f32scratch, 0)
	end
	if k == 11 then
		local def = fd.def or 0
		if typeof(def) == "integer" then def = integer.tonumber(def) end
		buffer.writef64(defScratch, 0, def)
		return node.lo == buffer.readu32(defScratch, 0) and node.hi == buffer.readu32(defScratch, 4)
	end
	return eq16(node.val16, defTo16(fd.def or 0))
end

emptyOwnBody = function(ov: any, rootName: string, desc: any): { any }
	if desc == nil or desc.fields == nil then return {} end
	local items: { any } = {}
	for idx1, fd in desc.fields do
		if fd.present ~= nil then continue end
		local oref, oe = ownSlotEntry(ov, rootName, desc.name, idx1, fd.id)
		if oref == 0 or oe == nil then continue end
		if oe.shape.kind == 13 and fd.s == "tab" and fd.t ~= nil then
			local subFields = emptyOwnBody(ov, rootName, S.lookup(fd.t))
			if #subFields > 0 then
				items[#items + 1] = { ord = fd.o or (idx1 - 1), ref = oref, node = { kind = 13, fields = subFields } }
			end
		elseif oe.shape.kind == 14 and oe.shape.min == oe.shape.max and oe.shape.elem == 13 and oe.shape.min > 0 then
			local subName = if fd.e ~= nil and fd.e.t ~= nil then fd.e.t else fd.t
			local subDesc = if subName ~= nil then S.lookup(subName) else nil
			local elems = {}
			for j = 1, oe.shape.min do
				elems[j] = { kind = 13, fields = emptyOwnBody(ov, rootName, subDesc) }
			end
			items[#items + 1] = { ord = fd.o or (idx1 - 1), ref = oref, node = { kind = 14, min = oe.shape.min, max = oe.shape.max, elemKind = 13, elems = elems } }
		end
	end
	return items
end

-- bornOwnElem is one fresh element of an own array, as the constructor is
-- born: the slot a grown fixed bound adds (docs/SPEC-TABLES.md §21.1). Zero,
-- false, empty text, an empty union, None for an enum, a record at its
-- declared defaults; never the field's whole default.
bornOwnElem = function(ov: any, rootName: string, oe: any, fd: any): any
	local shape = oe.shape
	local k = shape.kind
	if k == 0 or k == 32 then return { kind = k } end
	if k == 1 then return { kind = 1, bit = 0 } end
	if k == 10 then
		if (shape.packing or 0) == 2 then
			return { kind = 10, packing = 2, bits = shape.bits, idx = select(1, quantizeF32(shape, 0)) }
		end
		return { kind = 10, packing = 0, raw32 = 0 }
	end
	if k == 11 then return { kind = 11, lo = 0, hi = 0 } end
	if k == 12 then return { kind = 12, max = shape.max, str = "" } end
	if k == 13 then
		local subDesc = if fd ~= nil and fd.t ~= nil then S.lookup(fd.t) else nil
		return { kind = 13, fields = emptyOwnBody(ov, rootName, subDesc) }
	end
	if k == 14 then
		local inner = { shape = oe.element or newShape(), element = if oe.element ~= nil then oe.element.element else nil }
		local elems = {}
		if shape.min == shape.max then
			for j = 1, shape.min do
				elems[j] = bornOwnElem(ov, rootName, inner, fd)
			end
		end
		return { kind = 14, min = shape.min, max = shape.max, elemKind = shape.elem, elems = elems }
	end
	if k == 15 then return { kind = 15, armRef = 0, payload = nil } end
	if k == 16 then return { kind = 16, max = shape.max, pairs = {} } end
	if k == 30 then return { kind = 30, ref = 0 } end
	local val16 = buffer.create(16)
	local packed16 = checkAndPackInt16ForOwn(val16, shape)
	if packed16 == nil then
		-- a ranged element whose range excludes zero is born at its min
		val16, packed16 = shape.base16, buffer.create(16)
	end
	return { kind = k, packing = shape.packing or 0, bits = shape.bits or kindBits(k), raw16 = packed16, val16 = val16 }
end

-- ownFieldElides forward-declared above transcodeValue
transcodeBody = function(r: any, pv: any, ov: any, rootName: string, desc: any, rep: any): ({ any }?, boolean)
	local items = {}
	local seenPresent: { [integer]: boolean } = {}
	while true do
		local pref, ok = get(r, pv.refBits)
		if not ok then return nil, false end
		if pref == 0 then break end
		local pe = entryOf(pv, pref)
		if pe == nil then return nil, false end
		local fd = fieldById(desc, pe.id)
		local oref, oe = 0, nil
		if fd ~= nil then
			seenPresent[fd.id] = true
			oref, oe = ownSlotEntry(ov, rootName, desc.name, (fd.o or 0) + 1, pe.id)
		end
		if fd == nil or oref == 0 or oe == nil then
			local st: any = { v = pv, refBits = pv.refBits, owner = nil, nonFinite = nil }
			if not skipShape(r, st, pe, nil, nil, true) then return nil, false end
			rep.unknown = (rep.unknown or 0) + 1
		else
			local node, vok = transcodeValue(r, pv, ov, rootName, pe, oe, fd, rep)
			if not vok or node == nil then return nil, false end
			if not ownFieldElides(ov, fd, oe, node) then
				items[#items + 1] = { ord = fd.o or 0, ref = oref, node = node }
			end
		end
	end
	if desc ~= nil and desc.fields ~= nil then
		for idx1, fd in desc.fields do
			if seenPresent[fd.id] or fd.present ~= nil then continue end
			local oref, oe = ownSlotEntry(ov, rootName, desc.name, idx1, fd.id)
			if oref == 0 or oe == nil then continue end
			if oe.shape.kind == 13 and fd.s == "tab" and fd.t ~= nil then
				local subFields = emptyOwnBody(ov, rootName, S.lookup(fd.t))
				if #subFields > 0 then
					items[#items + 1] = { ord = fd.o or (idx1 - 1), ref = oref, node = { kind = 13, fields = subFields } }
				end
			elseif oe.shape.kind == 14 and oe.shape.min == oe.shape.max and oe.shape.elem == 13 and oe.shape.min > 0 then
				local subName = if fd.e ~= nil and fd.e.t ~= nil then fd.e.t else fd.t
				local subDesc = if subName ~= nil then S.lookup(subName) else nil
				local elems = {}
				for j = 1, oe.shape.min do
					elems[j] = { kind = 13, fields = emptyOwnBody(ov, rootName, subDesc) }
				end
				items[#items + 1] = { ord = fd.o or (idx1 - 1), ref = oref, node = { kind = 14, min = oe.shape.min, max = oe.shape.max, elemKind = 13, elems = elems } }
			end
		end
	end
	table.sort(items, function(a: any, b: any) return (a.ord :: number) < (b.ord :: number) end)
	return items, true
end

local function bwPut(w: any, val: number, bits: number)
	for i = 0, bits - 1 do
		local b = math.floor(val / 2 ^ i) % 2
		local bi = math.floor(w.off / 8) + 1
		if w.bytes[bi] == nil then w.bytes[bi] = 0 end
		if b ~= 0 then
			w.bytes[bi] = bit32.bor(w.bytes[bi], bit32.lshift(1, w.off % 8))
		end
		w.off = w.off + 1
	end
end

local function bwPut16(w: any, raw16: buffer, bits: number)
	for i = 0, bits - 1 do
		bwPut(w, bit16(raw16, i), 1)
	end
end

local function bwAlign(w: any)
	local rem = w.off % 8
	if rem ~= 0 then
		w.off = w.off + (8 - rem)
		local bi = math.floor((w.off - 1) / 8) + 1
		if w.bytes[bi] == nil then w.bytes[bi] = 0 end
	end
end

local encodeOwnValue: any
local function encodeOwnBody(w: any, ov: any, fields: { any })
	for _, it in fields do
		bwPut(w, it.ref, ov.refBits)
		encodeOwnValue(w, ov, it.node)
	end
	bwPut(w, 0, ov.refBits)
end

encodeOwnValue = function(w: any, ov: any, node: any)
	local k = node.kind
	if k == 0 or k == 32 then
		return
	elseif k == 1 then
		bwPut(w, node.bit, 1)
	elseif k == 10 then
		if node.packing == 2 then
			bwPut(w, node.idx, node.bits)
		else
			bwPut(w, node.raw32, 32)
		end
	elseif k == 11 then
		bwPut(w, node.lo, 32)
		bwPut(w, node.hi, 32)
	elseif k == 12 then
		bwPut(w, #node.str, bitsReq(0, node.max))
		bwAlign(w)
		for i = 1, #node.str do
			local bi = math.floor(w.off / 8) + 1
			w.bytes[bi] = string.byte(node.str, i)
			w.off = w.off + 8
		end
	elseif k == 13 then
		encodeOwnBody(w, ov, node.fields)
	elseif k == 14 then
		bwPut(w, #node.elems - node.min, bitsReq(node.min, node.max))
		if node.elemKind == 6 then bwAlign(w) end
		for _, el in node.elems do
			encodeOwnValue(w, ov, el)
		end
	elseif k == 15 then
		bwPut(w, node.armRef, ov.refBits)
		if node.armRef ~= 0 and node.payload ~= nil then
			encodeOwnValue(w, ov, node.payload)
		end
	elseif k == 16 then
		bwPut(w, #node.pairs, bitsReq(0, node.max))
		for _, p in node.pairs do
			bwPut(w, p.keyRef, ov.refBits)
			encodeOwnValue(w, ov, p.val)
		end
	elseif k == 30 then
		bwPut(w, node.ref, ov.refBits)
	else
		bwPut16(w, node.raw16, node.bits)
	end
end

local function packOwnBatch(bodies: { any }, sealed: boolean): (buffer, number)
	local w = { bytes = {} :: { number }, off = 0 }
	local ov = bodies[1].ownVocab
	for _, b in bodies do
		encodeOwnBody(w, ov, b.ownTree)
	end
	local payloadBytes = math.ceil(w.off / 8)
	local batchBytes = 2 + payloadBytes
	local size = batchBytes + (if sealed then 4 else 0)
	local out = buffer.create(size)
	buffer.fill(out, 0, 0, size)
	buffer.writeu8(out, 0, 2)
	buffer.writeu8(out, 1, #bodies - 1)
	for i = 1, payloadBytes do
		buffer.writeu8(out, 1 + i, w.bytes[i] or 0)
	end
	if sealed then
		buffer.writeu32(out, batchBytes, crc32c(out, batchBytes))
	end
	return out, size
end

-- loadMessagesWalk is the walk batch read (docs/SPEC-TABLES.md §6.5,
-- §3.3): evolution may recover a value, but damage discards the whole batch.
function M.loadMessagesWalk(v: any, root: string, data: buffer, capacity: number, report: any?): (number, any)
	local desc = messageRoot(root)
	if report == nil then report = {} end
	reset(report)
	local len = buffer.len(data)
	if len < 1 then report.malformed = true; return -1, {} end
	if buffer.readu8(data, 0) ~= 2 then return -1, refuse(report, "newer_form") end
	-- THE INTEGRITY TRAILER IS CHECKED BEFORE ANY BODY IS DECODED
	-- (docs/SPEC-TABLES.md §2.14, §3.3): the form byte is read first, then a
	-- declaring root's batch must end in the CRC-32C of the bytes before it,
	-- and only then is the trailer removed and §3.3's batch read. A table
	-- without the key reads the bytes it read before.
	if desc.integrity ~= nil and desc.integrity ~= "" then
		local body = integrityOpen(data)
		if body == nil then
			report.malformed = true
			report.reason = "integrity_mismatch"
			return -1, {}
		end
		data = body
		len = buffer.len(data)
	end
	if not v.announced then return -1, refuse(report, "no_vocabulary") end
	if len < 2 then report.malformed = true; return -1, {} end
	local count = buffer.readu8(data, 1) + 1
	if count > capacity then
		refuse(report, "batch_too_large")
		return count, {}
	end
	local got, bodies, malformed, dep, nonFinite = walkBodies(v, data, desc, count, false)
	if nonFinite ~= nil then
		-- the refusal outranks every verdict a body could reach after it
		refuseNonFinite(report, nonFinite, true)
		return -1, {}
	end
	if malformed then
		report.malformed = true
		return 0, {}
	end
	local ownBv = S.buildOf(desc.name)
	if ownBv ~= nil then
		local ov = getOwnVocab(desc.name)
		if ov ~= nil then
			local transcoded = {}
			for i = 1, got do
				local b = bodies[i]
				local br = newReader(b.buffer, b.from)
				br.n = b.to
				local tree, tok = transcodeBody(br, v, ov, desc.name, desc, report)
				if not tok or tree == nil then
					return got, {}
				end
				transcoded[i] = { ownTree = tree, ownVocab = ov, from = b.from, to = b.to }
			end
			return got, transcoded
		end
	end
	if dep.found then
		local copy = buffer.create(len)
		buffer.copy(copy, 0, data, 0, len)
		local got2, bodies2, malformed2 = walkBodies(v, copy, desc, count, true)
		if malformed2 then
			report.malformed = true
			return 0, {}
		end
		return got2, bodies2
	end
	return got, bodies
end

-- LoadMessages is the PLAIN batch read, and it is STRICT (docs/SPEC-TABLES.md
-- §6.5, Go's <T>LoadMessages over <T>loadMessagesWalk): a batch the
-- walk read found damaged, or whose report counts a clamp or a kind that
-- moved, answers -1 with no body handed back and report.reason the one name
-- (wire_damaged, clamped, kind_mismatch), and a refusal keeps its own name.
-- unknown and widened alone never refuse.
function M.LoadMessages(v: any, root: string, data: buffer, capacity: number, report: any?): (number, any)
	local rep: any = report or {}
	local count, bodies = M.loadMessagesWalk(v, root, data, capacity, rep)
	if count < 0 then return count, bodies end
	-- a refusal the walk read already named answers as it did (batch_too_large
	-- keeps the count the caller needs)
	if rep.reason ~= nil and rep.reason ~= "" then return count, bodies end
	if rep.malformed then rep.reason = "wire_damaged"; return -1, {} end
	if (rep.clamped or 0) > 0 then rep.reason = "clamped"; return -1, {} end
	if (rep.kindMismatch or 0) > 0 then rep.reason = "kind_mismatch"; return -1, {} end
	return count, bodies
end

local function bodyBits(bodies: any): number
	local bits = 0
	for _, b in bodies do
		bits = bits + (b.to - b.from)
	end
	return bits
end

-- deprecatedField is the strict write's refusal: a body handed to a writer
-- that still holds a deprecated variant names <Table>.<field>, exactly as a
-- value does (docs/SPEC-TABLES.md §2.15). Bodies returned by LoadMessages are
-- already normalized and find none.
local function deprecatedField(v: any, root: string, bodies: any): (string?, string?)
	local desc = S.lookup(root)
	for _, b in bodies do
		local st: any = { v = v, refBits = v.refBits, owner = nil, nonFinite = nil }
		local r = newReader(b.buffer, b.from)
		r.n = b.to
		local dep = { write = false, found = false, field = nil, table = nil }
		local walked = skipBody(r, st, desc, dep)
		-- EVERY WRITER REFUSES A NaN OR AN INFINITY (docs/SPEC-TABLES.md §3.3)
		if st.nonFinite ~= nil then return nil, st.nonFinite end
		if walked and dep.found then
			return dep.field or "?", nil
		end
	end
	return nil, nil
end

local function refuseDeprecated(report: any, field: string): boolean
	report.verdict = 1
	report.reason = "invalid_value"
	report.field = field
	return false
end

local msgScratch = buffer.create(16)

local function mNewWriter(measuring: boolean): any
	return { b = if measuring then nil else buffer.create(64), bits = 0, count = measuring }
end

local function mReserve(w: any, addBits: number)
	if w.count then return end
	local need = math.ceil((w.bits + addBits) / 8)
	local cap = buffer.len(w.b)
	if need <= cap then return end
	while cap < need do cap = cap * 2 end
	local nb = buffer.create(cap)
	buffer.fill(nb, 0, 0, cap)
	buffer.copy(nb, 0, w.b, 0, math.ceil(w.bits / 8))
	w.b = nb
end

local function mPut(w: any, value: number, width: number)
	if width <= 0 then return end
	if not w.count then
		mReserve(w, width)
		local at = w.bits
		local sh = at % 8
		local byteAt = math.floor(at / 8)
		local got = 0
		while got < width do
			local take = math.min(8 - sh, width - got)
			local byte = buffer.readu8(w.b, byteAt)
			local chunk = math.floor(value / 2 ^ got) % 2 ^ take
			buffer.writeu8(w.b, byteAt, byte + chunk * 2 ^ sh)
			got = got + take
			sh = sh + take
			if sh == 8 then sh = 0; byteAt = byteAt + 1 end
		end
	end
	w.bits = w.bits + width
end

local function mAlign(w: any)
	local rem = w.bits % 8
	if rem ~= 0 then mPut(w, 0, 8 - rem) end
end

local function mPutBytes(w: any, src: buffer, len: number)
	mAlign(w)
	if len <= 0 then return end
	if not w.count then
		mReserve(w, len * 8)
		buffer.copy(w.b, math.floor(w.bits / 8), src, 0, len)
	end
	w.bits = w.bits + len * 8
end

local function mPutIntBits(w: any, val: any, signed: boolean, shape: any, width: number)
	if width <= 0 then return end
	buffer.fill(msgScratch, 0, 0, 16)
	if typeof(val) == "table" then
		buffer.writeinteger(msgScratch, 0, val.lo)
		buffer.writeinteger(msgScratch, 8, val.hi)
	elseif typeof(val) == "integer" then
		buffer.writeinteger(msgScratch, 0, val)
		if signed and integer.lt(val, 0i) then buffer.fill(msgScratch, 8, 0xFF, 8) end
	else
		buffer.writeu32(msgScratch, 0, val % 2 ^ 32)
		buffer.writeu32(msgScratch, 4, math.floor(val / 2 ^ 32) % 2 ^ 32)
		if signed and val < 0 then buffer.fill(msgScratch, 8, 0xFF, 8) end
	end
	if shape.packing == 1 then
		local baseWords = { 0, 0, 0, 0 }
		if shape.baseLo ~= nil then
			local bs = buffer.create(16)
			buffer.writeinteger(bs, 0, shape.baseLo)
			buffer.writeinteger(bs, 8, shape.baseHi)
			for i = 1, 4 do baseWords[i] = buffer.readu32(bs, (i - 1) * 4) end
		elseif shape.base ~= nil then
			baseWords[1] = shape.base % 2 ^ 32
			baseWords[2] = math.floor(shape.base / 2 ^ 32) % 2 ^ 32
		end
		local borrow = 0
		for i = 0, 3 do
			local cur = buffer.readu32(msgScratch, i * 4)
			local sub = cur - baseWords[i + 1] - borrow
			if sub < 0 then
				sub = sub + 2 ^ 32
				borrow = 1
			else
				borrow = 0
			end
			buffer.writeu32(msgScratch, i * 4, sub)
		end
	end
	local rem = width
	local wordIdx = 0
	while rem > 0 do
		local take = math.min(32, rem)
		local word = buffer.readu32(msgScratch, wordIdx * 4)
		if take < 32 then word = word % (2 ^ take) end
		mPut(w, word, take)
		rem = rem - take
		wordIdx = wordIdx + 1
	end
end

local function mIs128(x: any): boolean
	return typeof(x) == "table" and x.lo ~= nil and x.hi ~= nil
end

local function mCmpLt(a: any, b: any, signed: boolean): boolean
	if mIs128(a) and mIs128(b) then
		if a.hi ~= b.hi then
			return if signed then integer.lt(a.hi, b.hi) else integer.ult(a.hi, b.hi)
		end
		return integer.ult(a.lo, b.lo)
	end
	if typeof(a) == "integer" or typeof(b) == "integer" then
		local ia = if typeof(a) == "integer" then a else integer.create(a)
		local ib = if typeof(b) == "integer" then b else integer.create(b)
		return if signed then integer.lt(ia, ib) else integer.ult(ia, ib)
	end
	return a < b
end

local function mValidScalar(e: any, val: any): (boolean, string?)
	if e.s == "bool" then
		return typeof(val) == "boolean", nil
	elseif e.s == "float" then
		if typeof(val) ~= "number" then return false, "non_finite_float" end
		if e.f32 then
			buffer.writef32(msgScratch, 0, val)
			local bits = buffer.readu32(msgScratch, 0)
			if bit32.band(bits, 0x7F800000) == 0x7F800000 then return false, "non_finite_float" end
		else
			if val ~= val or val == math.huge or val == -math.huge then return false, "non_finite_float" end
		end
		-- the declared range is compared at float32 for a float32 field
		-- (docs/SPEC.md §4.6)
		local held = if e.f32 then asF32(val) else val
		if e.lo ~= nil and (held < e.lo or held > e.hi) then return false, "invalid_value" end
		return true, nil
	elseif e.s == "int" then
		local ty = typeof(val)
		if e.kw == 16 then
			if not mIs128(val) then return false, "invalid_value" end
		elseif e.kw == 8 then
			if ty == "number" and val == math.floor(val) and math.abs(val) <= 9007199254740991 then
				val = integer.create(val)
				ty = "integer"
			end
			if ty ~= "integer" then return false, "invalid_value" end
		else
			if ty ~= "number" or val ~= math.floor(val) then return false, "invalid_value" end
			local n = e.kw * 8
			local lo = if e.signed then -(2 ^ (n - 1)) else 0
			local hi = if e.signed then 2 ^ (n - 1) - 1 else 2 ^ n - 1
			if val < lo or val > hi then return false, "invalid_value" end
		end
		if e.lo ~= nil then
			local lo, hi = e.lo, e.hi
			if e.fbits ~= nil and e.fbits > 0 then
				if typeof(lo) == "integer" then
					lo = integer.lshift(lo, integer.create(e.fbits))
					hi = integer.lshift(hi, integer.create(e.fbits))
				elseif not mIs128(lo) then
					lo = lo * (2 ^ e.fbits)
					hi = hi * (2 ^ e.fbits)
				end
			end
			local isSigned = e.signed == true
			if mCmpLt(val, lo, isSigned) or mCmpLt(hi, val, isSigned) then return false, "invalid_value" end
		end
		if e.bits ~= nil and e.bits > 0 and e.bits < 64 then
			local top: any = if typeof(val) == "integer" then integer.sub(integer.lshift(1i, integer.create(e.bits)), 1i) else (2 ^ e.bits - 1)
			if mCmpLt(top, val, false) then return false, "invalid_value" end
		end
		return true, nil
	end
	return true, nil
end

local function mAtDefault(fd: any, val: any): boolean
	if fd.s == "bool" then return (val == true) == (fd.def == true) end
	if fd.s == "enum" then return val == (fd.def or 0) end
	if fd.s == "float" then
		local def = fd.def or 0.0
		if fd.lo ~= nil then
			if val == 0 then val = 0 end
			if def == 0 then def = 0 end
		end
		if fd.f32 then
			buffer.writef32(msgScratch, 0, val)
			local a = buffer.readu32(msgScratch, 0)
			buffer.writef32(msgScratch, 0, def)
			local b = buffer.readu32(msgScratch, 0)
			if fd.lo ~= nil then
				if a == 0x80000000 then a = 0 end
				if b == 0x80000000 then b = 0 end
			end
			return a == b
		end
		buffer.writef64(msgScratch, 0, val)
		local alo, ahi = buffer.readu32(msgScratch, 0), buffer.readu32(msgScratch, 4)
		buffer.writef64(msgScratch, 0, def)
		local blo, bhi = buffer.readu32(msgScratch, 0), buffer.readu32(msgScratch, 4)
		return alo == blo and ahi == bhi
	end
	if fd.s == "int" then
		local def = fd.def or (if fd.kw == 8 then 0i else 0)
		if mIs128(val) or mIs128(def) then
			local alo = if mIs128(val) then val.lo else 0i
			local ahi = if mIs128(val) then val.hi else 0i
			local blo = if mIs128(def) then def.lo else 0i
			local bhi = if mIs128(def) then def.hi else 0i
			return alo == blo and ahi == bhi
		end
		if typeof(val) == typeof(def) then return val == def end
		local na = if typeof(val) == "integer" then integer.tonumber(val) else val
		local nb = if typeof(def) == "integer" then integer.tonumber(def) else def
		return na == nb
	end
	return false
end

local mEncodeBody: any
local mEncodeArm: any

local function mFail(c: any, reason: string, owner: string?, fname: string?): boolean
	if c.reason == nil then
		c.reason = reason
		c.field = (owner or "?") .. "." .. (fname or "?")
	end
	return false
end

local function mEncodeScalar(c: any, owner: string, e: any, kind: number, shape: any, val: any, fname: string): boolean
	local ok, why = mValidScalar(e, val)
	if not ok then return mFail(c, why or "invalid_value", owner, fname) end
	-- a ranged float's -0 is stored as +0 (docs/SPEC-TABLES.md §3.3)
	if (kind == 10 or kind == 11) and e.lo ~= nil and val == 0 then val = 0 end
	if kind == 1 then
		mPut(c.w, if val then 1 else 0, 1)
		return true
	elseif kind == 10 then
		if shape.packing == 2 then
			local norm = asF32(asF32(val - shape.qmin) / asF32(shape.qmax - shape.qmin))
			if norm < 0 then norm = 0 elseif norm > 1 then norm = 1 end
			local idx = math.floor(asF32(norm * shape.count) + 0.5)
			if idx > shape.count then idx = shape.count end
			mPut(c.w, idx, shape.bits)
			return true
		end
		buffer.writef32(msgScratch, 0, val)
		mPut(c.w, buffer.readu32(msgScratch, 0), 32)
		return true
	elseif kind == 11 then
		buffer.writef64(msgScratch, 0, val)
		mPut(c.w, buffer.readu32(msgScratch, 0), 32)
		mPut(c.w, buffer.readu32(msgScratch, 4), 32)
		return true
	end
	mPutIntBits(c.w, val, e.signed == true, shape, shape.bits)
	return true
end

local function mEncodeEnum(c: any, owner: string, e: any, val: any, fname: string): boolean
	if typeof(val) ~= "number" or val ~= math.floor(val) or val < 0 or val > #(e.vs or {}) then
		return mFail(c, "invalid_value", owner, fname)
	end
	if val == 0 then
		mPut(c.w, 0, c.refBits)
		return true
	end
	local pair = e.vs[val]
	if pair == nil or pair[3] == true then
		return mFail(c, "invalid_value", owner, fname)
	end
	local slot = c.byId[pair[2]]
	if slot == nil then return mFail(c, "invalid_value", owner, fname) end
	mPut(c.w, slot, c.refBits)
	return true
end

local function mCountBits(shape: any): number
	if shape.max <= shape.min then return 0 end
	return bitsReq(0, shape.max - shape.min)
end

local function mAligns(kind: number, shape: any): boolean
	if kind == 12 then return true end
	if kind == 14 and shape.elem == 6 and shape.element ~= nil and shape.element.packing == 0 and shape.element.bits == 8 then
		return true
	end
	return false
end

local function mEncodeElem(c: any, owner: string, fd: any, shape: any, x: any): boolean
	local e = fd.e
	local ek = shape.elem
	local inner = shape.element or newShape()
	if ek == 13 then
		return mEncodeBody(c, S.lookup(e.t), x)
	elseif ek == 30 then
		return mEncodeEnum(c, owner, e, x, fd.name)
	elseif ek == 15 then
		if typeof(x) ~= "table" then return mFail(c, "invalid_value", owner, fd.name) end
		local un = S.lookup(e.t)
		local tag = x.Type or 0
		if typeof(tag) ~= "number" or tag ~= math.floor(tag) or tag < 0 or tag > #un.arms then
			return mFail(c, "invalid_value", owner, fd.name)
		end
		if tag == 0 then
			mPut(c.w, 0, c.refBits)
			return true
		end
		return mEncodeArm(c, owner, un, x, fd.name)
	end
	return mEncodeScalar(c, owner, e, ek, inner, x, fd.name)
end

local function mEncodeFieldPayload(c: any, owner: string, fd: any, entrySlot: number, entry: any, holder: any): boolean
	local shape = entry.shape
	local s = fd.s
	local val = holder[fd.slot]
	if s == "str" or s == "bytes" then
		local n = holder[fd.slot .. "Length"]
		if n == nil and typeof(val) == "buffer" then n = buffer.len(val) end
		if typeof(val) ~= "buffer" or typeof(n) ~= "number" or n ~= math.floor(n) or n < 0 or n > (fd.bound or 0) or n > buffer.len(val) then
			return mFail(c, "invalid_value", owner, fd.name)
		end
		if s == "str" and not textOk(val, 0, n) then
			return mFail(c, "invalid_value", owner, fd.name)
		end
		local cb = if s == "bytes" then mCountBits(shape) else bitsReq(0, shape.max)
		mPut(c.w, n, cb)
		mAlign(c.w)
		mPutBytes(c.w, val, n)
		return true
	elseif s == "tab" then
		return mEncodeBody(c, S.lookup(fd.t), val)
	elseif s == "union" then
		if typeof(val) ~= "table" then return mFail(c, "invalid_value", owner, fd.name) end
		local un = S.lookup(fd.t)
		local tag = val.Type or 0
		if typeof(tag) ~= "number" or tag ~= math.floor(tag) or tag < 0 or tag > #un.arms then
			return mFail(c, "invalid_value", owner, fd.name)
		end
		if tag == 0 then
			mPut(c.w, 0, c.refBits)
			return true
		end
		return mEncodeArm(c, owner, un, val, fd.name)
	elseif s == "enum" then
		return mEncodeEnum(c, owner, fd, val, fd.name)
	elseif s == "arr" then
		if fd.array == "keyed" then
			local present = {}
			for slotIdx = 1, fd.bound do
				local x = val[slotIdx]
				local pair = fd.keyvs[slotIdx]
				if pair == nil then continue end
				local probe = { w = mNewWriter(true), refBits = c.refBits, byId = c.byId, entries = c.entries, reason = nil, field = nil }
				if not mEncodeElem(probe, owner, fd, shape, x) then
					c.reason, c.field = probe.reason, probe.field
					return false
				end
				local rides = false
				if shape.elem == 13 then rides = probe.w.bits > c.refBits
				elseif shape.elem == 15 then rides = (typeof(x) == "table" and (x.Type or 0) ~= 0)
				else rides = not mAtDefault(fd.e, x) end
				if rides then
					if pair[3] == true then return mFail(c, "invalid_value", owner, fd.name) end
					present[#present + 1] = slotIdx
				end
			end
			mPut(c.w, #present, bitsReq(0, shape.max))
			for _, slotIdx in present do
				local kslot = c.byId[fd.keyvs[slotIdx][2]]
				mPut(c.w, kslot, c.refBits)
				if not mEncodeElem(c, owner, fd, shape, val[slotIdx]) then return false end
			end
			return true
		end
		local count = fd.bound or 0
		if fd.array == "counted" then
			count = holder[fd.slot .. "Count"] or 0
			if typeof(count) ~= "number" or count ~= math.floor(count) or count < (fd.min or 0) or count > (fd.bound or 0) then
				return mFail(c, "invalid_value", owner, fd.name)
			end
		end
		local cb = mCountBits(shape)
		if cb > 0 then mPut(c.w, count - shape.min, cb) end
		if mAligns(shape.kind, shape) then mAlign(c.w) end
		for i = 1, count do
			local elVal = if typeof(val) == "buffer" then buffer.readu8(val, i - 1) else val[i]
			if not mEncodeElem(c, owner, fd, shape, elVal) then return false end
		end
		return true
	end
	return mEncodeScalar(c, owner, fd, shape.kind, shape, val, fd.name)
end

mEncodeArm = function(c: any, owner: string, un: any, u: any, fname: string): boolean
	local tag = u.Type or 0
	local arm = un.arms[tag]
	if arm == nil then return mFail(c, "invalid_value", owner, fname) end
	local slot = c.byId[arm.id]
	if slot == nil then return mFail(c, "invalid_value", owner, fname) end
	mPut(c.w, slot, c.refBits)
	if arm.s == "void" then return true end
	local entry = c.entries[slot]
	return mEncodeFieldPayload(c, un.name, arm, slot, entry, u)
end

mEncodeBody = function(c: any, desc: any, rec: any): boolean
	if typeof(rec) ~= "table" then return mFail(c, "invalid_value", desc.name, "") end
	for _, fd in desc.fields do
		if fd.guard ~= nil then
			local active = true
			for _, g in fd.guard do
				if rec[g[1]] ~= g[2] then active = false; break end
			end
			if not active then continue end
		end
		local slot = c.byId[fd.id]
		if slot == nil then return mFail(c, "invalid_value", desc.name, fd.name) end
		local entry = c.entries[slot]
		if fd.present ~= nil then
			if not rec[fd.present] then continue end
			mPut(c.w, slot, c.refBits)
			if not mEncodeFieldPayload(c, desc.name, fd, slot, entry, rec) then return false end
			continue
		end
		local probe = { w = mNewWriter(true), refBits = c.refBits, byId = c.byId, entries = c.entries, reason = nil, field = nil }
		if not mEncodeFieldPayload(probe, desc.name, fd, slot, entry, rec) then
			c.reason, c.field = probe.reason, probe.field
			return false
		end
		if not fd.required then
			local val = rec[fd.slot]
			local elide = false
			if fd.s == "bool" or fd.s == "int" or fd.s == "float" or fd.s == "enum" then
				elide = mAtDefault(fd, val)
			elseif fd.s == "str" or fd.s == "bytes" then
				local n = rec[fd.slot .. "Length"] or 0
				local def = fd.def or ""
				elide = (n == #def and (n == 0 or buffer.readstring(val, 0, n) == def))
			elseif fd.s == "tab" then
				elide = (probe.w.bits <= c.refBits)
			elseif fd.s == "union" then
				elide = ((val.Type or 0) == 0)
			elseif fd.s == "arr" then
				if fd.array == "counted" then
					-- A COUNTED ARRAY IS BORN AT ITS MINIMUM (docs/SPEC-TABLES.md
					-- §22.2, tablewire messageElides): with a positive minimum
					-- it elides at exactly that count of default elements (a
					-- table element by a body that is its terminator alone);
					-- the probe above has already refused a count off [min, max]
					local n = rec[fd.slot .. "Count"] or 0
					local born = fd.min or 0
					if born == 0 then
						elide = (n == 0)
					elseif n == born then
						local allDef = true
						for i = 1, n do
							local el = if typeof(val) == "buffer" then buffer.readu8(val, i - 1) else val[i]
							if fd.e.s == "tab" then
								local ep = { w = mNewWriter(true), refBits = c.refBits, byId = c.byId, entries = c.entries, reason = nil, field = nil }
								if not mEncodeBody(ep, S.lookup(fd.e.t), el) then
									c.reason, c.field = ep.reason, ep.field
									return false
								end
								if ep.w.bits > c.refBits then allDef = false; break end
							elseif fd.e.s == "union" then
								if (el.Type or 0) ~= 0 then allDef = false; break end
							elseif not mAtDefault(fd.e, el) then
								allDef = false; break
							end
						end
						elide = allDef
					end
				elseif fd.array == "keyed" then
					elide = (probe.w.bits <= bitsReq(0, entry.shape.max))
				elseif fd.array == "fixed" and fd.e.s ~= "tab" then
					local allDef = true
					for i = 1, fd.bound or 0 do
						local el = if typeof(val) == "buffer" then buffer.readu8(val, i - 1) else val[i]
						if fd.e.s == "union" then
							if (el.Type or 0) ~= 0 then allDef = false; break end
						elseif not mAtDefault(fd.e, el) then
							allDef = false; break
						end
					end
					elide = allDef
				end
			end
			if elide then continue end
		end
		mPut(c.w, slot, c.refBits)
		if not mEncodeFieldPayload(c, desc.name, fd, slot, entry, rec) then return false end
	end
	mPut(c.w, 0, c.refBits)
	return true
end

local function mWriteRecordBatch(v: any, desc: any, bodies: any, measuring: boolean, report: any): (buffer?, number)
	reset(report)
	if not v.announced then
		refuse(report, "no_vocabulary")
		return nil, -1
	end
	local byId: { [integer]: number } = {}
	for idx, e in v.entries do
		if byId[e.id] == nil then byId[e.id] = idx end
	end
	local c: any = { w = mNewWriter(measuring), refBits = v.refBits, byId = byId, entries = v.entries, reason = nil, field = nil }
	for _, rec in bodies do
		if not mEncodeBody(c, desc, rec) then
			if c.reason == "non_finite_float" then
				refuseNonFinite(report, c.field or "?", false)
			else
				report.verdict = 1
				report.reason = c.reason or "invalid_value"
				report.field = c.field
			end
			return nil, -1
		end
	end
	mAlign(c.w)
	local sealed = desc.integrity ~= nil and desc.integrity ~= ""
	local batchBytes = 2 + math.floor(c.w.bits / 8)
	local size = batchBytes + (if sealed then 4 else 0)
	if measuring then return nil, size end
	local out = buffer.create(size)
	buffer.fill(out, 0, 0, size)
	buffer.writeu8(out, 0, 2)
	buffer.writeu8(out, 1, #bodies - 1)
	if c.w.bits > 0 then
		buffer.copy(out, 2, c.w.b, 0, math.floor(c.w.bits / 8))
	end
	if sealed then
		buffer.writeu32(out, batchBytes, crc32c(out, batchBytes))
	end
	return out, size
end

local function mIsBitSliceBatch(bodies: any): boolean
	return #bodies >= 1 and typeof(bodies[1]) == "table" and (typeof(bodies[1].buffer) == "buffer" or bodies[1].ownTree ~= nil)
end

function M.MeasureMessages(v: any, root: string, bodies: any, report: any?): number
	local desc = messageRoot(root)
	if report == nil then report = {} end
	local n = #bodies
	if n >= 1 and n <= BATCH_MAX and not mIsBitSliceBatch(bodies) then
		local _, sz = mWriteRecordBatch(v, desc, bodies, true, report)
		return sz
	end
	reset(report)
	if n < 1 or n > BATCH_MAX then
		refuse(report, "batch_too_large")
		return -1
	end
	if bodies[1] ~= nil and bodies[1].ownTree ~= nil then
		local sealed = desc.integrity ~= nil and desc.integrity ~= ""
		local _, sz = packOwnBatch(bodies, sealed)
		return sz
	end
	local bad, nonFinite = deprecatedField(v, root, bodies)
	if nonFinite ~= nil then
		refuseNonFinite(report, nonFinite, false)
		return -1
	end
	if bad ~= nil then
		refuseDeprecated(report, bad)
		return -1
	end
	-- THE MEASURE COUNTS THE TRAILER when the root declares integrity
	-- (docs/SPEC-TABLES.md §2.14, §3.3): four bytes more than the batch.
	local size = 2 + math.ceil(bodyBits(bodies) / 8)
	if desc.integrity ~= nil and desc.integrity ~= "" then size = size + 4 end
	return size
end

local function copyBits(dst: buffer, dstOff: number, src: buffer, srcOff: number, width: number)
	for i = 0, width - 1 do
		local sbit = srcOff + i
		local sb = math.floor(sbit / 8)
		local sv = math.floor(buffer.readu8(src, sb) / 2 ^ (sbit % 8)) % 2
		local dbit = dstOff + i
		local db = math.floor(dbit / 8)
		local dv = buffer.readu8(dst, db)
		local mask = 2 ^ (dbit % 8)
		if sv == 0 then
			buffer.writeu8(dst, db, dv % 256 - ((math.floor(dv / mask) % 2) * mask))
		else
			buffer.writeu8(dst, db, dv % 256 + (1 - math.floor(dv / mask) % 2) * mask)
		end
	end
end

function M.SaveMessages(v: any, root: string, bodies: any, report: any?): (buffer?, number)
	local desc = messageRoot(root)
	if report == nil then report = {} end
	local n = #bodies
	if n >= 1 and n <= BATCH_MAX and not mIsBitSliceBatch(bodies) then
		return mWriteRecordBatch(v, desc, bodies, false, report)
	end
	reset(report)
	if n < 1 or n > BATCH_MAX then
		refuse(report, "batch_too_large")
		return nil, -1
	end
	if bodies[1] ~= nil and bodies[1].ownTree ~= nil then
		local sealed = desc.integrity ~= nil and desc.integrity ~= ""
		return packOwnBatch(bodies, sealed)
	end
	local bad, nonFinite = deprecatedField(v, root, bodies)
	if nonFinite ~= nil then
		refuseNonFinite(report, nonFinite, false)
		return nil, -1
	end
	if bad ~= nil then
		refuseDeprecated(report, bad)
		return nil, -1
	end
	local bits = bodyBits(bodies)
	local batchBytes = 2 + math.ceil(bits / 8)
	-- THE WRITER EMITS THE BATCH AND THEN APPENDS THE TRAILER for a root
	-- that declares integrity (docs/SPEC-TABLES.md §2.14, §3.3): four bytes,
	-- little-endian, the CRC-32C of the form byte, the count byte and the
	-- padded bit stream. A table without the key writes its old bytes.
	local sealed = desc.integrity ~= nil and desc.integrity ~= ""
	local size = batchBytes + (if sealed then 4 else 0)
	local out = buffer.create(size)
	buffer.fill(out, 0, 0, size)
	buffer.writeu8(out, 0, 2)
	buffer.writeu8(out, 1, n - 1)
	local at = 16
	for _, b in bodies do
		copyBits(out, at, b.buffer, b.from, b.to - b.from)
		at = at + (b.to - b.from)
	end
	if sealed then
		buffer.writeu32(out, batchBytes, crc32c(out, batchBytes))
	end
	return out, size
end

return table.freeze(M)
