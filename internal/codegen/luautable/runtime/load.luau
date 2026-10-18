--!strict
-- load.luau — the Luau table runtime's VARIABLE FORM (form 1) tolerant load
-- (docs/SPEC-TABLES.md §3, §3.1, §4; docs/PORT-LUAU.md, THE CARD MAP). A port
-- of the JavaScript leg's TableVariableLoad walk
-- (internal/codegen/jstable/variableruntime.go) and of the Go reference engine
-- (internal/tablewire decode.go, decodenodes.go). Emitted beside the unit's
-- <Base>Table.luau modules by internal/codegen/luautable, which embeds
-- runtime/*.luau by glob.
--
-- The load is DRIVEN BY THE DESCRIPTOR (docs/SPEC-TABLES.md §8.1): one entry
-- per field naming its wire id (§5), its wire kind (§3), its shape and the
-- facts the shape needs. Every rule lives once, here.
--
-- THE REPORT (docs/SPEC-TABLES.md §4): the six counters and the verdict. The
-- caller owns it; the codec never makes one.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")

local L = {}

-- THE RESERVED IDS (docs/SPEC-TABLES.md §3.1, §5). They are integers, the
-- same width the id table's entries are read at.
local NodeTableId: integer = -1i                  -- 0xffffffffffffffff
local BuildVersionId: integer = -2i               -- 0xfffffffffffffffe
local MessageVocabularyId: integer = -3i          -- 0xfffffffffffffffd
local StringBlobId: integer = 0x704be0d8faaffc58i -- FNV-1a 64 of "string"
local BytesBlobId: integer = 0x2f2ec0474f1c4fe4i  -- FNV-1a 64 of "bytes"
local MapKeyId: integer = 0x3dc94a19365b10eci     -- FNV-1a 64 of "key"

local function new_reader(bytes: buffer, at: number, e: number)
	return { b = bytes, at = at, e = e, bad = false }
end

-- a FAILED LEB LEAVES THE CURSOR WHERE IT STARTED (docs/SPEC-TABLES.md §3)
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

local function r_i8(r: any): number
	local v = r_u8(r)
	if v >= 0x80 then return v - 0x100 end
	return v
end

local function r_u16(r: any): number
	if r.at + 2 > r.e then r.bad = true; return 0 end
	local v = buffer.readu16(r.b, r.at)
	r.at = r.at + 2
	return v
end

local function r_i16(r: any): number
	local v = r_u16(r)
	if v >= 0x8000 then return v - 0x10000 end
	return v
end

local function r_u32(r: any): number
	if r.at + 4 > r.e then r.bad = true; return 0 end
	local v = buffer.readu32(r.b, r.at)
	r.at = r.at + 4
	return v
end

local function r_i32(r: any): number
	local v = r_u32(r)
	if v >= 0x80000000 then return v - 0x100000000 end
	return v
end

-- a 64-bit value is Luau's native integer, the value's own bit pattern
local function r_i64(r: any): integer
	if r.at + 8 > r.e then r.bad = true; return 0i end
	local v = buffer.readinteger(r.b, r.at)
	r.at = r.at + 8
	return v
end

local function r_u64(r: any): integer
	return r_i64(r)
end

local function r_f64(r: any): number
	if r.at + 8 > r.e then r.bad = true; return 0.0 end
	local v = buffer.readf64(r.b, r.at)
	r.at = r.at + 8
	return v
end

-- a float32 rides by its bits and never through a conversion (§4.3, M21)
local function r_f32bits(r: any): number
	if r.at + 4 > r.e then r.bad = true; return 0.0 end
	local bits = buffer.readu32(r.b, r.at)
	r.at = r.at + 4
	local f = buffer.create(4)
	buffer.writeu32(f, 0, bits)
	return buffer.readf32(f, 0)
end

-- a 128-bit value is its two 64-bit halves, the low one first (§3)
local function r_int128(r: any): any
	local lo = r_i64(r)
	local hi = r_i64(r)
	return { lo = lo, hi = hi }
end

-- skipping a field the reader cannot name (docs/SPEC-TABLES.md §3)
local function skip(r: any, kind: number): boolean
	if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then
		if r.at + 1 > r.e then return false end; r.at = r.at + 1; return true
	elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then
		if r.at + 2 > r.e then return false end; r.at = r.at + 2; return true
	elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then
		if r.at + 4 > r.e then return false end; r.at = r.at + 4; return true
	elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then
		if r.at + 8 > r.e then return false end; r.at = r.at + 8; return true
	elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then
		if r.at + 16 > r.e then return false end; r.at = r.at + 16; return true
	elseif kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
		local n = r_leb(r); if r.bad then return false end
		if r.at + n > r.e then return false end; r.at = r.at + n; return true
	elseif kind == 15 then
		local arm = r_leb(r); if r.bad then return false end
		if arm ~= 0 then
			if r.at + 1 > r.e then return false end
			r_u8(r)
			local n = r_leb(r); if r.bad then return false end
			if r.at + n > r.e then return false end; r.at = r.at + n
		end
		return true
	elseif kind == 17 or kind == 30 then
		r_leb(r); return not r.bad
	end
	return false
end

-- THE WIDENING RUNGS (§4): a narrower kind on its own signedness ladder, and
-- f32 into f64.
local function widens(kind: number, declared: number): boolean
	if declared == 3 or declared == 4 or declared == 5 then return kind >= 2 and kind < declared end
	if declared == 7 or declared == 8 or declared == 9 then return kind >= 6 and kind < declared end
	if declared == 11 then return kind == 10 end
	return false
end

local function width_of(kind: number): number
	if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then return 1
	elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then return 2
	elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then return 4
	elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then return 8
	elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then return 16
	end
	return 0
end

-- UTF-8, byte-exact and validating (docs/SPEC-TABLES.md §3): a zero byte, a
-- truncated sequence, an overlong form, a surrogate or a value past U+10FFFF
-- is not text.
local function utf8_ok(b: buffer, at: number, n: number): boolean
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

-- THE ID TABLE AND THE ROOT BODY'S FRAMING (docs/SPEC-TABLES.md §3): a form
-- byte this reader does not know REFUSES; damage is malformed.
local function open(bytes: buffer, report: any): any
	local len = buffer.len(bytes)
	if len < 1 then report.malformed = true; return nil end
	if buffer.readu8(bytes, 0) ~= 1 then report.refused = report.refused + 1; return nil end
	if len < 9 then report.malformed = true; return nil end
	local count = integer.tonumber(buffer.readinteger(bytes, len - 8))
	if count > math.floor((len - 9) / 8) then report.malformed = true; return nil end
	local endpos = len - 8 - count * 8
	local ids = {}
	local seen: { [integer]: boolean } = {}
	for i = 0, count - 1 do
		local id = buffer.readinteger(bytes, endpos + i * 8)
		if seen[id] then report.malformed = true; return nil end
		seen[id] = true
		ids[i + 1] = id
	end
	return { table = ids, e = endpos }
end

-- the root body's reserved node-table field (docs/SPEC-TABLES.md §3.1); the
-- numbering is whole or it is nothing
local function node_table_scan(bytes: buffer, ids: { any }, endpos: number, report: any): any
	local w = new_reader(bytes, 1, endpos)
	local payload: buffer? = nil
	local present = false
	local bad = false
	while not bad do
		local ref = r_leb(w)
		if w.bad then break end
		if ref == 0 then break end
		if ref > #ids then break end
		if w.at >= w.e then break end
		local id = ids[ref]
		local kind = r_u8(w)
		if id == NodeTableId then
			present = true
			if kind ~= 12 then bad = true; break end
			local n = r_leb(w)
			if w.bad or w.at + n > w.e then bad = true; break end
			local p = buffer.create(n)
			buffer.copy(p, 0, bytes, w.at, n)
			payload = p
			w.at = w.at + n
		else
			if not skip(w, kind) then break end
		end
	end
	if present and bad then report.malformed = true; return nil end
	if not present then return {} end
	local rec = new_reader(payload :: buffer, 0, buffer.len(payload :: buffer))
	local count = r_leb(rec)
	if rec.bad then report.malformed = true; return nil end
	local raw = {}
	while rec.at < rec.e do
		local tref = r_leb(rec)
		if rec.bad or tref == 0 or tref > #ids then report.malformed = true; return nil end
		local tlen = r_leb(rec)
		if rec.bad or rec.at + tlen > rec.e then report.malformed = true; return nil end
		local body = buffer.create(tlen)
		buffer.copy(body, 0, payload :: buffer, rec.at, tlen)
		raw[#raw + 1] = { tid = ids[tref], body = body }
		rec.at = rec.at + tlen
	end
	if count ~= #raw then report.malformed = true; return nil end
	return raw
end

-- THE LINK: each descriptor learns its fields by id, once (docs/SPEC-TABLES.md
-- §3, §8.1). A field's t names a descriptor through the registry.
local byIdCache: { [any]: { [integer]: any } } = {}
local typeCache: { [any]: { [integer]: any } } = {}

local function fields_by_id(desc: any): { [integer]: any }
	local m = byIdCache[desc]
	if m == nil then
		m = {}
		for _, f in desc.fields do m[f.id] = f end
		byIdCache[desc] = m
	end
	return m
end

local function place_types(desc: any): { [integer]: any }
	local m = typeCache[desc]
	if m == nil then
		m = {}
		if desc.place ~= nil then
			for _, name in desc.place.tables do
				local d = S.lookup(name)
				m[d.id] = d
			end
		end
		typeCache[desc] = m
	end
	return m
end

local function is128(x: any): boolean
	return typeof(x) == "table" and x.lo ~= nil and x.hi ~= nil
end

-- an Int128 compares as a signed two's-complement value: the high half first
local function cmp_lt(a: any, b: any): boolean
	if is128(a) and is128(b) then
		if a.hi ~= b.hi then return integer.lt(a.hi, b.hi) end
		return integer.lt(a.lo, b.lo)
	end
	if typeof(a) == "integer" and typeof(b) == "integer" then return integer.lt(a, b) end
	return a < b
end

local function ucmp_lt(a: any, b: any): boolean
	if is128(a) and is128(b) then
		if a.hi ~= b.hi then return integer.ult(a.hi, b.hi) end
		return integer.ult(a.lo, b.lo)
	end
	if typeof(a) == "integer" and typeof(b) == "integer" then return integer.ult(a, b) end
	return a < b
end

local function ucmp_gt(a: any, b: any): boolean
	if is128(a) and is128(b) then
		if a.hi ~= b.hi then return integer.ult(b.hi, a.hi) end
		return integer.ult(b.lo, a.lo)
	end
	if typeof(a) == "integer" and typeof(b) == "integer" then return integer.ult(b, a) end
	return a > b
end

local function cmp_gt(a: any, b: any): boolean
	if is128(a) and is128(b) then
		if a.hi ~= b.hi then return integer.lt(b.hi, a.hi) end
		return integer.lt(b.lo, a.lo)
	end
	if typeof(a) == "integer" and typeof(b) == "integer" then return integer.lt(b, a) end
	return a > b
end

-- a fixed field's declared bounds are WHOLE units; the wire carries the RAW
-- scaled integer (docs/SPEC-TABLES.md §3, §4), so a bound shifts by F.
local function i128_shl(v: any, sh: number): any
	if sh == 0 then return v end
	if sh >= 64 then return { lo = 0i, hi = integer.lshift(v.lo, integer.create(sh - 64)) } end
	local hi = integer.lshift(v.hi, integer.create(sh))
	if sh > 0 then hi = integer.bor(hi, integer.rshift(v.lo, integer.create(64 - sh))) end
	return { lo = integer.lshift(v.lo, integer.create(sh)), hi = hi }
end

local function bound_shift(v: any, fbits: number): any
	if v == nil then return nil end
	if is128(v) then return i128_shl(v, fbits) end
	if typeof(v) == "integer" then return integer.lshift(v, integer.create(fbits)) end
	return v * (2 ^ fbits)
end

local function scalar_at(r: any, fd: any, kind: number, report: any): any
	local width = width_of(kind)
	if width == 0 then report.malformed = true; return nil end
	if r.at + width > r.e then report.malformed = true; return nil end
	local x: any
	if kind == 1 then x = r_u8(r) ~= 0
	elseif kind == 10 then x = r_f32bits(r)
	elseif kind == 11 then x = r_f64(r)
	elseif kind == 2 then x = r_i8(r)
	elseif kind == 3 then x = r_i16(r)
	elseif kind == 4 then x = r_i32(r)
	elseif kind == 5 then x = r_i64(r)
	elseif kind == 6 then x = r_u8(r)
	elseif kind == 7 then x = r_u16(r)
	elseif kind == 8 then x = r_u32(r)
	elseif kind == 9 then x = r_u64(r)
	elseif kind == 36 then x = r_i64(r)
	elseif kind == 18 or kind == 19 then x = r_int128(r)
	elseif kind >= 20 and kind <= 29 then
		local signed = kind < 25
		if kind == 20 or kind == 25 then
			x = signed and r_i8(r) or r_u8(r)
		elseif kind == 21 or kind == 26 then
			x = signed and r_i16(r) or r_u16(r)
		elseif kind == 22 or kind == 27 then
			x = signed and r_i32(r) or r_u32(r)
		elseif kind == 23 or kind == 28 then
			x = r_i64(r)
		else
			x = r_int128(r)
		end
	else report.malformed = true; return nil end
	-- §4: a narrower value widens into this declaration's domain
	if fd.dom == "integer" and typeof(x) == "number" then x = integer.create(x) end
	local lo, hi = fd.lo, fd.hi
	if lo ~= nil and fd.fbits ~= nil and fd.fbits > 0 then
		lo, hi = bound_shift(lo, fd.fbits), bound_shift(hi, fd.fbits)
	end
	if lo ~= nil then
		local lt, gt = cmp_lt, cmp_gt
		if fd.signed == false then lt, gt = ucmp_lt, ucmp_gt end
		if lt(x, lo) then x = lo; report.clamped = report.clamped + 1
		elseif gt(x, hi) then x = hi; report.clamped = report.clamped + 1 end
	end
	if fd.bits ~= nil and fd.bits > 0 and fd.bits < width * 8 then
		local bmax: any = 2 ^ fd.bits - 1
		if typeof(x) == "integer" then bmax = integer.create(bmax) end
		if cmp_gt(x, bmax) then x = bmax; report.clamped = report.clamped + 1 end
	end
	return x
end

local _table: { any } = {}
local _pending: { any } = {}
local _nodes: { any } = {}
local _rootValue: any = nil
local _rootDesc: any = nil
local _report: any = nil

local read_body: any
local read_field: any
local read_array: any
local read_element: any
local read_union: any
local read_map: any

-- an enum reference (§3): 0 is None, a reference past the id table is damage,
-- a variant this enum cannot name reads None and counts unknown
local function enum_read(fd: any, r: any, report: any)
	local ref = r_leb(r)
	if r.bad then report.malformed = true; return nil, false end
	if ref == 0 then return nil, true end
	if ref > #_table then report.malformed = true; return nil, false end
	local id = _table[ref]
	for _, v in fd.vs do
		if v[2] == id then return v[1], true end
	end
	report.unknown = report.unknown + 1
	return nil, true
end

-- a MAP rides as an array of tables (§2.8): the key before the slot, a
-- repeated key last-wins and counted duplicate, a key kind the declaration
-- does not name emptying the map for one kind_mismatch.
local function key_token(v: any, fd: any): any
	if v == nil then return "null" end
	if typeof(v) == "integer" then return "i" .. tostring(v) end
	if typeof(v) == "string" then return "s" .. v end
	if typeof(v) == "table" and v.lo ~= nil and v.hi ~= nil then
		return "w" .. tostring(v.hi) .. ":" .. tostring(v.lo)
	end
	return "n" .. tostring(v)
end

read_map = function(fd: any, r: any, report: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return false end
	local endpos = r.at + n
	if n >= 2 then
		local sub = new_reader(r.b, r.at, endpos)
		local ek = r_u8(sub)
		local count = r_leb(sub)
		if sub.bad then report.malformed = true; r.at = endpos; return true end
		if ek ~= 13 then report.kindMismatch = report.kindMismatch + 1; r.at = endpos; return true end
		local entry = S.lookup(fd.t)
		local keyfd = fields_by_id(entry)[MapKeyId]
		local seen: { [any]: boolean } = {}
		local landed: { any } = {}
		local widened = false
		for _ = 1, count do
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then report.malformed = true; break end
			local body = new_reader(sub.b, sub.at, sub.at + elemLen)
			sub.at = sub.at + elemLen
			local vals: any = {}
			local keyVal: any = nil
			local keySeen = false
			local kindBad = false
			while true do
				local ref = r_leb(body)
				if body.bad then report.malformed = true; break end
				if ref == 0 then break end
				if ref > #_table then report.malformed = true; break end
				local id = _table[ref]
				local kind = r_u8(body)
				if id == MapKeyId and keyfd ~= nil then
					if kind ~= keyfd.k then
						if widens(kind, keyfd.k) then
							local x = scalar_at(body, keyfd, kind, report)
							if x == nil then break end
							keyVal = x
							if not widened then widened = true; report.widened = report.widened + 1 end
						else
							kindBad = true
							if not skip(body, kind) then report.malformed = true; break end
						end
					else
						local ok = read_field(keyfd, body, vals)
						if not ok then break end
						keyVal = vals[keyfd.slot]
					end
					keySeen = true
				elseif id == MapKeyId then
					if not skip(body, kind) then report.malformed = true; break end
				else
					local efd = fields_by_id(entry)[id]
					if efd == nil then
						report.unknown = report.unknown + 1
						if not skip(body, kind) then report.malformed = true; break end
					elseif kind ~= efd.k then
						if widens(kind, efd.k) then
							local x = scalar_at(body, efd, kind, report)
							if x == nil then break end
							report.widened = report.widened + 1
						else
							report.kindMismatch = report.kindMismatch + 1
							if not skip(body, kind) then report.malformed = true; break end
						end
					else
						if not read_field(efd, body, vals) then break end
					end
				end
			end
			if kindBad then
				report.kindMismatch = report.kindMismatch + 1
				break
			end
			if keySeen then
				local tok = key_token(keyVal, keyfd)
				if seen[tok] then
					report.duplicate = report.duplicate + 1
					landed[#landed] = keyVal
				else
					seen[tok] = true
					landed[#landed + 1] = keyVal
				end
			end
		end
	end
	r.at = endpos
	return true
end

-- AN ENUM-KEYED ARRAY (§2.4, §3.2): its own kind 16, each element a key
-- reference and then its framed payload. A key this reader cannot name is
-- skipped; an element kind the declaration does not name is one kind_mismatch.
local function read_keyed(fd: any, r: any, report: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return false end
	local endpos = r.at + n
	if n >= 2 then
		local sub = new_reader(r.b, r.at, endpos)
		local ek = r_u8(sub)
		local count = r_leb(sub)
		if sub.bad then report.malformed = true; r.at = endpos; return true end
		if ek ~= fd.ek then
			if not widens(ek, fd.ek) then report.kindMismatch = report.kindMismatch + 1; r.at = endpos; return true end
			report.widened = report.widened + 1
		end
		local e = fd.e
		for _ = 1, count do
			local key = r_leb(sub)
			if sub.bad then report.malformed = true; break end
			if key == 0 or key > #_table then report.malformed = true; break end
			if fd.keyvs ~= nil then
				local keyID = _table[key]
				local named = false
				for _, v in fd.keyvs do
					if v[2] == keyID then named = true; break end
				end
				if not named then report.unknown = report.unknown + 1 end
			end
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then report.malformed = true; break end
			local payload = new_reader(sub.b, sub.at, sub.at + elemLen)
			sub.at = sub.at + elemLen
			if e ~= nil and e.s == "tab" then
				local d = S.lookup(e.t)
				local inst = d.new()
				read_body(d, payload, inst, true)
				if payload.at ~= payload.e then report.malformed = true end
			elseif e ~= nil and e.s == "enum" then
				enum_read(e, payload, report)
			else
				scalar_at(payload, e or {}, ek, report)
			end
		end
	end
	r.at = endpos
	return true
end

-- AN ARRAY BODY (§2.9, §3): a body shorter than its header is inert, a
-- damaged count is malformed, a foreign element kind is one kind_mismatch, a
-- bounded array's excess is clamped, and elements are bounded by the body.
read_array = function(fd: any, r: any, report: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return false end
	local endpos = r.at + n
	if n >= 2 then
		local sub = new_reader(r.b, r.at, endpos)
		local ek = r_u8(sub)
		local count = r_leb(sub)
		if sub.bad then report.malformed = true; r.at = endpos; return true end
		if ek ~= fd.ek then
			if not widens(ek, fd.ek) then report.kindMismatch = report.kindMismatch + 1; r.at = endpos; return true end
			report.widened = report.widened + 1
		end
		local keep = count
		if fd.array == "list" then
			if count > 0x7FFFFFFF then report.malformed = true; r.at = endpos; return true end
		elseif count > fd.bound then
			keep = fd.bound
			report.clamped = report.clamped + 1
		end
		local out = {}
		for i = 1, keep do
			if sub.at >= sub.e then report.malformed = true; break end
			if fd.s == "bytes" then
				if sub.at + 1 > sub.e then report.malformed = true; break end
				sub.at = sub.at + 1
			elseif not read_element(fd, sub, fd.e, out, report, ek) then break end
		end
		if fd.array == "counted" then out.bound = fd.bound end
	end
	r.at = endpos
	return true
end

read_element = function(fd: any, r: any, e: any, out: any, report: any, ek: number): boolean
	if e == nil then
		local x = scalar_at(r, {}, ek, report)
		if x == nil then return false end
		out[#out + 1] = x
		return true
	end
	if e.s == "ptr" then
		local index = r_leb(r)
		if r.bad then report.malformed = true; return false end
		out[#out + 1] = nil
		_pending[#_pending + 1] = { holder = nil, key = nil, index = index, d = e }
		return true
	elseif e.s == "enum" then
		local x, ok = enum_read(e, r, report)
		if not ok then return false end
		out[#out + 1] = x
		return true
	elseif e.s == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then report.malformed = true; return false end
		local sub = new_reader(r.b, r.at, r.at + n)
		local inst = S.lookup(e.t).new()
		read_body(S.lookup(e.t), sub, inst, true)
		if sub.at ~= sub.e then report.malformed = true; inst = S.lookup(e.t).new() end
		r.at = r.at + n
		out[#out + 1] = inst
		return true
	elseif e.s == "union" then
		out[#out + 1] = read_union(e, r, report)
		return true
	end
	local x = scalar_at(r, e, ek, report)
	if x == nil then return false end
	out[#out + 1] = x
	return true
end

-- AN ARM HEADER IS A FIELD HEADER (§3, §2.6): the arm id reference, the arm's
-- kind byte, L, then L bytes of payload. An arm this reader cannot name is
-- unknown and the union reads None; a retyped arm is a kind mismatch.
read_union = function(fd: any, r: any, report: any): any
	local un = S.lookup(fd.t)
	local ref = r_leb(r)
	if r.bad then report.malformed = true; return nil end
	if ref == 0 then return nil end
	if ref > #_table then report.malformed = true; return nil end
	local armID = _table[ref]
	local kind = r_u8(r)
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return nil end
	local sub = new_reader(r.b, r.at, r.at + n)
	local arm: any = nil
	for _, a in un.arms do
		if a.id == armID then arm = a; break end
	end
	local want = arm == nil and 0 or (arm.s == "void" and 32 or arm.k)
	if arm == nil then
		report.unknown = report.unknown + 1
	elseif kind ~= want then
		if widens(kind, want) then
			if n == width_of(kind) then
				if scalar_at(sub, arm, kind, report) ~= nil then report.widened = report.widened + 1 end
			else report.malformed = true end
		else
			report.kindMismatch = report.kindMismatch + 1
		end
	elseif arm.s == "tab" then
		local inst = S.lookup(arm.t).new()
		read_body(S.lookup(arm.t), sub, inst, true)
		if sub.at ~= sub.e then report.malformed = true end
	elseif arm.s == "void" then
		-- a payload-free arm carries L = 0
	else
		read_field(arm, sub, {})
	end
	r.at = r.at + n
	return nil
end

read_field = function(fd: any, r: any, v: any): boolean
	local s = fd.s
	if s == "bool" or s == "int" or s == "float" then
		local x = scalar_at(r, fd, fd.k, _report)
		if x == nil then return false end
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = x end
		return true
	elseif s == "str" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; return false end
		if fd.wide then
			if n % 2 ~= 0 then _report.malformed = true; r.at = r.at + n; return true end
			if fd.bound ~= nil and n / 2 > fd.bound then _report.clamped = _report.clamped + 1 end
			r.at = r.at + n
			return true
		end
		if not utf8_ok(r.b, r.at, n) then
			_report.malformed = true
			r.at = r.at + n
			return true
		end
		if fd.bound ~= nil and n > fd.bound then _report.clamped = _report.clamped + 1 end
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = buffer.readstring(r.b, r.at, n) end
		r.at = r.at + n
		return true
	elseif s == "text" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; return false end
		r.at = r.at + n
		return true
	elseif s == "enum" then
		local x, ok = enum_read(fd, r, _report)
		if not ok then return false end
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = x end
		return true
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		local index = r_leb(r)
		if r.bad then _report.malformed = true; return false end
		_pending[#_pending + 1] = { holder = v, key = fd.slot, index = index, d = fd }
		return true
	elseif s == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; return false end
		local sub = new_reader(r.b, r.at, r.at + n)
		local d = S.lookup(fd.t)
		local inst = d.new()
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = inst end
		read_body(d, sub, inst, true)
		if sub.at ~= sub.e then _report.malformed = true; inst = d.new() end
		r.at = r.at + n
		return true
	elseif s == "union" then
		local x = read_union(fd, r, _report)
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = x end
		return true
	elseif s == "bytes" or s == "arr" then
		if s == "arr" and fd.array == "keyed" then
			return read_keyed(fd, r, _report)
		end
		return read_array(fd, r, _report)
	elseif s == "map" then
		return read_map(fd, r, _report)
	end
	_report.malformed = true
	return false
end


-- RESOLUTION (§3.1): 0 is null, 1 is the root, k + 1 is record k. A record of
-- another kind than the slot declares is a kind mismatch, an index past the
-- numbering is malformed, and a numbering that could not be read resolves
-- nothing.
local function resolve(report: any)
	for _, p in _pending do
		local value: any = nil
		if p.index ~= 0 then
			if p.index == 1 then
				if p.d.s == "ptr" and p.d.t == _rootDesc.name then value = _rootValue
				else report.kindMismatch = report.kindMismatch + 1 end
			elseif p.index - 2 >= #_nodes then
				report.malformed = true
			else
				local n = _nodes[p.index - 1]
				if n ~= nil then
					if p.d.s == "sblob" then
						if n.kind == "s" then value = n.blob else report.kindMismatch = report.kindMismatch + 1 end
					elseif p.d.s == "bblob" then
						if n.kind == "b" then value = n.blob else report.kindMismatch = report.kindMismatch + 1 end
					elseif n.kind == "t" and n.desc.name == p.d.t then
						value = n.v
					else
						report.kindMismatch = report.kindMismatch + 1
					end
				end
			end
		end
		if p.holder ~= nil and p.key ~= nil then p.holder[p.key] = value end
	end
end

read_body = function(desc: any, r: any, v: any, nested: boolean): boolean
	local byId = fields_by_id(desc)
	for _ = 1, math.huge do
		local ref = r_leb(r)
		if r.bad then _report.malformed = true; return false end
		if ref == 0 then return true end
		if ref > #_table then _report.malformed = true; return false end
		if r.at >= r.e then _report.malformed = true; return false end
		local kind = r_u8(r)
		local id = _table[ref]
		if id == BuildVersionId or id == MessageVocabularyId then _report.malformed = true; return false end
		if id == NodeTableId then
			if nested then _report.malformed = true; return false end
			if desc.variable then
				if not skip(r, kind) then _report.malformed = true; return false end
				continue
			end
		end
		local fd = byId[id]
		if fd == nil then
			_report.unknown = _report.unknown + 1
			if not skip(r, kind) then _report.malformed = true; return false end
		elseif kind ~= fd.k then
			if widens(kind, fd.k) then
				local x = scalar_at(r, fd, kind, _report)
				if x == nil then return false end
				if fd.slot ~= nil and v ~= nil then v[fd.slot] = x end
				_report.widened = _report.widened + 1
			else
				_report.kindMismatch = _report.kindMismatch + 1
				if not skip(r, kind) then _report.malformed = true; return false end
			end
		else
			if not read_field(fd, r, v) then return false end
		end
	end
	return false
end

-- Load loads a form-1 wire at the table descriptor `root`, fills report, and
-- returns the root value (or nil when refused). The verdict is the caller's,
-- from report.refused.
function L.load(root: string, bytes: buffer, report: any): any
	local o = open(bytes, report)
	if o == nil then return nil end
	_table = o.table
	_report = report
	_pending = {}
	_nodes = {}
	local desc = S.lookup(root)
	_rootDesc = desc
	local v = desc.new()
	_rootValue = v
	local raw: any = {}
	if desc.variable then raw = node_table_scan(bytes, o.table, o.e, report) end
	if raw ~= nil then
		local place = place_types(desc)
		for _, rec in raw do
			local tid = rec.tid
			if tid == StringBlobId and desc.place.str then
				if not utf8_ok(rec.body, 0, buffer.len(rec.body)) then
					report.malformed = true
					_nodes[#_nodes + 1] = nil
				else
					_nodes[#_nodes + 1] = { kind = "s", blob = rec.body }
				end
			elseif tid == BytesBlobId and desc.place.bytes then
				_nodes[#_nodes + 1] = { kind = "b", blob = rec.body }
			else
				local nc = place[tid]
				if nc == nil then
					report.unknown = report.unknown + 1
					_nodes[#_nodes + 1] = nil
				else
					local inst = nc.new()
					_nodes[#_nodes + 1] = { kind = "t", desc = nc, v = inst, body = rec.body }
				end
			end
		end
		for _, n in _nodes do
			if n ~= nil and n.kind == "t" then
				local br = new_reader(n.body, 0, buffer.len(n.body))
				read_body(n.desc, br, n.v, true)
			end
		end
	end
	local rootReader = new_reader(bytes, 1, o.e)
	read_body(desc, rootReader, v, false)
	resolve(report)
	return v
end

return table.freeze(L)
