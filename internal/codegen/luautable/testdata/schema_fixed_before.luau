--!strict
-- schema_fixed.luau — the Luau table runtime's FIXED FORM (docs/SPEC-TABLES.md §3.4,
-- form byte 3), the one reader and writer for a record. Emitted beside the
-- unit's <Base>Table.luau modules by internal/codegen/luautable and required
-- from them as require("./schema_fixed"). The layout, hash and record size are the
-- emitter's static data (fixedform.go); this file only reads and writes the
-- bytes those facts describe (docs/FIXED-FORM-ALGORITHM.md §1, §3, §4).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")

local F = {}

export type LayoutEntry = { id: integer, kind: number, size: number, children: number }
export type Known = { hash: integer, layout: { LayoutEntry }, layoutBytes: number, recordBytes: number }
-- a root's fixed facts: its own hash, record BODY size and layout, and the
-- lineage the build laid down with its floor (nil: the one entry, its own)
export type Fixed = { hash: integer, recordBytes: number, layout: { LayoutEntry }, lineage: { Known }?, floor: number? }
export type Refusal = { name: string, malformed: boolean?, field: string? }

local HEADER = 16
local HASH_AT = 8
local ENTRY = 17
local LAYOUT_HEADER = 4
local RECORD_MAX = 65536
local PLAN_MAX = 4096
local COUNT = 4
local PRESENT = 1
local TRAILER = 4

local function refuse(name: string, malformed: boolean?): Refusal
	return { name = name, malformed = malformed == true }
end

-- crc32c is CRC-32C, the Castagnoli polynomial (docs/SPEC-TABLES.md §2.14):
-- initial value and final XOR 0xFFFFFFFF, reflected 0x82F63B78.
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

-- THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14, §3.4 THE FRAMING): four
-- bytes, little-endian, the CRC-32C of every byte before them, after the last
-- record and so the last four bytes of a declaring root's file.
local function declaresIntegrity(d: any): boolean
	return d.integrity ~= nil and d.integrity ~= ""
end

local function integrityHolds(b: buffer): boolean
	local n = buffer.len(b)
	return n >= 5 and buffer.readu32(b, n - 4) == crc32c(b, n - 4)
end

local function withoutTrailer(b: buffer): buffer
	local n = buffer.len(b) - TRAILER
	local out = buffer.create(n)
	buffer.copy(out, 0, b, 0, n)
	return out
end

local function signed(kind: number): boolean
	return kind == 2 or kind == 3 or kind == 4 or kind == 5 or
		(kind >= 20 and kind <= 24) or kind == 11
end

-- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3, §3.4; the same rule
-- as packets). A read tests the RAW BITS before they become a
-- value, and a NaN or an infinity refuses the file as non_finite_float, naming
-- <Owner>.<field>, before the record lands; a write refuses one the same way
-- (a float32 tests its NARROWED bits, so a double past the float32 range is
-- refused, never written as an infinity). Every finite float32 widens to a
-- double exactly, so the conversions are the ordinary ones.
local scratch4 = buffer.create(4)
local function f32ToDouble(bits: number): number
	buffer.writeu32(scratch4, 0, bits)
	return buffer.readf32(scratch4, 0)
end

local function doubleToF32(v: number): number
	buffer.writef32(scratch4, 0, v)
	return buffer.readu32(scratch4, 0)
end

-- the body and the field a walk is in, for the refusal's name
local _owner = "?"
local _field = "?"
local function nonFinite(f: any): Refusal
	return { name = "non_finite_float", malformed = false, field = _owner .. "." .. (f.name or _field) }
end

local function asInt(v: any): integer
	if typeof(v) == "integer" then return v end
	return integer.create(v)
end

-- parse builds an offset tree from the writer's layout. `p` is the pre-order
-- index, and every node records its absolute record offset and child roots.
type Node = {
	id: integer, kind: number, size: number, off: number,
	children: { Node }, index: number, byId: { [integer]: Node }?,
}

-- A CHILD LIES INSIDE ITS PARENT (docs/SPEC-TABLES.md §3.4, THE LAYOUT;
-- docs/FIXED-FORM-ALGORITHM.md §1.2): every record offset the plan reads is
-- derived from these sizes, so a child whose end passes its parent's is a leaf
-- read past the record. The parent's own end is `off + size`.
local function inside(c: Node, off: number, size: number): boolean
	return c.off >= off and c.off + c.size <= off + size
end

