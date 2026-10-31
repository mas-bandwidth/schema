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
		st.nonFinite = (st.owner or "?") .. "." .. (fd.name or "?")
	end
	return true
end

-- whether a shape is a plain float the walk must test: kind 10 unpacked, or 11
local function plainFloat(shape: any, fd: any): boolean
	return fd ~= nil and fd.s == "float" and ((shape.kind == 10 and shape.packing == 0) or shape.kind == 11)
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
	if k == 12 then
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
	if kind > 32 then r.bad = true; return e end
	e.shape = shapeRead(r, kind)
	if r.bad then return e end
	if kind == 14 or kind == 16 then
		local elem = rU8(r)
		if elem > 32 or elem == 12 then r.bad = true; return e end
		e.shape.elem = elem
		e.element = shapeRead(r, elem)
		e.shape.element = e.element
		if r.bad then return e end
		if elem == 14 or elem == 16 then
			local inner = rU8(r)
			if inner > 32 or inner == 12 then r.bad = true; return e end
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
-- (docs/SPEC-TABLES.md §3.3: "The ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY
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
	local count = integer.tonumber(buffer.readinteger(data, len - 8))
	if count > math.floor((len - 9) / 8) then report.malformed = true; return false end
	-- the id table holds at most 4096 entries (docs/SPEC-TABLES.md §3,
	-- ir.TableWireIdTableBound), whatever the buffer would hold
	if count > 4096 then report.malformed = true; return false end
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
	return { s = f.s, t = f.t, vs = f.vs, def = f.def, e = f.e, name = name }
end

local function variantIndex(vs: any, id: integer): number
	for i, pair in vs do
		if pair[2] == id then return i end
	end
	return 0
end

local function refOfId(st: any, id: integer): number
	for i = 1, st.v.count do
		if st.v.entries[i].id == id then return i end
	end
	return 0
end

local function countOf(r: any, s: any): (number, boolean)
	local bits = bitsReq(s.min, s.max)
	local raw, ok = get(r, bits)
	if not ok then return 0, false end
	return raw + s.min, true
end

skipBody = function(r: any, st: any, desc: any, dep: any): boolean
	while true do
		local ref, ok = get(r, st.refBits)
			if not ok then return false end
		if ref == 0 then return true end
		local e = entryOf(st.v, ref)
		if e == nil or reserved(e.id) then return false end
		if dep ~= nil and desc ~= nil then dep.table = desc.name end
		if desc ~= nil then st.owner = desc.name end
		if not skipShape(r, st, e, fieldById(desc, e.id), dep) then return false end
	end
end

skipShape = function(r: any, st: any, e: any, fd: any, dep: any): boolean
	local k = e.shape.kind
	if k == 0 or k == 32 then return true end
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
		if ref == 0 then return true end
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
		if not ok then return false end
		if k == 12 and not align(r) then return false end
		return skip(r, n * (if k == 12 then 8 else 16))
	elseif k == 14 or k == 16 then
		local n, ok = countOf(r, e.shape)
		if not ok then return false end
		local ef = nil
		if fd ~= nil then
			if fd.e ~= nil then ef = named(fd.e, fd.name)
			elseif fd.s == "map" then ef = { s = "tab", t = fd.t, name = fd.name } end
		end
		if k == 16 then
			for _ = 1, n do
				local key, kok = get(r, st.refBits)
				if not kok then return false end
				local ken = entryOf(st.v, key)
				if ken == nil or reserved(ken.id) or ken.shape.kind ~= 0 then return false end
				-- A KEYED ARRAY's key is a slot, not a caller value: the key
				-- may name a retired variant and the element is what is read
				-- and written (Go's variantWireId key=true, §2.15).
				if not skipShape(r, st, { shape = e.element }, ef, dep) then return false end
			end
			return true
		end
		if e.shape.elem == 6 and not align(r) then return false end
		local ik = e.element.kind
		if plainFloat(e.element, ef) then
			for _ = 1, n do
				if not floatStep(r, st, ik, ef) then return false end
			end
			return true
		end
		if ik ~= 12 and ik ~= 13 and ik ~= 14 and ik ~= 15 and ik ~= 16 and ik ~= 17 and ik ~= 30 and ik ~= 31 then
			return skip(r, n * e.element.bits)
		end
		for _ = 1, n do
			if not skipShape(r, st, { shape = e.element }, ef, dep) then return false end
		end
		return true
	elseif k == 31 then
		if not align(r) then return false end
		local n, ok = get(r, 32)
		return ok and skip(r, n * 8)
	end
	if plainFloat(e.shape, fd) then return floatStep(r, st, k, fd) end
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
	local st: any = { v = v, refBits = v.refBits, owner = nil, nonFinite = nil }
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
-- every read, tolerant and strict alike, and every writer, the field named.
local function refuseNonFinite(report: any, field: string, read: boolean)
	report.verdict = 1
	report.reason = if read then "non_finite_float: " .. field else "non_finite_float"
	report.nonFiniteFloat = field
	report.field = field
end

-- LoadMessagesTolerant is the TOLERANT batch read (docs/SPEC-TABLES.md §6.5,
-- §3.3): evolution may recover a value, but damage discards the whole batch.
function M.LoadMessagesTolerant(v: any, root: string, data: buffer, capacity: number, report: any?): (number, any)
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
-- §6.5, Go's <T>LoadMessages over <T>LoadMessagesTolerant): a batch the
-- tolerant read found damaged, or whose report counts a clamp or a kind that
-- moved, answers -1 with no body handed back and report.reason the one name
-- (wire_damaged, clamped, kind_mismatch), and a refusal keeps its own name.
-- unknown and widened alone never refuse.
function M.LoadMessages(v: any, root: string, data: buffer, capacity: number, report: any?): (number, any)
	local rep: any = report or {}
	local count, bodies = M.LoadMessagesTolerant(v, root, data, capacity, rep)
	if count < 0 then return count, bodies end
	-- a refusal the tolerant read already named answers as it did (batch_too_large
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

function M.MeasureMessages(v: any, root: string, bodies: any, report: any?): number
	local desc = messageRoot(root)
	if report == nil then report = {} end
	reset(report)
	local n = #bodies
	if n < 1 or n > BATCH_MAX then
		refuse(report, "batch_too_large")
		return -1
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
	reset(report)
	local n = #bodies
	if n < 1 or n > BATCH_MAX then
		refuse(report, "batch_too_large")
		return nil, -1
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
