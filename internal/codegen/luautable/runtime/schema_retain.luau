--!strict
-- schema_retain.luau — the Luau table runtime's RETAIN-UNKNOWN opt-in on the load
-- (docs/SPEC-TABLES.md §6.6). LoadRetain answers the value the ordinary
-- variable-form load answers, and beside it the retained set: every unknown
-- field this build cannot name, with its id, its kind, its body path and its
-- payload with every reference resolved.
--
-- THE LOAD IS REUSED, NOT COPIED: the value comes from schema_load.luau's own walk
-- (require("./schema_load")), which is what keeps this opt-in additive. The capturing
-- walk below is §6.6's resolving walk and nothing else: it reads the framing
-- the plain read already accepted and rewrites a retained record's references
-- to the 64-bit ids they name. A record it cannot take is one `retain_lost`
-- and never `malformed`, because retention can lose a field but can never turn
-- a good read into a bad one (§6.6).
--
-- A RECORD IS READER-PRIVATE (§6.6). Its layout here is the reference's own:
--   u32 total, u32 node, u32 depth, u32 payload length, u64 id, u8 kind,
--   u8 placed, depth * (u32 field ordinal, u32 element index), payload.
-- Nothing writes one to disk and nothing compares two ports' buffers.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local Load = require("./schema_load")
local Save = require("./schema_save")

local R = {}

local NodeTableId: integer = -1i
local BuildVersionId: integer = -2i
local MessageVocabularyId: integer = -3i
local StringBlobId: integer = 0x704be0d8faaffc58i
local BytesBlobId: integer = 0x2f2ec0474f1c4fe4i

local function new_reader(bytes: buffer, at: number, e: number)
	return { b = bytes, at = at, e = e, bad = false }
end

local function r_leb(r: any): number
	local start = r.at
	local v = 0
	local s = 0
	for i = 0, 9 do
		if r.at >= r.e then r.bad = true; r.at = start; return 0 end
		local x = buffer.readu8(r.b, r.at)
		r.at = r.at + 1
		if i == 9 and x > 1 then r.bad = true; r.at = start; return 0 end
		v = v + (x % 0x80) * (2 ^ s)
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

local function r_u8(r: any): number
	if r.at >= r.e then r.bad = true; return 0 end
	local v = buffer.readu8(r.b, r.at)
	r.at = r.at + 1
	return v
end

-- the width of a scalar kind, or 0 when the kind is not a fixed-width scalar
-- (docs/SPEC-TABLES.md §3)
local function width_of(kind: number): number
	if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then return 1
	elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then return 2
	elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then return 4
	elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then return 8
	elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then return 16
	end
	return 0
end

-- skip moves the cursor over a field payload by its kind alone (§3). It is
-- the plain read's own rule: a failed skip is the plain read's malformed.
local function skip(r: any, kind: number): boolean
	local w = width_of(kind)
	if w > 0 then
		if r.at + w > r.e then return false end
		r.at = r.at + w
		return true
	end
	if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
		local n = r_leb(r); if r.bad then return false end
		if r.at + n > r.e then return false end
		r.at = r.at + n
		return true
	elseif kind == 15 then
		local ref = r_leb(r); if r.bad or ref == 0 then return true end
		r_u8(r)
		local n = r_leb(r); if r.bad or r.at + n > r.e then return false end
		r.at = r.at + n
		return true
	elseif kind == 17 or kind == 30 then
		r_leb(r); return not r.bad
	end
	return false
end

-- THE RESOLVED-WRITER: lengths are written after the content they frame, so a
-- capture is one pass (§6.6).
local function new_out()
	return { parts = {}, len = 0 }
end

