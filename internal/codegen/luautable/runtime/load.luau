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

-- an enum reference (§3, §2.15): a retired id reads its field default, not unknown; 0 is None, a reference past the id table is damage,
-- a variant this enum cannot name reads None and counts unknown. The value is
-- the variant's number, None 0 (docs/PORT-LUAU.md, THE TYPE MAPPING).
local function enum_read(fd: any, r: any, report: any): (any, boolean)
	local ref = r_leb(r)
	if r.bad then report.malformed = true; return nil, false end
	if ref == 0 then return 0, true end
	if ref > #_table then report.malformed = true; return nil, false end
	local id = _table[ref]
	for i, v in fd.vs do
		if v[2] == id then
			if v[3] == true then return fd.def or 0, true end
			return i, true
		end
	end
	report.unknown = report.unknown + 1
	return 0, true
end

-- a MAP rides as an array of tables (§2.8): the key before the slot, a
-- repeated key last-wins and counted duplicate, a key kind the declaration
-- does not name emptying the map for one kind_mismatch. The entries land in
-- the holder's slot as the {Entry} array the type mapping names
-- (docs/PORT-LUAU.md, THE TYPE MAPPING).
local function key_token(v: any): any
	if v == nil then return "null" end
	if typeof(v) == "integer" then return "i" .. tostring(v) end
	if typeof(v) == "string" then return "s" .. v end
	if typeof(v) == "table" and v.lo ~= nil and v.hi ~= nil then
		return "w" .. tostring(v.hi) .. ":" .. tostring(v.lo)
	end
	return "n" .. tostring(v)
end

-- the identity an entry landed under: a string key is its used bytes (§2.8)
local function entry_key(keyfd: any, inst: any): any
	local k = inst[keyfd.slot]
	if typeof(k) == "buffer" then
		local n = inst[keyfd.slot .. "Length"]
		if n == nil then n = buffer.len(k) end
		return "s" .. buffer.readstring(k, 0, n)
	end
	return key_token(k)
end

-- A CLAMP KEEPS WHOLE CODE POINTS (docs/SPEC-TABLES.md §3, KIND 12 / 33): a
-- string past its bound keeps the last whole UTF-8 code point within it, and
-- a wide string its first N code units less an orphan high surrogate.
local function clamp_utf8(b: buffer, at: number, keep: number): number
	while keep > 0 do
		local x = buffer.readu8(b, at + keep)
		if x < 0x80 or x > 0xBF then break end
		keep = keep - 1
	end
	return keep
end

local function clamp_wide(b: buffer, at: number, units: number): number
	if units > 0 then
		local last = buffer.readu16(b, at + (units - 1) * 2)
		if last >= 0xD800 and last <= 0xDBFF then units = units - 1 end
	end
	return units
end

-- a string(N), wstring(N) or bytes(N) value lands in its buffer with its used
-- length beside it (docs/PORT-LUAU.md, THE TYPE MAPPING; SPEC §4.7)
local function store_bytes(v: any, slot: string, src: buffer, at: number, nbytes: number, length: number)
	local dst = v[slot]
	if typeof(dst) ~= "buffer" or buffer.len(dst) < nbytes then
		dst = buffer.create(nbytes)
		v[slot] = dst
	end
	buffer.fill(dst, 0, 0)
	buffer.copy(dst, 0, src, at, nbytes)
	v[slot .. "Length"] = length
end

-- an unbounded text value or blob is a buffer of exactly its bytes (§2.5, §2.13.1)
local function copy_out(src: buffer, at: number, n: number): buffer
	local b = buffer.create(n)
	buffer.copy(b, 0, src, at, n)
	return b
end

