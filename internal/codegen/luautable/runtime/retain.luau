--!strict
-- retain.luau — the Luau table runtime's RETAIN-UNKNOWN opt-in on the load
-- (docs/SPEC-TABLES.md §6.6). LoadRetain answers the value the ordinary
-- variable-form load answers, and beside it the retained set: every unknown
-- field this build cannot name, with its id, its kind, its body path and its
-- payload with every reference resolved.
--
-- THE LOAD IS REUSED, NOT COPIED: the value comes from load.luau's own walk
-- (require("./load")), which is what keeps this opt-in additive. The capturing
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
local Load = require("./load")

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

local function o_u32(o: any, v: number)
	o_byte(o, v % 256)
	o_byte(o, math.floor(v / 256) % 256)
	o_byte(o, math.floor(v / 65536) % 256)
	o_byte(o, math.floor(v / 16777216) % 256)
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
		if not resolve_payload(r, ids, kind, out, depth + 1) then return false end
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
		o_u32(out, 0)
		if not resolve_body(r, ids, out, depth + 1, r.at + n) then return false end
		r.at = r.at + n
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
			local mark, startLen = #out.parts + 1, out.len
			o_u32(out, 0)
			if not resolve_content(r, ids, ek, out, depth + 1, r.at + elen) then return false end
			if not patch_len(out, mark, startLen) then return false end
			r.at = r.at + elen
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
			local mark, startLen = #out.parts + 1, out.len
			o_u32(out, 0)
			if not resolve_content(r, ids, ek, out, depth + 1, r.at + elen) then return false end
			if not patch_len(out, mark, startLen) then return false end
			r.at = r.at + elen
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
			local mark, startLen = #out.parts + 1, out.len
			o_u32(out, 0)
			if not resolve_content(r, ids, ek, out, depth + 1, r.at + elen) then return false end
			if not patch_len(out, mark, startLen) then return false end
			r.at = r.at + elen
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

local function capture(store: any, report: any, ids: { [number]: integer }, node: number, steps: { any }, id: integer, kind: number, r: any)
	local start = r.at
	if not skip(r, kind) then return end
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
			capture(store, report, _ids, node, steps, id, kind, r)
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

-- LoadRetain is the ordinary load's answer beside §6.6's retained set. The
-- value comes from load.luau's own walk; the capturing walk adds nothing to
-- the plain read's verdict and never raises `malformed`. `capacity` is the
-- byte ceiling the caller owns: the retained store never grows past it and a
-- whole record that does not fit counts one `retain_lost` (§6.6).
function R.LoadRetain(root: string, bytes: buffer, report: any, capacity: number?)
	if report.retained == nil then report.retained = 0 end
	if report.retainLost == nil then report.retainLost = 0 end
	local cap: number = capacity or 65536
	if cap < 0 then cap = 0 end
	local new_store = function()
		return { bytes = buffer.create(cap), cap = cap, used = 0, count = 0 }
	end
	local v = Load.load(root, bytes, report)
	if v == nil then
		return v, new_store()
	end
	local store = new_store()
	local len = buffer.len(bytes)
	local count = 0
	if len >= 9 then
		count = integer.tonumber(buffer.readinteger(bytes, len - 8))
	end
	local endpos = len - 8 - count * 8
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
	return v, store
end

return table.freeze(R)