local function o_raw(o: any, src: buffer, at: number, n: number)
	o.parts[#o.parts + 1] = buffer.readstring(src, at, n)
	o.len = o.len + n
end

local function o_byte(o: any, v: number)
	o.parts[#o.parts + 1] = string.char(v % 256)
	o.len = o.len + 1
end

local function o_leb(o: any, v: number)
	while v >= 128 do
		o_byte(o, (v % 128) + 128)
		v = math.floor(v / 128)
	end
	o_byte(o, v)
end

-- ONE part of four bytes, never four parts: patch_len rewrites a length's
-- placeholder as the single part at its mark, so a split placeholder would
-- leave three stray bytes behind the patched one
local function o_u32(o: any, v: number)
	o.parts[#o.parts + 1] = string.char(v % 256, math.floor(v / 256) % 256, math.floor(v / 65536) % 256, math.floor(v / 16777216) % 256)
	o.len = o.len + 4
end

local function o_u64(o: any, v: integer)
	local b = buffer.create(8)
	buffer.writeinteger(b, 0, v)
	o_raw(o, b, 0, 8)
end

local function out_string(o: any): string
	return table.concat(o.parts)
end

-- references resolve through the file's id table; a zero is only legal where
-- the shape allows one, and the three reserved ids name no field (§3.1, §3.3)
local function resolve_ref(r: any, ids: { any }, zero: boolean): (integer, boolean)
	local ref = r_leb(r)
	if r.bad then return 0i, false end
	if ref == 0 then return 0i, zero end
	if ref > #ids then return 0i, false end
	local id = ids[ref]
	if id == NodeTableId or id == BuildVersionId or id == MessageVocabularyId then return 0i, false end
	return id, true
end

local resolve_body: any
local resolve_payload: any
local resolve_arm: any
local resolve_content: any
local patch_len: any

-- one body's fields, each with its reference widened to an id (§6.6)
resolve_body = function(r: any, ids: { any }, out: any, depth: number, e: number): boolean
	if depth > 64 then return false end
	while true do
		if r.at > e then return false end
		local id, ok = resolve_ref(r, ids, true)
		if not ok then return false end
		if id == 0i then
			o_u64(out, 0i)
			return r.at == e
		end
		if r.at >= e then return false end
		o_u64(out, id)
		local kind = r_u8(r)
		o_byte(out, kind)
		-- the field's own payload is NOT a nested body: the one step into a
		-- framed body is taken by resolve_payload's own kind arm, exactly as
		-- schema-go's walkPayload hands the same depth to framed (§6.6)
		if not resolve_payload(r, ids, kind, out, depth) then return false end
		if r.at > e then return false end
	end
end

-- a field payload by its kind. Kinds 13 and 14 recurse, 15 resolves an arm
-- header, 30 a variant reference, 16 every keyed slot's key, and everything
-- self-framed is copied with its length recomputed (§6.6).
resolve_payload = function(r: any, ids: { any }, kind: number, out: any, depth: number): boolean
	if depth > 64 then return false end
	local w = width_of(kind)
	if w > 0 then
		if r.at + w > r.e then return false end
		o_raw(out, r.b, r.at, w)
		r.at = r.at + w
		return true
	end
	if kind == 12 or kind == 31 or kind == 33 then
		local n = r_leb(r); if r.bad or r.at + n > r.e then return false end
		o_leb(out, n)
		o_raw(out, r.b, r.at, n)
		r.at = r.at + n
		return true
	elseif kind == 32 then
		local n = r_leb(r); if r.bad or n ~= 0 or r.at + n > r.e then return false end
		o_leb(out, n)
		r.at = r.at + n
		return true
	elseif kind == 30 then
		local id, ok = resolve_ref(r, ids, true)
		if not ok then return false end
		o_u64(out, id)
		return true
	elseif kind == 17 then
		return false
	elseif kind == 13 then
		local n = r_leb(r); if r.bad or r.at + n > r.e then return false end
		local mark, startLen = #out.parts + 1, out.len
		local e = r.at + n
		o_u32(out, 0)
		if not resolve_body(r, ids, out, depth + 1, e) then return false end
		r.at = e
		return patch_len(out, mark, startLen)
	elseif kind == 14 then
		local n = r_leb(r); if r.bad or r.at + n > r.e then return false end
		local e = r.at + n
		if r.at + 1 > e then return false end
		local ek = r_u8(r); o_byte(out, ek)
		local count = r_leb(r); if r.bad then return false end
		o_leb(out, count)
		for _ = 1, count do
			local elen = r_leb(r); if r.bad or r.at + elen > e then return false end
			local ee = r.at + elen
			local mark, startLen = #out.parts + 1, out.len
			o_u32(out, 0)
			if not resolve_content(r, ids, ek, out, depth + 1, ee) then return false end
			if not patch_len(out, mark, startLen) then return false end
			r.at = ee
		end
		return r.at == e
	elseif kind == 16 then
		local n = r_leb(r); if r.bad or r.at + n > r.e then return false end
		local e = r.at + n
		if r.at + 1 > e then return false end
		local ek = r_u8(r); o_byte(out, ek)
		local count = r_leb(r); if r.bad then return false end
		o_leb(out, count)
		for _ = 1, count do
			local key, ok = resolve_ref(r, ids, false); if not ok then return false end
			o_u64(out, key)
			local elen = r_leb(r); if r.bad or r.at + elen > e then return false end
			local ee = r.at + elen
			local mark, startLen = #out.parts + 1, out.len
			o_u32(out, 0)
			if not resolve_content(r, ids, ek, out, depth + 1, ee) then return false end
			if not patch_len(out, mark, startLen) then return false end
			r.at = ee
		end
		return r.at == e
	elseif kind == 15 then
		local id, ok = resolve_ref(r, ids, true); if not ok then return false end
		o_u64(out, id)
		if id == 0i then return true end
		local armKind = r_u8(r); o_byte(out, armKind)
		local n = r_leb(r); if r.bad or r.at + n > r.e then return false end
		local sub = new_reader(r.b, r.at, r.at + n)
		if not resolve_arm(sub, ids, armKind, out, depth + 1) then return false end
		if sub.at ~= sub.e then return false end
		r.at = r.at + n
		return true
	end
	return false
end

-- THE ARM PAYLOAD (§3's arm payload table): a string, wstring or text is its
-- bytes with no length of its own, bytes and an array are an array body, and
-- a table is a body's fields.
resolve_arm = function(r: any, ids: { any }, kind: number, out: any, depth: number): boolean
	if depth > 64 then return false end
	local w = width_of(kind)
	if w > 0 then
		if r.at + w > r.e then return false end
		o_raw(out, r.b, r.at, w)
		r.at = r.at + w
		return true
	elseif kind == 12 or kind == 31 or kind == 33 then
		local n = r.e - r.at
		o_leb(out, n)
		o_raw(out, r.b, r.at, n)
		r.at = r.e
		return true
	elseif kind == 32 then
		return r.at == r.e
	elseif kind == 30 then
		local id, ok = resolve_ref(r, ids, true); if not ok then return false end
		o_u64(out, id)
		return true
	elseif kind == 13 then
		local mark, startLen = #out.parts + 1, out.len
		o_u32(out, 0)
		if not resolve_body(r, ids, out, depth + 1, r.e) then return false end
		return patch_len(out, mark, startLen)
	elseif kind == 14 then
		local ek = r_u8(r); o_byte(out, ek)
		local count = r_leb(r); if r.bad then return false end
		o_leb(out, count)
		for _ = 1, count do
			local elen = r_leb(r); if r.bad or r.at + elen > r.e then return false end
			local ee = r.at + elen
			local mark, startLen = #out.parts + 1, out.len
			o_u32(out, 0)
			if not resolve_content(r, ids, ek, out, depth + 1, ee) then return false end
			if not patch_len(out, mark, startLen) then return false end
			r.at = ee
		end
		return r.at == r.e
	elseif kind == 17 then
		return false
	end
	return false
end

-- one framed element's content: a table body is kind 13, a union kind 15, and
-- anything else is its own payload (§6.6)
resolve_content = function(r: any, ids: { any }, kind: number, out: any, depth: number, e: number): boolean
	if r.at > e then return false end
	if kind == 13 then
		return resolve_body(r, ids, out, depth + 1, e) and r.at == e
	elseif kind == 15 then
		return resolve_payload(r, ids, 15, out, depth + 1) and r.at == e
	end
	return resolve_payload(r, ids, kind, out, depth + 1) and r.at == e
end

patch_len = function(out: any, mark: number, startLen: number): boolean
	local n = out.len - startLen - 4
	if n < 0 or n > 0xFFFFFFFF then return false end
	out.parts[mark] = string.char(n % 256, math.floor(n / 256) % 256, math.floor(n / 65536) % 256, math.floor(n / 16777216) % 256)
	return true
end

-- ---- THE CAPTURING WALK (docs/SPEC-TABLES.md §6.6) ------------------------

local byIdCache: { [any]: { [integer]: any } } = {}

local function fields_by_id(desc: any): { [integer]: any }
	local m = byIdCache[desc]
	if m == nil then
		m = {}
		for _, f in desc.fields do m[f.id] = f end
		byIdCache[desc] = m
	end
	return m
end

-- one retained record, in the reader-private layout this file states above. The
-- store is the CALLER'S BOUNDED BUFFER (docs/SPEC-TABLES.md §6.6): a record the
-- remaining capacity cannot hold WHOLE is dropped and counts one `retain_lost`,
-- and the buffer never grows with file-controlled data and never holds a
-- truncated field. `store.cap` is the ceiling the caller set; this store alloc
-- nothing beyond it.
local function store_record(store: any, node: number, steps: { any }, id: integer, kind: number, payload: string): boolean
	local depth = #steps
	local plen = #payload
	local total = 26 + depth * 8 + plen
	if store.used + total > store.cap then
		return false
	end
	local b = store.bytes
	buffer.writeu32(b, store.used, total)
	buffer.writeu32(b, store.used + 4, node)
	buffer.writeu32(b, store.used + 8, depth)
	buffer.writeu32(b, store.used + 12, plen)
	buffer.writeinteger(b, store.used + 16, id)
	buffer.writeu8(b, store.used + 24, kind)
	buffer.writeu8(b, store.used + 25, 0)
	for i, st in steps do
		buffer.writeu32(b, store.used + 26 + (i - 1) * 8, st.o)
		buffer.writeu32(b, store.used + 30 + (i - 1) * 8, st.i)
	end
	if plen > 0 then
		buffer.copy(b, store.used + 26 + depth * 8, buffer.fromstring(payload), 0, plen)
	end
	store.used = store.used + total
	store.count = store.count + 1
	return true
end

local walk_body: any
local walk_field: any
local walk_union: any
local walk_array: any
local walk_map: any
local walk_element: any
local walk_enum: any
local _ids: { [number]: integer } = {}

-- A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (docs/SPEC-TABLES.md
-- §3.3, §6.6; ruling 3, 2026-10-27): kind 10's four bytes and kind 11's eight
-- are tested on their raw bits, and a NaN or an infinity refuses the read,
-- uniformity winning over carrying bytes this reader cannot name. The name is
-- the body's table and the id it cannot name, <Table>.0x<id>.
local _nonFinite: string? = nil
local idScratch = buffer.create(8)
local function retainedFloatName(owner: string, id: integer): string
	buffer.writeinteger(idScratch, 0, id)
	return string.format("%s.0x%08x%08x", owner, buffer.readu32(idScratch, 4), buffer.readu32(idScratch, 0))
end

local function capture(store: any, report: any, ids: { [number]: integer }, node: number, steps: { any }, id: integer, kind: number, r: any, owner: string)
	local start = r.at
	if not skip(r, kind) then return end
	if (kind == 10 and r.at - start == 4 and bit32.band(buffer.readu32(r.b, start), 0x7F800000) == 0x7F800000)
		or (kind == 11 and r.at - start == 8 and bit32.band(buffer.readu32(r.b, start + 4), 0x7FF00000) == 0x7FF00000) then
		if _nonFinite == nil then _nonFinite = retainedFloatName(owner, id) end
		return
	end
	local out = new_out()
	local src = new_reader(r.b, start, r.at)
	if not resolve_payload(src, ids, kind, out, 0) or src.at ~= src.e then
		report.retainLost = report.retainLost + 1
		return
	end
	if not store_record(store, node, steps, id, kind, out_string(out)) then
		report.retainLost = report.retainLost + 1
		return
	end
	report.retained = report.retained + 1
end

local function push(steps: { any }, o: number, i: number)
	steps[#steps + 1] = { o = o, i = i }
end

local function pop(steps: { any })
	steps[#steps] = nil
end

-- a known ENUM field: an unknown variant reference is an excluded class
walk_enum = function(fd: any, r: any, report: any)
	local ref = r_leb(r)
	if r.bad or ref == 0 then return end
	if ref > 0 then
		local id = nil
		-- the id table is closed over by the caller through _ids (below)
		id = _ids[ref]
		if id ~= nil then
			local known = false
			for _, v in fd.vs or {} do
				if v[2] == id then known = true; break end
			end
			if not known then report.retainLost = report.retainLost + 1 end
		end
	end
end

-- a union: an arm this build cannot name is an excluded class (§6.6)
walk_union = function(fd: any, r: any, steps: { any }, store: any, report: any, node: number)
	local ref = r_leb(r)
	if r.bad or ref == 0 then return end
	if ref > #_ids then return end
	local armID = _ids[ref]
	local kind = r_u8(r)
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then return end
	local sub = new_reader(r.b, r.at, r.at + n)
	local un = S.lookup(fd.t)
	local arm: any = nil
	for _, a in un.arms do
		if a.id == armID then arm = a; break end
	end
	if arm == nil then
		report.retainLost = report.retainLost + 1
		r.at = r.at + n
		return
	end
	local want = arm.s == "void" and 32 or arm.k
	if kind ~= want then r.at = r.at + n; return end
	push(steps, fd.o or 0, arm.tag or 0)
	if arm.s == "tab" then
		walk_body(S.lookup(arm.t), sub, steps, store, report, node)
	elseif arm.s == "arr" then
		walk_array(arm, sub, steps, store, report, node)
	elseif arm.s == "map" then
		walk_map(arm, sub, steps, store, report, node)
	end
	pop(steps)
	r.at = r.at + n
end

-- one array or keyed array body (§2.9, §3.2)
walk_array = function(fd: any, r: any, steps: { any }, store: any, report: any, node: number)
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then return end
	local e = r.at + n
	if n < 2 then r.at = e; return end
	local sub = new_reader(r.b, r.at, e)
	local ek = r_u8(sub)
	local count = r_leb(sub)
	if sub.bad then r.at = e; return end
	if fd.array == "keyed" then
		for _ = 1, count do
			local keyref = r_leb(sub)
			if sub.bad then break end
			local at = 0
			if keyref > 0 and keyref <= #_ids then
				local keyID = _ids[keyref]
				for i, kv in fd.keyvs or {} do
					if kv[2] == keyID then at = i; break end
				end
			end
			local elen = r_leb(sub)
			if sub.bad or sub.at + elen > sub.e then break end
			if at == 0 then
				report.retainLost = report.retainLost + 1
				sub.at = sub.at + elen
			else
				push(steps, fd.o or 0, at - 1)
				walk_element(fd.e, sub, steps, store, report, node, elen)
				pop(steps)
			end
		end
	else
		local el = fd.e or {}
		for i = 1, count do
			if ek == 13 then
				local elen = r_leb(sub)
				if sub.bad or sub.at + elen > sub.e then break end
				push(steps, fd.o or 0, i - 1)
				local body = new_reader(sub.b, sub.at, sub.at + elen)
				if el.s == "tab" then walk_body(S.lookup(el.t), body, steps, store, report, node) end
				pop(steps)
				sub.at = sub.at + elen
			elseif ek == 15 then
				push(steps, fd.o or 0, i - 1)
				walk_union(el, sub, steps, store, report, node)
				pop(steps)
			else
				local w = width_of(ek)
				if w > 0 then
					if sub.at + w > sub.e then break end
					sub.at = sub.at + w
				else
					break
				end
			end
		end
	end
	r.at = e
end

-- one framed element's body (an array element or a keyed slot)
walk_element = function(el: any, sub: any, steps: { any }, store: any, report: any, node: number, elen: number)
	local body = new_reader(sub.b, sub.at, sub.at + elen)
	if el ~= nil and el.s == "tab" then
		walk_body(S.lookup(el.t), body, steps, store, report, node)
	elseif el ~= nil and el.s == "union" then
		walk_union(el, body, steps, store, report, node)
	elseif el ~= nil and el.s == "arr" then
		walk_array(el, body, steps, store, report, node)
	end
	sub.at = sub.at + elen
end

-- a map: its entries are framed bodies, and a key this build cannot name is
-- an excluded class (§2.8, §6.6)
walk_map = function(fd: any, r: any, steps: { any }, store: any, report: any, node: number)
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then return end
	local e = r.at + n
	if n < 2 then r.at = e; return end
	local sub = new_reader(r.b, r.at, e)
	local ek = r_u8(sub)
	local count = r_leb(sub)
	if sub.bad or ek ~= 13 then r.at = e; return end
	local entry = S.lookup(fd.t)
	for i = 1, count do
		local elen = r_leb(sub)
		if sub.bad or sub.at + elen > sub.e then break end
		push(steps, fd.o or 0, i - 1)
		local body = new_reader(sub.b, sub.at, sub.at + elen)
		walk_body(entry, body, steps, store, report, node)
		pop(steps)
		sub.at = sub.at + elen
	end
	r.at = e
end

-- one known field: descend through the shapes that hold bodies, and skip the
-- rest by its kind
walk_field = function(fd: any, r: any, steps: { any }, store: any, report: any, node: number)
	local sh = fd.s
	if sh == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then return end
		push(steps, fd.o or 0, 0)
		walk_body(S.lookup(fd.t), new_reader(r.b, r.at, r.at + n), steps, store, report, node)
		pop(steps)
		r.at = r.at + n
	elseif sh == "union" then
		walk_union(fd, r, steps, store, report, node)
	elseif sh == "arr" then
		walk_array(fd, r, steps, store, report, node)
	elseif sh == "map" then
		walk_map(fd, r, steps, store, report, node)
	elseif sh == "enum" then
		walk_enum(fd, r, report)
	else
		if not skip(r, fd.k) then return end
	end
end

-- one body: the unknown ids are captured, the known ones descended or skipped
walk_body = function(desc: any, r: any, steps: { any }, store: any, report: any, node: number)
	local byId = fields_by_id(desc)
	for _ = 1, math.huge do
		local ref = r_leb(r)
		if r.bad or ref == 0 then return end
		if ref > #_ids then return end
		if r.at >= r.e then return end
		local kind = r_u8(r)
		local id = _ids[ref]
		if id == NodeTableId or id == BuildVersionId or id == MessageVocabularyId then return end
		local fd = byId[id]
		if fd == nil then
			capture(store, report, _ids, node, steps, id, kind, r, desc.name)
		elseif kind == fd.k then
			walk_field(fd, r, steps, store, report, node)
		else
			if not skip(r, kind) then return end
		end
	end
end

-- THE ROOT BODY AND EVERY NODE RECORD (§3.1). The node table is the writer's
-- numbering: its unknown node types are an excluded class, and a known record
-- is walked under its own first path step (k + 1).
local function node_records(bytes: buffer, ids: { any }, endpos: number): any
	local w = new_reader(bytes, 1, endpos)
	local payload: buffer? = nil
	local present = false
	while true do
		local ref = r_leb(w)
		if w.bad or ref == 0 or ref > #ids then break end
		if w.at >= w.e then break end
		local id = ids[ref]
		local kind = r_u8(w)
		if id == NodeTableId then
			if kind ~= 12 then break end
			local n = r_leb(w)
			if w.bad or w.at + n > w.e then break end
			local p = buffer.create(n)
			buffer.copy(p, 0, bytes, w.at, n)
			payload = p
			present = true
			w.at = w.at + n
		else
			if not skip(w, kind) then break end
		end
	end
	if not present then return {} end
	local rec = new_reader(payload :: buffer, 0, buffer.len(payload :: buffer))
	local count = r_leb(rec)
	if rec.bad then return {} end
	local raw = {}
	while rec.at < rec.e do
		local tref = r_leb(rec)
		if rec.bad or tref == 0 or tref > #ids then break end
		local tlen = r_leb(rec)
		if rec.bad or rec.at + tlen > rec.e then break end
		local body = buffer.create(tlen)
		buffer.copy(body, 0, payload :: buffer, rec.at, tlen)
		raw[#raw + 1] = { tid = ids[tref], body = body }
		rec.at = rec.at + tlen
	end
	if count ~= #raw then return {} end
	return raw
end

-- LoadRetain is the ordinary load's answer beside §6.6's retained set, and
-- LoadRetainTolerant the tolerant load's (§6.5: the plain name is the strict
-- read, and the recovering read is reached by its explicit name, as
-- schema_load.luau's loadTolerant is). The value comes from schema_load.luau's
-- own walk; the capturing walk adds nothing to the read's verdict and never
-- raises `malformed`, and a file whose id table the tolerant read could not
-- frame keeps no record at all. `capacity` is the
-- byte ceiling the caller owns: the retained store never grows past it and a
-- whole record that does not fit counts one `retain_lost` (§6.6). `idCount` is
-- the retained-id list's capacity, a COUNT: the save numbers an id this build
-- cannot name into that list, and a record whose ids would pass it is dropped
-- and counts one `retain_lost` there (§6.6). It defaults to C / 8, the bound
-- no file can beat.
local function load_retain(read: any, root: string, bytes: buffer, report: any, capacity: number?, idCount: number?)
	if report.retained == nil then report.retained = 0 end
	if report.retainLost == nil then report.retainLost = 0 end
	local cap: number = capacity or 65536
	if cap < 0 then cap = 0 end
	local idCap: number = idCount or math.floor(cap / 8)
	if idCap < 0 then idCap = 0 end
	local new_store = function()
		return { bytes = buffer.create(cap), cap = cap, used = 0, count = 0, idCap = idCap }
	end
	local v = read(root, bytes, report)
	if v == nil then
		return v, new_store()
	end
	_nonFinite = nil
	local store = new_store()
	local len = buffer.len(bytes)
	local count = 0
	if len >= 9 then
		count = integer.tonumber(buffer.readinteger(bytes, len - 8))
	end
	local endpos = len - 8 - count * 8
	if count < 0 or endpos < 1 then
		-- an id table the tolerant read could not frame: nothing to capture
		return v, store
	end
	local ids = {}
	for i = 0, count - 1 do
		ids[i + 1] = buffer.readinteger(bytes, endpos + i * 8)
	end
	_ids = ids
	local desc = S.lookup(root)
	local raw = node_records(bytes, ids, endpos)
	local place = {}
	if desc.place ~= nil then
		for _, name in desc.place.tables do
			local d = S.lookup(name)
			place[d.id] = d
		end
	end
	for k, rec in raw do
		if rec.tid == StringBlobId then
			-- a retained blob node is not a body: nothing to capture
		elseif rec.tid == BytesBlobId then
			-- likewise
		else
			local nc = place[rec.tid]
			if nc == nil then
				report.retainLost = report.retainLost + 1
			else
				walk_body(nc, new_reader(rec.body, 0, buffer.len(rec.body)), {}, store, report, k + 1)
			end
		end
	end
	walk_body(desc, new_reader(bytes, 1, endpos), {}, store, report, 1)
	if _nonFinite ~= nil then
		-- the strict read's own refusal, as a known float's (§3.3)
		report.nonFiniteFloat = _nonFinite
		report.reason = "non_finite_float"
		report.field = _nonFinite
		return nil, new_store()
	end
	return v, store
end

function R.LoadRetain(root: string, bytes: buffer, report: any, capacity: number?, idCount: number?)
	return load_retain(Load.load, root, bytes, report, capacity, idCount)
end

function R.LoadRetainTolerant(root: string, bytes: buffer, report: any, capacity: number?, idCount: number?)
	return load_retain(Load.loadTolerant, root, bytes, report, capacity, idCount)
end

-- ---------------------------------------------------------------------------
-- THE RETAINING SAVE (docs/SPEC-TABLES.md §6.6)
--
-- SaveRetain saves the value exactly as schema_save.luau does, then writes each kept
-- record back at the end of the body that carried it, in the order retained.
-- A record the saved path no longer names is dropped and counts one
-- retain_lost; the save itself is never refused by retention.
--
-- The plain bytes are re-read into a small tree so a body's retained fields
-- can be added before the length that frames it is written. A table body's
-- tail sits BEFORE the root's node table, as schema-go writes it, and every
-- retained id is interned into the one id table in the same first-use order.

local function save_leb(v: number): string
	local parts = {}
	while v >= 128 do
		parts[#parts + 1] = string.char((v % 128) + 128)
		v = math.floor(v / 128)
	end
	parts[#parts + 1] = string.char(v)
	return table.concat(parts)
end

local function save_concat(parts: { string }): string
	return table.concat(parts)
end

local function save_reader(s: string): any
	return { s = s, at = 1 }
end

local function save_u8(r: any): number
	local v = string.byte(r.s, r.at)
	r.at = r.at + 1
	return v
end

local function save_leb_read(r: any): number
	local v, sh = 0, 0
	while true do
		local x = save_u8(r)
		v = v + (x % 128) * (2 ^ sh)
		if x < 128 then return v end
		sh = sh + 7
	end
end

local function save_take(r: any, n: number): string
	local v = string.sub(r.s, r.at, r.at + n - 1)
	r.at = r.at + n
	return v
end

-- a wire payload's own length, for a kind the tree keeps verbatim
local function save_skip(r: any, kind: number): boolean
	local w = width_of(kind)
	if w > 0 then
		r.at = r.at + w
		return true
	elseif kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
		local n = save_leb_read(r)
		r.at = r.at + n
		return true
	elseif kind == 15 then
		local ref = save_leb_read(r)
		if ref == 0 then return true end
		save_u8(r)
		local n = save_leb_read(r)
		r.at = r.at + n
		return true
	elseif kind == 30 or kind == 17 then
		save_leb_read(r)
		return true
	end
	return false
end

local save_parse_body: any
local save_parse_payload: any
local save_parse_array: any
local save_parse_keyed: any
local save_parse_union: any
local save_parse_arm: any
local save_parse_nodes: any

save_parse_body = function(r: any, ids: { any }): any
	local fields = {}
	while true do
		local ref = save_leb_read(r)
		if ref == 0 then break end
		local kind = save_u8(r)
		local node: any
		local id = ids[ref]
		if kind == 12 and id == NodeTableId then
			node = save_parse_nodes(r, ids)
		else
			node = save_parse_payload(r, kind, ids)
		end
		fields[#fields + 1] = { ref = ref, kind = kind, node = node }
	end
	return { fields = fields }
end

save_parse_payload = function(r: any, kind: number, ids: { any }): any
	if kind == 13 then
		local n = save_leb_read(r)
		local body = save_parse_body(save_reader(save_take(r, n)), ids)
		return { t = "tab", body = body }
	elseif kind == 14 then
		local n = save_leb_read(r)
		local sub = save_reader(save_take(r, n))
		return save_parse_array(sub, ids)
	elseif kind == 16 then
		local n = save_leb_read(r)
		local sub = save_reader(save_take(r, n))
		return save_parse_keyed(sub, ids)
	elseif kind == 15 then
		return save_parse_union(r, ids)
	elseif kind == 30 then
		-- an enum variant REFERENCE names an id, so the emit re-interns it
		-- into the new id table rather than copying the old index
		return { t = "enum", ref = save_leb_read(r) }
	end
	local start = r.at
	save_skip(r, kind)
	return { t = "raw", b = string.sub(r.s, start, r.at - 1) }
end

save_parse_array = function(r: any, ids: { any }): any
	local ek = save_u8(r)
	local count = save_leb_read(r)
	local elems = {}
	for i = 1, count do
		if ek == 13 then
			local n = save_leb_read(r)
			elems[i] = { kind = "body", body = save_parse_body(save_reader(save_take(r, n)), ids) }
		elseif ek == 15 then
			elems[i] = { kind = "union", node = save_parse_union(r, ids) }
		elseif ek == 30 then
			elems[i] = { kind = "enum", ref = save_leb_read(r) }
		else
			local start = r.at
			save_skip(r, ek)
			elems[i] = { kind = "raw", b = string.sub(r.s, start, r.at - 1) }
		end
	end
	return { t = "arr", ek = ek, elems = elems }
end

save_parse_keyed = function(r: any, ids: { any }): any
	local ek = save_u8(r)
	local count = save_leb_read(r)
	local slots = {}
	for i = 1, count do
		local key = save_leb_read(r)
		local n = save_leb_read(r)
		local sub = save_reader(save_take(r, n))
		local content: any
		if ek == 13 then
			content = { kind = "body", body = save_parse_body(sub, ids) }
		elseif ek == 15 then
			content = { kind = "union", node = save_parse_union(sub, ids) }
		else
			local node = save_parse_payload(sub, ek, ids)
			content = { kind = "node", node = node }
		end
		content.key = key
		slots[i] = content
	end
	return { t = "keyed", ek = ek, slots = slots }
end

save_parse_union = function(r: any, ids: { any }): any
	local ref = save_leb_read(r)
	if ref == 0 then return { t = "union", ref = 0 } end
	local ak = save_u8(r)
	local n = save_leb_read(r)
	local sub = save_reader(save_take(r, n))
	return { t = "union", ref = ref, ak = ak, arm = save_parse_arm(sub, ak, ids) }
end

save_parse_arm = function(r: any, kind: number, ids: { any }): any
	if kind == 13 then
		return { kind = "body", body = save_parse_body(r, ids) }
	elseif kind == 14 then
		return { kind = "arr", node = save_parse_array(r, ids) }
	elseif kind == 16 then
		return { kind = "keyed", node = save_parse_keyed(r, ids) }
	elseif kind == 15 then
		return { kind = "union", node = save_parse_union(r, ids) }
	elseif kind == 12 or kind == 31 or kind == 33 then
		local n = r.at <= #r.s and (#r.s - r.at + 1) or 0
		return { kind = "raw", b = save_take(r, n) }
	elseif kind == 30 then
		return { kind = "enum", ref = save_leb_read(r) }
	elseif kind == 32 then
		return { kind = "raw", b = "" }
	end
	local w = width_of(kind)
	if w > 0 then
		return { kind = "raw", b = save_take(r, w) }
	end
	return { kind = "raw", b = "" }
end

save_parse_nodes = function(r: any, ids: { any }): any
	local n = save_leb_read(r)
	local q = save_reader(save_take(r, n))
	local count = save_leb_read(q)
	local nodes: { any } = {}
	for i = 1, count do
		local tref = save_leb_read(q)
		local len = save_leb_read(q)
		local data = save_take(q, len)
		local tid = ids[tref]
		if tid == StringBlobId or tid == BytesBlobId then
			nodes[i] = { tref = tref, blob = data }
		else
			nodes[i] = { tref = tref, body = save_parse_body(save_reader(data), ids) }
		end
	end
	return { t = "nodes", nodes = nodes }
end

local function res_u32(c: any): number
	if c.at + 4 > c.e then c.bad = true; return 0 end
	local v = buffer.readu32(c.b, c.at)
	c.at = c.at + 4
	return v
end

local function res_u64(c: any): integer
	if c.at + 8 > c.e then c.bad = true; return 0i end
	local v = buffer.readinteger(c.b, c.at)
	c.at = c.at + 8
	return v
end

local function res_sub(c: any, n: number): any
	if n < 0 or c.at + n > c.e then c.bad = true; return { b = c.b, at = c.at, e = c.at, bad = true } end
	local q = { b = c.b, at = c.at, e = c.at + n, bad = false }
	c.at = c.at + n
	return q
end

local function res_take(c: any, n: number): string
	if n < 0 or c.at + n > c.e then c.bad = true; return "" end
	local s = buffer.readstring(c.b, c.at, n)
	c.at = c.at + n
	return s
end

local function save_ref(cx: any, id: integer): number
	if id == 0i then return 0 end
	local r = cx.refmap[id]
	if r ~= nil then return r end
	-- A RETAINED ID PAST THE CAPACITY (§6.6): inside a retained record, an id
	-- this build cannot name takes an entry of the caller's list; when the list
	-- is full the record is lost, and emit_record unwinds what it interned
	if cx.inRecord and cx.idCap ~= nil and not S.knownId(id) then
		if cx.unknownUsed >= cx.idCap then
			cx.recordLost = true
			return 0
		end
		cx.unknownUsed = cx.unknownUsed + 1
	end
	cx.out_ids[#cx.out_ids + 1] = id
	r = #cx.out_ids
	cx.refmap[id] = r
	return r
end

local save_res_body: any
local save_res_payload: any
local save_res_array: any
local save_res_keyed: any
local save_res_union: any
local save_res_arm: any

save_res_body = function(cx: any, c: any): string
	local parts = {}
	while true do
		local id = res_u64(c)
		if c.bad then return "" end
		if id == 0i then
			parts[#parts + 1] = "\0"
			return save_concat(parts)
		end
		if c.at >= c.e then c.bad = true; return "" end
		local kind = r_u8(c)
		parts[#parts + 1] = save_leb(save_ref(cx, id))
		parts[#parts + 1] = string.char(kind)
		parts[#parts + 1] = save_res_payload(cx, c, kind)
		if c.bad then return "" end
	end
end

save_res_payload = function(cx: any, c: any, kind: number): string
	if kind == 30 then
		local id = res_u64(c)
		if c.bad then return "" end
		if id == 0i then return save_leb(0) end
		return save_leb(save_ref(cx, id))
	elseif kind == 15 then
		return save_res_union(cx, c)
	elseif kind == 13 then
		local n = res_u32(c)
		local sub = res_sub(c, n)
		local body = save_res_body(cx, sub)
		if sub.bad then c.bad = true end
		if c.bad then return "" end
		return save_leb(#body) .. body
	elseif kind == 14 then
		local content = save_res_array(cx, c)
		if c.bad then return "" end
		return save_leb(#content) .. content
	elseif kind == 16 then
		local content = save_res_keyed(cx, c)
		if c.bad then return "" end
		return save_leb(#content) .. content
	end
	local start = c.at
	if not skip(c, kind) then c.bad = true; return "" end
	return buffer.readstring(c.b, start, c.at - start)
end

save_res_array = function(cx: any, c: any): string
	local parts = {}
	local ek = r_u8(c)
	if c.bad then return "" end
	local count = r_leb(c)
	if c.bad then return "" end
	parts[#parts + 1] = string.char(ek)
	parts[#parts + 1] = save_leb(count)
	for _ = 1, count do
		local n = res_u32(c)
		local sub = res_sub(c, n)
		local elem: string
		if ek == 13 then
			local body = save_res_body(cx, sub)
			elem = save_leb(#body) .. body
		else
			elem = save_res_payload(cx, sub, ek)
		end
		if sub.bad then c.bad = true end
		if c.bad then return "" end
		parts[#parts + 1] = elem
	end
	return save_concat(parts)
end

save_res_keyed = function(cx: any, c: any): string
	local parts = {}
	local ek = r_u8(c)
	if c.bad then return "" end
	local count = r_leb(c)
	if c.bad then return "" end
	parts[#parts + 1] = string.char(ek)
	parts[#parts + 1] = save_leb(count)
	for _ = 1, count do
		local key = res_u64(c)
		local keyRef = save_ref(cx, key)
		local n = res_u32(c)
		local sub = res_sub(c, n)
		local content: string
		if ek == 13 then
			content = save_res_body(cx, sub)
		else
			content = save_res_payload(cx, sub, ek)
		end
		if sub.bad then c.bad = true end
		if c.bad then return "" end
		parts[#parts + 1] = save_leb(keyRef)
		parts[#parts + 1] = save_leb(#content)
		parts[#parts + 1] = content
	end
	return save_concat(parts)
end

save_res_union = function(cx: any, c: any): string
	local id = res_u64(c)
	if c.bad then return "" end
	if id == 0i then return save_leb(0) end
	if c.at >= c.e then c.bad = true; return "" end
	local ref = save_ref(cx, id)
	local ak = r_u8(c)
	local arm = save_res_arm(cx, c, ak)
	if c.bad then return "" end
	return save_leb(ref) .. string.char(ak) .. save_leb(#arm) .. arm
end

save_res_arm = function(cx: any, c: any, kind: number): string
	if kind == 13 then
		-- a table arm's resolved body is framed by its u32 length, as
		-- resolve_arm writes it (§6.6's record payload)
		local n = res_u32(c)
		local sub = res_sub(c, n)
		local body = save_res_body(cx, sub)
		if sub.bad then c.bad = true; return "" end
		return body
	elseif kind == 14 then
		return save_res_array(cx, c)
	elseif kind == 16 then
		return save_res_keyed(cx, c)
	elseif kind == 15 then
		return save_res_union(cx, c)
	elseif kind == 12 or kind == 31 or kind == 33 then
		local n = r_leb(c)
		if c.bad then return "" end
		return res_take(c, n)
	elseif kind == 32 then
		return ""
	elseif kind == 30 then
		local id = res_u64(c)
		if c.bad then return "" end
		if id == 0i then return save_leb(0) end
		return save_leb(save_ref(cx, id))
	end
	local w = width_of(kind)
	if w > 0 then return res_take(c, w) end
	c.bad = true
	return ""
end

local save_emit_body: any
local save_emit_payload: any
local save_emit_array: any
local save_emit_keyed: any
local save_emit_union: any

-- an enum variant reference, re-interned (0 is no variant)
local function save_enum_ref(cx: any, ref: number): string
	if ref == 0 then return save_leb(0) end
	return save_leb(save_ref(cx, cx.ids[ref]))
end

save_emit_body = function(cx: any, body: any): string
	local parts = {}
	for _, f in body.fields do
		if f.rec ~= nil then
			-- the record's own id is interned BEFORE its payload's, in the
			-- order the bytes are written, as the reference writer interns
			local mark, unknownMark = #cx.out_ids, cx.unknownUsed
			cx.inRecord, cx.recordLost = true, false
			local ref = save_ref(cx, f.rec.id)
			local c = { b = buffer.fromstring(f.rec.payload), at = 0, e = #f.rec.payload, bad = false }
			local payload = save_res_payload(cx, c, f.rec.kind)
			cx.inRecord = false
			if cx.recordLost then
				-- the record is not written at all, and every id it interned
				-- is taken back, as the reference's Truncate(mark) does
				for k = #cx.out_ids, mark + 1, -1 do
					cx.refmap[cx.out_ids[k]] = nil
					cx.out_ids[k] = nil
				end
				cx.unknownUsed = unknownMark
				cx.recordLost = false
				cx.dropped = cx.dropped + 1
				continue
			end
			if c.bad or cx.bad then cx.bad = true; return "" end
			parts[#parts + 1] = save_leb(ref)
			parts[#parts + 1] = string.char(f.rec.kind)
			parts[#parts + 1] = payload
		else
			parts[#parts + 1] = save_leb(save_ref(cx, cx.ids[f.ref]))
			parts[#parts + 1] = string.char(f.kind)
			parts[#parts + 1] = save_emit_payload(cx, f.node, f.kind)
		end
		if cx.bad then return "" end
	end
	parts[#parts + 1] = "\0"
	return save_concat(parts)
end

save_emit_payload = function(cx: any, node: any, kind: number): string
	if node.t == "tab" then
		local body = save_emit_body(cx, node.body)
		if cx.bad then return "" end
		return save_leb(#body) .. body
	elseif node.t == "arr" then
		local content = save_emit_array(cx, node)
		if cx.bad then return "" end
		return save_leb(#content) .. content
	elseif node.t == "keyed" then
		local content = save_emit_keyed(cx, node)
		if cx.bad then return "" end
		return save_leb(#content) .. content
	elseif node.t == "union" then
		return save_emit_union(cx, node)
	elseif node.t == "nodes" then
		local parts = {}
		parts[#parts + 1] = save_leb(#node.nodes)
		for _, n in node.nodes do
			parts[#parts + 1] = save_leb(save_ref(cx, cx.ids[n.tref]))
			if n.blob ~= nil then
				parts[#parts + 1] = save_leb(#n.blob)
				parts[#parts + 1] = n.blob
			else
				local body = save_emit_body(cx, n.body)
				if cx.bad then return "" end
				parts[#parts + 1] = save_leb(#body)
				parts[#parts + 1] = body
			end
		end
		local content = save_concat(parts)
		return save_leb(#content) .. content
	elseif node.t == "enum" then
		return save_enum_ref(cx, node.ref)
	end
	return node.b
end

save_emit_array = function(cx: any, node: any): string
	local parts = { string.char(node.ek), save_leb(#node.elems) }
	for _, e in node.elems do
		if e.kind == "body" then
			local body = save_emit_body(cx, e.body)
			if cx.bad then return "" end
			parts[#parts + 1] = save_leb(#body) .. body
		elseif e.kind == "union" then
			parts[#parts + 1] = save_emit_union(cx, e.node)
		elseif e.kind == "enum" then
			parts[#parts + 1] = save_enum_ref(cx, e.ref)
		else
			parts[#parts + 1] = e.b
		end
	end
	return save_concat(parts)
end

save_emit_keyed = function(cx: any, node: any): string
	local parts = { string.char(node.ek), save_leb(#node.slots) }
	for _, s in node.slots do
		local key = save_ref(cx, cx.ids[s.key])
		local content: string
		if s.kind == "body" then
			content = save_emit_body(cx, s.body)
		elseif s.kind == "union" then
			content = save_emit_union(cx, s.node)
		else
			content = save_emit_payload(cx, s.node, node.ek)
		end
		if cx.bad then return "" end
		parts[#parts + 1] = save_leb(key)
		parts[#parts + 1] = save_leb(#content)
		parts[#parts + 1] = content
	end
	return save_concat(parts)
end

save_emit_union = function(cx: any, node: any): string
	if node.ref == 0 then return save_leb(0) end
	local ref = save_ref(cx, cx.ids[node.ref])
	local arm: string
	if node.arm.kind == "body" then
		arm = save_emit_body(cx, node.arm.body)
	elseif node.arm.kind == "arr" then
		arm = save_emit_array(cx, node.arm.node)
	elseif node.arm.kind == "keyed" then
		arm = save_emit_keyed(cx, node.arm.node)
	elseif node.arm.kind == "union" then
		arm = save_emit_union(cx, node.arm.node)
	elseif node.arm.kind == "enum" then
		arm = save_enum_ref(cx, node.arm.ref)
	else
		arm = node.arm.b
	end
	if cx.bad then return "" end
	return save_leb(ref) .. string.char(node.ak) .. save_leb(#arm) .. arm
end

local function save_field_of(body: any, ids: { any }, desc: any, o: number): any
	for _, f in body.fields do
		if f.rec == nil then
			local fd = fields_by_id(desc)[ids[f.ref]]
			if fd ~= nil and (fd.o or 0) == o then return f end
		end
	end
	return nil
end

local save_locate_body: any
local save_locate_element: any
local save_locate_union: any
local save_locate_array: any

save_locate_body = function(body: any, desc: any, steps: { any }, pos: number): any
	if pos > #steps then return body end
	local st = steps[pos]
	local fd = nil
	for _, f in desc.fields do
		if (f.o or 0) == st.o then fd = f; break end
	end
	if fd == nil then return nil end
	local f = save_field_of(body, _ids, desc, st.o)
	if f == nil then return nil end
	local s = fd.s
	if s == "tab" then
		if f.node.t ~= "tab" then return nil end
		return save_locate_body(f.node.body, S.lookup(fd.t), steps, pos + 1)
	elseif s == "union" then
		return save_locate_union(fd, f.node, steps, pos)
	elseif s == "arr" then
		if fd.array == "keyed" then
			-- an enum-keyed array is kind 16 on the wire, parsed as "keyed"
			-- slots each naming its variant (docs/SPEC-TABLES.md §3)
			if f.node.t ~= "keyed" then return nil end
			local kv = fd.keyvs and fd.keyvs[st.i + 1]
			if kv == nil then return nil end
			for _, slot in f.node.slots do
				if _ids[slot.key] == kv[2] then
					return save_locate_element(fd, slot, steps, pos + 1)
				end
			end
			return nil
		end
		if f.node.t ~= "arr" then return nil end
		local e = f.node.elems[st.i + 1]
		if e == nil then return nil end
		return save_locate_element(fd, e, steps, pos + 1)
	elseif s == "map" then
		if f.node.t ~= "arr" then return nil end
		local e = f.node.elems[st.i + 1]
		if e == nil or e.kind ~= "body" then return nil end
		return save_locate_body(e.body, S.lookup(fd.t), steps, pos + 1)
	end
	return nil
end

save_locate_element = function(fd: any, e: any, steps: { any }, pos: number): any
	local el = fd.e
	if el == nil then return nil end
	if e.kind == "body" and el.s == "tab" then
		return save_locate_body(e.body, S.lookup(el.t), steps, pos)
	elseif e.kind == "union" and el.s == "union" then
		return save_locate_union(el, e.node, steps, pos)
	end
	return nil
end

save_locate_union = function(fd: any, node: any, steps: { any }, pos: number): any
	if pos > #steps then return nil end
	if node == nil or node.t ~= "union" then return nil end
	local st = steps[pos]
	-- fd is the FIELD line holding the union; its arms are the union
	-- descriptor's, named by fd.t, exactly as the capturing walk reads them
	local un = S.lookup(fd.t)
	if un == nil then return nil end
	local arm = nil
	for _, a in un.arms do
		if (a.tag or 0) == st.i then arm = a; break end
	end
	if arm == nil then return nil end
	if arm.s == "tab" then
		if node.arm.kind ~= "body" then return nil end
		return save_locate_body(node.arm.body, S.lookup(arm.t), steps, pos + 1)
	elseif arm.s == "arr" then
		if node.arm.kind ~= "arr" then return nil end
		return save_locate_array(arm, node.arm.node, steps, pos + 1)
	elseif arm.s == "map" then
		if node.arm.kind ~= "arr" then return nil end
		local e = node.arm.node.elems[st.i + 1]
		if e == nil or e.kind ~= "body" then return nil end
		return save_locate_body(e.body, S.lookup(arm.t), steps, pos + 1)
	elseif arm.s == "union" then
		if node.arm.kind ~= "union" then return nil end
		return save_locate_union(arm, node.arm.node, steps, pos + 1)
	end
	return nil
end

save_locate_array = function(fd: any, node: any, steps: { any }, pos: number): any
	if pos > #steps then return nil end
	local st = steps[pos]
	local e = node.elems[st.i + 1]
	if e == nil then return nil end
	return save_locate_element(fd, e, steps, pos + 1)
end

local function save_crc32c(s: string): number
	local crc = 0xFFFFFFFF
	for i = 1, #s do
		crc = bit32.bxor(crc, string.byte(s, i))
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

local function save_u32_bytes(v: number): string
	return string.char(v % 256, math.floor(v / 256) % 256, math.floor(v / 65536) % 256, math.floor(v / 16777216) % 256)
end

local function save_u64_bytes(v: integer): string
	local b = buffer.create(8)
	buffer.writeinteger(b, 0, v)
	return buffer.readstring(b, 0, 8)
end

-- SaveRetain is the ordinary save with the kept records written back at the
-- end of their own bodies (docs/SPEC-TABLES.md §6.6). It returns the wire
-- buffer, or nil and the save's refusal. `report` is optional; when it is
-- given, one retain_lost is counted for every record the path no longer names.
function R.SaveRetain(root: string, value: any, retained: any, report: any?): (buffer?, any?)
	local desc = S.lookup(root)
	local plain, refused = Save.save(desc, value)
	if plain == nil then return nil, refused end
	if retained == nil or retained.used == 0 then
		return plain, nil
	end
	if report ~= nil and report.retainLost == nil then report.retainLost = 0 end

	local wire = buffer.readstring(plain, 0, buffer.len(plain))
	local trailer = 0
	if desc.integrity ~= nil and desc.integrity ~= "" then trailer = 4 end
	local tail = #wire - trailer
	local count = 0
	if tail >= 8 then
		local b = buffer.fromstring(string.sub(wire, tail - 7, tail))
		count = integer.tonumber(buffer.readinteger(b, 0))
	end
	local endpos = tail - 8 - count * 8
	local ids = {}
	local b = buffer.fromstring(wire)
	for i = 0, count - 1 do
		ids[i + 1] = buffer.readinteger(b, endpos + i * 8)
	end
	_ids = ids

	local rootReader = save_reader(string.sub(wire, 2, endpos))
	local rootBody = save_parse_body(rootReader, ids)

	local place = {}
	if desc.place ~= nil then
		for _, name in desc.place.tables do
			local d = S.lookup(name)
			place[d.id] = d
		end
	end

	local rootAt = #rootBody.fields + 1
	for i, f in rootBody.fields do
		if f.rec == nil and ids[f.ref] == NodeTableId then rootAt = i; break end
	end

	local lost = 0
	local at = 0
	local store = retained
	while at < store.used do
		local total = buffer.readu32(store.bytes, at)
		local node = buffer.readu32(store.bytes, at + 4)
		local depth = buffer.readu32(store.bytes, at + 8)
		local plen = buffer.readu32(store.bytes, at + 12)
		local id = buffer.readinteger(store.bytes, at + 16)
		local kind = buffer.readu8(store.bytes, at + 24)
		local steps = {}
		for i = 0, depth - 1 do
			steps[i + 1] = {
				o = buffer.readu32(store.bytes, at + 26 + i * 8),
				i = buffer.readu32(store.bytes, at + 30 + i * 8),
			}
		end
		local payload = buffer.readstring(store.bytes, at + 26 + depth * 8, plen)
		local rec = { id = id, kind = kind, payload = payload }
		local target: any = nil
		local tdesc: any = nil
		if node == 1 then
			target, tdesc = rootBody, desc
		elseif node <= #rootBody.fields + 1 then
			for _, f in rootBody.fields do
				if f.node ~= nil and f.node.t == "nodes" and node - 1 <= #f.node.nodes then
					local nr = f.node.nodes[node - 1]
					target = nr.body
					tdesc = place[ids[nr.tref]]
					break
				end
			end
		end
		local ok = target ~= nil and tdesc ~= nil
		local dest: any = nil
		if ok then
			dest = save_locate_body(target, tdesc, steps, 1)
			ok = dest ~= nil
		end
			if ok then
			local check = { b = buffer.fromstring(payload), at = 0, e = #payload, bad = false }
			local dummy: any = { refmap = {}, out_ids = {}, ids = ids, bad = false }
			save_res_payload(dummy, check, kind)
			if check.bad or dummy.bad then ok = false end
		end
		if ok then
			if dest == rootBody then
				table.insert(rootBody.fields, rootAt, { rec = rec })
				rootAt = rootAt + 1
			else
				dest.fields[#dest.fields + 1] = { rec = rec }
			end
		else
			lost = lost + 1
		end
		at = at + total
	end

	local cx: any = { refmap = {}, out_ids = {}, ids = ids, bad = false, idCap = store.idCap, unknownUsed = 0, inRecord = false, recordLost = false, dropped = 0 }
	local rootBytes = save_emit_body(cx, rootBody)
	if cx.bad then return nil end
	lost = lost + cx.dropped
	local parts = { string.char(1), rootBytes }
	for _, id in cx.out_ids do
		parts[#parts + 1] = save_u64_bytes(id)
	end
	parts[#parts + 1] = save_u64_bytes(integer.create(#cx.out_ids))
	local out = save_concat(parts)
	if trailer > 0 then
		out = out .. save_u32_bytes(save_crc32c(out))
	end
	if report ~= nil then report.retainLost = report.retainLost + lost end
	return buffer.fromstring(out), nil
end


return table.freeze(R)