read_map = function(fd: any, r: any, report: any, v: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return false end
	local endpos = r.at + n
	local landed: { any } = {}
	if n >= 2 then
		local sub = new_reader(r.b, r.at, endpos)
		local ek = r_u8(sub)
		local count = r_leb(sub)
		if sub.bad then report.malformed = true; r.at = endpos; return true end
		if ek ~= 13 then report.kindMismatch = report.kindMismatch + 1; r.at = endpos; return true end
		local entry = S.lookup(fd.t)
		local keyfd = fields_by_id(entry)[MapKeyId]
		local seen: { [any]: number } = {}
		local widened = false
		for _ = 1, count do
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then report.malformed = true; break end
			local body = new_reader(sub.b, sub.at, sub.at + elemLen)
			sub.at = sub.at + elemLen
			local vals: any = entry.new()
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
							vals[keyfd.slot] = x
							if not widened then widened = true; report.widened = report.widened + 1 end
						else
							kindBad = true
							if not skip(body, kind) then report.malformed = true; break end
						end
					else
						local ok = read_field(keyfd, body, vals)
						if not ok then break end
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
							if efd.slot ~= nil then vals[efd.slot] = x end
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
				landed = {}
				break
			end
			if keySeen then
				local tok = entry_key(keyfd, vals)
				local at = seen[tok]
				if at ~= nil then
					report.duplicate = report.duplicate + 1
					landed[at] = vals
				else
					landed[#landed + 1] = vals
					seen[tok] = #landed
				end
			end
		end
	end
	if v ~= nil and fd.slot ~= nil then v[fd.slot] = landed end
	r.at = endpos
	return true
end

-- AN ENUM-KEYED ARRAY (§2.4, §3.2): its own kind 16, each element a key
-- reference and then its framed payload. A key this reader cannot name is
-- skipped; an element kind the declaration does not name is one kind_mismatch.
-- Key variant k lands at index k (docs/PORT-LUAU.md, THE TYPE MAPPING).
local function read_keyed(fd: any, r: any, report: any, v: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return false end
	local endpos = r.at + n
	local dst: any = nil
	if v ~= nil and fd.slot ~= nil then dst = v[fd.slot] end
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
			local at = 0
			if fd.keyvs ~= nil then
				local keyID = _table[key]
				for i, kv in fd.keyvs do
					if kv[2] == keyID then at = i; break end
				end
			end
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then report.malformed = true; break end
			-- a key this reader cannot name is skipped by its length (§2.4, §3.2)
			if at == 0 then
				report.unknown = report.unknown + 1
				sub.at = sub.at + elemLen
				continue
			end
			local payload = new_reader(sub.b, sub.at, sub.at + elemLen)
			sub.at = sub.at + elemLen
			local x: any = nil
			if e ~= nil and e.s == "tab" then
				local d = S.lookup(e.t)
				local inst = d.new()
				read_body(d, payload, inst, true)
				if payload.at ~= payload.e then report.malformed = true end
				x = inst
			elseif e ~= nil and e.s == "enum" then
				x = enum_read(e, payload, report)
			else
				x = scalar_at(payload, e or {}, ek, report)
			end
			if at > 0 and dst ~= nil and x ~= nil then dst[at] = x end
		end
	end
	r.at = endpos
	return true
end

local array_body: any

-- AN ARRAY BODY (§2.9, §3): a body shorter than its header is inert, a
-- damaged count is malformed, a foreign element kind is one kind_mismatch, a
-- bounded array's excess is clamped, and elements are bounded by the body.
read_array = function(fd: any, r: any, report: any, v: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return false end
	local endpos = r.at + n
	if n >= 2 then
		array_body(fd, new_reader(r.b, r.at, endpos), report, v)
	end
	r.at = endpos
	return true
end

-- the body after its L: the element kind, the count, the elements. They land
-- in the holder's slot: a list is a fresh array, a fixed or counted array is
-- written in place with its count beside it, [N]uint8 and bytes(N) are buffers
-- (docs/PORT-LUAU.md, THE TYPE MAPPING).
array_body = function(fd: any, sub: any, report: any, v: any)
	local ek = r_u8(sub)
	local count = r_leb(sub)
	if sub.bad then report.malformed = true; return end
	if ek ~= fd.ek then
		if not widens(ek, fd.ek) then report.kindMismatch = report.kindMismatch + 1; return end
		report.widened = report.widened + 1
	end
	local keep = count
	if fd.array == "list" then
		if count > 0x7FFFFFFF then report.malformed = true; return end
	elseif count > fd.bound then
		keep = fd.bound
		report.clamped = report.clamped + 1
	end
	local slot = fd.slot
	local holder: any = nil
	if v ~= nil and slot ~= nil and slot ~= "" then holder = v end
	if fd.s == "bytes" then
		-- bytes(N) rides as an array of u8 (§2.5)
		local have = keep
		if sub.at + have > sub.e then have = sub.e - sub.at; report.malformed = true end
		if holder ~= nil then store_bytes(holder, slot, sub.b, sub.at, have, have) end
		sub.at = sub.at + have
		return
	end
	local dst: any
	if holder == nil then
		dst = {}
	elseif fd.array == "list" then
		dst = {}
		holder[slot] = dst
	else
		dst = holder[slot]
		if dst == nil then dst = {}; holder[slot] = dst end
	end
	local lens: any = nil
	if holder ~= nil then lens = holder[slot .. "Length"] end
	local got = 0
	for i = 1, keep do
		if sub.at >= sub.e then report.malformed = true; break end
		if not read_element(fd, sub, fd.e, dst, i, report, ek, lens) then break end
		got = i
	end
	if fd.array == "counted" and holder ~= nil then holder[slot .. "Count"] = got end
	-- a list of pointers may hold null slots, which a Luau length cannot see:
	-- its live length rides as n, the table.pack convention, and the save reads it
	if fd.array == "list" and fd.e ~= nil and (fd.e.s == "ptr" or fd.e.s == "sblob" or fd.e.s == "bblob") then dst.n = got end
end

-- an element lands at its 1-based index; a [N]uint8 is a 0-based buffer
-- (docs/PORT-LUAU.md, THE TYPE MAPPING)
local function put(dst: any, i: number, x: any)
	if typeof(dst) == "buffer" then
		buffer.writeu8(dst, i - 1, x)
	else
		dst[i] = x
	end
end

read_element = function(fd: any, r: any, e: any, dst: any, i: number, report: any, ek: number, lens: any): boolean
	if e == nil then
		local x = scalar_at(r, {}, ek, report)
		if x == nil then return false end
		put(dst, i, x)
		return true
	end
	if e.s == "ptr" or e.s == "sblob" or e.s == "bblob" then
		local index = r_leb(r)
		if r.bad then report.malformed = true; return false end
		-- a null in a list is false, so the list is never cut short; a fixed or
		-- counted array stays nil-filled (docs/PORT-LUAU.md, THE TYPE MAPPING, Tables)
		local null: any = nil
		if fd.array == "list" then null = false end
		dst[i] = null
		_pending[#_pending + 1] = { holder = dst, key = i, index = index, d = e, null = null }
		return true
	elseif e.s == "enum" then
		local x, ok = enum_read(e, r, report)
		if not ok then return false end
		dst[i] = x
		return true
	elseif e.s == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then report.malformed = true; return false end
		local sub = new_reader(r.b, r.at, r.at + n)
		local inst = S.lookup(e.t).new()
		read_body(S.lookup(e.t), sub, inst, true)
		if sub.at ~= sub.e then report.malformed = true; inst = S.lookup(e.t).new() end
		r.at = r.at + n
		dst[i] = inst
		return true
	elseif e.s == "union" then
		dst[i] = read_union(e, r, report)
		return true
	elseif e.s == "str" or e.s == "text" then
		-- a text element is kind 12: its own L, then L bytes (§2.13, §3)
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then report.malformed = true; return false end
		if e.wide then
			-- a wstring element: L is a byte length, twice the code units (§3)
			if n % 2 ~= 0 then report.malformed = true; r.at = r.at + n; return true end
			local units = n / 2
			if e.bound ~= nil and units > e.bound then units = clamp_wide(r.b, r.at, e.bound); report.clamped = report.clamped + 1 end
			local b = dst[i]
			if typeof(b) ~= "buffer" or buffer.len(b) < units * 2 then b = buffer.create(units * 2); dst[i] = b end
			buffer.fill(b, 0, 0)
			buffer.copy(b, 0, r.b, r.at, units * 2)
			if lens ~= nil then lens[i] = units end
			r.at = r.at + n
			return true
		end
		if not utf8_ok(r.b, r.at, n) then
			report.malformed = true
			r.at = r.at + n
			return true
		end
		if e.s == "text" then
			dst[i] = copy_out(r.b, r.at, n)
		else
			local keep = n
			if e.bound ~= nil and keep > e.bound then keep = clamp_utf8(r.b, r.at, e.bound); report.clamped = report.clamped + 1 end
			local b = dst[i]
			if typeof(b) ~= "buffer" or buffer.len(b) < keep then b = buffer.create(keep); dst[i] = b end
			buffer.fill(b, 0, 0)
			buffer.copy(b, 0, r.b, r.at, keep)
			if lens ~= nil then lens[i] = keep end
		end
		r.at = r.at + n
		return true
	end
	local x = scalar_at(r, e, ek, report)
	if x == nil then return false end
	put(dst, i, x)
	return true
end

-- ONE ARM's payload, the bytes under the arm's L (§3's arm payload table): a
-- string is its bytes with no length of its own, bytes(N) and an array are the
-- array body, everything else is what a field of that type carries.
local function read_arm(arm: any, sub: any, u: any): boolean
	local s = arm.s
	if s == "str" then
		local n = sub.e - sub.at
		if arm.wide then
			if n % 2 ~= 0 then _report.malformed = true; sub.at = sub.e; return false end
			local units = n / 2
			if arm.bound ~= nil and units > arm.bound then units = clamp_wide(sub.b, sub.at, arm.bound); _report.clamped = _report.clamped + 1 end
			store_bytes(u, arm.slot, sub.b, sub.at, units * 2, units)
		else
			if not utf8_ok(sub.b, sub.at, n) then _report.malformed = true; sub.at = sub.e; return false end
			local keep = n
			if arm.bound ~= nil and keep > arm.bound then keep = clamp_utf8(sub.b, sub.at, arm.bound); _report.clamped = _report.clamped + 1 end
			store_bytes(u, arm.slot, sub.b, sub.at, keep, keep)
		end
		sub.at = sub.e
		return true
	elseif s == "text" then
		u[arm.slot] = copy_out(sub.b, sub.at, sub.e - sub.at)
		sub.at = sub.e
		return true
	elseif s == "bytes" or s == "arr" then
		if sub.e - sub.at >= 2 then array_body(arm, sub, _report, u) end
		sub.at = sub.e
		return true
	end
	return read_field(arm, sub, u)
end

-- AN ARM HEADER IS A FIELD HEADER (§3, §2.6): the arm id reference, the arm's
-- kind byte, L, then L bytes of payload. An arm this reader cannot name is
-- unknown and the union reads None; a retyped arm is a kind mismatch. The
-- value is the union's { Type, <Arm> } (docs/PORT-LUAU.md, THE TYPE MAPPING).
read_union = function(fd: any, r: any, report: any): any
	local un = S.lookup(fd.t)
	local u = un.new()
	local ref = r_leb(r)
	if r.bad then report.malformed = true; return u end
	if ref == 0 then return u end
	if ref > #_table then report.malformed = true; return u end
	local armID = _table[ref]
	local kind = r_u8(r)
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return u end
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
				local x = scalar_at(sub, arm, kind, report)
				if x ~= nil then
					report.widened = report.widened + 1
					u[arm.slot] = x
					u.Type = arm.tag
				end
			else report.malformed = true end
		else
			report.kindMismatch = report.kindMismatch + 1
		end
	elseif arm.s == "tab" then
		local inst = S.lookup(arm.t).new()
		read_body(S.lookup(arm.t), sub, inst, true)
		if sub.at ~= sub.e then report.malformed = true end
		u[arm.slot] = inst
		u.Type = arm.tag
	elseif arm.s == "void" then
		-- a payload-free arm carries L = 0
		u.Type = arm.tag
	else
		if read_arm(arm, sub, u) then u.Type = arm.tag end
	end
	r.at = r.at + n
	return u
end

-- ONE FIELD (docs/SPEC-TABLES.md §3): every value read is stored in its slot
-- in the shape docs/PORT-LUAU.md THE TYPE MAPPING names; a string clamps at a
-- whole code point and a wide string at a whole code unit (§3, KIND 12 / 33).
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
			local units = n / 2
			if fd.bound ~= nil and units > fd.bound then units = clamp_wide(r.b, r.at, fd.bound); _report.clamped = _report.clamped + 1 end
			if fd.slot ~= nil and v ~= nil then store_bytes(v, fd.slot, r.b, r.at, units * 2, units) end
			r.at = r.at + n
			return true
		end
		if not utf8_ok(r.b, r.at, n) then
			_report.malformed = true
			r.at = r.at + n
			return true
		end
		local keep = n
		if fd.bound ~= nil and n > fd.bound then keep = clamp_utf8(r.b, r.at, fd.bound); _report.clamped = _report.clamped + 1 end
		if fd.slot ~= nil and v ~= nil then store_bytes(v, fd.slot, r.b, r.at, keep, keep) end
		r.at = r.at + n
		return true
	elseif s == "text" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; return false end
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = copy_out(r.b, r.at, n) end
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
			return read_keyed(fd, r, _report, v)
		end
		return read_array(fd, r, _report, v)
	elseif s == "map" then
		return read_map(fd, r, _report, v)
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
		if value == nil then value = p.null end
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
				if fd.present ~= nil and v ~= nil then v[fd.present] = true end
				_report.widened = _report.widened + 1
			else
				_report.kindMismatch = _report.kindMismatch + 1
				if not skip(r, kind) then _report.malformed = true; return false end
			end
		else
			if not read_field(fd, r, v) then return false end
			-- ?T: a field that rode is present (§2.3)
			if fd.present ~= nil and v ~= nil then v[fd.present] = true end
		end
	end
	return false
end

-- loadTolerant loads a form-1 wire at the table descriptor `root`, fills
-- report, and returns the recovered root value; it is the TOLERANT walk and
-- only this explicit name reaches it (docs/SPEC-TABLES.md §6.5: the plain
-- name is the strict read, so a recovered value never comes back under it).
function L.loadTolerant(root: string, bytes: buffer, report: any): any
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

-- the counters that refuse, in TableReport.Err()'s order (docs/SPEC-TABLES.md
-- §6.5), each with its §11 name; load.luau spells kind_mismatch kindMismatch
local REFUSING = { "malformed", "clamped", "kindMismatch" }
local NAMES: { [string]: string } = {
	malformed = "malformed",
	clamped = "clamped",
	kindMismatch = "kind_mismatch",
}

-- a report at rest, in load.luau's shape (docs/SPEC-TABLES.md §4)
local function fresh(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end

local function moved(report: any, k: string): boolean
	if k == "malformed" then return report.malformed == true end
	return report[k] > 0
end

-- an id this build cannot name has no name, so it is spelled by its wire id
-- (docs/SPEC-TABLES.md §11: a refusal names the field)
local function idName(id: integer): string
	local b = buffer.create(8)
	buffer.writeinteger(b, 0, id)
	local out = { "#" }
	for i = 7, 0, -1 do
		out[#out + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(out)
end

-- CRC-32C, the Castagnoli polynomial, reflected 0x82F63B78, initial and final
-- 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14). No table: a strict read is not hot.
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

-- §2.14's order: the form byte first (the caller has read it), then the
-- trailer before any field is decoded. A file too short to hold one, or whose
-- last four bytes are not the CRC-32C of the bytes before them, is the
-- whole-file damage named integrity_mismatch; otherwise the trailer comes off.
local function integrityOpen(b: buffer): buffer?
	local n = buffer.len(b)
	if n < 5 or buffer.readu32(b, n - 4) ~= crc32c(b, n - 4) then return nil end
	local out = buffer.create(n - 4)
	buffer.copy(out, 0, b, 0, n - 4)
	return out
end

-- THE FRAMING WALK (docs/SPEC-TABLES.md §3): a field header is an id
-- reference and a kind byte, and every kind's payload is stepped over by its
-- own framing, the rule load.luau's skip states. It reads no value.
local function leb(b: buffer, at: number, e: number): (number?, number)
	local v = 0
	local s = 0
	for i = 0, 9 do
		if at + i >= e then return nil, at end
		local x = buffer.readu8(b, at + i)
		if i == 9 and x > 1 then return nil, at end
		v = v + (x % 0x80) * (2 ^ s)
		if x < 0x80 then
			if i > 0 and x == 0 then return nil, at end
			return v, at + i + 1
		end
		s = s + 7
	end
	return nil, at
end

local function lebBytes(n: number): string
	local out = {}
	repeat
		local x = n % 0x80
		n = (n - x) / 0x80
		if n > 0 then x = x + 0x80 end
		out[#out + 1] = string.char(x)
	until n == 0
	return table.concat(out)
end

-- the end of one payload of `kind` at `at`, or nil when it does not fit: the
-- payload table of docs/SPEC-TABLES.md §3, the rule load.luau's skip states
local function stepOver(b: buffer, at: number, e: number, kind: number): number?
	local fixedWidth = 0
	if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then fixedWidth = 1
	elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then fixedWidth = 2
	elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then fixedWidth = 4
	elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then fixedWidth = 8
	elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then fixedWidth = 16
	end
	if fixedWidth > 0 then
		if at + fixedWidth > e then return nil end
		return at + fixedWidth
	end
	if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
		local n, p = leb(b, at, e)
		if n == nil or p + n > e then return nil end
		return p + n
	elseif kind == 15 then
		local arm, p = leb(b, at, e)
		if arm == nil then return nil end
		if arm == 0 then return p end
		if p + 1 > e then return nil end
		local n, q = leb(b, p + 1, e)
		if n == nil or q + n > e then return nil end
		return q + n
	elseif kind == 17 or kind == 30 then
		local v, p = leb(b, at, e)
		if v == nil then return nil end
		return p
	end
	return nil
end

-- one body's field headers in wire order (docs/SPEC-TABLES.md §3: a body is
-- field headers up to a zero reference), each { s, e, ps, id, kind } (s the
-- header's first byte, ps the payload's, e one past it), and where its
-- terminator sits; nil when the framing does not walk to a terminator
local function walkBody(b: buffer, at: number, e: number, ids: { integer }): any
	local fields = {}
	local p = at
	while true do
		local ref, q = leb(b, p, e)
		if ref == nil then return nil end
		if ref == 0 then return { fields = fields, term = p } end
		if ref > #ids or q >= e then return nil end
		local kind = buffer.readu8(b, q)
		local stop = stepOver(b, q + 1, e, kind)
		if stop == nil then return nil end
		fields[#fields + 1] = { s = p, e = stop, ps = q + 1, id = ids[ref], kind = kind }
		p = stop
	end
end

-- the id table and the root body's span (docs/SPEC-TABLES.md §3: the id
-- table's count is the file's last eight bytes and its ids precede it), as
-- load.luau's open reads them, except that a count that does not frame (a
-- negative one included) is refused
local function frame(b: buffer): any
	local len = buffer.len(b)
	if len < 9 then return nil end
	local count = integer.tonumber(buffer.readinteger(b, len - 8))
	if count < 0 or count > math.floor((len - 9) / 8) then return nil end
	local endpos = len - 8 - count * 8
	local ids: { integer } = {}
	for i = 0, count - 1 do
		ids[i + 1] = buffer.readinteger(b, endpos + i * 8)
	end
	return { ids = ids, e = endpos, tail = buffer.readstring(b, endpos, len - endpos) }
end

local function bytesOf(b: buffer, s: number, e: number): string
	return buffer.readstring(b, s, e - s)
end

local function fieldById(fields: { any }, id: integer): any
	for _, f in fields do
		if f.id == id then return f end
	end
	return nil
end

-- THE WITNESS: the first field the walk saw move a counter (docs/SPEC-TABLES.md
-- §11, a refusal names the field). The pinned Luau carries no debug.getlocal,
-- so the field is not read off the load's frames: it is found by asking the
-- load itself. The file is rebuilt with only a PREFIX of a body's fields (the
-- id table and every other byte unchanged) and loaded again into a fresh
-- report; the first prefix whose last field moves the counter names that
-- field. The walk is the load's own order (§3.1: the node records' bodies
-- first, then the root body, then resolution, whose counters belong to the
-- pointer field that queued them), and it descends into an inline table's
-- body the same way and into a union to the arm its header selects, so a
-- counter names the innermost field that holds it. An id the reader cannot
-- name is spelled #<hex>, and a counter no field holds (a node table's own
-- framing, a blob record's text) names none.
local function witness(root: string, b: buffer, k: string): string?
	local fr = frame(b)
	if fr == nil then return nil end
	local ids = fr.ids
	local desc = S.lookup(root)
	local top = walkBody(b, 1, fr.e, ids)
	if top == nil then return nil end

	local function probe(wire: string): boolean
		local rep = fresh()
		L.loadTolerant(root, buffer.fromstring(wire), rep)
		return moved(rep, k)
	end

	-- refine names the innermost field of `fd`, whose header and payload are
	-- seg in b, that moves k, given rebuild(segment) -> the whole file
	local refine: (fd: any, seg: any, rebuild: (string) -> string) -> string?
	local function isolate(fields: { any }, f: any, rebuild: (string) -> string): string?
		-- f.fields is the body's field list, f.desc its descriptor; rebuild
		-- takes a whole body (fields and terminator)
		for i = 1, #f.fields do
			local parts = {}
			for j = 1, i do
				local s = f.fields[j]
				parts[j] = bytesOf(b, s.s, s.e)
			end
			if probe(rebuild(table.concat(parts) .. "\0")) then
				local seg = f.fields[i]
				local fd = fieldById(fields, seg.id)
				if fd == nil then return idName(seg.id) end
				local before = table.concat(parts, "", 1, i - 1)
				return refine(fd, seg, function(one: string): string
					return rebuild(before .. one .. "\0")
				end) or fd.name
			end
		end
		return nil
	end
	refine = function(fd: any, seg: any, rebuild: (string) -> string): string?
		if fd.s == "tab" and seg.kind == 13 and fd.t ~= nil then
			local n, bs = leb(b, seg.ps, seg.e)
			if n == nil then return fd.name end
			local inner = walkBody(b, bs, bs + n, ids)
			if inner == nil then return fd.name end
			local head = bytesOf(b, seg.s, seg.ps)
			return isolate(S.lookup(fd.t).fields, inner, function(body: string): string
				return rebuild(head .. lebBytes(#body) .. body)
			end) or fd.name
		elseif fd.s == "union" and seg.kind == 15 and fd.t ~= nil then
			local ref = leb(b, seg.ps, seg.e)
			if ref == nil or ref == 0 or ref > #ids then return fd.name end
			local arm = fieldById(S.lookup(fd.t).arms, ids[ref])
			if arm == nil then return idName(ids[ref]) end
			return arm.name
		end
		return fd.name
	end

	-- the root body, rebuilt from a list of its own segments
	local function wireOf(body: string): string
		return "\1" .. body .. fr.tail
	end

	-- the node records (§3.1), read before the root body when the root is of
	-- the variable class: the node table field is kept and every other root
	-- field dropped, and records past the one under test are emptied in place
	-- (same type, same index), so every pointer still resolves
	local nodeSeg: any = nil
	for _, s in top.fields do
		if s.id == NodeTableId then nodeSeg = s end
	end
	if desc.variable and nodeSeg ~= nil and nodeSeg.kind == 12 then
		local n, ps = leb(b, nodeSeg.ps, nodeSeg.e)
		local recs: { any } = {}
		local ok = n ~= nil
		local count = 0
		if ok then
			local c, p = leb(b, ps, ps + (n :: number))
			ok = c ~= nil
			count = c or 0
			while ok and p < ps + (n :: number) do
				local tref, q = leb(b, p, ps + (n :: number))
				if tref == nil or tref == 0 or tref > #ids then ok = false; break end
				local tlen, r = leb(b, q, ps + (n :: number))
				if tlen == nil or r + tlen > ps + (n :: number) then ok = false; break end
				recs[#recs + 1] = { tref = tref, tid = ids[tref], s = r, e = r + tlen }
				p = r + tlen
			end
			ok = ok and count == #recs
		end
		if ok then
			local head = bytesOf(b, nodeSeg.s, nodeSeg.ps)
			local function empty(rec: any): string
				if rec.tid == StringBlobId or rec.tid == BytesBlobId then return "" end
				return "\0"
			end
			local function nodeWire(bodyOf: (number, any) -> string): string
				local parts = { lebBytes(#recs) }
				for i, rec in recs do
					local body = bodyOf(i, rec)
					parts[#parts + 1] = lebBytes(rec.tref) .. lebBytes(#body) .. body
				end
				local payload = table.concat(parts)
				return wireOf(head .. lebBytes(#payload) .. payload .. "\0")
			end
			local placed: { [integer]: any } = {}
			for _, name in desc.place.tables do
				local d = S.lookup(name)
				placed[d.id] = d
			end
			for upto = 1, #recs + 1 do
				local j = upto - 1 -- the records kept whole: none, then one more each pass
				local wire = nodeWire(function(i: number, rec: any): string
					if i <= j then return bytesOf(b, rec.s, rec.e) end
					return empty(rec)
				end)
				if probe(wire) then
					if j == 0 then
						-- no record body holds it: a record of a type this
						-- build cannot place, or the numbering's own framing
						if k == "unknown" then
							for _, rec in recs do
								local blob = (rec.tid == StringBlobId and desc.place.str) or (rec.tid == BytesBlobId and desc.place.bytes)
								if not blob and placed[rec.tid] == nil then return idName(rec.tid) end
							end
						end
						return nil
					end
					local rec = recs[j]
					local rd = placed[rec.tid]
					if rd == nil then return nil end
					local body = walkBody(b, rec.s, rec.e, ids)
					if body == nil then return nil end
					return isolate(rd.fields, body, function(one: string): string
						return nodeWire(function(i: number, r: any): string
							if i < j then return bytesOf(b, r.s, r.e) end
							if i == j then return one end
							return empty(r)
						end)
					end)
				end
			end
		end
	end

	-- the root body: the node table field kept where it stands, and the
	-- root's other fields a prefix at a time. With none of them the counter
	-- already moved means the node table's own framing moved it (a numbering
	-- that does not read, §3.1), which no field holds
	if desc.variable and nodeSeg ~= nil and probe(wireOf(bytesOf(b, nodeSeg.s, nodeSeg.e) .. "\0")) then
		return nil
	end
	local others = {}
	for _, s in top.fields do
		if not (desc.variable and s == nodeSeg) then others[#others + 1] = s end
	end
	for i = 1, #others do
		local parts = {}
		local taken = 0
		for _, s in top.fields do
			if desc.variable and s == nodeSeg then
				parts[#parts + 1] = bytesOf(b, s.s, s.e)
			elseif taken < i then
				taken += 1
				parts[#parts + 1] = bytesOf(b, s.s, s.e)
			end
		end
		if probe(wireOf(table.concat(parts) .. "\0")) then
			local seg = others[i]
			local fd = fieldById(desc.fields, seg.id)
			if fd == nil then return idName(seg.id) end
			return refine(fd, seg, function(one: string): string
				local out = {}
				local t = 0
				for _, s in top.fields do
					if desc.variable and s == nodeSeg then
						out[#out + 1] = bytesOf(b, s.s, s.e)
					elseif t < i then
						t += 1
						out[#out + 1] = t == i and one or bytesOf(b, s.s, s.e)
					end
				end
				return wireOf(table.concat(out) .. "\0")
			end) or fd.name
		end
	end
	return nil
end

-- the report's reset: every counter at rest, no reason, no field
local function reset(report: any)
	report.malformed = false
	report.refused = 0
	report.unknown = 0
	report.kindMismatch = 0
	report.clamped = 0
	report.widened = 0
	report.duplicate = 0
	report.reason = nil
	report.field = nil
	report.unknownField = nil
	report.duplicateField = nil
end


-- THE VARIABLE FORM'S STRICT READ (docs/SPEC-TABLES.md §6.5): the one answer
-- for form 1, over the tolerant walk above. It resets the caller's report,
-- applies the file's own framing (integrity, the id table and the root body's
-- terminator) and then the refusing counters in TableReport.Err()'s order, so a
-- wire always answers one name. R.ReadStrict's fixed-form dispatch lives in
-- refusal.luau; this is the half a form-1 file reaches, including a declared
-- fixed root handed its form-1 wire.
function L.ReadStrict(root: string, bytes: buffer, report: any): any
	local rep: any = report or {}
	reset(rep)
	if bytes == nil or buffer.len(bytes) < 1 then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local b = bytes :: buffer
	local form = buffer.readu8(b, 0)
	if form == 2 then rep.reason = "message_form_as_file"; return nil end
	if form ~= 1 then rep.reason = "newer_form"; return nil end
	local d = S.lookup(root)
	if d.integrity ~= nil and d.integrity ~= "" then
		local body = integrityOpen(b)
		if body == nil then rep.malformed = true; rep.reason = "integrity_mismatch"; return nil end
		b = body
	end
	local fr = frame(b)
	local top = fr ~= nil and walkBody(b, 1, fr.e, fr.ids) or nil
	if fr == nil or (top ~= nil and top.term + 1 ~= fr.e) then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local v = L.loadTolerant(root, b, rep)
	if v == nil and rep.malformed then
		rep.reason = "wire_damaged"
		return nil
	end
	for _, k in REFUSING do
		if moved(rep, k) then
			rep.reason = NAMES[k]
			rep.field = witness(root, b, k)
			return nil
		end
	end
	if rep.unknown > 0 then rep.unknownField = witness(root, b, "unknown") end
	if rep.duplicate > 0 then rep.duplicateField = witness(root, b, "duplicate") end
	return v
end

-- resetReport is the report at rest, shared with refusal.luau's fixed path.
function L.resetReport(report: any)
	reset(report)
end

-- THE PLAIN NAME IS THE STRICT READ (docs/SPEC-TABLES.md §6.5): every read
-- entry point reached by the plain name refuses what the strict read refuses
-- and hands back no recovered value, with the caller's report overwritten by
-- this read's counters and report.reason the one name the strict read answers.
-- The strict check lives HERE, beside the walk it judges, rather than calling
-- refusal.luau: the pinned Luau refuses a require cycle at load time, and
-- load.luau -> refusal.luau -> load.luau is exactly one. refusal.luau's
-- ReadStrict still owns the fixed-form dispatch and delegates form 1 here.
function L.load(root: string, bytes: buffer, report: any): any
	return L.ReadStrict(root, bytes, report)
end

return table.freeze(L)