local function parseNode(layout: { LayoutEntry }, i: number, off: number): (Node, number)
	-- THE WALK NEVER INDEXES PAST THE LAYOUT (docs/FIXED-FORM-ALGORITHM.md §1.1
	-- rule 5): a child count is a stranger's number, and layout[i] past #layout
	-- is nil, a raw interpreter error rather than a refusal by name.
	if i < 1 or i > #layout then error(refuse("layout_malformed"), 0) end
	local e = layout[i]
	local node: Node = { id = e.id, kind = e.kind, size = e.size, off = off, children = {}, index = i }
	i = i + 1
	if e.kind == 13 then
		local cursor = off
		local byId: { [integer]: Node } = {}
		for _ = 1, e.children do
			local c
			c, i = parseNode(layout, i, cursor)
			if not inside(c, off, e.size) then error(refuse("layout_malformed"), 0) end
			table.insert(node.children, c)
			-- the first child by an id is the one MATCH finds (§5.2)
			if byId[c.id] == nil then byId[c.id] = c end
			cursor = cursor + c.size
		end
		node.byId = byId
	elseif e.kind == 15 then
		local kids = {}
		for _ = 1, e.children do
			local c
			c, i = parseNode(layout, i, off)
			table.insert(kids, c)
		end
		local widest = 0
		for _, c in kids do
			if c.size > widest then widest = c.size end
		end
		local tagw = e.size - widest
		if tagw < 1 then tagw = 1 end
		local function shift(c: Node, d: number)
			c.off = c.off + d
			for _, k in c.children do shift(k, d) end
		end
		for _, c in kids do
			shift(c, tagw)
			if not inside(c, off, e.size) then error(refuse("layout_malformed"), 0) end
			table.insert(node.children, c)
		end
	elseif e.kind == 35 then
		local c
		c, i = parseNode(layout, i, off + PRESENT)
		if not inside(c, off, e.size) then error(refuse("layout_malformed"), 0) end
		table.insert(node.children, c)
	elseif e.kind == 16 then
		local k
		k, i = parseNode(layout, i, off)
		if not inside(k, off, e.size) then error(refuse("layout_malformed"), 0) end
		local c
		c, i = parseNode(layout, i, off)
		if not inside(c, off, e.size) then error(refuse("layout_malformed"), 0) end
		table.insert(node.children, k)
		table.insert(node.children, c)
	elseif e.kind == 14 then
		local c
		c, i = parseNode(layout, i, off)
		if not inside(c, off, e.size) then error(refuse("layout_malformed"), 0) end
		table.insert(node.children, c)
	elseif e.kind == 30 then
		for _ = 1, e.children do
			local c
			c, i = parseNode(layout, i, off)
			if not inside(c, off, e.size) then error(refuse("layout_malformed"), 0) end
			table.insert(node.children, c)
		end
	end
	return node, i
end

local function compile(fixed: Fixed): Node
	local layout = fixed.layout
	if #layout == 0 then error(refuse("layout_malformed"), 0) end
	if #layout > PLAN_MAX then error(refuse("plan_too_large"), 0) end
	local root, next = parseNode(layout, 1, 0)
	-- THE PRE-ORDER WALK CONSUMES EXACTLY #layout ENTRIES (docs/FIXED-FORM-
	-- ALGORITHM.md §1.1 rule 5): `next` is the index after the last entry the
	-- root's child counts claim, so anything but #layout + 1 leaves entries no
	-- walk reaches.
	if next ~= #layout + 1 then error(refuse("layout_malformed"), 0) end
	if root.size > RECORD_MAX then error(refuse("layout_record_too_large"), 0) end
	-- THE ROOT'S SIZE IS THE RECORD BODY F.load COPIES (docs/SPEC-TABLES.md
	-- §3.4 THE RECORD; docs/FIXED-FORM-ALGORITHM.md §3, §5.3): a root that
	-- claims more than writer.recordBytes would read past the record buffer.
	if root.size ~= fixed.recordBytes then error(refuse("layout_malformed"), 0) end
	return root
end

local function childById(n: Node, id: integer): Node?
	for _, c in n.children do
		if c.id == id then return c end
	end
	return nil
end

local function childAt(n: Node, k: number): Node?
	return n.children[k]
end

local function shift(n: Node, d: number)
	n.off = n.off + d
	for _, c in n.children do shift(c, d) end
end

-- THE CONTENT RULES over a text field's USED units (docs/SPEC-TABLES.md §3.4;
-- docs/FIXED-FORM-ALGORITHM.md §4.5, §5.4 fix 11): UTF-8 with no zero byte,
-- paired non-zero UTF-16 code units for wstring. bytes(N) carries no text rule.
local function utf8IllFormed(buf: buffer, at: number, n: number): boolean
	local i = 0
	while i < n do
		local c = buffer.readu8(buf, at + i)
		if c == 0 then return true end
		if c < 0x80 then
			i = i + 1
		elseif c >= 0xC2 and c <= 0xDF then
			if i + 1 >= n then return true end
			local c1 = buffer.readu8(buf, at + i + 1)
			if c1 < 0x80 or c1 > 0xBF then return true end
			i = i + 2
		elseif c >= 0xE0 and c <= 0xEF then
			if i + 2 >= n then return true end
			local c1 = buffer.readu8(buf, at + i + 1)
			local c2 = buffer.readu8(buf, at + i + 2)
			local lo1, hi1 = 0x80, 0xBF
			if c == 0xE0 then lo1 = 0xA0
			elseif c == 0xED then hi1 = 0x9F end
			if c1 < lo1 or c1 > hi1 or c2 < 0x80 or c2 > 0xBF then return true end
			i = i + 3
		elseif c >= 0xF0 and c <= 0xF4 then
			if i + 3 >= n then return true end
			local c1 = buffer.readu8(buf, at + i + 1)
			local c2 = buffer.readu8(buf, at + i + 2)
			local c3 = buffer.readu8(buf, at + i + 3)
			local lo1, hi1 = 0x80, 0xBF
			if c == 0xF0 then lo1 = 0x90
			elseif c == 0xF4 then hi1 = 0x8F end
			if c1 < lo1 or c1 > hi1 or c2 < 0x80 or c2 > 0xBF or c3 < 0x80 or c3 > 0xBF then return true end
			i = i + 4
		else
			return true
		end
	end
	return false
end

local function wideIllFormed(buf: buffer, at: number, units: number): boolean
	local i = 0
	while i < units do
		local u = buffer.readu16(buf, at + i * 2)
		if u == 0 then return true end
		if u >= 0xD800 and u <= 0xDBFF then
			if i + 1 >= units then return true end
			local lo = buffer.readu16(buf, at + (i + 1) * 2)
			if lo < 0xDC00 or lo > 0xDFFF then return true end
			i = i + 2
		elseif u >= 0xDC00 and u <= 0xDFFF then
			return true
		else
			i = i + 1
		end
	end
	return false
end

