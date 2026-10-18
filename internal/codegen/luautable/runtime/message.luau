--!strict
-- message.luau — the Luau table runtime's MESSAGE FORM (docs/SPEC-TABLES.md
-- §3.3), form byte 2: a batch of bitpacked bodies under one announced
-- vocabulary. The announcement is an ordinary form-1 file whose body carries
-- the build version and the vocabulary; the batch is the form byte, the
-- count, and one continuous bit stream. This file is the one walk both
-- directions share; it reads the announced entries and the descriptors
-- luautable.go emits as data (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")

local M = {}

local BATCH_MAX = 256
local NODE_ID: integer = -1i          -- 0xffffffffffffffff
local VERSION_ID: integer = -2i       -- 0xfffffffffffffffe
local VOCAB_ID: integer = -3i         -- 0xfffffffffffffffd
local BYTES_ID: integer = 0x2f2ec0474f1c4fe4i
local STRING_ID: integer = 0x704be0d8faaffc58i

local function bitsReq(lo: number, hi: number): number
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

local function rLeb(r: any): number
	local v = 0
	local s = 0
	for i = 0, 9 do
		if r.at >= r.e then r.bad = true; return 0 end
		local x = buffer.readu8(r.b, r.at)
		r.at = r.at + 1
		v = v + (x % 0x80) * 2 ^ s
		if x < 0x80 then return v end
		s = s + 7
	end
	r.bad = true
	return 0
end


local function kindBits(k: number): number
	if k == 1 then return 1 end
	if k == 2 or k == 6 or k == 20 or k == 25 then return 8 end
	if k == 3 or k == 7 or k == 21 or k == 26 then return 16 end
	if k == 4 or k == 8 or k == 10 or k == 22 or k == 27 then return 32 end
	if k == 5 or k == 9 or k == 11 or k == 23 or k == 28 then return 64 end
	if k == 18 or k == 19 or k == 24 or k == 29 then return 128 end
	return 0
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
			s.bits = n
			if k >= 18 then
				if r.at + 16 > r.e then r.bad = true; return s end
				r.at = r.at + 16
			else
				rLeb(r)
			end
			return s
		elseif packing == 2 and k == 10 then
			if r.at + 12 > r.e then r.bad = true; return s end
			r.at = r.at + 12
			return s
		else
			r.bad = true
			return s
		end
	end
	if k == 12 or k == 33 then
		s.max = rLeb(r)
	elseif k == 14 then
		s.min = rLeb(r)
		s.max = rLeb(r)
	elseif k == 16 then
		s.max = rLeb(r)
	end
	return s
end

local function entryRead(r: any): any
	local e = { id = 0i, shape = newShape(), element = newShape() }
	if r.at + 9 > r.e then r.bad = true; return e end
	e.id = buffer.readinteger(r.b, r.at)
	r.at = r.at + 8
	local kind = rU8(r)
	if kind > 33 then r.bad = true; return e end
	e.shape = shapeRead(r, kind)
	if r.bad then return e end
	if kind == 14 or kind == 16 then
		local elem = rU8(r)
		if elem > 33 or elem == 12 or elem == 33 then r.bad = true; return e end
		e.shape.elem = elem
		e.element = shapeRead(r, elem)
		e.shape.element = e.element
		if r.bad then return e end
		if elem == 14 or elem == 16 then
			local inner = rU8(r)
			if inner > 33 or inner == 12 or inner == 33 then r.bad = true; return e end
		end
	end
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
	elseif kind == 12 or kind == 13 or kind == 14 or kind == 15 or kind == 16 or kind == 31 or kind == 33 then
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
-- (docs/SPEC-TABLES.md §3.3: "The ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY
-- TWO STRICT CHECKS"; a refusal is terminal and a second is
-- `second_announcement`).
function M.NewVocabulary(): any
	return { entries = {}, count = 0, refBits = 1, buildVersion = 0i, announced = false, refused = false, maxBytes = 65536 }
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
	local count = integer.tonumber(buffer.readinteger(data, len - 8))
	if count > math.floor((len - 9) / 8) then report.malformed = true; return false end
	local start = len - 8 - count * 8
	local ids = {}
	for i = 0, count - 1 do
		ids[i + 1] = buffer.readinteger(data, start + i * 8)
	end
	local r = { b = data, at = 1, e = start, bad = false }
	local versions = 0
	local vocabs = 0
	local words: buffer? = nil
	while true do
		local ref = rLeb(r)
		if r.bad then report.malformed = true; return false end
		if ref == 0 then break end
		if ref > #ids then report.malformed = true; return false end
		local kind = rU8(r)
		local id = ids[ref]
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
			words = buffer.create(m)
			buffer.copy(words :: buffer, 0, data, sub.at, m)
			vocabs = vocabs + 1
		else
			report.unknown = (report.unknown or 0) + 1
			if not skipStep(r, kind) then report.malformed = true; return false end
		end
	end
	if versions ~= 1 or vocabs ~= 1 then report.malformed = true; return false end
	local w = { b = words :: buffer, at = 0, e = buffer.len(words :: buffer), bad = false }
	local entries = {}
	local n = 0
	while w.at < w.e do
		local e = entryRead(w)
		if w.bad then report.malformed = true; return false end
		n = n + 1
		entries[n] = e
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

local function countOf(r: any, s: any): (number, boolean)
	local bits = bitsReq(s.min, s.max)
	local raw, ok = get(r, bits)
	if not ok then return 0, false end
	return raw + s.min, true
end

skipBody = function(r: any, st: any): boolean
	while true do
		local ref, ok = get(r, st.refBits)
			if not ok then return false end
		if ref == 0 then return true end
		local e = entryOf(st.v, ref)
		if e == nil or reserved(e.id) then return false end
		if not skipShape(r, st, e) then return false end
	end
end

skipShape = function(r: any, st: any, e: any): boolean
	local k = e.shape.kind
	if k == 0 or k == 32 then return true end
	if k == 30 then
		local ref, ok = get(r, st.refBits)
		if not ok then return false end
		if ref == 0 then return true end
		local en = entryOf(st.v, ref)
		return en ~= nil and not reserved(en.id) and en.shape.kind == 0
	elseif k == 17 then
		if st.indexBits <= 0 then return false end
		return skip(r, st.indexBits)
	elseif k == 13 then
		return skipBody(r, st)
	elseif k == 15 then
		local ref, ok = get(r, st.refBits)
		if not ok then return false end
		if ref == 0 then return true end
		local arm = entryOf(st.v, ref)
		if arm == nil or reserved(arm.id) or arm.shape.kind == 0 then return false end
		return skipShape(r, st, arm)
	elseif k == 12 or k == 33 then
		local n, ok = countOf(r, e.shape)
		if not ok then return false end
		if k == 12 and not align(r) then return false end
		return skip(r, n * (if k == 12 then 8 else 16))
	elseif k == 14 or k == 16 then
		local n, ok = countOf(r, e.shape)
		if not ok then return false end
		if k == 16 then
			for _ = 1, n do
				local key, kok = get(r, st.refBits)
				if not kok then return false end
				local ken = entryOf(st.v, key)
				if ken == nil or reserved(ken.id) or ken.shape.kind ~= 0 then return false end
				if not skipShape(r, st, { shape = e.element }) then return false end
			end
			return true
		end
		if e.shape.elem == 6 and not align(r) then return false end
		local ik = e.element.kind
		if ik ~= 12 and ik ~= 13 and ik ~= 14 and ik ~= 15 and ik ~= 16 and ik ~= 17 and ik ~= 30 and ik ~= 31 then
			return skip(r, n * e.element.bits)
		end
		for _ = 1, n do
			if not skipShape(r, st, { shape = e.element }) then return false end
		end
		return true
	elseif k == 31 then
		if not align(r) then return false end
		local n, ok = get(r, 32)
		return ok and skip(r, n * 8)
	end
	return skip(r, e.shape.bits)
end

-- skipRoot reads a body, whose first field is the NODE TABLE when a variable
-- root reaches one (docs/SPEC-TABLES.md §3.3: "THE NODE TABLE, WHEN A MESSAGE
-- HAS ONE, IS THE FIRST FIELD OF THE ROOT BODY").
local function skipRoot(r: any, st: any, desc: any): boolean
	if desc.variable then
		local at = r.off
		local ref, ok = get(r, st.refBits)
		if not ok then return false end
		if ref ~= 0 then
			local e = entryOf(st.v, ref)
			if e == nil then return false end
			if e.id == NODE_ID then
				local nodes, nok = get(r, 32)
				if not nok then return false end
				st.indexBits = bitsReq(0, nodes + 1)
				for _ = 1, nodes do
					local tref, tok = get(r, st.refBits)
					if not tok then return false end
					local te = entryOf(st.v, tref)
					if te == nil or reserved(te.id) or te.shape.kind ~= 0 then return false end
					if te.id == BYTES_ID or te.id == STRING_ID then
						local n, bok = get(r, 32)
						if not bok or not align(r) or not skip(r, n * 8) then return false end
					elseif not skipBody(r, st) then
						return false
					end
				end
				return skipBody(r, st)
			end
		end
		r.off = at
	end
	st.indexBits = 0
	return skipBody(r, st)
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

function M.LoadMessages(v: any, root: string, data: buffer, capacity: number, report: any?): (number, any)
	if report == nil then report = {} end
	reset(report)
	local len = buffer.len(data)
	if len < 1 then report.malformed = true; return -1, {} end
	if buffer.readu8(data, 0) ~= 2 then return -1, refuse(report, "newer_form") end
	if not v.announced then return -1, refuse(report, "no_vocabulary") end
	if len < 2 then report.malformed = true; return -1, {} end
	local count = buffer.readu8(data, 1) + 1
	if count > capacity then
		refuse(report, "batch_too_large")
		return count, {}
	end
	local desc = S.lookup(root)
	local st = { v = v, refBits = v.refBits, indexBits = 0 }
	local r = newReader(data, 16)
	local bodies = {}
	local at = 0
	for i = 1, count do
		local from = r.off
		if not skipRoot(r, st, desc) then
			report.malformed = true
			return i - 1, bodies
		end
		at = at + 1
		bodies[at] = { buffer = data, from = from, to = r.off }
	end
	if not align(r) or r.off ~= r.n then
		report.malformed = true
		return count, bodies
	end
	return count, bodies
end

local function bodyBits(bodies: any): number
	local bits = 0
	for _, b in bodies do
		bits = bits + (b.to - b.from)
	end
	return bits
end

function M.MeasureMessages(v: any, root: string, bodies: any, report: any?): number
	if report == nil then report = {} end
	reset(report)
	local n = #bodies
	if n < 1 or n > BATCH_MAX then
		refuse(report, "batch_too_large")
		return -1
	end
	return 2 + math.ceil(bodyBits(bodies) / 8)
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
	if report == nil then report = {} end
	reset(report)
	local n = #bodies
	if n < 1 or n > BATCH_MAX then
		refuse(report, "batch_too_large")
		return nil, -1
	end
	local bits = bodyBits(bodies)
	local size = 2 + math.ceil(bits / 8)
	local out = buffer.create(size)
	buffer.fill(out, 0, 0, size)
	buffer.writeu8(out, 0, 2)
	buffer.writeu8(out, 1, n - 1)
	local at = 16
	for _, b in bodies do
		copyBits(out, at, b.buffer, b.from, b.to - b.from)
		at = at + (b.to - b.from)
	end
	return out, size
end

return table.freeze(M)
