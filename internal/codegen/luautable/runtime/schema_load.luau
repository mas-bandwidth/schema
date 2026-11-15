--!strict
-- schema_load.luau — the Luau table runtime's VARIABLE FORM (form 1) walk load
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
local F = require("./schema_fixed")

local L = {}

-- the id table's first-use order is checked at the open, once the entries are
-- known and before any value is read (docs/SPEC-TABLES.md §3).
local idTableOrder: any = nil

-- the whole-body walks one read has taken: a test seam that counts the load's
-- own passes over the file, so a cost gate holds a count and never a clock
local _walks = 0

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

-- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; the same rule
-- as packets): its RAW BITS are tested before they become a value, and an
-- exponent of all ones (a NaN of any sign or payload, or an infinity) is
-- refused as non_finite_float. Every finite float32 widens to a double
-- exactly, so the read is the ordinary conversion; no bit surgery is owed.
local f32scratch = buffer.create(4)
local function f32_of(bits: number): number
	buffer.writeu32(f32scratch, 0, bits)
	return buffer.readf32(f32scratch, 0)
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
	elseif kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 then
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
	-- the 128-BIT RUNGS (docs/SPEC-TABLES.md §4): a reader that declares
	-- i128/u128 widens every narrower kind on its own ladder, exactly as
	-- ir.TableKindWidens says
	if declared == 18 then return kind >= 2 and kind <= 5 end
	if declared == 19 then return kind >= 6 and kind <= 9 end
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

-- ir.TableWireIdTableBound: the most entries an id table may hold (§3)
local ID_TABLE_BOUND = 4096

-- THE ID TABLE AND THE ROOT BODY'S FRAMING (docs/SPEC-TABLES.md §3): a form
-- byte this reader does not know REFUSES; damage is malformed.
local function open(bytes: buffer, report: any): any
	local len = buffer.len(bytes)
	if len < 1 then report.malformed = true; return nil end
	if buffer.readu8(bytes, 0) ~= 1 then report.refused = report.refused + 1; return nil end
	if len < 9 then report.malformed = true; return nil end
	-- the count is the unsigned u64 of docs/SPEC-TABLES.md §3, not a signed
	-- integer: a high-bit count is huge, never negative, and refuses here
	local count = integer.tonumber(buffer.readinteger(bytes, len - 8))
	if count < 0 or count > math.floor((len - 9) / 8) then report.malformed = true; return nil end
	-- THE ENTRY COUNT HAS A MAXIMUM (docs/SPEC-TABLES.md §3, THE DISTINCTNESS
	-- CHECK): a table of more than ID_TABLE_BOUND entries is malformed on the
	-- whole-wire rule, whatever the buffer would hold
	if count > ID_TABLE_BOUND then report.malformed = true; return nil end
	local endpos = len - 8 - count * 8
	local ids = {}
	local seen: { [integer]: boolean } = {}
	for i = 0, count - 1 do
		local id = buffer.readinteger(bytes, endpos + i * 8)
		if seen[id] then report.malformed = true; return nil end
		seen[id] = true
		ids[i + 1] = id
	end
	-- THE ID TABLE IS FIRST-USE ORDER (§3): an entry no reference names, or
	-- one whose first reference comes after another entry's, is a spelling no
	-- conforming writer emits.
	if not idTableOrder(bytes, 1, endpos, ids) then report.malformed = true; return nil end
	return { table = ids, e = endpos }
end

-- the root body's reserved node-table field (docs/SPEC-TABLES.md §3.1); the
-- numbering is whole or it is nothing
local function node_table_scan(bytes: buffer, ids: { any }, endpos: number, report: any): any
	local w = new_reader(bytes, 1, endpos)
	local payload: buffer? = nil
	local present = false
	local bad = false
	local after = false
	while not bad do
		local ref = r_leb(w)
		if w.bad then break end
		if ref == 0 then break end
		if after then bad = true; break end
		if ref > #ids then break end
		if w.at >= w.e then break end
		local id = ids[ref]
		local kind = r_u8(w)
		if id == NodeTableId then
			if present then bad = true; break end
			present = true
			if kind ~= 12 then bad = true; break end
			local n = r_leb(w)
			if w.bad or w.at + n > w.e then bad = true; break end
			local p = buffer.create(n)
			buffer.copy(p, 0, bytes, w.at, n)
			payload = p
			w.at = w.at + n
			after = true
		else
			if not skip(w, kind) then break end
		end
	end
	if present and bad then report.malformed = true; return nil end
	if not present then return {} end
	local rec = new_reader(payload :: buffer, 0, buffer.len(payload :: buffer))
	local count = r_leb(rec)
	if rec.bad then report.malformed = true; return nil end
	-- AN EMPTY NODE TABLE IS NOT WRITTEN (§3.1): a writer with no records
	-- appends none, so a present table of count zero is non-canonical.
	if count == 0 then report.malformed = true; return nil end
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
local posCache: { [any]: { [integer]: number } } = {}
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

-- a field's place in its writer's declaration order (docs/SPEC-TABLES.md §3),
-- which is the order a conforming writer emits and the reader requires.
local function field_positions(desc: any): { [integer]: number }
	local m = posCache[desc]
	if m == nil then
		m = {}
		for i, f in desc.fields do m[f.id] = i end
		posCache[desc] = m
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

-- an Int128 compares as a signed two's-complement value: the high half first,
-- and the LOW HALF UNSIGNED once the high halves agree — the sign lives in the
-- high half, so the low half is a magnitude, never a signed quantity
-- (docs/SPEC-TABLES.md §4: a bounded 128-bit integer clamps on the raw scale)
local function cmp_lt(a: any, b: any): boolean
	if is128(a) and is128(b) then
		if a.hi ~= b.hi then return integer.lt(a.hi, b.hi) end
		return integer.ult(a.lo, b.lo)
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
		return integer.ult(b.lo, a.lo)
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

-- THE NON-FINITE REFUSAL (docs/SPEC-TABLES.md §3.3): the first float a walk
-- met holding a NaN or an infinity, named <Owner>.<field>. The walk goes on,
-- so the framing it meets is the one a finite wire meets, and the load
-- answers the refusal at its end. _ownerName and _fieldName are the body and
-- the field the walk is in, for an element or arm descriptor that has none.
local _nonFinite: string? = nil
local _ownerName: string = "?"
local _fieldName: string = "?"

-- THE FIRST FIELD THAT MOVED A COUNTER (docs/SPEC-TABLES.md §11): the walk
-- records it where the counter moves, so naming the field costs one
-- assignment and never a second read
local _firstField: { [string]: string } = {}
local function noteField(k: string, name: string)
	if _firstField[k] == nil then _firstField[k] = name end
end

local function idName(id: integer): string
	local b = buffer.create(8)
	buffer.writeinteger(b, 0, id)
	local out = { "#" }
	for i = 7, 0, -1 do
		out[#out + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(out)
end

-- refuseNonFinite records the first non-finite float (docs/SPEC-TABLES.md §3.3)
local function refuseNonFinite(fd: any)
	if _nonFinite == nil then
		_nonFinite = _ownerName .. "." .. (fd.name or _fieldName)
	end
end

local function scalar_at(r: any, fd: any, kind: number, report: any): any
	local width = width_of(kind)
	if width == 0 then report.malformed = true; return nil end
	if r.at + width > r.e then report.malformed = true; return nil end
	local x: any
	if kind == 1 then
		-- a bool byte past 1 is malformed, and non-zero is still true (docs/SPEC-TABLES.md §3)
		local b = r_u8(r)
		if b > 1 then report.malformed = true end
		x = b ~= 0
	elseif kind == 10 then
		local bits = r_u32(r)
		if bit32.band(bits, 0x7F800000) == 0x7F800000 then
			refuseNonFinite(fd)
			x = 0.0
		else
			x = f32_of(bits)
			-- a ranged float's -0 lands as +0, uncounted (docs/SPEC-TABLES.md
			-- §3.3, the special values); a plain float keeps its sign
			if fd.s == "float" and fd.lo ~= nil and x == 0 then x = 0 end
		end
	elseif kind == 11 then
		-- the high half holds the whole exponent field; the width is checked above
		if bit32.band(buffer.readu32(r.b, r.at + 4), 0x7FF00000) == 0x7FF00000 then
			refuseNonFinite(fd)
			r.at = r.at + 8
			x = 0.0
		else
			x = r_f64(r)
		end
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
	-- A 128-BIT DECLARATION WIDENS A NARROWER WIRE KIND (docs/SPEC-TABLES.md
	-- §4): the value is sign-extended on the signed ladder and zero-extended on the unsigned
	-- one, so the declared bounds and the save see one 128-bit shape.
	if fd.kw == 16 and not is128(x) then
		local lo = if typeof(x) == "integer" then x else integer.create(x)
		local hi = 0i
		if fd.signed == true and ((typeof(x) == "integer" and integer.lt(x, 0i)) or (typeof(x) == "number" and x < 0)) then
			hi = -1i
		end
		x = { lo = lo, hi = hi }
	end
	local lo, hi = fd.lo, fd.hi
	if lo ~= nil and fd.fbits ~= nil and fd.fbits > 0 then
		lo, hi = bound_shift(lo, fd.fbits), bound_shift(hi, fd.fbits)
	end
	if lo ~= nil then
		local lt, gt = cmp_lt, cmp_gt
		if fd.signed == false then lt, gt = ucmp_lt, ucmp_gt end
		if lt(x, lo) then x = lo; report.clamped = report.clamped + 1; noteField("clamped", _fieldName)
		elseif gt(x, hi) then x = hi; report.clamped = report.clamped + 1; noteField("clamped", _fieldName) end
	end
	if fd.bits ~= nil and fd.bits > 0 and fd.bits < width * 8 then
		-- bits(N) is unsigned in [0, 2^N - 1] (docs/SPEC-TABLES.md §16.2): an
		-- integer top is built exactly, not through a double, and compared unsigned.
		local bmax: any
		if typeof(x) == "integer" then
			bmax = integer.sub(integer.lshift(1i, integer.create(fd.bits)), 1i)
		else
			bmax = 2 ^ fd.bits - 1
		end
		if ucmp_gt(x, bmax) then x = bmax; report.clamped = report.clamped + 1; noteField("clamped", _fieldName) end
	end
	return x
end

local _table: { any } = {}
local _pending: { any } = {}
-- the union FIELD whose arm reference is being read: an element or a nested
-- arm keeps its positional None (§2.6, §3.3).
local _unionField: any = nil
local _nodes: { any } = {}
local _rootValue: any = nil
local _rootDesc: any = nil
local _report: any = nil
local _numberingGood: boolean = true
-- THE STORAGE CAP REFUSAL (docs/SPEC-TABLES.md §2.9): a `[]` or map count past
-- the int32 storage cap refuses the whole read by the name
-- count_over_extent_cap. It is not damage: malformed stays unset, no counter
-- moves, and the partial value is discarded. The walk goes on to its end, so
-- the report and the non-finite witness are taken at the first refusal and put
-- back when the walk ends: what the walk met after it is not this read's answer.
local _countRefused: boolean = false
local _countReport: any = nil
local _countNonFinite: string? = nil
local _countField: { [string]: string } = {}