-- THE READ IS ONE PATH OVER TWO LAYOUTS (docs/FIXED-FORM-ALGORITHM.md §4, §5.2
-- PLAN/MATCH/EMIT): the WRITER's layout, the lineage entry the file's hash
-- selected, walked beside the READER's own, with the descriptors naming where
-- each value lands. For the reader's own hash the two layouts are one and every
-- pair is the identity; for an older lineage entry each pair takes §5.2's EMIT:
-- a field matched by id, a ladder widen counted `widened`, a kind that moved
-- left at its default and counted once (`kind_mismatch`, at compile), a writer
-- field no reader field names counted once (`unknown`, at compile), the
-- writer's own bounds held on every count, length, tag and ordinal (bill
-- §12.5), and the reader's tail its declared default. The destination is a
-- FRESH VALUE from the descriptor's constructor, which is this leg's prefill:
-- every field the plan does not land keeps the declared default (§4.3, §5.9
-- #35: the leg lays the value surface, field by field). Positions are the
-- writer node's record offset plus `at`, the record's start in the file, so a
-- record is read in place and never copied.

-- MISSING marks a pair EMIT lands nothing for (a kind that moved): the field
-- keeps the default the constructor gave it.
local MISSING = table.freeze({})

type Cx = { widened: number, clamped: number }

-- THE LADDER (docs/FIXED-FORM-ALGORITHM.md §5.1, Go's TableFixedWidens): a
-- wider integer of the same signedness, a wider fixed-point rung at equal F,
-- f32 into f64.
local function ladder(from: number, to: number): boolean
	if from >= 6 and from <= 9 and to >= 6 and to <= 9 then return to > from end
	if from >= 2 and from <= 5 and to >= 2 and to <= 5 then return to > from end
	if from >= 20 and from <= 24 and to >= 20 and to <= 24 then return to > from end
	if from >= 25 and from <= 29 and to >= 25 and to <= 29 then return to > from end
	return from == 10 and to == 11
end

local function leU(buf: buffer, at: number, width: number): number
	local v = 0
	for b = 0, width - 1 do
		v = v + buffer.readu8(buf, at + b) * 2 ^ (8 * b)
	end
	return v
end

local function widestArm(n: Node): number
	local widest = 0
	for _, c in n.children do
		if c.size > widest then widest = c.size end
	end
	return widest
end

local function tagWidth(n: Node): number
	local w = n.size - widestArm(n)
	if w < 1 then w = 1 end
	if w > 8 then w = 8 end
	return w
end

local function childIndexById(children: { Node }, id: integer): number
	for k, c in children do
		if c.id == id then return k end
	end
	return 0
end

-- a scalar at the WRITER's width and signedness, in the READER's value domain
local function readScalar(f: any, w: Node, buf: buffer, at: number): any
	local k = w.kind
	if f.s == "bool" then
		return buffer.readu8(buf, at) ~= 0
	elseif f.s == "float" then
		if k == 10 then
			local bits = buffer.readu32(buf, at)
			if bit32.band(bits, 0x7F800000) == 0x7F800000 then error(nonFinite(f), 0) end
			return f32ToDouble(bits)
		end
		if bit32.band(buffer.readu32(buf, at + 4), 0x7FF00000) == 0x7FF00000 then error(nonFinite(f), 0) end
		return buffer.readf64(buf, at)
	end
	local width = w.size
	local v: any
	if width == 1 then
		v = signed(k) and buffer.readi8(buf, at) or buffer.readu8(buf, at)
	elseif width == 2 then
		v = signed(k) and buffer.readi16(buf, at) or buffer.readu16(buf, at)
	elseif width == 4 then
		v = signed(k) and buffer.readi32(buf, at) or buffer.readu32(buf, at)
	elseif width == 8 then
		local raw = buffer.readinteger(buf, at)
		if f.dom == "integer" then return raw end
		return integer.tonumber(raw)
	else
		v = 0
	end
	if f.dom == "integer" then return integer.create(v) end
	return v
end

local function lessThan(a: any, b: any, isSigned: boolean): boolean
	if typeof(a) == "integer" and typeof(b) == "integer" then
		if isSigned then return integer.lt(a, b) end
		return integer.ult(a, b)
	end
	return a < b
end

-- THE BOUNDS PASS (docs/FIXED-FORM-ALGORITHM.md §4.6), run on the value as it
-- lands: a ranged scalar clamps to the declared range (a fixed-point bound is
-- in value units and the storage is raw, so it shifts by F), a bits(N) to
-- 2^N - 1, each counting `clamped`. A compressed float's bound is the
-- READER's (schema#1164, statement B); an integer range is the reader's too,
-- which on a lawful lineage contains the writer's.
local function bounds(f: any, v: any, cx: Cx): any
	local lo, hi = f.lo, f.hi
	if lo ~= nil and hi ~= nil and typeof(lo) ~= "table" then
		if f.fbits ~= nil and f.fbits > 0 then
			if typeof(lo) == "integer" then
				lo = integer.lshift(lo, integer.create(f.fbits))
				hi = integer.lshift(hi :: integer, integer.create(f.fbits))
			else
				lo = lo * 2 ^ f.fbits
				hi = hi * 2 ^ f.fbits
			end
		end
		if typeof(v) == "integer" and typeof(lo) == "number" then
			lo, hi = integer.create(lo), integer.create(hi)
		end
		local isSigned = f.signed ~= false
		if f.s == "float" then
			if v == v then
				if v < lo then v = lo; cx.clamped += 1
				elseif v > hi then v = hi; cx.clamped += 1 end
			end
		elseif lessThan(v, lo, isSigned) then
			v = lo; cx.clamped += 1
		elseif lessThan(hi, v, isSigned) then
			v = hi; cx.clamped += 1
		end
	end
	if f.bits ~= nil and f.bits > 0 and f.bits < 53 then
		local top: any = 2 ^ f.bits - 1
		if typeof(v) == "integer" then top = integer.create(top) end
		if lessThan(top, v, false) then v = top; cx.clamped += 1 end
	end
	return v
end

local readPair: (f: any, w: Node, r: Node, buf: buffer, at: number, cx: Cx, def: any) -> (any, any)

local function readTable(d: any, w: Node, r: Node, buf: buffer, at: number, cx: Cx): any
	local v = d.new()
	local wById = w.byId :: { [integer]: Node }
	local rById = r.byId :: { [integer]: Node }
	for _, f in d.fields do
		local wc = wById[f.id]
		local rc = rById[f.id]
		_owner, _field = d.name, f.name
		if wc ~= nil and rc ~= nil then
			local slot = f.slot
			local got, companion = MISSING, nil
			if f.present ~= nil then
				if wc.kind == 35 and rc.kind == 35 then
					local present = buffer.readu8(buf, wc.off + at) ~= 0
					v[f.present] = present
					if present then
						got, companion = readPair(f, wc.children[1], rc.children[1], buf, at, cx, v[slot])
					end
				elseif rc.kind == 35 then
					-- T INTO ?T (bill §12.8): the writer always carried the
					-- value, so it is present
					got, companion = readPair(f, wc, rc.children[1], buf, at, cx, v[slot])
					if got ~= MISSING then v[f.present] = true end
				end
			elseif wc.kind ~= 35 then
				got, companion = readPair(f, wc, rc, buf, at, cx, v[slot])
			end
			if got ~= MISSING then
				v[slot] = got
				if f.s == "str" or f.s == "bytes" then
					v[slot .. "Length"] = companion
				elseif f.s == "arr" and (f.array == "counted" or f.array == "list") then
					v[slot .. "Count"] = companion
				end
			end
		end
	end
	return v
end

local function readText(f: any, w: Node, buf: buffer, at: number): (any, any)
	local unit = if w.kind == 33 then 2 else 1
	local cap = math.floor((w.size - COUNT) / unit)
	local length = buffer.readu32(buf, w.off + at)
	-- the WRITER's cap (bill §12.5, §4.5): a length past it refuses by name
	-- before a byte lands, then the USED units are held to the content rule
	if length > cap then error(refuse("length_over_bound"), 0) end
	if unit == 2 then
		if wideIllFormed(buf, w.off + at + COUNT, length) then error(refuse("text_ill_formed"), 0) end
	elseif utf8IllFormed(buf, w.off + at + COUNT, length) then
		error(refuse("text_ill_formed"), 0)
	end
	local bound = f.bound or length
	if length > bound then length = bound end
	local b = buffer.create(bound * unit)
	if length > 0 then buffer.copy(b, 0, buf, w.off + at + COUNT, length * unit) end
	return b, length
end

local function readEnum(f: any, w: Node, r: Node, buf: buffer, at: number, cx: Cx): any
	local raw = leU(buf, w.off + at, w.size)
	local v = 0
	if raw ~= 0 then
		if raw > #w.children then
			-- one past the WRITER's own variant count lands None and counts
			-- (§4.6, §5.4), on the compiled plan exactly as on the identity one
			cx.clamped += 1
		elseif w.size < r.size then
			-- a grown ordinal width (bill §12.7): widened, unsigned
			cx.widened += 1
			v = raw
		elseif w == r then
			v = raw
		else
			-- the remap (§5.2 EMIT kind 30): the reader's position of the
			-- writer's variant raw, by id; 0 when the reader has no such name
			v = childIndexById(r.children, w.children[raw].id)
		end
	end
	local pair = f.vs and f.vs[v]
	if pair ~= nil and pair[3] == true then return f.def or 0 end
	return v
end

local function readUnion(f: any, w: Node, r: Node, buf: buffer, at: number, cx: Cx): any
	local d = S.lookup(f.t)
	local v = d.new()
	local tag = leU(buf, w.off + at, tagWidth(w))
	if tag == 0 then
		v.Type = 0
		return v
	end
	if tag > #w.children then
		-- a tag past the WRITER's arm count lands None and counts (§4.6)
		v.Type = 0
		cx.clamped += 1
		return v
	end
	local k = if w == r then tag else childIndexById(r.children, w.children[tag].id)
	v.Type = k
	if k == 0 then return v end
	local arm = d.arms[k]
	if arm ~= nil and arm.s ~= "void" then
		local got, companion = readPair(arm, w.children[tag], r.children[k], buf, at, cx, v[arm.slot])
		if got ~= MISSING then
			v[arm.slot] = got
			if arm.s == "str" or arm.s == "bytes" then
				v[arm.slot .. "Length"] = companion
			elseif arm.s == "arr" and (arm.array == "counted" or arm.array == "list") then
				v[arm.slot .. "Count"] = companion
			end
		end
	end
	return v
end

local function readArray(f: any, w: Node, r: Node, buf: buffer, at: number, cx: Cx, def: any): (any, any)
	local we, re = w.children[1], r.children[1]
	if f.s == "bytes" then
		local length = buffer.readu32(buf, w.off + at)
		local cap = w.size - COUNT
		if length > cap then error(refuse("length_over_bound"), 0) end
		local bound = f.bound or length
		if length > bound then length = bound end
		local b = buffer.create(bound)
		if length > 0 then buffer.copy(b, 0, buf, w.off + at + COUNT, length) end
		return b, length
	end
	local head = if f.array == "counted" then COUNT else 0
	local theirs = if we.size > 0 then math.floor((w.size - head) / we.size) else 0
	local mine = if re.size > 0 then math.floor((r.size - head) / re.size) else 0
	local e = f.e
	if f.array == "counted" then
		local count = buffer.readu32(buf, w.off + at)
		-- the bound THE PLAN CARRIES is the WRITER's own (bill §12.5)
		if count > theirs then error(refuse("count_over_bound"), 0) end
		if count > mine then count = mine end
		local out = table.create(count)
		for i = 1, count do
			local got = readPair(e, we, re, buf, at + head + (i - 1) * we.size, cx, nil)
			out[i] = if got == MISSING then nil else got
		end
		return out, count
	end
	-- a fixed array: the writer's elements land, the reader's slots past them
	-- keep THE ELEMENT'S DEFAULTS (§5.2 EMIT kind 14, bill §12.6)
	local out = table.create(mine)
	for i = 1, mine do
		out[i] = if def ~= nil then def[i] else nil
	end
	for i = 1, math.min(theirs, mine) do
		local got = readPair(e, we, re, buf, at + (i - 1) * we.size, cx, out[i])
		if got ~= MISSING then out[i] = got end
	end
	return out, nil
end

local function readKeyed(f: any, w: Node, r: Node, buf: buffer, at: number, cx: Cx, def: any): any
	local wkey, we = w.children[1], w.children[2]
	local rkey, re = r.children[1], r.children[2]
	local slots = #rkey.children
	local out = table.create(slots)
	for i = 1, slots do
		out[i] = if def ~= nil then def[i] else nil
	end
	-- per reader slot, the writer's slot of the key variant with the same id
	-- (§5.2 EMIT kind 16); a slot an appended key opened keeps its default
	for i = 1, slots do
		local j = if w == r then i else childIndexById(wkey.children, rkey.children[i].id)
		if j ~= 0 then
			local got = readPair(f.e, we, re, buf, at + (j - 1) * we.size, cx, out[i])
			if got ~= MISSING then out[i] = got end
		end
	end
	return out
end

readPair = function(f: any, w: Node, r: Node, buf: buffer, at: number, cx: Cx, def: any): (any, any)
	if w.kind ~= r.kind then
		if ladder(w.kind, r.kind) then
			cx.widened += 1
			return bounds(f, readScalar(f, w, buf, w.off + at), cx), nil
		end
		return MISSING, nil
	end
	local k = w.kind
	if k == 13 then
		return readTable(S.lookup(f.t), w, r, buf, at, cx), nil
	elseif k == 15 then
		return readUnion(f, w, r, buf, at, cx), nil
	elseif k == 14 then
		return readArray(f, w, r, buf, at, cx, def)
	elseif k == 16 then
		return readKeyed(f, w, r, buf, at, cx, def), nil
	elseif k == 12 or k == 33 then
		return readText(f, w, buf, at)
	elseif k == 30 then
		return readEnum(f, w, r, buf, at, cx), nil
	end
	if w.size ~= r.size then
		-- a same-kind leaf at a grown storage width (a bits(N)): ZERO-extended
		-- (§5.2, "same-kind widen ... ZERO")
		if w.size < r.size and r.size <= 8 then
			cx.widened += 1
		else
			return MISSING, nil
		end
	end
	return bounds(f, readScalar(f, w, buf, w.off + at), cx), nil
end

-- THE CENSUS (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH and EMIT, §5.4): ONCE PER
-- WRITER-READER PAIR, at compile and never per record — an array element is visited once,
-- whatever its count — `unknown` for a writer field no reader field names and
-- `kind_mismatch` for a pair whose kind moved off every ladder.
type Census = { unknown: number, kindMismatch: number }

local censusPair: (f: any, w: Node, r: Node, c: Census) -> ()

local function censusTable(d: any, w: Node, r: Node, c: Census)
	local rById = r.byId :: { [integer]: Node }
	for _, wc in w.children do
		if rById[wc.id] == nil then c.unknown += 1 end
	end
	local wById = w.byId :: { [integer]: Node }
	for _, f in d.fields do
		local wc, rc = wById[f.id], rById[f.id]
		if wc ~= nil and rc ~= nil then
			if rc.kind == 35 and wc.kind == 35 then
				censusPair(f, wc.children[1], rc.children[1], c)
			elseif rc.kind == 35 then
				censusPair(f, wc, rc.children[1], c)
			elseif wc.kind == 35 then
				c.kindMismatch += 1
			else
				censusPair(f, wc, rc, c)
			end
		end
	end
end

censusPair = function(f: any, w: Node, r: Node, c: Census)
	if w.kind ~= r.kind then
		if not ladder(w.kind, r.kind) then c.kindMismatch += 1 end
		return
	end
	local k = w.kind
	if k == 13 then
		censusTable(S.lookup(f.t), w, r, c)
	elseif k == 14 then
		if f.s ~= "bytes" then censusPair(f.e, w.children[1], r.children[1], c) end
	elseif k == 16 then
		censusPair(f.e, w.children[2], r.children[2], c)
	elseif k == 15 then
		local d = S.lookup(f.t)
		for i, rarm in r.children do
			local j = childIndexById(w.children, rarm.id)
			local arm = d.arms[i]
			if j ~= 0 and arm ~= nil and arm.s ~= "void" then
				censusPair(arm, w.children[j], rarm, c)
			end
		end
	elseif k ~= 12 and k ~= 33 and k ~= 30 and w.size ~= r.size then
		if not (w.size < r.size and r.size <= 8) then c.kindMismatch += 1 end
	end
end

local function writeLeaf(f: any, n: Node, v: any, buf: buffer, off: number)
	if f.s == "bool" then
		buffer.writeu8(buf, off, v and 1 or 0)
	elseif f.s == "float" then
		if n.kind == 10 then
			local bits = doubleToF32(v)
			if bit32.band(bits, 0x7F800000) == 0x7F800000 then error(nonFinite(f), 0) end
			buffer.writeu32(buf, off, bits)
		else
			if v ~= v or v == math.huge or v == -math.huge then error(nonFinite(f), 0) end
			buffer.writef64(buf, off, v)
		end
	elseif f.s == "enum" then
		local raw = v
		for b = 0, n.size - 1 do
			buffer.writeu8(buf, off + b, raw % 256)
			raw = math.floor(raw / 256)
		end
	elseif f.s == "int" then
		local w = n.size
		if w == 1 then
			buffer.writeu8(buf, off, v % 256)
		elseif w == 2 then
			buffer.writeu16(buf, off, v % 65536)
		elseif w == 4 then
			buffer.writeu32(buf, off, v % 4294967296)
		elseif w == 8 then
			if f.dom == "integer" then
				buffer.writeinteger(buf, off, asInt(v))
			else
				buffer.writeinteger(buf, off, asInt(v))
			end
		end
	end
end

function writeOne(f: any, n: Node, value: any, buf: buffer)
	local slot = f.slot
	local own = value
	if slot ~= nil then own = value[slot] end
	if f.s == "str" then
		local length = value[slot .. "Length"] or 0
		buffer.writeu32(buf, n.off, length)
		local b = own
		if b ~= nil and length > 0 then
			buffer.copy(buf, n.off + COUNT, b, 0, length * (f.wide and 2 or 1))
		end
	elseif f.s == "bytes" then
		local length = value[slot .. "Length"] or 0
		buffer.writeu32(buf, n.off, length)
		if length > 0 then buffer.copy(buf, n.off + COUNT, own, 0, length) end
	elseif f.s == "tab" then
		local d = S.lookup(f.t)
		writeStruct(d, n, own, buf)
	elseif f.s == "union" then
		local d = S.lookup(f.t)
		local tag = own.Type or 0
		local widest = 0
		for _, c in n.children do if c.size > widest then widest = c.size end end
		local tagw = n.size - widest
		local raw = tag
		for b = 0, tagw - 1 do
			buffer.writeu8(buf, n.off + b, raw % 256)
			raw = math.floor(raw / 256)
		end
		if tag >= 1 then
			local arm = d.arms[tag]
			local c = childAt(n, tag)
			if arm ~= nil and c ~= nil and arm.s ~= "void" then
				writeOne(arm, c, own, buf)
			end
		end
	elseif f.s == "arr" then
		local e = f.e
		if f.array == "counted" then
			local count = value[slot .. "Count"] or 0
			buffer.writeu32(buf, n.off, count)
			local c = childAt(n, 1) :: Node
			shift(c, COUNT)
			for i = 1, count do
				writeOne(e, c, value[slot][i], buf)
				shift(c, c.size)
			end
			shift(c, -(COUNT + count * c.size))
		elseif f.array == "keyed" then
			local c = childAt(n, 2) :: Node
			for i = 1, f.bound or 0 do
				writeOne(e, c, value[slot][i], buf)
				shift(c, c.size)
			end
			shift(c, -(f.bound or 0) * c.size)
		else
			local c = childAt(n, 1) :: Node
			for i = 1, f.bound or 0 do
				writeOne(e, c, value[slot][i], buf)
				shift(c, c.size)
			end
			shift(c, -(f.bound or 0) * c.size)
		end
	else
		writeLeaf(f, n, own, buf, n.off)
	end
end

local function deprecatedValue(f: any, value: any, record: any): any
	if value == nil then return nil end
	if f.s == "enum" then
		local pair = f.vs[value]
		if pair ~= nil and pair[3] == true then return pair end
	elseif f.s == "arr" and f.e ~= nil and f.e.s == "enum" then
		local count = if f.array == "counted" then (record[f.slot .. "Count"] or 0) else (f.bound or 0)
		for i = 1, count do
			local pair = f.e.vs[value[i]]
			if pair ~= nil and pair[3] == true then return pair end
		end
	end
	return nil
end

function writeStruct(d: any, n: Node, value: any, buf: buffer)
	-- FIXED-WRITE-ANCHOR
	for _, f in d.fields do
		local c = childById(n, f.id)
		_owner, _field = d.name, f.name
		if c ~= nil and (f.present == nil or value[f.present]) then
			local pair = deprecatedValue(f, value[f.slot], value)
			if pair ~= nil then error({ name = "invalid_value", field = d.name .. "." .. f.name, variant = pair[1] }, 0) end
		end
		if c ~= nil then
			if f.present ~= nil then
				local present = value[f.present]
				buffer.writeu8(buf, c.off, present and 1 or 0)
				if present then
					writeOne(f, childAt(c, 1) :: Node, value, buf)
				end
			else
				writeOne(f, c, value, buf)
			end
		end
	end
end

local function u32(buf: buffer, at: number): number return buffer.readu32(buf, at) end
local function put32(buf: buffer, at: number, v: number) buffer.writeu32(buf, at, v) end
local function put64(buf: buffer, at: number, v: integer) buffer.writeinteger(buf, at, v) end

-- save writes one file: the form header, the writer's layout, and each record
-- behind its own eight-byte hash (docs/FIXED-FORM-ALGORITHM.md §2, §3), then
-- for a root that declares integrity = "crc32c" the four-byte trailer
-- (docs/SPEC-TABLES.md §2.14, §3.4 THE FRAMING).
function F.save(d: any, fixed: Fixed, values: { any }): buffer
	local layout = fixed.layout
	local trailer = if declaresIntegrity(d) then TRAILER else 0
	local bytes = HEADER + 4 + 4 + #layout * ENTRY
	local out = buffer.create(bytes + #values * (8 + fixed.recordBytes) + trailer)
	buffer.fill(out, 0, 0, bytes)
	buffer.writeu8(out, 0, 3)
	put64(out, HASH_AT, fixed.hash)
	put32(out, HEADER, #layout * ENTRY + 4)
	local p = HEADER + 4
	put32(out, p, #layout)
	p = p + 4
	for _, e in layout do
		put64(out, p, e.id)
		buffer.writeu8(out, p + 8, e.kind)
		put32(out, p + 9, e.size)
		put32(out, p + 13, e.children)
		p = p + ENTRY
	end
	local root = compile(fixed)
	local rec = buffer.create(fixed.recordBytes)
	for _, v in values do
		buffer.fill(rec, 0, 0, fixed.recordBytes)
		writeStruct(d, root, v, rec)
		put64(out, p, fixed.hash)
		p = p + 8
		buffer.copy(out, p, rec, 0, fixed.recordBytes)
		p = p + fixed.recordBytes
	end
	if trailer > 0 then
		buffer.writeu32(out, p, crc32c(out, p))
		p = p + TRAILER
	end
	return out
end

-- THE LINEAGE (docs/FIXED-FORM-ALGORITHM.md §5.2, COMPILE(lock, T)): one Known
-- per locked layout, OLDEST FIRST and the reader's own LAST, laid down by the
-- emitter as static data from the lock the build read (fixedlineage.go). A
-- Known carries §5.9 #19's four members: the wire hash a file is matched on,
-- the layout the LOCK recorded, its byte length, and recordBytes, THE WHOLE
-- RECORD (8 + the body; the lock's body plus the eight hash bytes the
-- compiler added once). A table with no lock has the one entry it can always
-- compute, its own.
-- a plan, compiled ONCE per lineage entry and cached by it (§4.2, §5.9 #3:
-- "build time" in a language with no constant evaluation is first use): the
-- writer's node tree, the reader's, and the census that lands once per read.
type Plan = { writer: Node, reader: Node, census: Census, refused: string? }
local plans: { [any]: { [any]: Plan } } = setmetatable({}, { __mode = "k" }) :: any

local function own(fixed: Fixed): Known
	return { hash = fixed.hash, layout = fixed.layout, layoutBytes = LAYOUT_HEADER + #fixed.layout * ENTRY, recordBytes = 8 + fixed.recordBytes }
end

local function planFor(d: any, fixed: Fixed, k: Known, identity: boolean): Plan
	local byEntry = plans[d]
	if byEntry == nil then
		byEntry = setmetatable({}, { __mode = "k" }) :: any
		plans[d] = byEntry
	end
	local key: any = if identity then fixed.layout else k
	local p = byEntry[key]
	if p ~= nil then return p end
	-- A LINEAGE ENTRY THAT WILL NOT BUILD IS A LANE CARRYING ITS NAME (§5.9
	-- #36), never a throw a first load discovers: the generator already
	-- failed the build on a lock entry that does not parse (fixedlineage.go),
	-- so this answers only an entry a caller handed in at run time.
	local ok, res = pcall(function(): any
		local reader = compile(fixed)
		if identity then
			return { writer = reader, reader = reader, census = { unknown = 0, kindMismatch = 0 } }
		end
		local writer = compile({ hash = k.hash, recordBytes = k.recordBytes - 8, layout = k.layout })
		local c: Census = { unknown = 0, kindMismatch = 0 }
		censusTable(d, writer, reader, c)
		return { writer = writer, reader = reader, census = c }
	end)
	if not ok then
		local name = if type(res) == "table" and (res :: any).name ~= nil then (res :: any).name else "layout_malformed"
		p = { writer = nil :: any, reader = nil :: any, census = { unknown = 0, kindMismatch = 0 }, refused = name }
	else
		p = res :: Plan
	end
	byEntry[key] = p
	return p
end

-- the file's L layout bytes at `at` against the Known the hash selected, byte
-- for byte (§5.3 step 7): the reader compares a layout, it never walks one
local function sameBytes(file: buffer, at: number, length: number, k: Known): boolean
	if length ~= k.layoutBytes or length ~= LAYOUT_HEADER + #k.layout * ENTRY then return false end
	if u32(file, at) ~= #k.layout then return false end
	local p = at + LAYOUT_HEADER
	for _, e in k.layout do
		if buffer.readinteger(file, p) ~= e.id or buffer.readu8(file, p + 8) ~= e.kind
			or u32(file, p + 9) ~= e.size or u32(file, p + 13) ~= e.children then
			return false
		end
		p = p + ENTRY
	end
	return true
end

-- loadTolerant is LOAD(R, file) (docs/FIXED-FORM-ALGORITHM.md §5.3), the one
-- reader, under its TOLERANT name (docs/SPEC-TABLES.md §6.5): every record of
-- the file as a fresh value with the counters on the report, or nil with
-- report.reason the refusal's name. The steps are §5.3's, in its order: the form byte before the
-- file's length, the twenty-byte header and its seven reserved bytes, the
-- layout length, the HEADER'S HASH TAKEN AS GIVEN and looked up in the lineage
-- (none is `layout_unknown` — `layout_newer` is reserved, §21.3 — and below the
-- floor `layout_unsupported`, both reporting the file's hash in
-- report.layoutHash and nothing else), the layout bytes compared with the
-- lock's (`layout_malformed`), the record arithmetic (`truncated`, malformed),
-- and per record its own hash (`no_layout`) before anything lands. A declaring
-- root's CRC-32C trailer is checked first (docs/SPEC-TABLES.md §2.14).
--
-- REFUSE IS TOTAL: the values are built and returned only when every record
-- read, so a refusal at record k lands nothing at all, and the counters move
-- only on a read that returns — the compile census once, after the loop
-- (§5.3, §5.9 #6). `batch_too_large` has no Luau surface: read takes no
-- capacity, so no batch can outgrow it; `plan_too_large` is the lineage
-- entry's own lane (a layout past PLAN_MAX entries).
function F.loadTolerant(d: any, fixed: Fixed, file: buffer, report: any?): { any }?
	local rep: any = report or {}
	rep.reason = nil
	rep.malformed = false
	rep.unknown, rep.kindMismatch, rep.clamped, rep.widened, rep.duplicate = 0, 0, 0, 0, 0
	rep.layoutHash = 0i
	rep.field = nil
	local function no(name: string, malformed: boolean?): nil
		rep.reason = name
		rep.malformed = malformed == true
		return nil
	end
	if declaresIntegrity(d) and buffer.len(file) >= 1 and buffer.readu8(file, 0) == 3 then
		if not integrityHolds(file) then return no("integrity_mismatch") end
		file = withoutTrailer(file)
	end
	-- 1, 2: the form byte, then the header (§5.3; docs/SPEC-TABLES.md §3.4)
	if buffer.len(file) < 1 then return no("truncated", true) end
	local form = buffer.readu8(file, 0)
	if form == 1 then return no("previous_form") end
	if form == 2 then return no("message_form_as_file") end
	if form ~= 3 then return no("newer_form") end
	if buffer.len(file) < HEADER + LAYOUT_HEADER then return no("truncated", true) end
	for at = 1, HASH_AT - 1 do
		if buffer.readu8(file, at) ~= 0 then return no("reserved_not_zero", true) end
	end
	-- 3: the layout's length
	local length = u32(file, HEADER)
	if HEADER + 4 + length > buffer.len(file) then return no("layout_malformed") end
	-- 4, 5, 6: the hash as given, against the lineage, then the floor
	local h = buffer.readinteger(file, HASH_AT)
	local lineage: { Known } = fixed.lineage or { own(fixed) }
	local i = 0
	for k, e in lineage do
		if e.hash == h then i = k; break end
	end
	if i == 0 then
		rep.layoutHash = h
		return no("layout_unknown")
	end
	if i <= (fixed.floor or 0) then
		rep.layoutHash = h
		return no("layout_unsupported")
	end
	-- 7: a known hash over different layout bytes is a lie about a known
	-- version, refused before any record byte is read
	local k = lineage[i]
	if not sameBytes(file, HEADER + 4, length, k) then return no("layout_malformed") end
	-- 8: the entry's plan; the reader's own hash is the identity, an INDEX
	-- comparison and never a hash a runtime computed (§5.3)
	local plan = planFor(d, fixed, k, k.hash == fixed.hash)
	if plan.refused ~= nil then return no(plan.refused) end
	-- 9: the records must fill the rest of the file exactly
	local at0 = HEADER + 4 + length
	local rest = buffer.len(file) - at0
	local stride = k.recordBytes
	if stride <= 8 or rest % stride ~= 0 then return no("truncated", true) end
	local count = rest // stride
	-- 11: EVERY record's hash before the first one lands (§5.3, TOTAL OVER
	-- THE BATCH)
	for r = 0, count - 1 do
		if buffer.readinteger(file, at0 + r * stride) ~= h then return no("no_layout") end
	end
	local cx: Cx = { widened = 0, clamped = 0 }
	local out = table.create(count)
	local ok, err = pcall(function()
		for r = 0, count - 1 do
			out[r + 1] = readTable(d, plan.writer, plan.reader, file, at0 + r * stride + 8, cx)
		end
	end)
	if not ok then
		if type(err) == "table" and (err :: any).name ~= nil then
			-- non_finite_float names its field (docs/SPEC-TABLES.md §3.3)
			rep.field = (err :: any).field
			return no((err :: any).name, (err :: any).malformed)
		end
		error(err, 0)
	end
	rep.widened, rep.clamped = cx.widened, cx.clamped
	rep.unknown, rep.kindMismatch = plan.census.unknown, plan.census.kindMismatch
	return out
end

-- read is the PLAIN fixed-form read, and it is STRICT (docs/SPEC-TABLES.md
-- §6.5, Go's <T>FixedLoad over <T>FixedLoadTolerant): nil for every refusal
-- of loadTolerant and for a read whose report the strict read refuses —
-- clamped, then kind_mismatch, TableReport.Err()'s order — with report.reason
-- that name and nothing handed back. unknown and widened alone never refuse.
-- A fixed refusal keeps its own §3.4 name (Strict error identity).
function F.read(d: any, fixed: Fixed, file: buffer, report: any?): { any }?
	local rep: any = report or {}
	local out = F.loadTolerant(d, fixed, file, rep)
	if out == nil then return nil end
	if rep.clamped > 0 then rep.reason = "clamped"; return nil end
	if rep.kindMismatch > 0 then rep.reason = "kind_mismatch"; return nil end
	return out
end

-- load is the plain strict read for a caller that wants the refusal raised:
-- the values, or an error { name, malformed, layoutHash } carrying the name
-- read reports.
function F.load(d: any, fixed: Fixed, file: buffer, report: any?): { any }
	local rep: any = report or {}
	local out = F.read(d, fixed, file, rep)
	if out == nil then
		error({ name = rep.reason, malformed = rep.malformed == true, layoutHash = rep.layoutHash, field = rep.field }, 0)
	end
	return out
end

-- check validates a layout the way §1.1's seven rules do and names the first
-- refusal; it never parses a record.
function F.check(fixed: Fixed): any
	local ok, err = pcall(function()
		local root = compile(fixed)
		if root.kind ~= 13 then error(refuse("layout_kind_invalid"), 0) end
	end)
	if ok then return nil end
	if type(err) == "table" and (err :: any).name ~= nil then return err end
	return refuse("layout_malformed")
end

-- fixed_layout is the FIXED FORM's layout constructor (docs/SPEC-TABLES.md
-- §3.4): the walk's entries as frozen data a load path reads to build its plan.
-- A layout is a constant, exactly as a descriptor is.
function F.fixed_layout(entries: { any }): { any }
	table.freeze(entries :: any)
	return entries
end

return table.freeze(F)
