--!strict
-- table_fixed.moo — the moo table runtime's FIXED FORM (docs/SPEC-TABLES.md
-- §3.4, form byte 3): the one reader and the one writer of a record. The
-- emitter lays each fixed root's static data down beside its descriptor
-- (internal/codegen/mootable fixedform.go: M.<Root>Fixed): the identity hash,
-- the record body's size, and the LINEAGE — the lock's layouts oldest first,
-- this build's own last, and the floor (docs/FIXED-FORM-ALGORITHM.md §5.2). A
-- port of the Luau leg's fixed.luau (the dev branch) onto moo's values
-- (table_values.moo), with main's rules: a file is SELECTED BY ITS HASH
-- against the lineage and its layout bytes are COMPARED to the lineage
-- entry's, never parsed (§5.3); every refusal is TOTAL (no counter moves, no
-- value lands); a non-finite float is refused on write and on read
-- (non_finite_float, SPEC-TABLES §3.3).
--
-- THE PLAN (§4). moo's storage is not the wire's (a string(N) is a moo
-- string, a union a table with a Type), so the plan is the RECORD-IMAGE shape
-- of §4.2: the record's bytes are the image, and a plan compiled ONCE per
-- lineage entry from that entry's layout bytes against this build's
-- descriptors is the scatter — each writer entry resolved by id to a field of
-- this reader's or to nothing, with the op the pair calls for: a landing at
-- equal width, a WIDEN (counted per entry per record), f32 into f64, an
-- enum ordinal or union tag REMAPPED by variant id, a count or length held to
-- the WRITER's bound. Every evolution decision is made at compile, the
-- `unknown` and `kind_mismatch` census included, and lands once per read that
-- returns (§5.4). A reader field the writer never wrote keeps the declared
-- default its constructor gave it: that is the prefill (§4.3).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local V = require("./table_values")

local F = {}

local FORM = 3
local HEADER = 16
local HASH_AT = 8
local LAYOUT_HEADER = 4
local ENTRY = 17
local RECORD_MAX = 65536
local DEPTH_MAX = 64
local PLAN_MAX = 4096
local COUNT = 4
local PRESENT = 1
local TRAILER = 4

local KIND_TABLE = 13
local KIND_ARRAY = 14
local KIND_UNION = 15
local KIND_KEYED = 16
local KIND_STRING = 12
local KIND_ENUM = 30
local KIND_NOPAYLOAD = 32
local KIND_WSTRING = 33
local KIND_OPTIONAL = 35

-- A refusal is raised as a table and caught at the entry points; nothing
-- else is ever raised on a hostile file.
local function refuse(name: string, malformed: boolean?, field: string?): any
	error({ fixedRefusal = true, name = name, malformed = malformed == true, field = field }, 0)
end

local function u32(b: buffer, at: number): number
	return buffer.readu32(b, at)
end

-- ---------------------------------------------------------------------------
-- THE LAYOUT (§1): a u32 count and seventeen-byte entries, an id (eight bytes,
-- held as its string), a kind byte, a u32 size and a u32 child count.

export type Node = {
	id: string,
	kind: number,
	size: number,
	off: number,
	children: { Node },
	-- the plan, filled once at compile
	pairs: { any }?,
	remap: { [number]: number }?,
	arms: { [number]: any }?,
	tagw: number?,
}

local function leafSize(kind: number): number
	if kind == 1 then
		return 1
	end
	return V.width(kind)
end