-- refuseCount records the first count past the storage cap (§2.9)
local function refuseCount(report: any)
	if _countRefused then return end
	_countRefused = true
	_countReport = table.clone(report)
	_countNonFinite = _nonFinite
	_countField = table.clone(_firstField)
end

-- restoreCount puts the report back as it stood at the first count refusal
local function restoreCount(report: any)
	local keys = {}
	for k in report do keys[#keys + 1] = k end
	for _, k in keys do report[k] = nil end
	for k, v in _countReport do report[k] = v end
	_nonFinite = _countNonFinite
	_firstField = table.clone(_countField)
end

local read_body: any
local read_field: any
local read_array: any
local read_element: any
local read_union: any
local read_map: any
local put: any

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
	noteField("unknown", _fieldName)
	return 0, true
end

-- A CLAMP KEEPS WHOLE CODE POINTS (docs/SPEC-TABLES.md §3, KIND 12): a string
-- past its bound keeps the last whole UTF-8 code point within it.
local function clamp_utf8(b: buffer, at: number, keep: number): number
	while keep > 0 do
		local x = buffer.readu8(b, at + keep)
		if x < 0x80 or x > 0xBF then break end
		keep = keep - 1
	end
	return keep
end

-- a string(N) or bytes(N) value lands in its buffer with its used
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

-- the key value of one entry, in the shape the order and bound rules compare
-- (docs/SPEC-TABLES.md §2.8): a string key its bytes as a Lua string, an
-- integer key its number.
local function loadFixDKeyValue(keyfd: any, vals: any): any
	if keyfd == nil then return nil end
	local b = vals[keyfd.slot]
	if typeof(b) == "buffer" then
		local n = vals[keyfd.slot .. "Length"]
		if n == nil then n = buffer.len(b) end
		return buffer.readstring(b, 0, n)
	end
	-- A BODY WITH NO KEY FIELD AT ALL HAS THE KEY'S DECLARED DEFAULT
	-- (docs/SPEC-TABLES.md §2.8): an unbounded text key's default is the
	-- empty string, never an absent value, so the order rule compares two
	-- keys and never an absent one.
	if b == nil and keyfd.s == "text" then return "" end
	return b
end

-- the ascending order of two keys: -1/0/1 (§2.8). A string key compares its
-- bytes; an integer key its number, signed only where the declaration is.
local function loadFixDKeyCompare(keyfd: any, a: any, b: any): number
	if typeof(a) == "string" and typeof(b) == "string" then
		if a < b then return -1 elseif a > b then return 1 end
		return 0
	end
	if keyfd ~= nil and keyfd.signed == false then
		if ucmp_lt(a, b) then return -1 elseif ucmp_gt(a, b) then return 1 end
		return 0
	end
	if cmp_lt(a, b) then return -1 elseif cmp_gt(a, b) then return 1 end
	return 0
end

-- KEYS NEVER CLAMP (docs/SPEC-TABLES.md §2.8): a string key past the
-- declaration's bound drops its whole entry and counts clamped; an integer key
-- always fits.
local function loadFixDKeyFits(keyfd: any, key: any): boolean
	if keyfd == nil or typeof(key) ~= "string" then return true end
	local bound = keyfd.bound
	if bound == nil or bound == 0 then return true end
	return #key <= bound
end

-- THE KEY SCAN (docs/SPEC-TABLES.md §2.8; internal/tablewire mapKey): scan the
-- entry's headers for the key's id and read it, the LAST occurrence winning. A
-- string key is read WHOLE, never clamped (its bound is the key-fits rule
-- below); an integer key lands at its declared range. A body with no key field
-- has the key's declared default. Returns the key, whether the LAST key kind
-- is the reader's, whether a key widened, and whether the scan was whole.
local function loadFixDScanKey(keyfd: any, body: any, entry: any): (any, boolean, boolean, boolean)
	local scan = new_reader(body.b, body.at, body.e)
	local keyvals: any = entry.new()
	local keyVal: any = loadFixDKeyValue(keyfd, keyvals)
	local kindBad = false
	local entryWidened = false
	local quiet = { malformed = false, clamped = 0 }
	while true do
		local ref = r_leb(scan)
		if scan.bad then return keyVal, kindBad, entryWidened, false end
		if ref == 0 then return keyVal, kindBad, entryWidened, scan.at == scan.e end
		if ref > #_table or scan.at >= scan.e then return keyVal, kindBad, entryWidened, false end
		local id = _table[ref]
		local kind = r_u8(scan)
		if id == MapKeyId and keyfd ~= nil then
			if kind ~= keyfd.k and widens(kind, keyfd.k) then
				entryWidened = true
				kindBad = false -- this occurrence replaces the earlier key kind
				local x = scalar_at(scan, keyfd, kind, quiet)
				if x == nil then return keyVal, kindBad, entryWidened, false end
				keyvals[keyfd.slot] = x
				keyVal = loadFixDKeyValue(keyfd, keyvals)
			else
				kindBad = kind ~= keyfd.k
				if kindBad then
					if not skip(scan, kind) then return keyVal, kindBad, entryWidened, false end
				elseif keyfd.s == "str" or keyfd.s == "text" then
					local n = r_leb(scan)
					if scan.bad or scan.at + n > scan.e then return keyVal, kindBad, entryWidened, false end
					if not utf8_ok(scan.b, scan.at, n) then return keyVal, kindBad, entryWidened, false end
					store_bytes(keyvals, keyfd.slot, scan.b, scan.at, n, n)
					scan.at = scan.at + n
					keyVal = loadFixDKeyValue(keyfd, keyvals)
				else
					local x = scalar_at(scan, keyfd, keyfd.k, quiet)
					if x == nil then return keyVal, kindBad, entryWidened, false end
					keyvals[keyfd.slot] = x
					keyVal = loadFixDKeyValue(keyfd, keyvals)
				end
			end
		else
			if not skip(scan, kind) then return keyVal, kindBad, entryWidened, false end
		end
	end
end

read_map = function(fd: any, r: any, report: any, v: any): boolean
	local mapName = _fieldName
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; noteField("malformed", mapName); return false end
	local endpos = r.at + n
	local landed: { any } = {}
	if n < 2 then report.malformed = true; noteField("malformed", mapName) end
	if n >= 2 then
		-- THE MAP'S HEADER IS READ AGAINST THE PARENT, as every array's is
		-- (internal/tablewire decode.go mapField, subTo; docs/SPEC-TABLES.md
		-- §2.8, §3): a count LEB128 that runs past the map's L still reads,
		-- the element kind is judged, and the entries walk an empty body.
		local hdr = new_reader(r.b, r.at, r.e)
		local ek = r_u8(hdr)
		local count = r_leb(hdr)
		if hdr.bad then report.malformed = true; noteField("malformed", mapName); r.at = endpos; return true end
		local sub = new_reader(r.b, hdr.at, if endpos < hdr.at then hdr.at else endpos)
		if ek ~= 13 then report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", mapName); r.at = endpos; return true end
		-- THE STORAGE CAP (docs/SPEC-TABLES.md §2.9, §2.2; internal/tablewire
		-- collectionCap): a count inside the map's L refuses the read as
		-- count_over_extent_cap when the walk ends, the refusal LoadMeasure
		-- names first. A count whose spelling CROSSED L is read against the
		-- parent (§3) where the measure, bounded by L, sees no count, so it is
		-- the region's recovery instead: damage, the walk resuming after L.
		if count > 0x7FFFFFFF then
			if hdr.at > endpos then
				report.malformed = true; noteField("malformed", mapName)
			else
				refuseCount(report)
			end
			r.at = endpos
			return true
		end
		local entry = S.lookup(fd.t)
		local entryFields = fields_by_id(entry)
		local keyfd = entryFields[MapKeyId]
		local widened = false
		local lastKey: any = nil
		local hasLast = false
		for _ = 1, count do
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then report.malformed = true; noteField("malformed", mapName); break end
			local body = new_reader(sub.b, sub.at, sub.at + elemLen)
			sub.at = sub.at + elemLen
			-- THE KEY BEFORE THE SLOT (docs/SPEC-TABLES.md §2.8): scan it
			-- before the entry body is counted, exactly as mapField does.
			local keyVal, kindBad, entryWidened, keyOk = loadFixDScanKey(keyfd, body, entry)
			if entryWidened and not widened then widened = true; report.widened = report.widened + 1; noteField("widened", mapName) end
			if kindBad then
				report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", mapName)
				landed = {}
				break
			end
			if not keyOk then report.malformed = true; noteField("malformed", mapName); break end
			if not loadFixDKeyFits(keyfd, keyVal) then
				report.clamped = report.clamped + 1; noteField("clamped", mapName)
			else
				local order = -1
				if hasLast then order = loadFixDKeyCompare(keyfd, lastKey, keyVal) end
				if order > 0 then
					report.malformed = true; noteField("malformed", mapName)
					break
				end
				-- the entry's body is read WHOLE now the key has chosen its
				-- place; a duplicate replaces the entry it repeats, whole.
				local vals: any = entry.new()
				while true do
					local ref = r_leb(body)
					if body.bad then report.malformed = true; noteField("malformed", mapName); break end
					if ref == 0 then break end
					if ref > #_table then report.malformed = true; noteField("malformed", mapName); break end
					local id = _table[ref]
					local kind = r_u8(body)
					-- THE RESERVED IDS IN A NESTED BODY ARE MALFORMED (§3.1):
					-- a map entry is one of those bodies, and the engine's
					-- bodyAt enforces the same rule there.
					if id == BuildVersionId or id == MessageVocabularyId or id == NodeTableId then
						report.malformed = true; noteField("malformed", mapName)
						break
					end
					local efd = entryFields[id]
					if efd == nil then
						report.unknown = report.unknown + 1; noteField("unknown", mapName)
						if not skip(body, kind) then report.malformed = true; noteField("malformed", mapName); break end
					elseif id == MapKeyId and keyfd ~= nil then
						if kind ~= keyfd.k then
							if widens(kind, keyfd.k) then
								local x = scalar_at(body, keyfd, kind, report)
								if x == nil then break end
								vals[keyfd.slot] = x
							else
								report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", mapName)
								if not skip(body, kind) then report.malformed = true; noteField("malformed", mapName); break end
							end
						else
							_unionField = if keyfd.s == "union" then keyfd else nil
							local keyok = read_field(keyfd, body, vals, true)
							_unionField = nil
							if not keyok then break end
						end
					elseif kind ~= efd.k then
						if widens(kind, efd.k) then
							local x = scalar_at(body, efd, kind, report)
							if x == nil then break end
							if efd.slot ~= nil then vals[efd.slot] = x end
							report.widened = report.widened + 1; noteField("widened", mapName)
						else
							report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", mapName)
							if not skip(body, kind) then report.malformed = true; noteField("malformed", mapName); break end
						end
					else
						_unionField = if efd.s == "union" then efd else nil
						local valok = read_field(efd, body, vals, true)
						_unionField = nil
						if not valok then break end
					end
				end
				-- THE KEY IS THE ENTRY'S OWN FIELD (docs/SPEC-TABLES.md §2.8):
				-- a body with no key field has the key's declared default,
				-- which for an unbounded text key is the empty string. The
				-- entry carries it, so the writer's key and the order rule
				-- both see the same key.
				if keyfd ~= nil and vals[keyfd.slot] == nil and keyfd.s == "text" then
					vals[keyfd.slot] = buffer.create(0)
				end
				if order == 0 then
					report.duplicate = report.duplicate + 1; noteField("duplicate", mapName)
					landed[#landed] = vals
				else
					landed[#landed + 1] = vals
					lastKey = loadFixDKeyValue(keyfd, vals)
					hasLast = true
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
	local keyedName = _fieldName
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; noteField("malformed", keyedName); return false end
	local endpos = r.at + n
	local dst: any = nil
	if v ~= nil and fd.slot ~= nil then dst = v[fd.slot] end
	if n < 2 then report.malformed = true; noteField("malformed", keyedName) end
	if n >= 2 then
		-- THE KEYED BODY'S HEADER IS READ AGAINST THE PARENT, as every
		-- array's is (internal/tablewire decode.go keyed, subTo;
		-- docs/SPEC-TABLES.md §3, §3.2): a pair count that runs past the
		-- body's L still reads, the element kind is judged before the
		-- triples, and the triples walk an empty body.
		local hdr = new_reader(r.b, r.at, r.e)
		local ek = r_u8(hdr)
		local count = r_leb(hdr)
		if hdr.bad then report.malformed = true; noteField("malformed", keyedName); r.at = endpos; return true end
		local sub = new_reader(r.b, hdr.at, if endpos < hdr.at then hdr.at else endpos)
		if ek ~= fd.ek then
			if not widens(ek, fd.ek) then report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", keyedName); r.at = endpos; return true end
			report.widened = report.widened + 1; noteField("widened", keyedName)
		end
		local e = fd.e
		for _ = 1, count do
			local key = r_leb(sub)
			if sub.bad then report.malformed = true; noteField("malformed", keyedName); break end
			if key == 0 or key > #_table then report.malformed = true; noteField("malformed", keyedName); break end
			local at = 0
			if fd.keyvs ~= nil then
				local keyID = _table[key]
				for i, kv in fd.keyvs do
					if kv[2] == keyID then at = i; break end
				end
			end
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then report.malformed = true; noteField("malformed", keyedName); break end
			-- a key this reader cannot name is skipped by its length (§2.4, §3.2)
			if at == 0 then
				report.unknown = report.unknown + 1; noteField("unknown", keyedName)
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
				if payload.at ~= payload.e then
					-- A SLOT WHOSE BODY DID NOT SPEND ITS L RESETS TO ITS DECLARED
					-- DEFAULTS (docs/SPEC-TABLES.md §3.2, §5): the engine's keyed
					-- read refills the slot, so a partial element never rides.
					report.malformed = true; noteField("malformed", keyedName)
					inst = d.new()
				end
				x = inst
			elseif e ~= nil and e.s == "enum" then
				x = enum_read(e, payload, report)
			else
				x = scalar_at(payload, e or {}, ek, report)
			end
			if at > 0 and dst ~= nil and x ~= nil then put(dst, at, x) end
		end
	end
	r.at = endpos
	return true
end

local array_body: any
local text_elem_bytes: any

-- AN ARRAY BODY (§2.9, §3): a body shorter than its header is damage, a
-- damaged count is malformed, a foreign element kind is one kind_mismatch, a
-- bounded array's excess is clamped, and elements are bounded by the body.
-- The second answer is the engine's `selected` (internal/tablewire decode.go,
-- arrayBody): false for a FOREIGN ELEMENT KIND (the body is not this array's),
-- a damaged count and a count past the list cap. §2.3 leaves an optional array
-- ABSENT on the element-kind case; a damaged count still marks it present.
read_array = function(fd: any, r: any, report: any, v: any): (boolean, boolean)
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; return false, false end
	local endpos = r.at + n
	local selected = true
	-- a body shorter than its header is damage, not an inert array
	-- (internal/tablewire wirecheck.go, arrayHeader)
	if n < 2 then report.malformed = true end
	if n >= 2 then
		-- THE HEADER IS READ AGAINST THE PARENT BUFFER (docs/SPEC-TABLES.md
		-- §2.9, the decoding cursor reads the count against the enclosing
		-- value buffer): a canonical LEB128
		-- count may be ten bytes and a body's `L` as short as two, so the count
		-- can finish past the body's own end while still sitting inside the
		-- parent. The engine reads it there (r.leb) and then bounds the ELEMENTS
		-- by `L`; passing the parent end keeps that asymmetry.
		selected = array_body(fd, new_reader(r.b, r.at, endpos), report, v, r.e)
	end
	r.at = endpos
	return true, selected
end

-- the body after its L: the element kind, the count, the elements. They land
-- in the holder's slot: a list is a fresh array, a fixed or counted array is
-- written in place with its count beside it, [N]uint8 and bytes(N) are buffers
-- (docs/PORT-LUAU.md, THE TYPE MAPPING). It answers whether the body
-- SELECTED this array, the engine's `selected` (internal/tablewire decode.go,
-- arrayBody): false for a foreign element kind, a damaged count and a count
-- past the list cap. A damaged count still leaves an optional FIELD present
-- (§2.3), so the presence bit is set here as the engine sets it.
array_body = function(fd: any, sub: any, report: any, v: any, headerEnd: number?): boolean
	-- headerEnd is the PARENT body's end for a field (read_array); an ARM's
	-- payload is its own frame, so the arm passes nil and the header is read
	-- against the arm's L exactly as the engine's arm reader does.
	local arrayName = _fieldName
	local head = new_reader(sub.b, sub.at, headerEnd or sub.e)
	local ek = r_u8(head)
	local count = r_leb(head)
	-- a count whose spelling ends past the body's own end CROSSED L: the
	-- measure, bounded by L, saw no count there
	local crossed = head.at > sub.e
	sub.at = if crossed then sub.e else head.at
	if head.bad then
		report.malformed = true; noteField("malformed", arrayName)
		if fd.present ~= nil and v ~= nil then v[fd.present] = true end
		return false
	end
	if ek ~= fd.ek then
		if not widens(ek, fd.ek) then report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", arrayName); return false end
		-- a list past the int32 storage cap refuses below and moves no
		-- counter (§2.9), the widening included; one whose count crossed L is
		-- damage below, and damage counts the widening (internal/tablewire
		-- capDamages)
		if not (fd.array == "list" and count > 0x7FFFFFFF and not crossed) then report.widened = report.widened + 1; noteField("widened", arrayName) end
	end
	local keep = count
	if fd.array == "list" then
		if count > 0x7FFFFFFF then
			-- THE STORAGE CAP (docs/SPEC-TABLES.md §2.9), read_map's rule:
			-- inside L the read's refusal, count_over_extent_cap when the walk
			-- ends; crossing L the region's damage
			if crossed then
				report.malformed = true; noteField("malformed", arrayName)
			else
				refuseCount(report)
			end
			return false
		end
	elseif count > fd.bound then
		keep = fd.bound
		report.clamped = report.clamped + 1; noteField("clamped", arrayName)
	end
	local slot = fd.slot
	local holder: any = nil
	if v ~= nil and slot ~= nil and slot ~= "" then holder = v end
	if fd.s == "bytes" then
		-- bytes(N) rides as an array of u8 (§2.5)
		local have = keep
		if sub.at + have > sub.e then have = sub.e - sub.at; report.malformed = true; noteField("malformed", arrayName) end
		if holder ~= nil then store_bytes(holder, slot, sub.b, sub.at, have, have) end
		sub.at = sub.at + have
		-- bytes left inside the array's L after its elements are damage
		-- (internal/tablewire decode.go, arrayBody)
		if have == keep and sub.at < sub.e then report.malformed = true; noteField("malformed", arrayName) end
		return true
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
	local spent = 0
	for i = 1, keep do
		if sub.at >= sub.e then report.malformed = true; noteField("malformed", arrayName); break end
		if not read_element(fd, sub, fd.e, dst, i, report, ek, lens) then break end
		-- A TEXT LIST'S TOTAL (docs/SPEC-TABLES.md §2.13): the running sum
		-- takes this element, and the first element that would carry it past
		-- the total is dropped with every later element, clamped counted once.
		if fd.total ~= nil then
			spent = spent + text_elem_bytes(fd.e, dst[i], if lens ~= nil then lens[i] else nil)
			if spent > fd.total then
				report.clamped = report.clamped + 1; noteField("clamped", arrayName)
				if fd.array == "list" then dst[i] = nil end
				got = i - 1
				break
			end
		end
		got = i
	end
	-- bytes left inside the array's L after its elements are damage
	-- (internal/tablewire decode.go, arrayBody): every declared element
	-- was read, yet the body still holds bytes. A clamped or text-total
	-- truncation leaves bytes by design and already refuses on its own
	-- counter, so it is not counted again here.
	if got == keep and keep == count and sub.at < sub.e then report.malformed = true; noteField("malformed", arrayName) end
	if fd.array == "counted" and holder ~= nil then holder[slot .. "Count"] = got end
	-- a list of pointers may hold null slots, which a Luau length cannot see:
	-- its live length rides as n, the table.pack convention, and the save reads it
	if fd.array == "list" and fd.e ~= nil and (fd.e.s == "ptr" or fd.e.s == "sblob" or fd.e.s == "bblob") then dst.n = got end
	return true
end

-- an element lands at its 1-based index; a [N]uint8 is a 0-based buffer
-- (docs/PORT-LUAU.md, THE TYPE MAPPING)
put = function(dst: any, i: number, x: any)
	if typeof(dst) == "buffer" then
		buffer.writeu8(dst, i - 1, x)
	else
		dst[i] = x
	end
end

-- the bytes one text-list element contributes to its total
-- (docs/SPEC-TABLES.md §2.13): a plain element's buffer length, or a ?element's value
-- when present, zero when absent.
text_elem_bytes = function(e: any, x: any, used: number?): number
	if e == nil or x == nil then return 0 end
	if e.s == "str" or e.s == "text" then
		-- a plain string element's USED bytes ride in its length companion
		if used ~= nil then return used end
		if typeof(x) == "buffer" then return buffer.len(x) end
		return 0
	end
	if e.s == "tab" then
		local vf = S.lookup(e.t).fields[1]
		if vf == nil then return 0 end
		if vf.present ~= nil and not x[vf.present] then return 0 end
		local b = x[vf.slot]
		if typeof(b) == "buffer" then
			local n = x[vf.slot .. "Length"]
			if n == nil then n = buffer.len(b) end
			return n
		end
	end
	return 0
end

read_element = function(fd: any, r: any, e: any, dst: any, i: number, report: any, ek: number, lens: any): boolean
	local elemName = _fieldName
	if e == nil then
		local x = scalar_at(r, {}, ek, report)
		if x == nil then return false end
		put(dst, i, x)
		return true
	end
	if e.s == "ptr" or e.s == "sblob" or e.s == "bblob" then
		local index = r_leb(r)
		if r.bad then report.malformed = true; noteField("malformed", elemName); return false end
		-- a null in a list is false, so the list is never cut short; a fixed or
		-- counted array stays nil-filled (docs/PORT-LUAU.md, THE TYPE MAPPING, Tables)
		local null: any = nil
		if fd.array == "list" then null = false end
		dst[i] = null
		_pending[#_pending + 1] = { holder = dst, key = i, index = index, d = e, null = null, name = elemName }
		return true
	elseif e.s == "enum" then
		local x, ok = enum_read(e, r, report)
		if not ok then return false end
		dst[i] = x
		return true
	elseif e.s == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then report.malformed = true; noteField("malformed", elemName); return false end
		local sub = new_reader(r.b, r.at, r.at + n)
		local inst = S.lookup(e.t).new()
		read_body(S.lookup(e.t), sub, inst, true)
		if sub.at ~= sub.e then report.malformed = true; noteField("malformed", elemName); inst = S.lookup(e.t).new() end
		r.at = r.at + n
		dst[i] = inst
		return true
	elseif e.s == "union" then
		-- THE ELEMENT IS RESET THE MOMENT IT IS REACHED (docs/SPEC-TABLES.md
		-- §3, §4): a repeat
		-- under a COUNTED or FIXED array's field id must not leave an arm an
		-- earlier occurrence decoded standing, even when its own framing is
		-- damaged. The engine's element() clears U and Tab before unionCell.
		-- A LIST is a fresh holder (array_body) whose tail the engine trims to
		-- the decoded prefix, so a failed element there stays absent and must
		-- not be planted as a None that the save would count.
		if fd.array ~= "list" then dst[i] = S.lookup(e.t).new() end
		local x, ok = read_union(e, r, report)
		if not ok then return false end
		dst[i] = x
		return true
	elseif e.s == "str" or e.s == "text" then
		-- a text element is kind 12: its own L, then L bytes (§2.13, §3)
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then report.malformed = true; noteField("malformed", elemName); return false end
		if not utf8_ok(r.b, r.at, n) then
			-- ill-formed UTF-8 or a zero byte in an element is malformed, and
			-- the element does not land: the list keeps the prefix the body
			-- covered, as the Go engine's element break leaves it (§2.13, §3)
			report.malformed = true; noteField("malformed", elemName)
			r.at = r.at + n
			return false
		end
		-- an UNBOUNDED TEXT element (docs/SPEC-TABLES.md §2.13.1) has no
		-- declared bound: the emitter spells it as a string with bound 0, and
		-- its bytes ride whole, never clamped.
		if e.s == "text" or (e.s == "str" and e.bound == 0) then
			dst[i] = copy_out(r.b, r.at, n)
		else
			local keep = n
			if e.bound ~= nil and keep > e.bound then keep = clamp_utf8(r.b, r.at, e.bound); report.clamped = report.clamped + 1; noteField("clamped", elemName) end
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

-- AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6, §3; docs/PORTING.md M18):
-- an arm's payload is exactly the bytes a field of its type puts after its own
-- framing prefix, so the arm is read by read_field, the field's own reader,
-- with the arm's L standing in for the field's length. What this function
-- adds is FRAMING ALONE, never a payload reader of its own:
--
--   - a length-shaped arm (text, bytes, an array, a table, a map) is read by
--     read_field from the arm's L, over a reader that ends where the arm's
--     payload ends, so its L is the field's L and every rule after it is the
--     field's;
--   - a fixed-width arm's L must be its kind's width, and a reference-shaped
--     arm's L must be exactly the reference it frames: either is the arm's own
--     framing damage, the union reading None and the parent reading on past L
--     (the engine tests the same facts, internal/tablewire decode.go arm);
--   - a POINTER arm's framing is checked BEFORE its reference is registered,
--     because the engine checks r.off == n before resolveCell, so a damaged
--     arm cannot produce a pointee-kind mismatch.
--
-- lpos is where the arm's L begins, sub the reader over its payload. The
-- answer is true when the arm's value is whole: read_field's third answer
-- says a payload was damaged inside its own L, and its second that an array
-- carried a foreign element kind; either reads None.
local function read_arm(arm: any, r: any, lpos: number, sub: any, u: any): boolean
	local s = arm.s
	if s == "bool" or s == "int" or s == "float" then
		local w = width_of(arm.k)
		if w > 0 and sub.e - sub.at ~= w then
			_report.malformed = true; noteField("malformed", _fieldName)
			return false
		end
		return (read_field(arm, sub, u))
	end
	if s == "ptr" or s == "enum" then
		if s == "ptr" then
			local framing = new_reader(sub.b, sub.at, sub.e)
			r_leb(framing)
			if framing.bad or framing.at ~= framing.e then
				_report.malformed = true; noteField("malformed", _fieldName)
				return false
			end
		end
		local ok = read_field(arm, sub, u)
		if ok and sub.at ~= sub.e then
			-- a reference-shaped arm's L is the byte count of the reference it
			-- frames (§3): an L that framed more is the arm's own damage
			_report.malformed = true; noteField("malformed", _fieldName)
			return false
		end
		return ok
	end
	if s == "union" then
		-- a NESTED UNION's field line has no L of its own: its arm line (the
		-- arm id reference, the kind, the arm's L) is the outer arm's payload
		return read_field(arm, sub, u) == true
	end
	local ok, selected, intact = read_field(arm, new_reader(r.b, lpos, sub.e), u)
	return ok == true and selected ~= false and intact ~= false
end

-- AN ARM HEADER IS A FIELD HEADER (§3, §2.6): the arm id reference, the arm's
-- kind byte, L, then L bytes of payload. An arm this reader cannot name is
-- unknown and the union reads None; a retyped arm is a kind mismatch. The
-- value is the union's { Type, <Arm> } (docs/PORT-LUAU.md, THE TYPE MAPPING).
read_union = function(fd: any, r: any, report: any): (any, boolean)
	local unionName = _fieldName
	local un = S.lookup(fd.t)
	local u = un.new()
	local ref = r_leb(r)
	if r.bad then report.malformed = true; noteField("malformed", unionName); return u, false end
	if ref == 0 then
		-- AN EXPLICIT NONE ARM IS NOT A WRITER'S SPELLING (§3.3 (c)): a
		-- conforming writer elides the field. An element's or a nested arm's
		-- None is positional and stays lawful.
		if _unionField == fd and fd.required ~= true then
			report.malformed = true; noteField("malformed", unionName); return u, false
		end
		return u, true
	end
	if ref > #_table then report.malformed = true; noteField("malformed", unionName); return u, false end
	local armID = _table[ref]
	local kind = r_u8(r)
	local lpos = r.at
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then report.malformed = true; noteField("malformed", unionName); return u, false end
	local sub = new_reader(r.b, r.at, r.at + n)
	local arm: any = nil
	for _, a in un.arms do
		if a.id == armID then arm = a; break end
	end
	if arm ~= nil then
		_ownerName = fd.t
		_fieldName = arm.name
	end
	local want = arm == nil and 0 or (arm.s == "void" and 32 or arm.k)
	if arm == nil then
		report.unknown = report.unknown + 1; noteField("unknown", idName(armID))
	elseif kind ~= want then
		if widens(kind, want) then
			if n == width_of(kind) then
				local x = scalar_at(sub, arm, kind, report)
				if x ~= nil then
					report.widened = report.widened + 1; noteField("widened", arm.name)
					u[arm.slot] = x
					u.Type = arm.tag
				end
			else report.malformed = true; noteField("malformed", arm.name) end
		else
			report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", arm.name)
		end
	elseif arm.s == "void" then
		-- a payload-free arm carries L = 0; an L that is not zero is that
		-- arm's own framing damage (docs/SPEC-TABLES.md §3)
		if n ~= 0 then
			report.malformed = true; noteField("malformed", arm.name)
		else
			u.Type = arm.tag
		end
	else
		if read_arm(arm, r, lpos, sub, u) then u.Type = arm.tag end
	end
	r.at = r.at + n
	return u, true
end

-- ONE FIELD (docs/SPEC-TABLES.md §3): every value read is stored in its slot
-- in the shape docs/PORT-LUAU.md THE TYPE MAPPING names; a string clamps at a
-- whole code point (§3, KIND 12).
-- It answers (ok, selected, intact): ok false stops the body; selected false
-- is an array of a FOREIGN ELEMENT KIND (§3); intact false is a payload
-- damaged inside its own L, which a field answers with its declared default
-- and a union arm (read_arm) with None.
read_field = function(fd: any, r: any, v: any, as_field: boolean?): any
	local fieldName = _fieldName
	local s = fd.s
	if s == "bool" or s == "int" or s == "float" then
		local x = scalar_at(r, fd, fd.k, _report)
		if x == nil then return false end
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = x end
		return true
	elseif s == "str" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; noteField("malformed", fieldName); return false end
		if not utf8_ok(r.b, r.at, n) then
			-- ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): the field
			-- reads its DECLARED DEFAULT, one malformed counts, and the parent
			-- reads on past L. An earlier occurrence of the field does not
			-- survive it (internal/tablewire decode.go field, FieldDefaultCell).
			_report.malformed = true; noteField("malformed", fieldName)
			if fd.slot ~= nil and v ~= nil then
				local def: string = fd.def or ""
				store_bytes(v, fd.slot, buffer.fromstring(def), 0, #def, #def)
			end
			r.at = r.at + n
			return true, nil, false
		end
		local keep = n
		if fd.bound ~= nil and n > fd.bound then keep = clamp_utf8(r.b, r.at, fd.bound); _report.clamped = _report.clamped + 1; noteField("clamped", fieldName) end
		if fd.slot ~= nil and v ~= nil then store_bytes(v, fd.slot, r.b, r.at, keep, keep) end
		r.at = r.at + n
		return true
	elseif s == "text" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; noteField("malformed", fieldName); return false end
		if not utf8_ok(r.b, r.at, n) then
			-- ILL-FORMED TEXT IS DAMAGE at any bound (docs/SPEC-TABLES.md §3,
			-- §4): one malformed counts and the parent reads on past L
			_report.malformed = true; noteField("malformed", fieldName)
			if fd.slot ~= nil and v ~= nil then v[fd.slot] = fd.def or "" end
			r.at = r.at + n
			return true, nil, false
		end
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
		if r.bad then _report.malformed = true; noteField("malformed", fieldName); return false end
		-- A NULL POINTER IS ELIDED, NOT WRITTEN (§3.1): an explicit index 0 in
		-- a non-required field is the non-canonical spelling §4 refuses. A
		-- pointer ARM and an array element keep their positional null.
		if index == 0 and as_field == true and fd.required ~= true then
			_report.malformed = true; noteField("malformed", fieldName); return false
		end
		_pending[#_pending + 1] = { holder = v, key = fd.slot, index = index, d = fd, name = fd.name or fieldName }
		return true
	elseif s == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; noteField("malformed", fieldName); return false end
		local sub = new_reader(r.b, r.at, r.at + n)
		local d = S.lookup(fd.t)
		local inst = d.new()
		if fd.slot ~= nil and v ~= nil then v[fd.slot] = inst end
		read_body(d, sub, inst, true)
		if sub.at ~= sub.e then
			-- A NESTED BODY THAT DID NOT SPEND ITS L FALLS BACK ON ITS DECLARED
			-- DEFAULTS (docs/SPEC-TABLES.md §3, §5): the engine refills the
			-- body, and the slot names the reset value rather than the partial
			-- read.
			_report.malformed = true; noteField("malformed", fieldName)
			inst = d.new()
			if fd.slot ~= nil and v ~= nil then v[fd.slot] = inst end
			r.at = r.at + n
			return true, nil, false
		end
		r.at = r.at + n
		return true
	elseif s == "union" then
		local x, ok = read_union(fd, r, _report)
		if not ok then return false end
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
	_report.malformed = true; noteField("malformed", fieldName)
	return false
end


-- ---- THE CYCLE CHECK (§3.1, §4; internal/tablewire decodenodes.go cyclic) --
--
-- No writer produces a cyclic wire, so a cycle in the LOADED graph is damage
-- and the whole load is refused. The vertices are the root and every record
-- the load placed; the edges are every pointer slot a vertex's by-value
-- closure stores, a slot under a false guard included, because the load
-- stored it. A byte buffer is no vertex. A cycle anywhere refuses, one the
-- root never reaches included.

-- the live count of a stored array: a list's length, a counted array's count
-- beside it, a fixed array's bound (§2.9)
local function stored_count(fd: any, holder: any, val: any): number
	if type(val) ~= "table" then
		return 0
	end
	if fd.array == "list" then
		return #val
	end
	if fd.array == "counted" then
		local c = holder[fd.slot .. "Count"]
		if type(c) ~= "number" then
			return 0
		end
		return math.min(c, fd.bound)
	end
	return fd.bound or 0
end

local stored_edges: any

local function stored_arm_edges(fd: any, u: any, index: { [any]: number }, out: { number })
	if type(u) ~= "table" or u.Type == nil or u.Type == 0 then
		return
	end
	local arm = S.lookup(fd.t).arms[u.Type]
	if arm == nil or arm.slot == nil then
		return
	end
	local val = u[arm.slot]
	local shape = arm.s
	if shape == "ptr" then
		if val then
			out[#out + 1] = index[val]
		end
	elseif shape == "tab" then
		stored_edges(S.lookup(arm.t), val, index, out)
	elseif shape == "union" then
		stored_arm_edges(arm, val, index, out)
	elseif shape == "arr" and arm.e ~= nil and arm.e.s == "ptr" then
		for i = 1, stored_count(arm, u, val) do
			if val[i] then
				out[#out + 1] = index[val[i]]
			end
		end
	end
end

-- stored_edges appends the vertex index of every pointer target v's by-value
-- closure stores. The recursion is by-value nesting only, whose depth the
-- schema fixes (§2); it never follows a pointer.
stored_edges = function(d: any, v: any, index: { [any]: number }, out: { number }): { number }
	if type(v) ~= "table" then
		return out
	end
	for _, fd in d.fields do
		if fd.slot == nil or (fd.present ~= nil and not v[fd.present]) then
			continue
		end
		local val = v[fd.slot]
		local s = fd.s
		if s == "ptr" then
			if val then
				out[#out + 1] = index[val]
			end
		elseif s == "tab" then
			stored_edges(S.lookup(fd.t), val, index, out)
		elseif s == "union" then
			stored_arm_edges(fd, val, index, out)
		elseif s == "map" then
			if type(val) == "table" then
				local entry = S.lookup(fd.t)
				for _, ev in val do
					stored_edges(entry, ev, index, out)
				end
			end
		elseif s == "arr" and type(val) == "table" and fd.e ~= nil then
			local e = fd.e
			local n = stored_count(fd, v, val)
			if e.s == "ptr" then
				for i = 1, n do
					if val[i] then
						out[#out + 1] = index[val[i]]
					end
				end
			elseif e.s == "union" then
				for i = 1, n do
					stored_arm_edges(e, val[i], index, out)
				end
			elseif e.s == "tab" then
				local td = S.lookup(e.t)
				for i = 1, n do
					stored_edges(td, val[i], index, out)
				end
			end
		end
	end
	return out
end

-- cyclic reports whether the loaded graph holds a cycle. Each vertex carries
-- one of three marks and is ENTERED ONCE: a pointer to an OPEN vertex is a
-- cycle, and a pointer to a CLOSED one is a legal share. The walk holds its
-- own stack, so it is linear in vertices plus slots and a pointer chain is
-- never a recursion depth.
local function cyclic(): boolean
	local verts: { any } = { { d = _rootDesc, v = _rootValue } }
	local index: { [any]: number } = { [_rootValue] = 1 }
	for _, n in _nodes do
		if n.kind == "t" and index[n.v] == nil then
			verts[#verts + 1] = { d = n.desc, v = n.v }
			index[n.v] = #verts
		end
	end
	local mark: { number } = table.create(#verts, 0) -- 0 unvisited, 1 open, 2 closed
	local stack: { any } = {}
	for start = 1, #verts do
		if mark[start] ~= 0 then
			continue
		end
		mark[start] = 1
		stack[1] = { at = start, out = stored_edges(verts[start].d, verts[start].v, index, {}), i = 0 }
		while #stack > 0 do
			local top = stack[#stack]
			if top.i >= #top.out then
				mark[top.at] = 2
				stack[#stack] = nil
			else
				top.i += 1
				local t = top.out[top.i]
				if t ~= nil then
					if mark[t] == 1 then
						return true
					end
					if mark[t] == 0 then
						mark[t] = 1
						stack[#stack + 1] = { at = t, out = stored_edges(verts[t].d, verts[t].v, index, {}), i = 0 }
					end
				end
			end
		end
	end
	return false
end

-- RESOLUTION (§3.1): 0 is null, 1 is the root, k + 1 is record k. A record of
-- another kind than the slot declares is a kind mismatch, an index past the
-- numbering is malformed, and a numbering that could not be read resolves
-- nothing.
local function resolve(report: any)
	-- §3.1: "No index ever resolves against a numbering that failed." A node
	-- table that could not be read whole leaves every pointer null and counts
	-- nothing here; the malformed event the framing counted stands alone.
	if not _numberingGood then return end
	-- a blob node is owned by its record: every slot past the first that names
	-- it counts one blob_shared and keeps the one node, never a copy
	-- (docs/SPEC-TABLES.md §2.5, §3.1)
	local landed: { [any]: boolean } = {}
	for _, p in _pending do
		local value: any = nil
		if p.index ~= 0 then
			if p.index == 1 then
				if p.d.s == "ptr" and p.d.t == _rootDesc.name then value = _rootValue
				else report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", p.name) end
			elseif p.index - 2 >= #_nodes then
				report.malformed = true; noteField("malformed", p.name)
			else
				local n = _nodes[p.index - 1]
				-- §3.1: a placeholder is a refused record; every pointer
				-- naming it reads null, counted once at the node, not here.
				-- A placeholder of a type this reader DECLARES is not a newer
				-- writer's type: naming it at a slot that is not its target is
				-- damage, and the whole load refuses.
				if n ~= nil then
					if n.kind == "x" then
						if n.declared then report.kindMismatch = report.kindMismatch + 1 end
					elseif p.d.s == "sblob" then
						if n.kind == "s" then
							if landed[n] then report.blobShared = (report.blobShared or 0) + 1; noteField("blobShared", p.name) end
							landed[n] = true
							value = n.blob
						else report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", p.name) end
					elseif p.d.s == "bblob" then
						if n.kind == "b" then
							if landed[n] then report.blobShared = (report.blobShared or 0) + 1; noteField("blobShared", p.name) end
							landed[n] = true
							value = n.blob
						else report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", p.name) end
					elseif n.kind == "t" and n.desc.name == p.d.t then
						value = n.v
					else
						report.kindMismatch = report.kindMismatch + 1; noteField("kindMismatch", p.name)
					end
				end
			end
		end
		if value == nil then value = p.null end
		if p.holder ~= nil and p.key ~= nil then p.holder[p.key] = value end
	end
end

-- a field's guard, a conjunction of (bool slot, required value) pairs on the
-- descriptor, holds when every pair matches the instance's storage
-- (docs/SPEC-TABLES.md §2.3, §3): a required field under a false guard is not
-- demanded, because the branch writes none of its fields.
local function guard_holds(fd: any, v: any): boolean
	if fd.guard == nil then return true end
	for _, pair in fd.guard do
		if v[pair[1]] ~= pair[2] then return false end
	end
	return true
end

-- A FIELD THE WRITER WOULD HAVE ELIDED (docs/SPEC-TABLES.md §3): a value a
-- conforming writer stores by its absence, never with a field header. A read
-- that meets the field anyway is the non-canonical spelling §4 refuses.
local function same_bytes(v: any, slot: string, def: string?)
	local b = v[slot]
	local n = v[slot .. "Length"]
	local d = def or ""
	if typeof(b) ~= "buffer" then return n == nil and #d == 0 end
	if n == nil then n = buffer.len(b) end
	if n ~= #d then return false end
	if n == 0 then return true end
	return buffer.readstring(b, 0, n) == d
end

local function elem_default(e: any, val: any)
	if e == nil then return val == nil or val == 0 end
	local istab = typeof(val) == "table"
	if e.s ~= "union" and istab then return false end
	local s = e.s
	if s == "bool" then return val == false or val == nil end
	if s == "int" then return val == 0 end
	if s == "float" then return val == 0 or val == 0.0 end
	if s == "enum" then return val == 0 or val == nil end
	if s == "ptr" or s == "sblob" or s == "bblob" then return not val end
	if s == "union" then return val == nil or (istab and val.Type == 0) end
	if s == "str" or s == "text" then
		if typeof(val) ~= "buffer" then return true end
		return buffer.len(val) == 0
	end
	return false
end

local field_at_default: any

field_at_default = function(fd: any, v: any)
	if fd.required == true then return false end
	if fd.present ~= nil then return false end
	if v == nil or fd.slot == nil then return false end
	local s = fd.s
	local val = v[fd.slot]
	if s == "ptr" or s == "sblob" or s == "bblob" then return false end
	if s == "str" or s == "bytes" then return same_bytes(v, fd.slot, fd.def) end
	if s == "text" then return typeof(val) ~= "buffer" or buffer.len(val) == 0 end
	if s == "map" then return false end
	if s == "arr" then
		if typeof(val) == "buffer" then return false end
		if fd.array == "keyed" then
			if val == nil then return true end
			for i = 1, fd.bound or 0 do if val[i] ~= nil then return false end end
			return true
		end
		if fd.array == "list" then
			local n = 0
			if val ~= nil then n = val.n or #val end
			return n == 0
		end
		if fd.array == "counted" then return (v[fd.slot .. "Count"] or 0) == 0 end
		if fd.e ~= nil and (fd.e.s == "tab" or fd.e.s == "ptr" or fd.e.s == "sblob" or fd.e.s == "bblob" or fd.e.s == "map" or fd.e.s == "arr") then return false end
		for i = 1, fd.bound or 0 do
			if not elem_default(fd.e, val ~= nil and val[i] or nil) then return false end
		end
		return true
	end
	if s == "union" then return false end
	if s == "tab" then return false end
	if s == "bool" then return (val == true) == (fd.def == true) end
	-- A FLOAT'S DEFAULT IS A BIT PATTERN, and -0 and +0 are different
	-- spellings; the writer decides elision on the raw bits, so the load leaves
	-- a float to the writer's own rule rather than comparing numbers here.
	if s == "float" then return false end
	if s == "int" then return val == (fd.def or 0) end
	-- An enum's default is the VARIANT its wire names, and a first variant is
	-- not the zero value a fresh enum holds; the writer's elision rule is its
	-- own, so the load leaves an enum to it.
	if s == "enum" then return false end
	return false
end

read_body = function(desc: any, r: any, v: any, nested: boolean): boolean
	local byId = fields_by_id(desc)
	local pos = field_positions(desc)
	local seen: { [integer]: boolean } = {}
	local lastPos = 0
	local afterNode = false
	for _ = 1, math.huge do
		local ref = r_leb(r)
		if r.bad then _report.malformed = true; return false end
		if ref == 0 then
			-- `| required` (docs/SPEC-TABLES.md §2.3): a record body that ends
			-- without a required field whose guard holds is missing_required,
			-- naming the field; a guard-off field is not demanded.
			local missing = {}
			for _, fd in desc.fields do
				if fd.required == true and not seen[fd.id] and guard_holds(fd, v) then
					missing[#missing + 1] = fd.name
				end
			end
			if #missing > 0 then
				_report.missingRequired = (_report.missingRequired or 0) + 1
				if _report.missingName == nil then _report.missingName = table.concat(missing, ", ") end
				return false
			end
			return true
		end
		if afterNode then _report.malformed = true; return false end
		if ref > #_table then _report.malformed = true; return false end
		if r.at >= r.e then _report.malformed = true; return false end
		local kind = r_u8(r)
		local id = _table[ref]
		-- a repeated field id in one body is damage (docs/SPEC-TABLES.md
		-- §6.5, wirecheck's R5); the value still lands last-wins on the walk
		if seen[id] then
			_report.duplicate = (_report.duplicate or 0) + 1
			local dup = byId[id]
			noteField("duplicate", if dup ~= nil then dup.name else idName(id))
		end
		seen[id] = true
		if id == BuildVersionId or id == MessageVocabularyId then _report.malformed = true; return false end
		if id == NodeTableId then
			if nested then _report.malformed = true; return false end
			if desc.variable then
				if not skip(r, kind) then _report.malformed = true; return false end
				afterNode = true
				continue
			end
		end
		local fd = byId[id]
		if fd ~= nil then
			_ownerName = desc.name
			_fieldName = fd.name
			-- A BODY'S FIELDS FOLLOW THE WRITER'S DECLARATION ORDER (§3):
			-- a field after one that comes later in the declaration is a
			-- spelling no conforming writer emits.
			local p = pos[id]
			if p ~= nil then
				if p <= lastPos then _report.malformed = true; noteField("malformed", fd.name); return false end
				lastPos = p
			end
		end
		if fd == nil then
			_report.unknown = _report.unknown + 1; noteField("unknown", idName(id))
			if not skip(r, kind) then _report.malformed = true; noteField("malformed", _fieldName); return false end
		elseif kind ~= fd.k then
			if widens(kind, fd.k) then
				local x = scalar_at(r, fd, kind, _report)
				if x == nil then return false end
				if fd.slot ~= nil and v ~= nil then v[fd.slot] = x end
				if fd.present ~= nil and v ~= nil then v[fd.present] = true end
				seen[id] = true
				_report.widened = _report.widened + 1; noteField("widened", fd.name)
			else
				_report.kindMismatch = _report.kindMismatch + 1; noteField("kindMismatch", fd.name)
				if not skip(r, kind) then _report.malformed = true; noteField("malformed", fd.name); return false end
			end
		else
			_unionField = if fd.s == "union" then fd else nil
			local ok, selected = read_field(fd, r, v, true)
			_unionField = nil
			if not ok then return false end
			if selected ~= false and field_at_default(fd, v) then
				_report.malformed = true; noteField("malformed", fd.name); return false
			end
			seen[id] = true
			-- ?T: a field that rode is present (§2.3) — EXCEPT an optional
			-- array whose body carries a FOREIGN ELEMENT KIND: the array is not
			-- this field's, so its declared default is ABSENT (§2.3, §3) and
			-- the save must elide it, as the engine does.
			if fd.present ~= nil and v ~= nil and selected ~= false then v[fd.present] = true end
		end
	end
	return false
end

-- Walk a file whose id table has opened. Keeping this separate lets the
-- strict wrapper retain wire_damaged for unopened input and its field/counter
-- diagnostics for a decoded value that the damage rule now discards (§4).
local function loadOpened(root: string, bytes: buffer, report: any, o: any): any
	_walks += 1
	_table = o.table
	_report = report
	_countRefused = false
	_countReport = nil
	_countNonFinite = nil
	_countField = {}
	_nonFinite = nil
	_firstField = {}
	_pending = {}
	_nodes = {}
	local desc = S.lookup(root)
	_rootDesc = desc
	local v = desc.new()
	_rootValue = v
	-- ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST ENTRY IS
	-- MALFORMED (docs/SPEC-TABLES.md §3), because no field claims it and the
	-- two ends of the file have met. The engine checks the terminator before
	-- any body is read and reads nothing when bytes follow it, so the body's
	-- fields are not counted and every counter stays 0.
	local walk = new_reader(bytes, 1, o.e)
	while true do
		local ref = r_leb(walk)
		if walk.bad then break end
		if ref == 0 then
			if walk.at ~= o.e then report.malformed = true; return nil end
			break
		end
		if ref > #_table or walk.at >= walk.e then break end
		local kind = r_u8(walk)
		if not skip(walk, kind) then break end
	end
	local raw: any = {}
	if desc.variable then raw = node_table_scan(bytes, o.table, o.e, report) end
	-- §3.1: a node table that failed to read whole is nothing; no index
	-- resolves against it, so resolve() reads every pointer null.
	_numberingGood = raw ~= nil
	if raw ~= nil then
		local place = place_types(desc)
		for _, rec in raw do
			local tid = rec.tid
			if tid == StringBlobId and desc.place.str then
				if not utf8_ok(rec.body, 0, buffer.len(rec.body)) then
					-- §3.1: a refused record KEEPS ITS INDEX, so it takes a
					-- placeholder slot; a pointer naming it resolves to nothing
					report.malformed = true
					_nodes[#_nodes + 1] = { kind = "x" }
				else
					_nodes[#_nodes + 1] = { kind = "s", blob = rec.body }
				end
			elseif tid == BytesBlobId and desc.place.bytes then
				_nodes[#_nodes + 1] = { kind = "b", blob = rec.body }
			else
				local nc = place[tid]
				if nc == nil then
					-- §3.1: a type id this reader cannot name is counted once
					-- here and KEEPS ITS INDEX; the placeholder grows the array.
					-- A type it DECLARES at a slot that is not its target is
					-- damage, so the placeholder remembers that it was named.
					report.unknown = report.unknown + 1
					noteField("unknown", idName(tid))
					local declared = S.tableId(tid) or tid == StringBlobId or tid == BytesBlobId
					_nodes[#_nodes + 1] = { kind = "x", declared = declared }
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
	-- a count past the storage cap answers the report as it stood at the
	-- refusal (§2.9)
	if _countRefused then restoreCount(report) end
	-- A CYCLIC WIRE IS DAMAGE (§3.1, §4): the stored graph is checked once
	-- every index has resolved
	if _numberingGood and not _countRefused and not report.malformed and cyclic() then
		report.malformed = true
	end
	-- A NaN OR AN INFINITY REFUSES THE READ, walk AND STRICT ALIKE
	-- (docs/SPEC-TABLES.md §3.3): nothing of the value lands, and the report
	-- names the field, non_finite_float: <Owner>.<field>.
	if _nonFinite ~= nil then
		report.nonFiniteFloat = _nonFinite
		report.reason = "non_finite_float: " .. _nonFinite
		report.refused = (report.refused or 0) + 1
		return nil
	end
	-- THE STORAGE CAP REFUSAL (§2.9): refused by name, malformed unset, no
	-- counter moved, the partial value discarded
	if _countRefused then
		report.refused = (report.refused or 0) + 1
		report.reason = "count_over_extent_cap"
		return nil
	end
	if report.malformed then return nil end
	return v
end

-- loadWalk returns a value only when the wire is whole. walk
-- evolution is not damage: malformed input is discarded on every read (§4).
local integrityOpen: (buffer) -> buffer?

-- loadBody is the walk read of a file whose integrity trailer, if its
-- root declares one, is already off: the id table, the node table and the
-- root body.
local function loadBody(root: string, bytes: buffer, report: any): any
	local o = open(bytes, report)
	if o == nil then return nil end
	return loadOpened(root, bytes, report, o)
end

-- A ROOT THAT DECLARES INTEGRITY (docs/SPEC-TABLES.md §2.14) opens its
-- trailer first, as the strict read does; a trailer that does not match is the
-- whole-file damage named integrity_mismatch.
function L.loadWalk(root: string, bytes: buffer, report: any): any
	local d = S.lookup(root)
	if d ~= nil and d.fixed then
		if bytes == nil or buffer.len(bytes) < 1 then
			report.refused = (report.refused or 0) + 1
			report.reason = "truncated"
			return nil
		end
		local fx: any = {}
		local out = F.loadWalk(d, nil :: any, bytes, fx)
		if out == nil then
			report.reason = fx.reason
			if fx.field ~= nil then report.field = fx.field end
			report.refused = (report.refused or 0) + 1
			return nil
		end
		if #out ~= 1 then
			report.reason = if #out == 0 then "truncated" else "batch_too_large"
			report.refused = (report.refused or 0) + 1
			return nil
		end
		report.unknown = (report.unknown or 0) + (fx.unknown or 0)
		report.kindMismatch = (report.kindMismatch or 0) + (fx.kindMismatch or 0)
		report.clamped = (report.clamped or 0) + (fx.clamped or 0)
		report.widened = (report.widened or 0) + (fx.widened or 0)
		report.duplicate = (report.duplicate or 0) + (fx.duplicate or 0)
		return out[1]
	end
	if d.integrity ~= nil and d.integrity ~= "" and bytes ~= nil and buffer.len(bytes) >= 1 and buffer.readu8(bytes, 0) == 1 then
		local body = integrityOpen(bytes)
		if body == nil then
			report.malformed = true
			report.reason = "integrity_mismatch"
			return nil
		end
		bytes = body
	end
	return loadBody(root, bytes, report)
end

-- the counters that refuse, in TableReport.Err()'s order (docs/SPEC-TABLES.md
-- §6.5), each with its §11 name; schema_load.luau spells kind_mismatch kindMismatch.
-- blob_shared sits where Go's Err() checks BlobShared, between malformed and
-- clamped (§2.5), so a wire that is both clamped and blob-shared refuses as
-- blob_shared.
-- A REPEATED MAP KEY IS DAMAGE (docs/plans/strict-only.md decision 5): the
-- strict read refuses it as malformed, never last-wins.
local REFUSING = { "missingRequired", "malformed", "blobShared", "clamped", "kindMismatch", "duplicate" }
local NAMES: { [string]: string } = {
	missingRequired = "missing_required",
	malformed = "malformed",
	blobShared = "blob_shared",
	clamped = "clamped",
	kindMismatch = "kind_mismatch",
	duplicate = "malformed",
}

-- a report at rest, in schema_load.luau's shape (docs/SPEC-TABLES.md §4)
local function fresh(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, missingRequired = 0, blobShared = 0 }
end

local function moved(report: any, k: string): boolean
	if k == "malformed" then return report.malformed == true end
	return report[k] > 0
end

-- an id this build cannot name has no name, so it is spelled by its wire id
-- (docs/SPEC-TABLES.md §11: a refusal names the field)
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
integrityOpen = function(b: buffer): buffer?
	local n = buffer.len(b)
	if n < 5 or buffer.readu32(b, n - 4) ~= crc32c(b, n - 4) then return nil end
	local out = buffer.create(n - 4)
	buffer.copy(out, 0, b, 0, n - 4)
	return out
end

-- THE FRAMING WALK (docs/SPEC-TABLES.md §3): a field header is an id
-- reference and a kind byte, and every kind's payload is stepped over by its
-- own framing, the rule schema_load.luau's skip states. It reads no value.
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
-- payload table of docs/SPEC-TABLES.md §3, the rule schema_load.luau's skip states
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
	if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 then
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

-- THE ID TABLE'S FIRST-USE ORDER (docs/SPEC-TABLES.md §3): a writer interns an
-- id when its reference is written, so the table is the order the file's id
-- references first name each entry. A walk of the framing alone checks that
-- order, including the references inside fields this reader cannot name, which
-- is where the value walk deliberately skips. The check refuses an unused
-- entry and an entry whose first use comes out of order; framing damage it
-- cannot step over is left to the reads that report it.
idTableOrder = function(b: buffer, at: number, e: number, ids: { integer }): boolean
	local used: { [integer]: boolean } = {}
	local nextPos = 1
	local markID: any
	local walkPayload: any
	local walkContent: any
	local walkBodyIDs: any

	markID = function(id: integer): boolean
		if used[id] then return true end
		used[id] = true
		if nextPos > #ids or ids[nextPos] ~= id then return false end
		nextPos = nextPos + 1
		return true
	end

	local function markRef(ref: number): boolean
		if ref == 0 or ref > #ids then return true end
		return markID(ids[ref])
	end

	-- walkContent reads one payload AFTER its own L, which is what a union's arm
	-- payload is: the arm header already carries the L.
	walkContent = function(s: number, stop: number, kind: number, depth: number): boolean
		if depth > 128 then return true end
		if kind == 12 or kind == 31 or kind == 32 then return true end
		if kind == 13 then return walkBodyIDs(s, stop, false, depth + 1) end
		if kind == 14 or kind == 16 then
			if stop - s < 2 then return true end
			local ek = buffer.readu8(b, s)
			local count, p = leb(b, s + 1, stop)
			if count == nil then return true end
			if kind == 16 then
				for _ = 1, count do
					local key, q = leb(b, p, stop)
					if key == nil then return true end
					if not markRef(key) then return false end
					local elen, after = leb(b, q, stop)
					if elen == nil or after + elen > stop then return true end
					-- A KEYED TRIPLE'S L FRAMES ITS ELEMENT WHOLE: the content after
					-- it is already unframed, unlike a positional element's line.
					if not walkContent(after, after + elen, ek, depth + 1) then return false end
					p = after + elen
				end
				return true
			end
			if width_of(ek) > 0 then return true end
			for _ = 1, count do
				local nxt = stepOver(b, p, stop, ek)
				if nxt == nil then return true end
				if not walkPayload(p, nxt, ek, depth + 1) then return false end
				p = nxt
			end
			return true
		end
		if kind == 30 then
			local ref = leb(b, s, stop)
			if ref == nil then return true end
			return markRef(ref)
		end
		if kind == 15 then
			local ref, p = leb(b, s, stop)
			if ref == nil then return true end
			if not markRef(ref) then return false end
			if ref == 0 then return true end
			if p >= stop then return true end
			local kb = buffer.readu8(b, p)
			local n, after = leb(b, p + 1, stop)
			if n == nil or after + n > stop then return true end
			return walkContent(after, after + n, kb, depth + 1)
		end
		return true
	end

	-- walkPayload consumes a field's or element's own L before walkContent.
	walkPayload = function(s: number, stop: number, kind: number, depth: number): boolean
		if depth > 128 then return true end
		if width_of(kind) > 0 then return true end
		if kind == 12 or kind == 31 or kind == 32 then return true end
		if kind == 13 or kind == 14 or kind == 16 then
			local n, p = leb(b, s, stop)
			if n == nil or p + n > stop then return true end
			return walkContent(p, p + n, kind, depth + 1)
		end
		return walkContent(s, stop, kind, depth + 1)
	end

	walkBodyIDs = function(s: number, stop: number, root: boolean, depth: number): boolean
		local body = walkBody(b, s, stop, ids)
		if body == nil then return true end
		for _, f in body.fields do
			if not markID(f.id) then return false end
			if f.id == NodeTableId and root then
				local n, ps = leb(b, f.ps, f.e)
				if n == nil or ps + n > f.e then return true end
				local count, p = leb(b, ps, ps + n)
				if count == nil then return true end
				for _ = 1, count do
					local tref, q = leb(b, p, ps + n)
					if tref == nil or tref == 0 or tref > #ids then return true end
					if not markRef(tref) then return false end
					local tlen, after = leb(b, q, ps + n)
					if tlen == nil or after + tlen > ps + n then return true end
					local tid = ids[tref]
					if tid ~= StringBlobId and tid ~= BytesBlobId then
						if not walkBodyIDs(after, after + tlen, false, depth + 1) then return false end
					end
					p = after + tlen
				end
			elseif not walkPayload(f.ps, f.e, f.kind, depth + 1) then
				return false
			end
		end
		return true
	end

	if not walkBodyIDs(at, e, true, 0) then return false end
	return nextPos > #ids
end

-- the id table and the root body's span (docs/SPEC-TABLES.md §3: the id
-- table's count is the file's last eight bytes and its ids precede it), as
-- schema_load.luau's open reads them, except that a count that does not frame (a
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

-- the report's reset: every counter at rest, no reason, no field
local function reset(report: any)
	report.malformed = false
	report.refused = 0
	report.unknown = 0
	report.kindMismatch = 0
	report.clamped = 0
	report.widened = 0
	report.duplicate = 0
	report.missingRequired = 0
	report.nonFiniteFloat = nil
	report.blobShared = 0
	report.missingName = nil
	report.reason = nil
	report.field = nil
	report.unknownField = nil
	report.duplicateField = nil
end


-- THE VARIABLE FORM'S STRICT READ (docs/SPEC-TABLES.md §6.5): the one answer
-- for form 1, over the walk walk above. It resets the caller's report,
-- applies the file's own framing (integrity, the id table and the root body's
-- terminator) and then the refusing counters in TableReport.Err()'s order, so a
-- wire always answers one name. R.ReadStrict's fixed-form dispatch lives in
-- schema_refusal.luau; this is the half a form-1 file reaches, including a declared
-- fixed root handed its form-1 wire.
-- THE WIRE-ALONE CONDITIONS THE VALUE WALK DOES NOT JUDGE (docs/SPEC-TABLES.md
-- §6.5, internal/tablewire wirecheck.go): skipped is not unread, so an unknown
-- field's framed bytes are walked by the same rules as a known one's, a pointer
-- index is judged against the numbering wherever it rides, and the node
-- table's own framing (its count, its records' type references and bodies)
-- is checked even at a root whose descriptor is not of the variable class.
local function wireDamage(b: buffer, top: any, ids: { integer }, rep: any): boolean
	local recordCount = 0
	local recs: { any } = {}
	local nodeSegs = 0
	for _, s in top.fields do
		if s.id == NodeTableId then
			nodeSegs += 1
			if nodeSegs > 1 then return true end
			if s.kind ~= 12 then return true end
			local n, ps = leb(b, s.ps, s.e)
			if n == nil or ps + n > s.e then return true end
			local c, p = leb(b, ps, ps + n)
			if c == nil then return true end
			while p < ps + n do
				local tref, q = leb(b, p, ps + n)
				if tref == nil or tref == 0 or tref > #ids then return true end
				local tlen, r = leb(b, q, ps + n)
				if tlen == nil or r + tlen > ps + n then return true end
				recs[#recs + 1] = { tid = ids[tref], s = r, e = r + tlen }
				p = r + tlen
			end
			if c ~= #recs then return true end
			recordCount = #recs
		end
	end
	local edges: { [number]: number } = {}
	local children: { { number } } = {}
	local curEdges: { number } = {}
	local depth = 0
	local checkFields: any
	local scanPayload: any
	local scanContent: any
	local function scanScalar(at: number, kind: number): boolean
		if kind == 1 then
			return buffer.readu8(b, at) > 1
		elseif kind == 10 then
			if bit32.band(buffer.readu32(b, at), 0x7F800000) == 0x7F800000 then
				refuseNonFinite({ name = "?" })
				rep.nonFiniteFloat = _nonFinite
			end
		elseif kind == 11 then
			if bit32.band(buffer.readu32(b, at + 4), 0x7FF00000) == 0x7FF00000 then
				refuseNonFinite({ name = "?" })
				rep.nonFiniteFloat = _nonFinite
			end
		end
		return false
	end
	local function scanArrayBody(s: number, e: number): boolean
		if e - s < 2 then return true end
		local ek = buffer.readu8(b, s)
		local count, p = leb(b, s + 1, e)
		if count == nil then return true end
		if count > 0x7fffffff then
			refuseCount(rep)
			return false
		end
		local w = width_of(ek)
		if w > 0 then
			if p + count * w > e then return true end
			if ek == 1 or ek == 10 or ek == 11 then
				for i = 0, count - 1 do
					if scanScalar(p + i * w, ek) then return true end
				end
			end
			p += count * w
		else
			for _ = 1, count do
				local nxt = stepOver(b, p, e, ek)
				if nxt == nil then return true end
				if scanPayload(p, nxt, ek) then return true end
				p = nxt
			end
		end
		-- bytes left inside an array's or map's L after its elements are damage
		return p ~= e
	end
	local function scanKeyedBody(s: number, e: number): boolean
		if e - s < 2 then return true end
		local ek = buffer.readu8(b, s)
		local count, p = leb(b, s + 1, e)
		if count == nil then return true end
		if count > 0x7fffffff then
			refuseCount(rep)
			return false
		end
		local seenKeys: { [number]: boolean } = {}
		for _ = 1, count do
			local key, q = leb(b, p, e)
			if key == nil or key == 0 or key > #ids then return true end
			if seenKeys[key] then return true end
			seenKeys[key] = true
			local n, after = leb(b, q, e)
			if n == nil or after + n > e then return true end
			if scanContent(after, after + n, ek) then return true end
			p = after + n
		end
		-- bytes left inside a keyed array's L after its slots are damage
		return p ~= e
	end
	scanContent = function(s: number, e: number, kind: number): boolean
		local w = width_of(kind)
		if w > 0 then
			if e - s ~= w then return true end
			return scanScalar(s, kind)
		end
		if kind == 12 then
			return not utf8_ok(b, s, e - s)
		elseif kind == 31 then
			return false
		elseif kind == 32 then
			return e ~= s
		elseif kind == 13 or kind == 14 or kind == 16 then
			if depth >= 128 then return true end
			depth += 1
			local bad = false
			if kind == 13 then
				local inner = walkBody(b, s, e, ids)
				bad = inner == nil or inner.term + 1 ~= e or checkFields(inner.fields, false)
			elseif kind == 14 then
				bad = scanArrayBody(s, e)
			else
				bad = scanKeyedBody(s, e)
			end
			depth -= 1
			return bad
		elseif kind == 17 or kind == 30 or kind == 15 then
			return scanPayload(s, e, kind)
		end
		return true
	end
	scanPayload = function(at: number, e: number, kind: number): boolean
		local w = width_of(kind)
		if w > 0 then
			if e - at ~= w then return true end
			return scanScalar(at, kind)
		end
		if kind == 17 then
			local idx, nxt = leb(b, at, e)
			if idx == nil or nxt ~= e or idx > recordCount + 1 then return true end
			if idx ~= 0 then
				edges[idx] = (edges[idx] or 0) + 1
				curEdges[#curEdges + 1] = idx
			end
			return false
		elseif kind == 30 then
			local ref, nxt = leb(b, at, e)
			return ref == nil or nxt ~= e or ref > #ids
		elseif kind == 12 or kind == 31 or kind == 32 or kind == 13 or kind == 14 or kind == 16 then
			local n, bs = leb(b, at, e)
			if n == nil or bs + n ~= e then return true end
			return scanContent(bs, bs + n, kind)
		elseif kind == 15 then
			local ref, p = leb(b, at, e)
			if ref == nil or ref > #ids then return true end
			if ref == 0 then return p ~= e end
			if p >= e then return true end
			local kb = buffer.readu8(b, p)
			local n, after = leb(b, p + 1, e)
			if n == nil or after + n ~= e then return true end
			local structural = kb == 13 or kb == 14 or kb == 16 or kb == 15
			if structural then
				if depth >= 128 then return true end
				depth += 1
			end
			local bad = scanContent(after, after + n, kb)
			if structural then
				depth -= 1
			end
			return bad
		end
		return true
	end
	checkFields = function(fields: any, isRoot: boolean): boolean
		local seen: { [integer]: boolean } = {}
		for _, f in fields do
			if seen[f.id] then return true end
			seen[f.id] = true
			if f.id == BuildVersionId or f.id == MessageVocabularyId then return true end
			if f.id == NodeTableId then
				if not isRoot or f.kind ~= 12 then return true end
				continue
			elseif scanPayload(f.ps, f.e, f.kind) then
				return true
			end
		end
		return false
	end
	children[1] = curEdges
	if checkFields(top.fields, true) then return true end
	for i, rec in recs do
		curEdges = {}
		children[i + 1] = curEdges
		if rec.tid == StringBlobId or rec.tid == BytesBlobId then
			if rec.e - rec.s > 0xffffffff then
				rep.reason = "blob_over_size_cap"
				return true
			end
			if rec.tid == StringBlobId and not utf8_ok(b, rec.s, rec.e - rec.s) then
				return true
			end
		else
			local inner = walkBody(b, rec.s, rec.e, ids)
			if inner == nil or inner.term + 1 ~= rec.e then return true end
			if checkFields(inner.fields, false) then return true end
		end
	end
	for i = 1, recordCount do
		local rec = recs[i]
		if (rec.tid == StringBlobId or rec.tid == BytesBlobId) and (edges[i + 1] or 0) > 1 then
			rep.blobShared = math.max(rep.blobShared or 0, 1)
		end
	end
	local mark: { number } = table.create(recordCount + 1, 0)
	local stackNode: { number } = { 1 }
	local stackNext: { number } = { 1 }
	mark[1] = 1
	while #stackNode > 0 do
		local topIdx = #stackNode
		local u = stackNode[topIdx]
		local out = children[u]
		local nxt = stackNext[topIdx]
		if nxt > #out then
			mark[u] = 2
			stackNode[topIdx] = nil
			stackNext[topIdx] = nil
		else
			stackNext[topIdx] = nxt + 1
			local vIdx = out[nxt]
			if mark[vIdx] == 1 then
				return true
			elseif mark[vIdx] == 0 then
				mark[vIdx] = 1
				stackNode[topIdx + 1] = vIdx
				stackNext[topIdx + 1] = 1
			end
		end
	end
	for i = 1, recordCount do
		if mark[i + 1] == 0 then return true end
	end
	return false
end

-- the variable-form half of the strict read, shared by ReadStrict (a
-- non-fixed root, or a fixed root's form-3 dispatch above it) and by
-- ReadStrictVariable (a fixed root handed form-1 bytes, for the framing
-- surface alone: docs/LAYERS/1-framing.md, test/conformance/go/fixed.go's
-- variableLoad). It assumes the report is reset and the buffer is non-empty.
local function strictVariable(root: string, bytes: buffer, rep: any, d: any): any
	local b = bytes
	local form = buffer.readu8(b, 0)
	if form == 2 then rep.reason = "message_form_as_file"; return nil end
	if form ~= 1 then rep.reason = "newer_form"; return nil end
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
	local opened = open(b, rep)
	if opened == nil then
		rep.reason = "wire_damaged"
		return nil
	end
	local v = loadOpened(root, b, rep, opened)
	if rep.nonFiniteFloat ~= nil then
		-- every read's refusal (docs/SPEC-TABLES.md §3.3), the field named
		rep.reason = "non_finite_float"
		rep.field = rep.nonFiniteFloat
		return nil
	end
	if _countRefused then
		-- the storage cap refusal (docs/SPEC-TABLES.md §2.9, §6.5): the walk
		-- already named it count_over_extent_cap and set refused
		return nil
	end
	-- WIRE-ALONE DAMAGE (docs/SPEC-TABLES.md §6.5): the conditions the value
	-- walk does not judge are declared after the named refusals, so an over-cap
	-- array/map count (count_over_extent_cap, §2.9) and a NaN or infinity
	-- (non_finite_float, §3.3) answer their own names and never fall through to
	-- wire_damaged.
	if top ~= nil then
		local damaged = wireDamage(b, top, fr.ids, rep)
		if rep.nonFiniteFloat ~= nil then
			rep.reason = "non_finite_float"
			rep.field = rep.nonFiniteFloat
			return nil
		end
		if _countRefused then
			-- the wire-alone walk's count refusal (§2.9): name it as the value
			-- walk does, so a refusal is never silent
			restoreCount(rep)
			rep.refused = (rep.refused or 0) + 1
			rep.reason = "count_over_extent_cap"
			return nil
		end
		if damaged then
			if rep.reason == "blob_over_size_cap" then return nil end
			rep.malformed = true
			rep.reason = "wire_damaged"
			return nil
		end
	end
	for _, k in REFUSING do
		if moved(rep, k) then
			if k == "missingRequired" then
				-- Go's TableMissingRequired text (docs/SPEC-TABLES.md §2.3)
				rep.reason = "missing_required: " .. (rep.missingName or "")
				rep.field = rep.missingName
			else
				rep.reason = NAMES[k]
				rep.field = _firstField[k]
			end
			return nil
		end
	end
	if rep.unknown > 0 then rep.unknownField = _firstField["unknown"] end
	return v
end

function L.ReadStrict(root: string, bytes: buffer, report: any): any
	local rep: any = report or {}
	reset(rep)
	if bytes == nil or buffer.len(bytes) < 1 then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local b = bytes :: buffer
	local d = S.lookup(root)
	if d ~= nil and d.fixed then
		local form = buffer.readu8(b, 0)
		if form == 2 then rep.reason = "message_form_as_file"; return nil end
		if form == 1 then rep.reason = "previous_form"; return nil end
		if form ~= 3 then rep.reason = "newer_form"; return nil end
		local fx: any = {}
		local out = F.read(d, nil :: any, b, fx)
		rep.unknown = (fx.unknown or 0)
		rep.kindMismatch = (fx.kindMismatch or 0)
		rep.clamped = (fx.clamped or 0)
		rep.widened = (fx.widened or 0)
		rep.duplicate = (fx.duplicate or 0)
		if out == nil then
			rep.reason = fx.reason
			if fx.field ~= nil then rep.field = fx.field end
			if fx.malformed == true then rep.malformed = true end
			return nil
		end
		if #out ~= 1 then
			rep.malformed = true
			rep.reason = "wire_damaged"
			return nil
		end
		return out[1]
	end
	return strictVariable(root, b, rep, d)
end

-- THE VARIABLE FORM'S STRICT READ AT ANY ROOT (docs/LAYERS/1-framing.md):
-- the framing surface's read of a form-1 file at a fixed root, where
-- ReadStrict itself answers previous_form. The descriptor is never
-- modified; the save that follows still sees the fixed root and writes
-- form 3.
function L.ReadStrictVariable(root: string, bytes: buffer, report: any): any
	local rep: any = report or {}
	reset(rep)
	if bytes == nil or buffer.len(bytes) < 1 then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	return strictVariable(root, bytes :: buffer, rep, S.lookup(root))
end

-- resetReport is the report at rest, shared with schema_refusal.luau's fixed path.
function L.resetReport(report: any)
	reset(report)
end

-- THE PLAIN NAME IS THE STRICT READ (docs/SPEC-TABLES.md §6.5): every read
-- entry point reached by the plain name refuses what the strict read refuses
-- and hands back no recovered value, with the caller's report overwritten by
-- this read's counters and report.reason the one name the strict read answers.
-- The strict check lives HERE, beside the walk it judges, rather than calling
-- schema_refusal.luau: the pinned Luau refuses a require cycle at load time, and
-- schema_load.luau -> schema_refusal.luau -> schema_load.luau is exactly one. schema_refusal.luau's
-- ReadStrict still owns the fixed-form dispatch and delegates form 1 here.
function L.load(root: string, bytes: buffer, report: any): any
	return L.ReadStrict(root, bytes, report)
end

-- THE WALK SEAM (a test reads a count, never a clock): the whole-body walks
-- this module has taken since the last reset
function L.walkCount(): number
	return _walks
end

function L.resetWalkCount()
	_walks = 0
end

return table.freeze(L)
