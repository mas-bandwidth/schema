--!strict
-- table_retain.moo — RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6) in moo: the
-- opt-in that keeps what this build cannot name through a region round trip.
-- LoadRetain is the tolerant load into a REGION with the caller's store on,
-- MeasureRetain and SaveRetain write the retained fields back at the END OF
-- THEIR OWN BODY, and the report accumulates across the pair. The walks are
-- the Go reference engine's (internal/tablewire retain.go, retainmessage.go);
-- the hooks they run through live in table_load.moo, table_save.moo and
-- table_message.moo, nil on every plain read and save.
--
-- THE STORE is the caller's two stores, by capacity: a byte capacity for the
-- retained records and an entry capacity for the retained ids' own list. It
-- never grows, and it is the whole of the memory this feature can command.
--
-- A RETAINED RECORD is READER-PRIVATE (§6.6): the field's id and kind, and its
-- payload with every reference replaced by the sixty-four-bit id it names and
-- every length written after the content it frames, at eight bytes. Nothing
-- outside this module reads one. It belongs to the BODY INSTANCE that carried
-- it (the table the load filled), and dies with it: a record whose instance the
-- save never reaches is a path that names no body, and counts one retain_lost.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local V = require("./table_values")
local L = require("./table_load")
local W = require("./table_save")
local MS = require("./table_message")
local Fl = require("./serialize_float")

local R = {}

local ZERO = "\0\0\0\0\0\0\0\0"
local NodeTableId = V.NodeTableId
local BuildVersionId = V.BuildVersionId
local MessageVocabularyId = V.MessageVocabularyId

-- THE WALK'S NESTING CAP (§6.6): a small stated constant; a record past it is
-- dropped on the same rule as any other shape the walk cannot take
local DEPTH_CAP = 64

local TWO32 = 4294967296

-- ---- the output builder: a growable buffer, little-endian ----------------

local function new_out(): any
	return { b = buffer.create(64), n = 0 }
end

local function reserve(o: any, k: number)
	local need = o.n + k
	local cap = buffer.len(o.b)
	if need <= cap then
		return
	end
	while cap < need do
		cap *= 2
	end
	local nb = buffer.create(cap)
	buffer.copy(nb, 0, o.b, 0, o.n)
	o.b = nb
end

local function o_u8(o: any, v: number)
	reserve(o, 1)
	buffer.writeu8(o.b, o.n, v)
	o.n += 1
end