-- parseLayout reads the trusted layout bytes the build laid down (the lock's
-- own, or this build's) into the pre-order node tree with absolute record
-- offsets, holding §1.1's rules: the count fits the length, every child
-- inside its parent, a table's size the sum of its fields', the walk consumes
-- exactly the entries, no deeper than 64.
local function parseLayout(bytes: string, recordBytes: number): Node
	local n = #bytes
	if n < LAYOUT_HEADER then
		refuse("layout_malformed")
	end
	local b = buffer.fromstring(bytes)
	local count = u32(b, 0)
	if count == 0 or n ~= LAYOUT_HEADER + count * ENTRY then
		refuse("layout_malformed")
	end
	if count > PLAN_MAX then
		refuse("plan_too_large")
	end
	local entries: { any } = table.create(count) :: { any }
	for i = 1, count do
		local p = LAYOUT_HEADER + (i - 1) * ENTRY
		local size = u32(b, p + 9)
		if size > RECORD_MAX then
			refuse("layout_record_too_large")
		end
		entries[i] = { id = buffer.readstring(b, p, 8), kind = buffer.readu8(b, p + 8), size = size, children = u32(b, p + 13) }
	end
	local function inside(c: Node, off: number, size: number)
		if c.off < off or c.off + c.size > off + size then
			refuse("layout_malformed")
		end
	end
	local function shift(c: Node, d: number)
		c.off += d
		for _, k in c.children do
			shift(k, d)
		end
	end
	local parse: (number, number, number) -> (Node, number)
	parse = function(i: number, off: number, depth: number): (Node, number)
		if i < 1 or i > count or depth > DEPTH_MAX then
			refuse("layout_malformed")
		end
		local e = entries[i]
		local node: Node = { id = e.id, kind = e.kind, size = e.size, off = off, children = {} }
		i += 1
		local kids = e.children
		if kids > count then
			refuse("layout_malformed")
		end
		if e.kind == KIND_TABLE then
			local cursor = off
			for _ = 1, kids do
				local c
				c, i = parse(i, cursor, depth + 1)
				inside(c, off, e.size)
				table.insert(node.children, c)
				cursor += c.size
			end
			if cursor - off ~= e.size then
				refuse("layout_malformed")
			end
		elseif e.kind == KIND_UNION then
			local widest = 0
			for _ = 1, kids do
				local c
				c, i = parse(i, off, depth + 1)
				table.insert(node.children, c)
				if c.size > widest then
					widest = c.size
				end
			end
			local tagw = e.size - widest
			if tagw ~= 1 and tagw ~= 2 and tagw ~= 4 and tagw ~= 8 then
				refuse("layout_malformed")
			end
			node.tagw = tagw
			for _, c in node.children do
				shift(c, tagw)
				inside(c, off, e.size)
			end
		elseif e.kind == KIND_OPTIONAL then
			if kids ~= 1 then
				refuse("layout_malformed")
			end
			local c
			c, i = parse(i, off + PRESENT, depth + 1)
			if c.size + PRESENT ~= e.size then
				refuse("layout_malformed")
			end
			table.insert(node.children, c)
		elseif e.kind == KIND_KEYED then
			if kids ~= 2 then
				refuse("layout_malformed")
			end
			local k, c
			k, i = parse(i, off, depth + 1)
			c, i = parse(i, off, depth + 1)
			if k.kind ~= KIND_ENUM or c.size == 0 or e.size % c.size ~= 0 then
				refuse("layout_malformed")
			end
			table.insert(node.children, k)
			table.insert(node.children, c)
		elseif e.kind == KIND_ARRAY then
			if kids ~= 1 then
				refuse("layout_malformed")
			end
			local c
			c, i = parse(i, off, depth + 1)
			if c.size == 0 or (e.size % c.size ~= 0 and (e.size - COUNT) % c.size ~= 0) then
				refuse("layout_malformed")
			end
			table.insert(node.children, c)
		elseif e.kind == KIND_ENUM then
			if e.size ~= 1 and e.size ~= 2 and e.size ~= 4 and e.size ~= 8 then
				refuse("layout_malformed")
			end
			for _ = 1, kids do
				local c
				c, i = parse(i, off, depth + 1)
				if c.kind ~= KIND_NOPAYLOAD then
					refuse("layout_malformed")
				end
				table.insert(node.children, c)
			end
		elseif e.kind == KIND_STRING or e.kind == KIND_WSTRING then
			if kids ~= 0 or e.size < COUNT then
				refuse("layout_malformed")
			end
		elseif e.kind == KIND_NOPAYLOAD then
			if kids ~= 0 or e.size ~= 0 then
				refuse("layout_malformed")
			end
		else
			-- a leaf is its STORAGE width, which a bits(N) holds wider than
			-- its kind (§3.4's constant-size table)
			local w = leafSize(e.kind)
			local sz = e.size
			if w == 0 or kids ~= 0 or (sz ~= 1 and sz ~= 2 and sz ~= 4 and sz ~= 8 and sz ~= 16) or sz < w then
				refuse("layout_malformed")
			end
		end
		return node, i
	end
	local root, next = parse(1, 0, 0)
	if next ~= count + 1 or root.kind ~= KIND_TABLE then
		refuse("layout_malformed")
	end
	if root.size ~= recordBytes then
		refuse("layout_malformed")
	end
	return root
end

-- ---------------------------------------------------------------------------
-- THE COMPILE (§4.2, §5.2): the writer's tree against this reader's
-- descriptors, once per lineage entry. A pair's op is decided here.

-- the kind this reader's own fixed writer gives a leaf (a timestamp is an
-- int64 on this form, docs/SPEC-TABLES.md §2.11)
local function readerKind(f: any): number
	if f.timestamp then
		return 5
	end
	return f.k or 0
end

local compileStruct: (d: any, n: Node, census: any) -> ()
local compileValue: (f: any, n: Node, census: any) -> boolean

-- the remap of a writer enum's ordinals onto this reader's variants, by
-- variant id (§5.4 `ordinal`): writer ordinal k (its k-th variant child) to
-- this reader's ordinal, 0 where the reader has no variant of that id
local function enumRemap(vs: { any }, n: Node): { [number]: number }
	local mine: { [string]: number } = {}
	for i, v in vs do
		mine[v[2]] = i
	end
	local out: { [number]: number } = {}
	for k, c in n.children do
		out[k] = mine[c.id] or 0
	end
	return out
end

compileValue = function(f: any, n: Node, census: any): boolean
	local s = f.s
	local k = n.kind
	if s == "bool" then
		return k == 1
	elseif s == "int" then
		local mine = readerKind(f)
		if k == mine then
			return true
		end
		-- a grown fixed(I, F) widens on its own signedness ladder, F held
		-- (§5.4 widen; SPEC §4.6: I is the raw value's width)
		if (k >= 20 and k <= 24 and mine > k and mine <= 24) or (k >= 25 and k <= 29 and mine > k and mine <= 29) then
			return true
		end
		return V.widens(k, mine) or (mine == 5 and k == 36)
	elseif s == "float" then
		return k == f.k or (k == 10 and f.k == 11)
	elseif s == "enum" then
		if k ~= KIND_ENUM then
			return false
		end
		n.remap = enumRemap(f.vs, n)
		return true
	elseif s == "str" then
		return k == (if f.wide then KIND_WSTRING else KIND_STRING)
	elseif s == "bytes" then
		return k == KIND_ARRAY and n.children[1].kind == 6 and n.children[1].size == 1
	elseif s == "tab" then
		if k ~= KIND_TABLE then
			return false
		end
		compileStruct(S.lookup(f.t), n, census)
		return true
	elseif s == "union" then
		if k ~= KIND_UNION then
			return false
		end
		local un = S.lookup(f.t)
		local byId: { [string]: any } = {}
		for _, a in un.arms do
			byId[a.id] = a
		end
		local arms: { [number]: any } = {}
		for wk, c in n.children do
			local a = byId[c.id]
			if a == nil then
				census.unknown += 1
			elseif a.s == "void" then
				if c.kind == KIND_NOPAYLOAD then
					arms[wk] = { arm = a }
				else
					census.kindMismatch += 1
				end
			elseif compileValue(a, c, census) then
				arms[wk] = { arm = a, node = c }
			else
				census.kindMismatch += 1
			end
		end
		n.arms = arms
		return true
	elseif s == "arr" then
		if f.array == "keyed" then
			if k ~= KIND_KEYED then
				return false
			end
			local key = n.children[1]
			local mine: { [string]: number } = {}
			for i, v in (f.keyvs or {}) do
				mine[v[2]] = i
			end
			local remap: { [number]: number } = {}
			for wk, c in key.children do
				remap[wk] = mine[c.id] or 0
			end
			n.remap = remap
			return compileValue(f.e, n.children[2], census)
		end
		if k ~= KIND_ARRAY then
			return false
		end
		return compileValue(f.e, n.children[1], census)
	end
	return false
end

compileStruct = function(d: any, n: Node, census: any)
	local byId: { [string]: any } = {}
	for _, f in d.fields do
		byId[f.id] = f
	end
	local pairs = {}
	for _, c in n.children do
		local f = byId[c.id]
		if f == nil then
			-- a writer field no reader field names: stepped over, counted once
			-- per peer at compile (§5.4 MATCH)
			census.unknown += 1
		else
			local node = c
			local ok = true
			-- a field the writer held plain and this reader declares ?T rode
			-- on every record: it lands PRESENT (the optional_add row)
			local always = false
			if f.present ~= nil then
				if c.kind == KIND_OPTIONAL then
					node = c.children[1]
				else
					always = true
				end
			elseif c.kind == KIND_OPTIONAL then
				ok = false
			end
			if ok and compileValue(f, node, census) then
				table.insert(pairs, { f = f, n = c, v = node, always = always })
			else
				census.kindMismatch += 1
			end
		end
	end
	n.pairs = pairs
end

-- a plan: the writer's tree with its pairs, and the census it owes
local function compilePlan(d: any, layout: string, recordBytes: number): any
	local root = parseLayout(layout, recordBytes)
	local census = { unknown = 0, kindMismatch = 0 }
	compileStruct(d, root, census)
	return { root = root, census = census, recordBytes = recordBytes }
end

-- ---------------------------------------------------------------------------
-- THE READ (§4.4 - §4.6): one walk of a record through a plan. `rep` is the
-- read's own counters, committed only when the whole batch lands.

local function fieldName(f: any): string?
	if f ~= nil and f.owner ~= nil then
		return f.owner .. "." .. f.name
	end
	return nil
end

-- the ordinal at a node's width; an eight-byte ordinal with a high half names
-- no variant
local function ordinalAt(rec: buffer, off: number, size: number): number
	if size == 1 then
		return buffer.readu8(rec, off)
	elseif size == 2 then
		return buffer.readu16(rec, off)
	elseif size == 4 then
		return u32(rec, off)
	end
	if u32(rec, off + 4) ~= 0 then
		return V.HUGE
	end
	return u32(rec, off)
end

local readValue: (f: any, n: Node, dx: number, rec: buffer, rep: any, named: any) -> (any, any, any)

-- Every node's offset is its FIRST instance's; an array element's instance i
-- sits `dx` bytes past it. The plan is shared and never mutated by a read.

-- the raw integer of `size` bytes at off: a number at four bytes or fewer, a
-- wide value at eight and sixteen
local function rawAt(rec: buffer, off: number, size: number, signed: boolean): any
	if size == 1 then
		return if signed then buffer.readi8(rec, off) else buffer.readu8(rec, off)
	elseif size == 2 then
		return if signed then buffer.readi16(rec, off) else buffer.readu16(rec, off)
	elseif size == 4 then
		return if signed then buffer.readi32(rec, off) else u32(rec, off)
	elseif size == 8 then
		local C = if signed then V.Int64 else V.Uint64
		return C.new(u32(rec, off), u32(rec, off + 4))
	end
	local C = if signed then V.Int128 else V.Uint128
	return C.new(u32(rec, off), u32(rec, off + 4), u32(rec, off + 8), u32(rec, off + 12))
end

-- one scalar leaf: the raw value at the WRITER's kind, widened to this
-- reader's (counted), clamped to the declared range and bits(N)'s ceiling in
-- the bounds pass (counted), then held as this field's storage
local function readScalar(f: any, n: Node, dx: number, rec: buffer, rep: any, named: any): any
	local k = n.kind
	local off = n.off + dx
	if f.s == "bool" then
		return buffer.readu8(rec, off) ~= 0
	end
	if k == 10 then
		local bits = u32(rec, off)
		if not V.finite32(bits) then
			refuse("non_finite_float", false, fieldName(named))
		end
		if f.k == 11 then
			rep.widened += 1
			local lo, hi = V.f32to64(bits)
			return V.Uint64.new(lo, hi)
		end
		if f.flo ~= nil then
			local lo, hi = V.f32to64(bits)
			if V.cmp64(lo, hi, f.flo[1], f.flo[2]) < 0 then
				rep.clamped += 1
				return f.flo32
			elseif V.cmp64(lo, hi, f.fhi[1], f.fhi[2]) > 0 then
				rep.clamped += 1
				return f.fhi32
			end
		end
		return bits
	elseif k == 11 then
		local lo, hi = u32(rec, off), u32(rec, off + 4)
		if not V.finite64(lo, hi) then
			refuse("non_finite_float", false, fieldName(named))
		end
		return V.Uint64.new(lo, hi)
	end
	-- the raw value at the writer's STORAGE width and its kind's signedness
	local x = rawAt(rec, off, n.size, V.signedKind(k))
	local kw = f.kw
	local signed = f.signed == true
	if n.size < (f.fw or kw) then
		rep.widened += 1
	end
	if n.size ~= kw then
		x = V.widen(x, kw, signed)
	end
	if f.lo ~= nil then
		if V.rawCmp(x, f.lo, kw, signed) < 0 then
			x = f.lo
			rep.clamped += 1
		elseif V.rawCmp(x, f.hi, kw, signed) > 0 then
			x = f.hi
			rep.clamped += 1
		end
	end
	if f.bits ~= nil and f.bits < kw * 8 then
		local cap = V.bitsCap(f.bits, kw)
		if V.rawCmp(x, cap, kw, false) > 0 then
			x = cap
			rep.clamped += 1
		end
	end
	return V.toStorage(f, x)
end

-- text's used units, held to the WRITER's cap and the content rule (§4.5)
local function readText(f: any, n: Node, dx: number, rec: buffer): (any, number)
	local off = n.off + dx
	local length = u32(rec, off)
	local unit = if f.wide then 2 else 1
	local cap = (n.size - COUNT) // unit
	if length > cap then
		refuse("length_over_bound")
	end
	local at = off + COUNT
	if f.wide then
		if not V.wide(rec, at, length * 2) then
			refuse("text_ill_formed")
		end
		local bound = f.bound or length
		local units = if length > bound then V.clampWide(rec, at, bound) else length
		local b = buffer.create(bound * 2)
		buffer.copy(b, 0, rec, at, units * 2)
		return b, units
	end
	if not V.utf8(rec, at, length) then
		refuse("text_ill_formed")
	end
	local keep = length
	if f.bound ~= nil and f.bound > 0 and keep > f.bound then
		keep = V.clampUtf8(rec, at, f.bound)
	end
	return buffer.readstring(rec, at, keep), keep
end

-- land one read value and its companions in a holder (a record or a union)
local function land(v: any, f: any, got: any, extra: any, lens: any)
	v[f.slot] = got
	if f.s == "arr" then
		if f.array == "counted" then
			v[f.slot .. "Count"] = extra
		end
		if lens ~= nil then
			v[f.slot .. "Length"] = lens
		end
	elseif f.s == "bytes" or (f.s == "str" and f.wide) then
		v[f.slot .. "Length"] = extra
	end
end

local function readStruct(d: any, n: Node, dx: number, rec: buffer, rep: any): any
	local v = d.new()
	for _, p in n.pairs :: { any } do
		local f = p.f
		if f.present ~= nil then
			local present = p.always or buffer.readu8(rec, p.n.off + dx) ~= 0
			v[f.present] = present
			if not present then
				continue
			end
		end
		local got, extra, lens = readValue(f, p.v, dx, rec, rep, f)
		land(v, f, got, extra, lens)
	end
	return v
end

-- the reader's own element at its construction (§4.3)
local function freshElement(e: any): any
	if e.s == "tab" or e.s == "union" then
		return S.lookup(e.t).new()
	elseif e.s == "bytes" then
		return buffer.create(e.bound)
	elseif e.s == "str" and e.wide then
		return buffer.create(e.bound * 2)
	elseif e.s == "str" then
		return ""
	elseif e.s == "bool" then
		return false
	end
	return e.def or 0
end

-- one element into dst at i (a table of values, or a buffer for [N]uint8)
local function readElement(e: any, c: Node, dx: number, rec: buffer, rep: any, named: any, dst: any, lens: any, i: number)
	local got, extra = readValue(e, c, dx, rec, rep, named)
	if type(dst) == "buffer" then
		buffer.writeu8(dst, i - 1, got)
	else
		dst[i] = got
		if lens ~= nil then
			lens[i] = extra
		end
	end
end

readValue = function(f: any, n: Node, dx: number, rec: buffer, rep: any, named: any): (any, any, any)
	local s = f.s
	local off = n.off + dx
	if s == "str" then
		local got, units = readText(f, n, dx, rec)
		return got, units, nil
	elseif s == "bytes" then
		local length = u32(rec, off)
		if length > n.size - COUNT then
			refuse("length_over_bound")
		end
		local bound = f.bound or length
		local keep = math.min(length, bound)
		local b = buffer.create(bound)
		buffer.copy(b, 0, rec, off + COUNT, keep)
		return b, keep, nil
	elseif s == "tab" then
		return readStruct(S.lookup(f.t), n, dx, rec, rep), nil, nil
	elseif s == "enum" then
		local raw = ordinalAt(rec, off, n.size)
		-- a grown ordinal width is a widen (§5.4)
		if n.size < (f.fw or n.size) then
			rep.widened += 1
		end
		if raw == 0 then
			return 0, nil, nil
		end
		local v = (n.remap :: { [number]: number })[raw]
		if v == nil or v == 0 then
			-- an ordinal past the writer's variants, or one this reader has
			-- no variant for, lands None, counted by the bounds pass (§4.6)
			rep.clamped += 1
			return 0, nil, nil
		end
		local pair = f.vs[v]
		if pair ~= nil and pair[3] == true then
			return f.def or 0, nil, nil
		end
		return v, nil, nil
	elseif s == "union" then
		local un = S.lookup(f.t)
		local u = un.new()
		local tag = ordinalAt(rec, off, n.tagw :: number)
		if tag == 0 then
			return u, nil, nil
		end
		local hit = (n.arms :: { [number]: any })[tag]
		if hit == nil then
			if tag > #n.children then
				rep.clamped += 1
			end
			return u, nil, nil
		end
		local a = hit.arm
		u.Type = a.tag
		if a.s ~= "void" then
			local got, extra, lens = readValue(a, hit.node, dx, rec, rep, a)
			land(u, a, got, extra, lens)
		end
		return u, nil, nil
	elseif s == "arr" then
		local e = f.e
		local holder: any
		if e.buf then
			holder = buffer.create(f.bound)
		else
			holder = table.create(f.bound)
			for i = 1, f.bound do
				holder[i] = freshElement(e)
			end
		end
		local lens: any = nil
		if e.s == "bytes" or (e.s == "str" and e.wide) then
			lens = table.create(f.bound, 0)
		end
		if f.array == "keyed" then
			local c = n.children[2]
			local remap = n.remap :: { [number]: number }
			local slots = n.size // c.size
			for k = 1, slots do
				local mine = remap[k]
				if mine ~= nil and mine > 0 and mine <= f.bound then
					readElement(e, c, dx + (k - 1) * c.size, rec, rep, named, holder, lens, mine)
				end
			end
			return holder, nil, lens
		end
		local c = n.children[1]
		local count: number
		local first = 0
		if f.array == "counted" then
			count = buffer.readi32(rec, off)
			local writerBound = (n.size - COUNT) // c.size
			if count < 0 or count > writerBound or count > f.bound then
				refuse("count_over_bound")
			end
			first = COUNT
		else
			count = math.min(n.size // c.size, f.bound)
		end
		for i = 1, count do
			readElement(e, c, dx + first + (i - 1) * c.size, rec, rep, named, holder, lens, i)
		end
		return holder, count, lens
	end
	return readScalar(f, n, dx, rec, rep, named), nil, nil
end

-- ---------------------------------------------------------------------------
-- THE WRITE (§3.1): this build's own layout, every field at its declared
-- width at its constant offset, an absent optional's payload zero. A value
-- outside its contract refuses the save by name and writes nothing.

local writeValue: any

local function writeOrdinal(out: buffer, off: number, size: number, v: number)
	if size == 1 then
		buffer.writeu8(out, off, v)
	elseif size == 2 then
		buffer.writeu16(out, off, v)
	else
		buffer.writeu32(out, off, v)
	end
end

-- the companion a field's value rides with: a Length or a Count
local function companion(f: any, v: any): any
	if f.s == "bytes" or (f.s == "str" and f.wide) then
		return v[f.slot .. "Length"]
	elseif f.s == "arr" then
		if f.array == "counted" then
			return { count = v[f.slot .. "Count"], lens = v[f.slot .. "Length"] }
		end
		return { lens = v[f.slot .. "Length"] }
	end
	return nil
end

local function writeStruct(d: any, n: Node, dx: number, v: any, out: buffer)
	if type(v) ~= "table" then
		refuse("invalid_value", false, d.name)
	end
	for _, p in n.pairs :: { any } do
		local f = p.f
		if f.present ~= nil then
			local present = v[f.present] == true
			buffer.writeu8(out, p.n.off + dx, if present then 1 else 0)
			if not present then
				continue
			end
		end
		writeValue(f, p.v, dx, v[f.slot], companion(f, v), out, f)
	end
end

writeValue = function(f: any, n: Node, dx: number, v: any, extra: any, out: buffer, named: any)
	local s = f.s
	local off = n.off + dx
	if s == "bool" then
		if type(v) ~= "boolean" then
			refuse("invalid_value", false, fieldName(named))
		end
		buffer.writeu8(out, off, if v then 1 else 0)
	elseif s == "float" then
		if f.f32 then
			if type(v) ~= "number" or v ~= math.floor(v) or v < 0 or v > 0xFFFFFFFF then
				refuse("invalid_value", false, fieldName(named))
			end
			if not V.finite32(v) then
				refuse("non_finite_float", false, fieldName(named))
			end
			buffer.writeu32(out, off, v)
		else
			if not V.Uint64.is(v) then
				refuse("invalid_value", false, fieldName(named))
			end
			if not V.finite64(v[1], v[2]) then
				refuse("non_finite_float", false, fieldName(named))
			end
			buffer.writeu32(out, off, v[1])
			buffer.writeu32(out, off + 4, v[2])
		end
	elseif s == "int" then
		local x = V.toRaw(f, v)
		if x == nil then
			refuse("invalid_value", false, fieldName(named))
		end
		local signed = f.signed == true
		if n.size >= 8 and type(x) == "number" then
			x = V.widen(x, n.size, signed)
		end
		V.writeRaw(out, off, n.size, signed, x)
	elseif s == "enum" then
		if type(v) ~= "number" or v ~= math.floor(v) or v < 0 or v > #f.vs then
			refuse("invalid_value", false, fieldName(named))
		end
		local pair = f.vs[v]
		if pair ~= nil and pair[3] == true then
			refuse("invalid_value", false, fieldName(named))
		end
		writeOrdinal(out, off, n.size, v)
	elseif s == "str" then
		if f.wide then
			local units = extra or 0
			if type(v) ~= "buffer" or units < 0 or units > f.bound or units * 2 > buffer.len(v) or not V.wide(v, 0, units * 2) then
				refuse("invalid_value", false, fieldName(named))
			end
			buffer.writeu32(out, off, units)
			buffer.copy(out, off + COUNT, v, 0, units * 2)
		else
			if type(v) ~= "string" or #v > f.bound or not V.utf8String(v) then
				refuse("invalid_value", false, fieldName(named))
			end
			buffer.writeu32(out, off, #v)
			buffer.writestring(out, off + COUNT, v)
		end
	elseif s == "bytes" then
		local length = extra or 0
		if type(v) ~= "buffer" or length < 0 or length > f.bound or length > buffer.len(v) then
			refuse("invalid_value", false, fieldName(named))
		end
		buffer.writeu32(out, off, length)
		buffer.copy(out, off + COUNT, v, 0, length)
	elseif s == "tab" then
		writeStruct(S.lookup(f.t), n, dx, v, out)
	elseif s == "union" then
		if type(v) ~= "table" then
			refuse("invalid_value", false, fieldName(named))
		end
		local un = S.lookup(f.t)
		local tag = v.Type or 0
		if type(tag) ~= "number" or tag ~= math.floor(tag) or tag < 0 or tag > #un.arms then
			refuse("invalid_value", false, fieldName(named))
		end
		writeOrdinal(out, off, n.tagw :: number, tag)
		if tag > 0 then
			local a = un.arms[tag]
			if a.s ~= "void" then
				local hit = (n.arms :: { [number]: any })[tag]
				writeValue(a, hit.node, dx, v[a.slot], companion(a, v), out, a)
			end
		end
	elseif s == "arr" then
		local e = f.e
		if type(v) ~= "table" and type(v) ~= "buffer" then
			refuse("invalid_value", false, fieldName(named))
		end
		local lens = if type(extra) == "table" then extra.lens else nil
		local function at(i: number): any
			if type(v) == "buffer" then
				return buffer.readu8(v, i - 1)
			end
			return v[i]
		end
		local function lenAt(i: number): any
			if type(lens) == "table" then
				return lens[i]
			end
			return nil
		end
		if f.array == "keyed" then
			local c = n.children[2]
			for k = 1, f.bound do
				writeValue(e, c, dx + (k - 1) * c.size, at(k), lenAt(k), out, named)
			end
			return
		end
		local c = n.children[1]
		local count = f.bound
		local first = 0
		if f.array == "counted" then
			count = if type(extra) == "table" then extra.count else 0
			if type(count) ~= "number" or count ~= math.floor(count) or count < 0 or count > f.bound then
				refuse("invalid_value", false, fieldName(named))
			end
			buffer.writeu32(out, off, count)
			first = COUNT
		end
		for i = 1, count do
			writeValue(e, c, dx + first + (i - 1) * c.size, at(i), lenAt(i), out, named)
		end
	else
		refuse("invalid_value", false, fieldName(named))
	end
end

-- ---------------------------------------------------------------------------
-- THE STATIC DATA (fixedform.go): M.<Root>Fixed. Plans are compiled on first
-- use, once per lineage entry, and cached beside the data.

export type Fixed = {
	name: string,
	hash: string,
	recordBytes: number,
	floor: number,
	lineage: { { hash: string, recordBytes: number, retired: boolean, layout: string } },
}

local plans: { [any]: { [number]: any } } = {}

local function planFor(d: any, fixed: any, i: number): any
	local cache = plans[fixed]
	if cache == nil then
		cache = {}
		plans[fixed] = cache
	end
	local p = cache[i]
	if p == nil then
		local e = fixed.lineage[i]
		p = compilePlan(d, e.layout, e.recordBytes)
		cache[i] = p
	end
	return p
end

-- fixed freezes a root's static data (a constant, as a descriptor is).
function F.fixed(spec: Fixed): Fixed
	for _, e in spec.lineage do
		table.freeze(e :: any)
	end
	table.freeze(spec.lineage :: any)
	table.freeze(spec :: any)
	return spec
end

local function declaresIntegrity(d: any): boolean
	return d.integrity ~= nil and d.integrity ~= ""
end

-- measure is the bytes save writes for count records: the header, the
-- layout, each record behind its hash, and a declaring root's trailer.
function F.measure(d: any, fixed: Fixed, count: number): number
	local own = fixed.lineage[#fixed.lineage]
	local trailer = if declaresIntegrity(d) then TRAILER else 0
	return HEADER + COUNT + #own.layout + count * (8 + fixed.recordBytes) + trailer
end

-- save writes one file (§2, §3): the form header, this build's layout, each
-- record behind its own hash, then a declaring root's CRC-32C trailer
-- (SPEC-TABLES §2.14). A value outside its contract refuses by name: nil and
-- { name, field }.
function F.save(d: any, fixed: Fixed, values: { any }): (buffer?, any?)
	local ok, res = pcall(function(): buffer
		local own = fixed.lineage[#fixed.lineage]
		local plan = planFor(d, fixed, #fixed.lineage)
		local total = F.measure(d, fixed, #values)
		local out = buffer.create(total)
		buffer.writeu8(out, 0, FORM)
		buffer.writestring(out, HASH_AT, fixed.hash)
		buffer.writeu32(out, HEADER, #own.layout)
		buffer.writestring(out, HEADER + COUNT, own.layout)
		local p = HEADER + COUNT + #own.layout
		local rec = buffer.create(fixed.recordBytes)
		for _, v in values do
			buffer.fill(rec, 0, 0)
			writeStruct(d, plan.root, 0, v, rec)
			buffer.writestring(out, p, fixed.hash)
			buffer.copy(out, p + 8, rec, 0, fixed.recordBytes)
			p += 8 + fixed.recordBytes
		end
		if declaresIntegrity(d) then
			buffer.writeu32(out, p, V.crc32c(out, p))
		end
		return out
	end)
	if ok then
		return res, nil
	end
	local r: any = res
	if type(r) == "table" and r.fixedRefusal then
		return nil, { name = r.name, field = r.field }
	end
	error(r, 0)
end

local function resetReport(report: any)
	report.malformed = false
	report.refused = 0
	report.unknown = 0
	report.kindMismatch = 0
	report.clamped = 0
	report.widened = 0
	report.duplicate = 0
	report.reason = nil
	report.field = nil
	report.layoutHash = nil
end

-- the load proper; raises a refusal, returns the values and the counters
local function loadAll(d: any, fixed: Fixed, file: buffer, report: any): { any }
	-- §5.3 step 1: the form byte before the length
	if buffer.len(file) < 1 then
		refuse("truncated", true)
	end
	local form = buffer.readu8(file, 0)
	if form == 1 then
		refuse("previous_form")
	elseif form == 2 then
		refuse("message_form_as_file")
	elseif form ~= FORM then
		refuse("newer_form")
	end
	-- THE INTEGRITY TRAILER is checked before the layout or any record
	-- (SPEC-TABLES §2.14, §3.4 THE FRAMING)
	if declaresIntegrity(d) then
		local n = buffer.len(file)
		if n < 5 or u32(file, n - TRAILER) ~= V.crc32c(file, n - TRAILER) then
			refuse("integrity_mismatch")
		end
		local body = buffer.create(n - TRAILER)
		buffer.copy(body, 0, file, 0, n - TRAILER)
		file = body
	end
	local len = buffer.len(file)
	-- step 2: the twenty-byte minimum and the seven reserved bytes
	if len < HEADER + COUNT then
		refuse("truncated", true)
	end
	for at = 1, HASH_AT - 1 do
		if buffer.readu8(file, at) ~= 0 then
			refuse("reserved_not_zero", true)
		end
	end
	-- step 3: the layout's length
	local L = u32(file, HEADER)
	if HEADER + COUNT + L > len then
		refuse("layout_malformed")
	end
	-- step 4 and 5: THE HEADER'S HASH, TAKEN AS GIVEN, selects the entry
	local h = buffer.readstring(file, HASH_AT, 8)
	local index = 0
	for i, e in fixed.lineage do
		if e.hash == h then
			index = i
			break
		end
	end
	if index == 0 then
		report.layoutHash = h
		refuse("layout_unknown")
	end
	-- step 6: the floor
	if index <= fixed.floor then
		report.layoutHash = h
		refuse("layout_unsupported")
	end
	-- step 7: the layout bytes are COMPARED to the entry's, never parsed
	local entry = fixed.lineage[index]
	if L ~= #entry.layout or buffer.readstring(file, HEADER + COUNT, L) ~= entry.layout then
		refuse("layout_malformed")
	end
	-- step 8: the plan, compiled once
	local plan = planFor(d, fixed, index)
	-- step 9: whole records
	local at = HEADER + COUNT + L
	local rest = len - at
	local stride = 8 + entry.recordBytes
	if entry.recordBytes <= 0 or rest % stride ~= 0 then
		refuse("truncated", true)
	end
	local count = rest // stride
	-- step 11, TOTAL OVER THE BATCH: every record's hash first
	for i = 0, count - 1 do
		if buffer.readstring(file, at + i * stride, 8) ~= h then
			refuse("no_layout")
		end
	end
	local rep = { clamped = 0, widened = 0 }
	local out = table.create(count)
	local rec = buffer.create(entry.recordBytes)
	for i = 0, count - 1 do
		buffer.copy(rec, 0, file, at + i * stride + 8, entry.recordBytes)
		out[i + 1] = readStruct(d, plan.root, 0, rec, rep)
	end
	report.unknown += plan.census.unknown
	report.kindMismatch += plan.census.kindMismatch
	report.clamped += rep.clamped
	report.widened += rep.widened
	return out
end

-- load reads every record of a form-3 file through the plan its header's
-- hash selects (§5.3), into fresh values. It answers the values, or nil with
-- report.reason the refusal's name (and report.malformed for the framing
-- damage §5.3 names: truncated, reserved_not_zero), report.layoutHash the
-- file's hash for layout_unknown and layout_unsupported, report.field the
-- field a non_finite_float names. A refusal is TOTAL: no counter moves.
function F.load(d: any, fixed: Fixed, file: buffer?, report: any?): { any }?
	local rep: any = report or {}
	resetReport(rep)
	if file == nil then
		rep.malformed = true
		rep.reason = "truncated"
		return nil
	end
	local ok, res = pcall(loadAll, d, fixed, file, rep)
	if ok then
		return res
	end
	local r: any = res
	if type(r) == "table" and r.fixedRefusal then
		local hash = rep.layoutHash
		resetReport(rep)
		rep.reason = r.name
		rep.malformed = r.malformed
		rep.field = r.field
		rep.layoutHash = hash
		return nil
	end
	error(r, 0)
end

-- check compiles every lineage entry's plan and names the first refusal, or
-- nil (§1.1, §5.9 #36): a lineage entry that would not build.
function F.check(d: any, fixed: Fixed): string?
	for i = 1, #fixed.lineage do
		local ok, res = pcall(planFor, d, fixed, i)
		if not ok then
			local r: any = res
			if type(r) == "table" and r.fixedRefusal then
				return r.name
			end
			error(r, 0)
		end
	end
	return nil
end

-- the names this runtime refuses with (SPEC-TABLES §3.4, §6.5;
-- docs/FIXED-FORM-ALGORITHM.md §5.3), for a caller that wants the spelling
F.Names = table.freeze({
	truncated = true, reserved_not_zero = true, previous_form = true, message_form_as_file = true,
	newer_form = true, layout_malformed = true, layout_unknown = true, layout_unsupported = true,
	layout_record_too_large = true, plan_too_large = true, no_layout = true, count_over_bound = true,
	length_over_bound = true, text_ill_formed = true, integrity_mismatch = true, non_finite_float = true,
	invalid_value = true,
})

return table.freeze(F)