local function o_u64(o: any, v: number)
	reserve(o, 8)
	buffer.writeu32(o.b, o.n, v % TWO32)
	buffer.writeu32(o.b, o.n + 4, v // TWO32)
	o.n += 8
end

local function o_id(o: any, id: string)
	reserve(o, 8)
	buffer.writestring(o.b, o.n, id)
	o.n += 8
end

local function o_copy(o: any, src: buffer, at: number, n: number)
	reserve(o, n)
	buffer.copy(o.b, o.n, src, at, n)
	o.n += n
end

-- one canonical LEB128 length, count, index or reference (§3)
local function o_leb(o: any, v: number)
	while v >= 128 do
		o_u8(o, (v % 128) + 128)
		v = v // 128
	end
	o_u8(o, v)
end

-- a fixed-width length written AFTER the content it frames: reserve it, then
-- patch it, so a record costs ONE pass to capture (§6.6)
local function o_mark(o: any): number
	local at = o.n
	o_u64(o, 0)
	return at
end

local function o_patch(o: any, at: number)
	local n = o.n - at - 8
	buffer.writeu32(o.b, at, n % TWO32)
	buffer.writeu32(o.b, at + 4, n // TWO32)
end

-- ---- a cursor over a payload's bytes --------------------------------------

local function new_cursor(b: buffer, at: number, e: number): any
	return { b = b, at = at, e = e }
end

local function c_leb(c: any): number?
	local v, at = V.leb(c.b, c.at, c.e)
	if v == nil then
		return nil
	end
	c.at = at
	return v
end

-- skipPayload steps one payload of `kind` by §3's framing rules alone: the
-- same rules the plain read's skip takes, over a cursor
local function skip_payload(c: any, kind: number): boolean
	local w = V.width(kind)
	if w > 0 then
		if c.at + w > c.e then
			return false
		end
		c.at += w
		return true
	end
	if kind == 12 or kind == 31 or kind == 32 or kind == 33 then
		local n = c_leb(c)
		if n == nil or c.at + n > c.e then
			return false
		end
		c.at += n
		return true
	end
	-- kinds 13, 14, 15, 16, 17 and 30 are never copied, so they never come
	-- back this way, and a kind outside the closed set is framing damage
	return false
end

-- ---- THE RESOLVING WALK, capture side -------------------------------------

local function reserved(id: string): boolean
	return id == ZERO or id == NodeTableId or id == BuildVersionId or id == MessageVocabularyId
end

-- id resolves one reference against the FILE's id table: a reference above
-- the entry count, at an entry of zero, or at one of the three reserved ids
-- DROPS THE RECORD
local function rs_id(rs: any, ref: number): string
	if ref == 0 or ref > #rs.ids then
		rs.bad = true
		return ZERO
	end
	local id = rs.ids[ref]
	if reserved(id) then
		rs.bad = true
		return ZERO
	end
	return id
end

local walk_payload: any
local walk_content: any

local function walk_framed(rs: any, c: any, kind: number, depth: number)
	local n = c_leb(c)
	if n == nil or c.at + n > c.e then
		rs.bad = true
		return
	end
	local sub = new_cursor(c.b, c.at, c.at + n)
	c.at += n
	local at = o_mark(rs.out)
	walk_content(rs, sub, kind, depth + 1)
	o_patch(rs.out, at)
end

local function walk_keyed_slot(rs: any, c: any, ek: number, depth: number)
	local ref = c_leb(c)
	if ref == nil then
		rs.bad = true
		return
	end
	o_id(rs.out, rs_id(rs, ref))
	walk_framed(rs, c, ek, depth)
end

local function walk_body(rs: any, c: any, depth: number)
	while true do
		if rs.bad then
			return
		end
		local ref = c_leb(c)
		if ref == nil then
			rs.bad = true
			return
		end
		if ref == 0 then
			-- THE TERMINATOR IS A REFERENCE, and in the resolved form a
			-- reference is a fixed eight-byte id
			o_u64(rs.out, 0)
			return
		end
		if c.at >= c.e then
			rs.bad = true
			return
		end
		local kind = buffer.readu8(c.b, c.at)
		c.at += 1
		o_id(rs.out, rs_id(rs, ref))
		o_u8(rs.out, kind)
		walk_payload(rs, c, kind, depth)
	end
end

-- the resolving walk over one SELF-FRAMED payload: a field's, an array
-- element's, a union arm's. KIND 17 IS WHAT THE WALK IS LOOKING FOR AS MUCH AS
-- A REFERENCE IS: meeting one anywhere drops the whole record (§6.6)
walk_payload = function(rs: any, c: any, kind: number, depth: number)
	if rs.bad then
		return
	end
	if kind == 17 then
		rs.bad = true
	elseif kind == 30 then
		local ref = c_leb(c)
		if ref == nil then
			rs.bad = true
			return
		end
		if ref == 0 then
			o_u64(rs.out, 0) -- the enum's None: a value, not a reference
			return
		end
		o_id(rs.out, rs_id(rs, ref))
	elseif kind == 15 then
		local ref = c_leb(c)
		if ref == nil then
			rs.bad = true
			return
		end
		if ref == 0 then
			o_u64(rs.out, 0) -- the union holding nothing
			return
		end
		o_id(rs.out, rs_id(rs, ref))
		if c.at >= c.e then
			rs.bad = true
			return
		end
		local armKind = buffer.readu8(c.b, c.at)
		c.at += 1
		o_u8(rs.out, armKind)
		walk_framed(rs, c, armKind, depth)
	elseif kind == 13 or kind == 14 or kind == 16 then
		walk_framed(rs, c, kind, depth)
	else
		-- every other payload is copied verbatim, its own length included
		local from = c.at
		if not skip_payload(c, kind) then
			rs.bad = true
			return
		end
		o_copy(rs.out, c.b, from, c.at - from)
	end
end

-- a payload ALREADY FRAMED by a length. THE CONTENT MUST BE CONSUMED EXACTLY:
-- the resolving walk is stricter than the plain read
walk_content = function(rs: any, c: any, kind: number, depth: number)
	if rs.bad then
		return
	end
	if depth > DEPTH_CAP then
		rs.bad = true
		return
	end
	if kind == 17 then
		rs.bad = true
		return
	elseif kind == 13 then
		walk_body(rs, c, depth)
	elseif kind == 14 or kind == 16 then
		if c.at >= c.e then
			rs.bad = true
			return
		end
		local ek = buffer.readu8(c.b, c.at)
		c.at += 1
		if ek == 17 then
			rs.bad = true -- an array whose element kind is 17 (§6.6)
			return
		end
		o_u8(rs.out, ek)
		local count = c_leb(c)
		if count == nil then
			rs.bad = true
			return
		end
		o_u64(rs.out, count)
		for _ = 1, count do
			if rs.bad then
				return
			end
			if kind == 16 then
				walk_keyed_slot(rs, c, ek, depth)
			else
				walk_payload(rs, c, ek, depth)
			end
		end
	elseif kind == 15 or kind == 30 then
		walk_payload(rs, c, kind, depth)
	else
		o_copy(rs.out, c.b, c.at, c.e - c.at)
		c.at = c.e
	end
	if not rs.bad and c.at ~= c.e then
		rs.bad = true
	end
end

-- ---- THE STORE ------------------------------------------------------------

-- newStore is the caller's two stores: capacity bytes for the retained
-- records, and idCapacity entries for the retained ids' own list
-- (`capacity / 8` is the count bound no file can beat, and the default)
function R.newStore(capacity: number, idCapacity: number?): any
	return {
		capacity = capacity,
		idCapacity = if idCapacity ~= nil then idCapacity else capacity // 8,
		used = 0,
		ids = {},
		idset = {},
		records = {},
		maps = {},
		arms = {},
		saveLost = 0,
	}
end

-- what the retention buffer holds, in bytes
function R.used(store: any): number
	return store.used
end

-- the retained-id list as the last save walk interned it, first use first
function R.ids(store: any): { string }
	return store.ids
end

local function reset_store(st: any)
	st.used = 0
	st.ids = {}
	st.idset = {}
	st.records = {}
	st.maps = {}
	st.arms = {}
	st.saveLost = 0
end

-- ---- THE HOOK: what a retaining read and save run through ------------------

local forget_body: any

-- forgetValue discards the records under one field's value, descending the
-- tree the load built (a body, an arm, an element, an entry); a pointer is
-- never followed, because no occurrence of a body ever supersedes a node
local function forget_value(st: any, fd: any, val: any)
	if type(val) ~= "table" then
		return
	end
	local s = fd.s
	if s == "tab" then
		forget_body(st, S.lookup(fd.t), val)
	elseif s == "union" then
		local un = S.lookup(fd.t)
		for _, a in un.arms do
			if a.slot ~= nil and val[a.slot] ~= nil then
				forget_value(st, a, val[a.slot])
			end
		end
	elseif s == "arr" then
		local e = fd.e
		if e ~= nil and (e.s == "tab" or e.s == "union") then
			for _, x in val do
				forget_value(st, e, x)
			end
		end
	elseif s == "map" then
		local d = S.lookup(fd.t)
		for _, x in val do
			forget_body(st, d, x)
		end
	end
end

-- forgetBody discards every record held under a body the wire is about to
-- write AGAIN, and under everything that body holds. THE DISCARD MOVES
-- NEITHER COUNTER: the writer superseded the data, so nothing was lost that the
-- load could have kept (§6.6)
forget_body = function(st: any, d: any, inst: any)
	if type(inst) ~= "table" then
		return
	end
	st.records[inst] = nil
	local held = st.maps[inst]
	if held ~= nil then
		st.maps[inst] = nil
		for _, list in held do
			for _, p in list do
				forget_body(st, p[1], p[2])
			end
		end
	end
	for _, fd in d.fields do
		if fd.slot ~= nil then
			local val = inst[fd.slot]
			if val ~= nil then
				forget_value(st, fd, val)
			end
		end
	end
end

local function forget_arms(st: any, holder: any, key: any)
	local byKey = st.arms[holder]
	if byKey == nil then
		return
	end
	if key == nil then
		st.arms[holder] = nil
		for _, list in byKey do
			for _, p in list do
				forget_body(st, p[1], p[2])
			end
		end
		return
	end
	local list = byKey[key]
	byKey[key] = nil
	if list ~= nil then
		for _, p in list do
			forget_body(st, p[1], p[2])
		end
	end
end

local function forget_map(st: any, holder: any, slot: any)
	local byKey = st.maps[holder]
	if byKey == nil then
		return
	end
	local list = byKey[slot]
	byKey[slot] = nil
	if list ~= nil then
		for _, p in list do
			forget_body(st, p[1], p[2])
		end
	end
end

-- the record the load keeps: the capacity and the counter, which every
-- capture answers the same way whatever form it read from
local function keep(st: any, inst: any, id: string, kind: number, p: buffer, n: number, report: any)
	local cost = 9 + n
	if st.used + cost > st.capacity then
		-- REFUSAL IS PER RECORD AND NEVER PARTIAL: the record is not written at
		-- all and the read continues
		report.retainLost += 1
		return
	end
	st.used += cost
	local recs = st.records[inst]
	if recs == nil then
		recs = {}
		st.records[inst] = recs
	end
	recs[#recs + 1] = { id = id, kind = kind, p = p, n = n, placed = false }
	report.retained += 1
end

local function hex_name(desc: any, id: string): any
	return { owner = desc.name, name = "0x" .. string.sub(V.idHex(id), 2) }
end

-- ---- THE EMIT SIDE ----------------------------------------------------------

local emit_payload: any
local emit_content: any

local function e_ref(em: any, o: any, id: string)
	if not S.isKnown(id) then
		-- a retained id takes its entry from the caller's list, and one used
		-- by two records takes ONE entry
		local st = em.st
		if not st.idset[id] then
			if #st.ids >= st.idCapacity then
				em.bad = true
				return
			end
			st.ids[#st.ids + 1] = id
			st.idset[id] = true
		end
	end
	o_leb(o, em.api.ref(em.c.ids, id))
end

local function e_id(em: any, c: any): string
	if c.at + 8 > c.e then
		em.bad = true
		return ZERO
	end
	local id = buffer.readstring(c.b, c.at, 8)
	c.at += 8
	return id
end

local function e_num(em: any, c: any): number
	if c.at + 8 > c.e then
		em.bad = true
		return 0
	end
	local v = buffer.readu32(c.b, c.at) + buffer.readu32(c.b, c.at + 4) * TWO32
	c.at += 8
	return v
end

local function e_framed_in(em: any, c: any): any
	local n = e_num(em, c)
	if em.bad or c.at + n > c.e then
		em.bad = true
		return new_cursor(c.b, 0, 0)
	end
	local sub = new_cursor(c.b, c.at, c.at + n)
	c.at += n
	return sub
end

-- a content and the CANONICAL LEB128 LENGTH that frames it: ONE POST-ORDER
-- PASS, the content built once and the length written from its size
local function e_framed(em: any, o: any, c: any, kind: number)
	local content = e_framed_in(em, c)
	if em.bad then
		return
	end
	local inner = new_out()
	emit_content(em, inner, content, kind)
	if em.bad then
		return
	end
	o_leb(o, inner.n)
	o_copy(o, inner.b, 0, inner.n)
end

local function e_body(em: any, o: any, c: any)
	while true do
		if em.bad then
			return
		end
		local id = e_id(em, c)
		if em.bad then
			return
		end
		if id == ZERO then
			o_u8(o, 0) -- the body ENDS AT ITS OWN ZERO REFERENCE
			return
		end
		if c.at >= c.e then
			em.bad = true
			return
		end
		local kind = buffer.readu8(c.b, c.at)
		c.at += 1
		e_ref(em, o, id)
		if em.bad then
			return
		end
		o_u8(o, kind)
		emit_payload(em, o, c, kind)
	end
end

emit_payload = function(em: any, o: any, c: any, kind: number)
	if em.bad then
		return
	end
	if kind == 30 then
		local id = e_id(em, c)
		if em.bad then
			return
		end
		if id == ZERO then
			o_leb(o, 0)
			return
		end
		e_ref(em, o, id)
	elseif kind == 15 then
		local arm = e_id(em, c)
		if em.bad then
			return
		end
		if arm == ZERO then
			o_leb(o, 0)
			return
		end
		e_ref(em, o, arm)
		if em.bad or c.at >= c.e then
			em.bad = true
			return
		end
		local armKind = buffer.readu8(c.b, c.at)
		c.at += 1
		o_u8(o, armKind)
		e_framed(em, o, c, armKind)
	elseif kind == 13 or kind == 14 or kind == 16 then
		e_framed(em, o, c, kind)
	else
		local from = c.at
		if not skip_payload(c, kind) then
			em.bad = true
			return
		end
		o_copy(o, c.b, from, c.at - from)
	end
end

emit_content = function(em: any, o: any, c: any, kind: number)
	if em.bad then
		return
	end
	if kind == 13 then
		e_body(em, o, c)
	elseif kind == 14 or kind == 16 then
		if c.at >= c.e then
			em.bad = true
			return
		end
		local ek = buffer.readu8(c.b, c.at)
		c.at += 1
		o_u8(o, ek)
		local count = e_num(em, c)
		o_leb(o, count)
		for _ = 1, count do
			if em.bad then
				return
			end
			if kind == 16 then
				local key = e_id(em, c)
				if em.bad then
					return
				end
				e_ref(em, o, key)
				e_framed(em, o, c, ek)
			else
				emit_payload(em, o, c, ek)
			end
		end
	elseif kind == 15 or kind == 30 then
		emit_payload(em, o, c, kind)
	else
		o_copy(o, c.b, c.at, c.e - c.at)
		c.at = c.e
	end
end


-- ---- THE MESSAGE FORM'S TRANSCODING WALK (retainmessage.go) -----------------
--
-- A retained record must carry the field's identity and bytes WITH EVERY
-- REFERENCE RESOLVED, and the table it is re-emitted into is a FILE's. So the
-- capture of a form-2 entry is a TRANSCODE as well as a resolve: a bitpacked
-- payload is read at the width its announced shape states and written at the
-- width the variable form spells, and from there it is the same record the
-- file's own capture makes and the same emitter writes back. The skip runs
-- first and the capture second, over the same bits.

local B = MS.bitio

-- one reference against the CONNECTION'S VOCABULARY: a reference of zero, one
-- above the entry count, and one naming a RESERVED id all drop the record
local function m_id(rs: any, t: any, ref: number): string
	local e = B.entryAt(t, ref)
	if e == nil or reserved(e.id) then
		rs.bad = true
		return ZERO
	end
	return e.id
end

local function m_ref(rs: any, t: any): string
	local ref, ok = B.reference(t)
	if not ok then
		rs.bad = true
		return ZERO
	end
	return m_id(rs, t, ref)
end

-- one text or blob payload and its FILE bytes: a string(N) or bytes(N) is a
-- length at its own width, the ALIGN, then the bytes; a wide string is a length
-- and then sixteen bits a code unit; the escape aligns, reads a thirty-two bit
-- L and takes L bytes opaque (§3.3). Answers the bytes as a buffer
local function m_opaque(rs: any, t: any, entry: any): buffer?
	local r = t.r
	local n = 0
	local kind = entry.kind
	if kind == 12 or kind == 33 then
		local raw, ok = B.get(r, B.bitsReq(0, entry.shape.max))
		if not ok then
			rs.bad = true
			return nil
		end
		n = raw
		if kind == 33 then
			if not B.has(r, n * 16) then
				rs.bad = true
				return nil
			end
			local units = buffer.create(n * 2)
			for i = 0, n - 1 do
				local u = B.get(r, 16)
				buffer.writeu16(units, i * 2, u)
			end
			return units
		end
		if not B.ralign(r) then
			rs.bad = true
			return nil
		end
	else
		if not B.ralign(r) then
			rs.bad = true
			return nil
		end
		local raw, ok = B.get(r, 32)
		if not ok then
			rs.bad = true
			return nil
		end
		n = raw
	end
	if not B.has(r, n * 8) then
		rs.bad = true
		return nil
	end
	local b = buffer.create(n)
	buffer.copy(b, 0, r.b, r.base + r.off // 8, n)
	r.off += n * 8
	return b
end

-- one integer at the FILE's own width for its kind, little endian, which is
-- two's complement where the kind is signed
local function m_word(rs: any, x: { number }, kind: number)
	local w = V.width(kind)
	local o = rs.out
	if w == 1 then
		o_u8(o, x[1] % 256)
	elseif w == 2 then
		o_u8(o, x[1] % 256)
		o_u8(o, (x[1] // 256) % 256)
	elseif w == 4 then
		reserve(o, 4)
		buffer.writeu32(o.b, o.n, x[1])
		o.n += 4
	elseif w == 8 then
		reserve(o, 8)
		buffer.writeu32(o.b, o.n, x[1])
		buffer.writeu32(o.b, o.n + 4, x[2])
		o.n += 8
	else
		rs.bad = true
	end
end

-- an f32 at the sender's packing: the IEEE bit pattern where it rides raw, the
-- float its INDEX names where the sender quantized it. An index above `count`
-- is rejected and drops the record
local function m_float32(rs: any, t: any, shape: any)
	local o = rs.out
	local bits: number
	if shape.packing == 2 then
		local index, ok = B.get(t.r, shape.bits)
		if not ok or shape.count == nil or index > shape.count then
			rs.bad = true
			return
		end
		local normalized = Fl.fdiv(Fl.fromUint(index), Fl.fromUint(shape.count))
		bits = Fl.fadd(Fl.fmul(normalized, shape.delta), shape.qmin)
	else
		local raw, ok = B.get(t.r, 32)
		if not ok then
			rs.bad = true
			return
		end
		bits = raw
	end
	reserve(o, 4)
	buffer.writeu32(o.b, o.n, bits)
	o.n += 4
end

-- ONE announced value: read at the width the shape states, written at the
-- width the variable form spells. NO CLAMP FIRES AND NO RANGE IS APPLIED: the
-- value is the SENDER's, under an id this reader cannot name, so there is no
-- declaration to bound it by
local function m_scalar(rs: any, t: any, entry: any)
	local kind = entry.kind
	local shape = entry.shape
	local o = rs.out
	if kind == 1 then
		local v, ok = B.get(t.r, 1)
		if not ok then
			rs.bad = true
			return
		end
		o_u8(o, v)
		return
	elseif kind == 11 then
		local lo, ok1 = B.get(t.r, 32)
		local hi, ok2 = B.get(t.r, 32)
		if not ok1 or not ok2 then
			rs.bad = true
			return
		end
		reserve(o, 8)
		buffer.writeu32(o.b, o.n, lo)
		buffer.writeu32(o.b, o.n + 4, hi)
		o.n += 8
		return
	elseif kind == 10 then
		m_float32(rs, t, shape)
		return
	end
	local width = B.valueBits(kind, shape)
	if width < 0 then
		rs.bad = true
		return
	end
	local x, ok = B.getLimbs(t.r, width)
	if not ok then
		rs.bad = true
		return
	end
	local ranged = shape.packing == 1
	local base = shape.base or { 0, 0, 0, 0 }
	if V.width(kind) == 16 then
		-- the 128-bit family: a ranged value is offset + base, mod 2^128
		if ranged then
			x = B.add4(x, base)
		end
		reserve(o, 16)
		for i = 1, 4 do
			buffer.writeu32(o.b, o.n + (i - 1) * 4, x[i])
		end
		o.n += 16
		return
	end
	if ranged then
		x = B.lowBits(B.add4(x, base), 64)
	elseif V.signedKind(kind) and width < 64 and width > 0 then
		x = B.lowBits(B.signExtend(x, width), 64)
	end
	m_word(rs, x, kind)
end

local m_payload: any
local m_content: any

local function m_framed(rs: any, t: any, entry: any, depth: number)
	local at = o_mark(rs.out)
	m_content(rs, t, entry, depth + 1)
	o_patch(rs.out, at)
end

local function m_body(rs: any, t: any, depth: number)
	while true do
		if rs.bad then
			return
		end
		local ref, ok = B.reference(t)
		if not ok then
			rs.bad = true
			return
		end
		if ref == 0 then
			o_u64(rs.out, 0) -- the terminator rides as an eight-byte id
			return
		end
		local entry = B.entryAt(t, ref)
		if entry == nil or reserved(entry.id) then
			rs.bad = true
			return
		end
		o_id(rs.out, entry.id)
		o_u8(rs.out, entry.kind)
		m_payload(rs, t, entry, depth)
	end
end

-- one array's or one keyed body's content: the element kind, the count and the
-- elements. A KEYED SLOT IS A TRIPLE, the key reference, the element's own
-- length and the element, and its keys resolve at every element kind
local function m_elements(rs: any, t: any, entry: any, depth: number)
	local shape = entry.shape
	if shape.elem == 17 then
		rs.bad = true
		return
	end
	local n = shape.min
	local width = B.countBits(shape)
	if entry.kind == 16 then
		width, n = B.bitsReq(0, shape.max), 0
	end
	if width > 0 then
		local raw, ok = B.get(t.r, width)
		if not ok then
			rs.bad = true
			return
		end
		n = raw
		if entry.kind == 14 then
			n += shape.min
		end
	end
	if B.aligns(entry.kind, shape) and not B.ralign(t.r) then
		rs.bad = true
		return
	end
	local element = { kind = shape.elem, shape = shape.inner or {} }
	local o = rs.out
	o_u8(o, shape.elem)
	o_u64(o, n)
	-- EVERY ELEMENT COSTS AT LEAST ONE OUTPUT BYTE, so a count above the budget
	-- is dropped before the loop spends a single step on it: nothing in this
	-- form is superlinear in a batch's length, including a count
	if rs.cap >= 0 and o.n + n > rs.cap then
		rs.bad = true
		return
	end
	for _ = 1, n do
		if rs.bad then
			return
		end
		if entry.kind == 16 then
			o_id(o, m_ref(rs, t))
			m_framed(rs, t, element, depth)
		else
			m_payload(rs, t, element, depth)
		end
		if rs.cap >= 0 and o.n > rs.cap then
			rs.bad = true
			return
		end
	end
end

m_content = function(rs: any, t: any, entry: any, depth: number)
	if rs.bad then
		return
	end
	if depth > DEPTH_CAP then
		rs.bad = true
		return
	end
	local kind = entry.kind
	if kind == 17 then
		rs.bad = true
	elseif kind == 13 then
		m_body(rs, t, depth)
	elseif kind == 14 or kind == 16 then
		m_elements(rs, t, entry, depth)
	elseif kind == 15 or kind == 30 then
		m_payload(rs, t, entry, depth)
	elseif kind == 32 then
		-- nothing rides, and the outer length is what says so
	elseif kind == 12 or kind == 31 or kind == 33 then
		-- a content copied whole carries no inner length: the outer one frames it
		local b = m_opaque(rs, t, entry)
		if b ~= nil then
			o_copy(rs.out, b, 0, buffer.len(b))
		end
	else
		m_scalar(rs, t, entry)
	end
end

m_payload = function(rs: any, t: any, entry: any, depth: number)
	if rs.bad then
		return
	end
	if rs.cap >= 0 and rs.out.n > rs.cap then
		-- A SINGLE APPEND (a blob, a nested content) can carry the record past
		-- the budget between two elements: dropped on the same rule
		rs.bad = true
		return
	end
	local kind = entry.kind
	if kind == 17 or kind == 0 then
		-- the node-index class; and a kind-0 entry is a NAME, which names no
		-- payload the variable form has a kind for
		rs.bad = true
	elseif kind == 32 then
		o_leb(rs.out, 0)
	elseif kind == 30 then
		local ref, ok = B.reference(t)
		if not ok then
			rs.bad = true
			return
		end
		if ref == 0 then
			o_u64(rs.out, 0) -- the enum's None: a value, not a reference
			return
		end
		o_id(rs.out, m_id(rs, t, ref))
	elseif kind == 15 then
		local ref, ok = B.reference(t)
		if not ok then
			rs.bad = true
			return
		end
		if ref == 0 then
			o_u64(rs.out, 0)
			return
		end
		local arm = B.entryAt(t, ref)
		if arm == nil or reserved(arm.id) or arm.kind == 0 then
			rs.bad = true
			return
		end
		o_id(rs.out, arm.id)
		o_u8(rs.out, arm.kind)
		m_framed(rs, t, arm, depth)
	elseif kind == 13 or kind == 14 or kind == 16 then
		m_framed(rs, t, entry, depth)
	elseif kind == 12 or kind == 31 or kind == 33 then
		-- an opaque payload rides behind a CANONICAL LEB128 LENGTH on the file
		local b = m_opaque(rs, t, entry)
		if b == nil then
			return
		end
		o_leb(rs.out, buffer.len(b))
		o_copy(rs.out, b, 0, buffer.len(b))
	else
		m_scalar(rs, t, entry)
	end
end

-- a retained entry of a float kind on its raw bits, at bit offset `at`,
-- without moving the cursor: true for every other kind, for a quantized float
-- (whose index names a finite value) and for bits the body does not hold
local function m_finite(t: any, entry: any, at: number): boolean
	local r = t.r
	local was = r.off
	r.off = at
	local ok = true
	if entry.kind == 11 then
		local lo, ok1 = B.get(r, 32)
		local hi, ok2 = B.get(r, 32)
		ok = not (ok1 and ok2) or V.finite64(lo, hi)
	elseif entry.kind == 10 and entry.shape.packing ~= 2 then
		local raw, ok1 = B.get(r, 32)
		ok = not ok1 or V.finite32(raw)
	end
	r.off = was
	return ok
end

-- ---- the hook itself --------------------------------------------------------

-- resetInPlace puts a body back on its declared defaults and keeps its identity,
-- and the identity of every table a nested field or a fixed or counted array of
-- tables holds (internal/tabletext Refill): the records keyed by those tables
-- stay where they were
local reset_in_place: any
reset_in_place = function(d: any, inst: any)
	local fresh = d.new()
	for _, fd in d.fields do
		local slot = fd.slot
		if slot ~= nil then
			local held = inst[slot]
			if fd.s == "tab" and type(held) == "table" then
				reset_in_place(S.lookup(fd.t), held)
				fresh[slot] = held
			elseif fd.s == "arr" and (fd.array == "fixed" or fd.array == "counted") and fd.e ~= nil and fd.e.s == "tab" and type(held) == "table" and type(fresh[slot]) == "table" then
				local ed = S.lookup(fd.e.t)
				for j, _ in fresh[slot] do
					local o = held[j]
					if type(o) == "table" then
						reset_in_place(ed, o)
						fresh[slot][j] = o
					end
				end
			end
		end
	end
	for k, _ in inst do
		inst[k] = nil
	end
	for k, v in fresh do
		inst[k] = v
	end
end

local function make_hook(st: any): any
	local h: any = {}

	-- one unknown this load or save could not keep: every exclusion counts it,
	-- so a caller reads ONE NUMBER (§6.6)
	function h.lost(report: any)
		report.retainLost += 1
	end

	-- capture is the whole of the load side for one unknown FILE field: the
	-- outer-kind exclusion, the resolving walk, the capacity, and the counter
	-- each answers. The bytes it walks are the ones the plain read's own skip
	-- already delimited, so the ADDED cost is one pass over the record
	function h.capture(inst: any, desc: any, id: string, kind: number, b: buffer, from: number, to: number, ids: { string }, report: any)
		-- A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (§3.3, §6.6): a NaN
		-- or an infinity refuses the read, uniformity winning over carrying
		-- bytes this reader cannot name
		if kind == 10 and to - from == 4 then
			if not V.finite32(buffer.readu32(b, from)) then
				L.nonFinite(report, hex_name(desc, id))
				return
			end
		elseif kind == 11 and to - from == 8 then
			if not V.finite64(buffer.readu32(b, from), buffer.readu32(b, from + 4)) then
				L.nonFinite(report, hex_name(desc, id))
				return
			end
		end
		if kind == 17 then
			-- A FIELD WHOSE PAYLOAD CARRIES A NODE INDEX (§6.6)
			report.retainLost += 1
			return
		end
		local rs = { ids = ids, out = new_out(), bad = false }
		local c = new_cursor(b, from, to)
		-- THE CAP COUNTS NESTED BODIES AND THE FIELD'S OWN PAYLOAD IS NOT ONE: the
		-- walk starts at zero and `framed` is the one step that takes a level
		walk_payload(rs, c, kind, 0)
		if rs.bad or c.at ~= to then
			-- the walk can meet damage the read never looked at: the record is
			-- dropped, one retain_lost counts, and `malformed` DOES NOT MOVE
			report.retainLost += 1
			return
		end
		keep(st, inst, id, kind, rs.out.b, rs.out.n, report)
	end

	-- captureMessage is the whole of the load side for one unknown ENTRY of a
	-- form-2 body: the outer-kind exclusion, the transcoding walk, the capacity,
	-- and the counter each answers. `at` is the bit where the payload began and
	-- `stop` where the skip left the reader, and the walk must land on `stop`
	-- exactly
	function h.captureMessage(t: any, inst: any, desc: any, entry: any, at: number, stop: number)
		local report = t.report
		if entry.kind == 17 then
			report.retainLost += 1
			return
		end
		if not m_finite(t, entry, at) then
			-- A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (§3.3, §6.6)
			report.nonFinite += 1
			if report.nonFiniteField == nil then
				local named = hex_name(desc, entry.id)
				report.nonFiniteField = named.owner .. "." .. named.name
			end
			return
		end
		-- THE TRANSCODING WALK IS BOUNDED BY WHAT THE STORE COULD STILL KEEP, NOT
		-- BY THE ANNOUNCED COUNT: a record that cannot fit is dropped, one
		-- retain_lost counts, and `malformed` does not move
		local rs = { out = new_out(), bad = false, cap = math.max(st.capacity - st.used - 9, 0) }
		local r = t.r
		local was = r.off
		r.off = at
		m_payload(rs, t, entry, 0)
		local landed = r.off
		r.off = was
		if rs.bad or landed ~= stop then
			report.retainLost += 1
			return
		end
		keep(st, inst, entry.id, entry.kind, rs.out.b, rs.out.n, report)
	end

	-- a body the wire is about to write AGAIN (nested table, array element,
	-- keyed slot, a map's duplicate key)
	function h.forgetBody(d: any, inst: any)
		forget_body(st, d, inst)
	end

	-- the union, array or map field at holder[slot] is read AGAIN, at the point
	-- the read COMMITS to replace what it held (§6.6)
	function h.supersede(holder: any, slot: any, fd: any)
		local s = fd.s
		local old = holder[slot]
		if s == "map" then
			forget_map(st, holder, slot)
			forget_value(st, fd, old)
		elseif s == "union" then
			forget_value(st, fd, old)
			forget_arms(st, holder, slot)
		elseif s == "arr" then
			forget_value(st, fd, old)
			forget_arms(st, holder, slot)
			if type(old) == "table" then
				forget_arms(st, old, nil)
			end
		end
	end

	-- a map entry's body, kept by the occurrence of the map that built it
	function h.builtMap(holder: any, slot: any, d: any, inst: any)
		local byKey = st.maps[holder]
		if byKey == nil then
			byKey = {}
			st.maps[holder] = byKey
		end
		local list = byKey[slot]
		if list == nil then
			list = {}
			byKey[slot] = list
		end
		list[#list + 1] = { d, inst }
	end

	-- the arm body this occurrence of a union cell built, whatever becomes of it
	function h.builtArm(holder: any, key: any, d: any, inst: any)
		local byKey = st.arms[holder]
		if byKey == nil then
			byKey = {}
			st.arms[holder] = byKey
		end
		local list = byKey[key]
		if list == nil then
			list = {}
			byKey[key] = list
		end
		list[#list + 1] = { d, inst }
	end

	-- DAMAGE IS NOT A LATER OCCURRENCE: a body that falls back on its declared
	-- defaults stays the body it was, and what it retained stays with it. The
	-- Go engine's Refill keeps the child tables a nested or fixed-array field
	-- holds, so their records are still placed at save
	function h.refill(d: any, inst: any)
		reset_in_place(d, inst)
	end

	-- tail writes one body's retained fields AT THE END OF THAT BODY, in the
	-- order retained. A record whose emit goes bad (a retained id the caller's
	-- list had no room for) is not written at all: one retain_lost counts and
	-- the save is never refused
	function h.tail(c: any, w: any, v: any, api: any)
		local recs = st.records[v]
		if recs == nil then
			return
		end
		for _, rec in recs do
			local em = { c = c, api = api, st = st, bad = false }
			local inner = new_out()
			-- ONE MARK, BOTH STORES: a record that goes bad PARTWAY has taken
			-- entries in either, and the reference undoes both at one mark
			local mark = api.mark(c.ids)
			local idMark = #st.ids
			e_ref(em, inner, rec.id)
			if not em.bad then
				o_u8(inner, rec.kind)
				emit_payload(em, inner, new_cursor(rec.p, 0, rec.n), rec.kind)
			end
			rec.placed = true -- the walk REACHED it, whatever came of the emit
			if em.bad then
				api.rollback(c.ids, mark)
				while #st.ids > idMark do
					st.idset[st.ids[#st.ids]] = nil
					st.ids[#st.ids] = nil
				end
				st.saveLost += 1
			else
				api.str(w, buffer.readstring(inner.b, 0, inner.n))
			end
		end
	end

	return h
end

-- ---- THE THREE VERBS --------------------------------------------------------

-- a fixed-class root is a value (§6.1): no region and no node directory, so
-- the anchor the round trip rests on does not exist. All three are REFUSED BY
-- NAME, on §11's rule for a surface a class does not carry
R.FixedClassRefusal = "fixed_class_root: LoadRetain, MeasureRetain and SaveRetain are the variable class's; a fixed-class root is a value and has no region to retain through (docs/SPEC-TABLES.md §6.6)"

-- LoadRetain is the tolerant load into a REGION with the caller's store on. It
-- answers the root as LoadTolerant does, nil when the read is refused. The
-- store is reset; a nil store is the plain tolerant read. THE REPORT
-- ACCUMULATES ACROSS THE PAIR: zero it once, read it after SaveRetain.
function R.LoadRetain(root: string, bytes: buffer, report: any, store: any): any
	local d = S.lookup(root)
	if d.fixed then
		report.reason = R.FixedClassRefusal
		return nil
	end
	if report.retained == nil then
		report.retained = 0
	end
	if report.retainLost == nil then
		report.retainLost = 0
	end
	local b = bytes
	if d.integrity ~= nil and d.integrity ~= "" then
		local body = L.integrityOpen(b)
		if body == nil then
			report.malformed = true
			report.reason = "integrity_mismatch"
			return nil
		end
		b = body
	end
	if store == nil then
		return L.loadTolerant(root, b, report)
	end
	reset_store(store)
	L.setRetain(make_hook(store))
	local ok, v = pcall(L.loadTolerant, root, b, report)
	L.setRetain(nil)
	if not ok then
		error(v, 0)
	end
	return v
end

local function prepare_save(st: any)
	st.ids = {}
	st.idset = {}
	st.saveLost = 0
	-- THE SAVE'S OWN MARK IS CLEARED BEFORE EVERY SAVE, which is what makes the
	-- second save of one region the first save byte for byte (§6.6)
	for _, recs in st.records do
		for _, rec in recs do
			rec.placed = false
		end
	end
end

-- MeasureRetain is the length SaveRetain writes, by the same walk storing
-- nothing: they drop the same records, so its answer always suffices. The
-- retained-id list fills as the walk interns, and the report is not touched:
-- the save is the one place a caller learns what was dropped
function R.MeasureRetain(root: string, v: any, store: any): (number?, any?)
	local d = S.lookup(root)
	if d.fixed then
		return nil, { name = "fixed_class_root", field = nil }
	end
	if store == nil then
		return W.measure(d, v)
	end
	prepare_save(store)
	W.setRetain(make_hook(store))
	local ok, n, why = pcall(W.measure, d, v)
	W.setRetain(nil)
	if not ok then
		error(n, 0)
	end
	return n, why
end

-- SaveRetain writes the root with the retained fields back at the end of the
-- bodies they came from. THE REPORT IS REQUIRED here where it is optional
-- everywhere else, because the save is the only place a caller learns that a
-- record was dropped: a nil report is refused. A save is never refused for a
-- full id list, and what it could not place counts retain_lost
function R.SaveRetain(root: string, v: any, store: any, report: any): (buffer?, any?)
	if report == nil then
		return nil, { name = "retain_needs_report", field = nil }
	end
	local d = S.lookup(root)
	if d.fixed then
		return nil, { name = "fixed_class_root", field = nil }
	end
	if store == nil then
		return W.save(d, v)
	end
	prepare_save(store)
	W.setRetain(make_hook(store))
	local ok, out, why = pcall(W.save, d, v)
	W.setRetain(nil)
	if not ok then
		error(out, 0)
	end
	report.retainLost += store.saveLost
	if out ~= nil then
		-- AND AT SAVE, A RETAINED RECORD WHOSE PATH NO LONGER NAMES A BODY COUNTS
		-- ONE retain_lost: the save walk never reached its body (§6.6)
		for _, recs in store.records do
			for _, rec in recs do
				if not rec.placed then
					report.retainLost += 1
				end
			end
		end
	end
	return out, why
end

-- LoadRetainMessages is LoadMessages with retention on (§3.3, §6.6): a BATCH
-- TAKES ONE REGION AND ONE RETENTION BUFFER A BODY, stores[k] belongs to the
-- k-th body, and what SaveRetain writes from that body is a FILE, which is the
-- one direction retention crosses the forms. A caller with fewer stores than
-- the batch has bodies is refused before a body is read, on the batch bound's
-- own terms.
function R.LoadRetainMessages(v: any, root: string, data: buffer, capacity: number, stores: { any }, report: any): (number, { any })
	local d = S.lookup(root)
	if d.fixed then
		report.reason = R.FixedClassRefusal
		return -1, {}
	end
	if report.retained == nil then
		report.retained = 0
	end
	if report.retainLost == nil then
		report.retainLost = 0
	end
	local hooks = table.create(#stores)
	for i, st in stores do
		reset_store(st)
		hooks[i] = make_hook(st)
	end
	return MS.LoadMessages(v, root, data, math.min(capacity, #stores), report, hooks)
end

return table.freeze(R)
