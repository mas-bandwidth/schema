--!strict
-- save.moo — the moo table runtime's VARIABLE FORM (form 1) SAVE and its
-- MEASURE (docs/SPEC-TABLES.md §3, §3.1, §6.3). A port of the Luau leg's
-- save.luau (the dev branch) onto moo's values (table_values.moo;
-- docs/PORT-MOO.md), with main's rules: a value outside its contract (a
-- non-finite float, §3.3; an integer its kind cannot hold; text that is not
-- UTF-8 or past its bound) is REFUSED, never written. The walk is the Go
-- reference engine's (internal/tablewire encode.go, nodes.go, nodetable.go).
--
-- The save is DRIVEN BY THE DESCRIPTOR (docs/SPEC-TABLES.md §8.1): the form
-- byte, the root's fields in declared order with each elided at its declared
-- default, the node table LAST in the root body for the pointers in
-- first-visit order, the terminator, then the id table in first-use order and
-- its count. MEASURE is the same walk over a writer that counts and stores
-- nothing, so measure and save cannot be two rules.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local St = require("./schema_tables")
local V = require("./table_values")

local S = {}

local Form = 1
local NodeTableId = V.NodeTableId
local StringBlobId = V.StringBlobId
local BytesBlobId = V.BytesBlobId
local MapKeyId = V.MapKeyId
local Uint64 = V.Uint64

-- ---- THE WRITER (docs/SPEC-TABLES.md §3): little-endian, LEB128 lengths --
-- A measuring writer (count = true) stores nothing and only counts.

local function new_writer(count: boolean): any
	if count then
		return { b = nil, n = 0, count = true }
	end
	return { b = buffer.create(64), n = 0, count = false }
end

local function reserve(w: any, k: number)
	local need = w.n + k
	local cap = buffer.len(w.b)
	if need <= cap then
		return
	end
	while cap < need do
		cap *= 2
	end
	local nb = buffer.create(cap)
	buffer.copy(nb, 0, w.b, 0, w.n)
	w.b = nb
end

local function w_u8(w: any, v: number)
	if not w.count then
		reserve(w, 1)
		buffer.writeu8(w.b, w.n, v)
	end
	w.n += 1
end

local function w_u32(w: any, v: number)
	if not w.count then
		reserve(w, 4)
		buffer.writeu32(w.b, w.n, v)
	end
	w.n += 4
end

-- a raw integer at its kind's width (table_values.moo writeRaw)
local function w_raw_int(w: any, kw: number, signed: boolean, x: any)
	if not w.count then
		reserve(w, kw)
		V.writeRaw(w.b, w.n, kw, signed, x)
	end
	w.n += kw
end

-- THE ONE CANONICAL SPELLING of a length, count, index or reference (§3)
local function w_leb(w: any, v: number)
	while v >= 128 do
		w_u8(w, (v % 128) + 128)
		v = v // 128
	end
	w_u8(w, v)
end

local function w_buf(w: any, src: buffer, at: number, n: number)
	if not w.count then
		reserve(w, n)
		buffer.copy(w.b, w.n, src, at, n)
	end
	w.n += n
end

local function w_str(w: any, s: string)
	local n = #s
	if not w.count then
		reserve(w, n)
		buffer.writestring(w.b, w.n, s)
	end
	w.n += n
end

-- a nested writer's bytes, framed by the caller
local function w_sub(w: any, sub: any)
	if not w.count then
		reserve(w, sub.n)
		buffer.copy(w.b, w.n, sub.b, 0, sub.n)
	end
	w.n += sub.n
end

-- ---- THE ID TABLE (§3): ids interned in first-use order, with a mark and a
-- rollback so an elided field costs the id table nothing either

local function new_ids(): any
	return { list = {}, index = {} }
end

local function ref(ids: any, id: string): number
	local r = ids.index[id]
	if r ~= nil then
		return r
	end
	ids.list[#ids.list + 1] = id
	ids.index[id] = #ids.list
	return #ids.list
end

local function mark(ids: any): number
	return #ids.list
end

local function rollback(ids: any, m: number)
	while #ids.list > m do
		local id = ids.list[#ids.list]
		ids.list[#ids.list] = nil
		ids.index[id] = nil
	end
end

-- ---- THE REFUSAL: the first value outside its contract, by name ---------

local function refuse(c: any, name: string, fd: any): boolean
	if c.why == nil then
		local field = if fd ~= nil and fd.owner ~= nil then fd.owner .. "." .. fd.name else nil
		c.why = { name = name, field = field }
	end
	return false
end

-- ---- VALUES (docs/PORT-MOO.md, THE VALUE MAPPING) -------------------------

-- the used bytes of a wstring(N) or bytes(N) value: its buffer and the length
-- beside it, in BYTES (a wstring's Length is in code units)
local function used(v: any, slot: string, wide: boolean?): (buffer?, number)
	local b = v[slot]
	if type(b) ~= "buffer" then
		return nil, 0
	end
	local n = v[slot .. "Length"]
	if type(n) ~= "number" then
		n = buffer.len(b)
	end
	if wide then
		n *= 2
	end
	if n > buffer.len(b) or n < 0 then
		n = buffer.len(b)
	end
	return b, n
end

local function equals_def(b: buffer?, n: number, def: string?): boolean
	local d = def or ""
	if b == nil then
		return #d == 0
	end
	if n ~= #d then
		return false
	end
	return buffer.readstring(b, 0, n) == d
end

-- the live count of an array: a list's length, a counted array's count beside
-- it, a fixed array's bound (§2.9)
local function live_count(fd: any, holder: any, val: any): number
	if fd.array == "list" then
		if type(val) == "buffer" then
			return buffer.len(val)
		end
		if type(val) ~= "table" then
			return 0
		end
		return #val
	end
	if fd.array == "counted" then
		local c = holder[fd.slot .. "Count"]
		if type(c) ~= "number" then
			c = 0
		end
		return math.min(c, fd.bound)
	end
	return fd.bound
end

local function elem_at(val: any, i: number): any
	if type(val) == "buffer" then
		return buffer.readu8(val, i - 1)
	end
	return val[i]
end

-- the bytes one text-list element contributes to its total (§2.13)
local function text_elem_used(e: any, x: any): number
	if e == nil or x == nil or x == false then
		return 0
	end
	if e.s == "str" or e.s == "text" then
		if type(x) == "string" then
			return #x
		end
		return 0
	end
	if e.s == "tab" then
		local vf = St.lookup(e.t).fields[1]
		if vf == nil then
			return 0
		end
		if vf.present ~= nil and not x[vf.present] then
			return 0
		end
		local s = x[vf.slot]
		if type(s) == "string" then
			return #s
		end
	end
	return 0
end

-- ---- MAP ORDER (§2.8; internal/tabletext MapKeyOrder): ascending by key, a
-- string key by its bytes, a signed integer key signed, every other unsigned

local function key_less(keyfd: any, a: any, b: any): boolean
	local ka = a[keyfd.slot]
	local kb = b[keyfd.slot]
	if type(ka) == "string" and type(kb) == "string" then
		return ka < kb
	end
	if type(ka) == "boolean" then
		return (not ka) and kb
	end
	if type(ka) == "number" and type(kb) == "number" then
		return ka < kb
	end
	local C = if keyfd.cls == "I64" then V.Int64 elseif keyfd.cls == "U64" then V.Uint64 elseif keyfd.cls == "I128" then V.Int128 else V.Uint128
	local ok, r = pcall(C.cmp, ka, kb)
	return ok and r < 0
end

local function map_order(fd: any, entries: { any }): { any }
	local entry = St.lookup(fd.t)
	local keyfd: any = nil
	for _, f in entry.fields do
		if f.id == MapKeyId then
			keyfd = f
		end
	end
	local order = table.clone(entries)
	if keyfd ~= nil then
		table.sort(order, function(a: any, b: any): boolean
			return key_less(keyfd, a, b)
		end)
	end
	return order
end

-- a field's guard holds when every (bool slot, value) pair matches; a field
-- under a FALSE GUARD is elided whatever its storage holds (§3), and its
-- edges are not visited either (§3.1)
local function guard_holds(fd: any, v: any): boolean
	if fd.guard == nil then
		return true
	end
	for _, pair in fd.guard do
		if v[pair[1]] ~= pair[2] then
			return false
		end
	end
	return true
end

-- ---- THE NUMBERING (§3.1; internal/tablewire nodes.go) -------------------
--
-- The first-visit order of a depth-first pre-order walk from the root over
-- POINTER EDGES, descending through every by-value edge to reach them. Node
-- index 1 is the root and record k is index k + 1. A reference to a node whose
-- descent is still open is a DATA CYCLE, and the save refuses it. A field the
-- writer does not write is not an edge.

local edges: any

local function arm_edges(fd: any, u: any, out: { any })
	if type(u) ~= "table" or u.Type == nil or u.Type == 0 then
		return
	end
	local un = St.lookup(fd.t)
	local arm = un.arms[u.Type]
	if arm == nil or arm.s == "void" then
		return
	end
	local val = u[arm.slot]
	if arm.s == "tab" then
		edges(St.lookup(arm.t), val, out)
	elseif arm.s == "sblob" or arm.s == "bblob" then
		if val ~= nil then
			out[#out + 1] = { blob = arm.s, val = val, field = un.name .. "." .. arm.name }
		end
	elseif arm.s == "ptr" then
		if val ~= nil then
			out[#out + 1] = { d = St.lookup(arm.t), val = val }
		end
	elseif arm.s == "arr" and arm.e ~= nil and arm.e.s == "ptr" then
		local d = St.lookup(arm.e.t)
		for i = 1, live_count(arm, u, val) do
			if val[i] then
				out[#out + 1] = { d = d, val = val[i] }
			end
		end
	elseif arm.s == "arr" and arm.e ~= nil and (arm.e.s == "sblob" or arm.e.s == "bblob") then
		for i = 1, live_count(arm, u, val) do
			if val[i] then
				out[#out + 1] = { blob = arm.e.s, val = val[i], field = un.name .. "." .. arm.name }
			end
		end
	elseif arm.s == "union" then
		arm_edges(arm, val, out)
	end
end

edges = function(d: any, v: any, out: { any }): { any }
	if type(v) ~= "table" then
		return out
	end
	for _, fd in d.fields do
		if not guard_holds(fd, v) then
			continue
		end
		if fd.present ~= nil and not v[fd.present] then
			continue
		end
		local val = v[fd.slot]
		local s = fd.s
		if s == "sblob" or s == "bblob" then
			if val ~= nil then
				out[#out + 1] = { blob = s, val = val, field = d.name .. "." .. fd.name }
			end
		elseif s == "ptr" then
			if val ~= nil then
				out[#out + 1] = { d = St.lookup(fd.t), val = val }
			end
		elseif s == "union" then
			arm_edges(fd, val, out)
		elseif s == "map" then
			if type(val) == "table" then
				local entry = St.lookup(fd.t)
				for _, ev in map_order(fd, val) do
					edges(entry, ev, out)
				end
			end
		elseif s == "tab" then
			edges(St.lookup(fd.t), val, out)
		elseif s == "arr" and type(val) == "table" then
			local e = fd.e
			if e.s == "ptr" then
				local pd = St.lookup(e.t)
				for i = 1, live_count(fd, v, val) do
					if val[i] then
						out[#out + 1] = { d = pd, val = val[i] }
					end
				end
			elseif e.s == "sblob" or e.s == "bblob" then
				for i = 1, live_count(fd, v, val) do
					if val[i] then
						out[#out + 1] = { blob = e.s, val = val[i], field = d.name .. "." .. fd.name }
					end
				end
			elseif e.s == "union" then
				for i = 1, live_count(fd, v, val) do
					arm_edges(e, val[i], out)
				end
			elseif e.s == "tab" then
				local td = St.lookup(e.t)
				for i = 1, live_count(fd, v, val) do
					if val[i] ~= nil then
						edges(td, val[i], out)
					end
				end
			end
		end
	end
	return out
end

-- THE WALK HOLDS ITS OWN STACK: a pointer chain is as deep as it is long.
-- shareBlobs is the cook's numbering (tablewire.NumberPacking, §7.6): a byte
-- buffer named from two slots is one node there, where the wire refuses it.
local function number(d: any, root: any, shareBlobs: boolean?): (any, any?)
	local nodes: any = { list = {}, index = {} }
	nodes.index[root] = 1
	local open: { [any]: boolean } = { [root] = true }
	local stack = { { v = root, edges = edges(d, root, {}), i = 0 } }
	while #stack > 0 do
		local top = stack[#stack]
		if top.i >= #top.edges then
			open[top.v] = nil
			stack[#stack] = nil
		else
			top.i += 1
			local e = top.edges[top.i]
			if open[e.val] then
				return nil, { name = "cycle", field = d.name }
			end
			-- A BLOB IS OWNED BY ONE SLOT (§2.5): a second slot naming the same
			-- buffer is refused by name, blob_shared
			if e.blob ~= nil and nodes.index[e.val] ~= nil and not shareBlobs then
				return nil, { name = "blob_shared", field = e.field }
			end
			if nodes.index[e.val] == nil then
				if e.blob ~= nil then
					if type(e.val) ~= "buffer" then
						return nil, { name = "invalid_value", field = e.field }
					end
					nodes.list[#nodes.list + 1] = { typeId = if e.blob == "sblob" then StringBlobId else BytesBlobId, blob = e.val, str = e.blob == "sblob", field = e.field }
					nodes.index[e.val] = #nodes.list + 1
				else
					if type(e.val) ~= "table" then
						return nil, { name = "invalid_value", field = e.d.name }
					end
					nodes.list[#nodes.list + 1] = { typeId = e.d.id, d = e.d, v = e.val }
					nodes.index[e.val] = #nodes.list + 1
					open[e.val] = true
					stack[#stack + 1] = { v = e.val, edges = edges(e.d, e.val, {}), i = 0 }
				end
			end
		end
	end
	return nodes, nil
end

-- ---- THE SAVE WALK (§3; internal/tablewire encode.go) ---------------------

local write_fields: any
local write_value: any
local write_arm_header: any

-- a field header is the id REFERENCE and the kind byte, the whole of it (§3)
local function header(c: any, w: any, id: string, kind: number)
	w_leb(w, ref(c.ids, id))
	w_u8(w, kind)
end

-- an enum value's reference (§2.15): the variant name's id, 0 for None, and
-- -1 for a value no live variant names, which the save refuses (§5)
local function enum_ref(c: any, e: any, val: any): number
	if val == nil or val == 0 then
		return 0
	end
	if type(val) ~= "number" then
		return -1
	end
	local pair = e.vs[val]
	if pair == nil or pair[3] == true then
		return -1
	end
	return ref(c.ids, pair[2])
end

-- one integer-shaped value at its kind's width (§2.11, §3)
local function write_int(c: any, w: any, e: any, val: any, named: any): boolean
	local x = V.toRaw(e, val)
	if x == nil then
		return refuse(c, "invalid_value", named)
	end
	w_raw_int(w, e.kw, e.signed == true, x)
	return true
end

-- one float as its bits (§3): binary32 a number, binary64 a Uint64; a NaN or
-- an infinity is refused on write (§3.3)
local function write_float(c: any, w: any, e: any, val: any, named: any): boolean
	if e.f32 then
		if type(val) ~= "number" or val ~= math.floor(val) or val < 0 or val > 0xFFFFFFFF then
			return refuse(c, "invalid_value", named)
		end
		if not V.finite32(val) then
			return refuse(c, "non_finite_float", named)
		end
		w_u32(w, val)
		return true
	end
	if not Uint64.is(val) then
		return refuse(c, "invalid_value", named)
	end
	if not V.finite64(val[1], val[2]) then
		return refuse(c, "non_finite_float", named)
	end
	w_u32(w, val[1])
	w_u32(w, val[2])
	return true
end

-- RETAIN-UNKNOWN's hook (docs/SPEC-TABLES.md §6.6; table_retain.moo): nil on
-- every plain save. A retaining save sets it for the one walk, and the kept
-- records go back AT THE END OF THEIR OWN BODY, through the id table this
-- save interns into (RETAIN_API).
local _retain: any = nil
local RETAIN_API = table.freeze({ ref = ref, mark = mark, rollback = rollback, str = w_str })

-- one body: its fields in declaration order, the retained tail when a
-- retaining save is on, then the one-byte zero reference that terminates it
-- (§3)
local function write_body(c: any, d: any, w: any, v: any): boolean
	if type(v) ~= "table" then
		return refuse(c, "invalid_value", nil)
	end
	if not write_fields(c, d, w, v) then
		return false
	end
	if _retain ~= nil then
		_retain.tail(c, w, v, RETAIN_API)
	end
	w_u8(w, 0)
	return true
end

-- a nested body built before the length that frames it (§3)
local function body_of(c: any, d: any, v: any): any
	local b = new_writer(c.count)
	if not write_body(c, d, b, v) then
		return nil
	end
	return b
end

-- moo text in a kind-12 element or arm: well-formed UTF-8, no zero byte, at
-- most its bound (bound 0 is none)
local function text_ok(s: any, bound: number?): boolean
	if type(s) ~= "string" then
		return false
	end
	if bound ~= nil and bound > 0 and #s > bound then
		return false
	end
	return V.utf8String(s)
end

-- one element at its element kind's own framing (§2.9, §3): a table element
-- carries its own length, a pointer its node index (0 is null), an enum its
-- variant reference, a text element its own L, a scalar its width. len is the
-- Length beside a wide or bytes element.
write_value = function(c: any, e: any, w: any, val: any, len: number?, named: any): boolean
	local s = e.s
	if s == "bool" then
		if type(val) ~= "boolean" then
			return refuse(c, "invalid_value", named)
		end
		w_u8(w, if val then 1 else 0)
	elseif s == "int" then
		return write_int(c, w, e, val, named)
	elseif s == "float" then
		return write_float(c, w, e, val, named)
	elseif s == "enum" then
		local vref = enum_ref(c, e, val)
		if vref < 0 then
			return refuse(c, "invalid_value", named)
		end
		w_leb(w, vref)
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		w_leb(w, if not val then 0 else c.nodes.index[val])
	elseif s == "tab" then
		local b = body_of(c, St.lookup(e.t), val)
		if b == nil then
			return false
		end
		w_leb(w, b.n)
		w_sub(w, b)
	elseif s == "union" then
		if type(val) ~= "table" or val.Type == 0 then
			w_leb(w, 0) -- a None element is the single zero byte
			return true
		end
		return write_arm_header(c, St.lookup(e.t), w, val)
	elseif s == "str" and e.wide then
		if type(val) ~= "buffer" then
			return refuse(c, "invalid_value", named)
		end
		local units = len or (buffer.len(val) // 2)
		if units * 2 > buffer.len(val) or (e.bound ~= nil and units > e.bound) or not V.wide(val, 0, units * 2) then
			return refuse(c, "invalid_value", named)
		end
		w_leb(w, units * 2)
		w_buf(w, val, 0, units * 2)
	elseif s == "str" or s == "text" then
		local text = if val == nil then "" else val
		if not text_ok(text, if s == "str" then e.bound else nil) then
			return refuse(c, "invalid_value", named)
		end
		w_leb(w, #text)
		w_str(w, text)
	else
		return refuse(c, "invalid_value", named)
	end
	return true
end

-- a positional array body under kind 14: the element kind, the count, then
-- the elements (§2.9, §3)
local function write_array(c: any, fd: any, w: any, holder: any, val: any, count: number): boolean
	if fd.bound ~= nil and fd.bound > 0 and fd.array ~= "list" and count > fd.bound then
		return refuse(c, "invalid_value", fd)
	end
	header(c, w, fd.id, 14)
	local b = new_writer(c.count)
	w_u8(b, fd.ek)
	w_leb(b, count)
	local lens = holder[fd.slot .. "Length"]
	for i = 1, count do
		local len: number? = nil
		if type(lens) == "table" then
			len = lens[i]
		end
		if not write_value(c, fd.e, b, elem_at(val, i), len, fd) then
			return false
		end
	end
	w_leb(w, b.n)
	w_sub(w, b)
	return true
end

-- the element's default, the value a reader prefills (§3, §5)
local function elem_is_default(e: any, val: any, len: number?): boolean
	local s = e.s
	if s == "bool" then
		return val == false or val == nil
	end
	if s == "int" or s == "float" then
		return V.sameStorage(e, val, e.def)
	end
	if s == "enum" then
		return val == 0 or val == nil
	end
	if s == "ptr" or s == "sblob" or s == "bblob" then
		return not val
	end
	if s == "union" then
		return type(val) ~= "table" or val.Type == 0
	end
	if s == "str" and e.wide then
		if type(val) ~= "buffer" then
			return true
		end
		return (len or 0) == 0
	end
	if s == "str" or s == "text" then
		return val == nil or val == ""
	end
	return false
end

-- AN ENUM-KEYED ARRAY (§3.2): kind 16, then one (key reference, L, element)
-- triple per slot not at its default, in variant order. An array with no
-- such slot is not written at all.
local function write_keyed(c: any, fd: any, w: any, val: any): boolean
	local m = mark(c.ids)
	local fref = ref(c.ids, fd.id)
	local body = new_writer(c.count)
	local count = 0
	local e = fd.e
	for slot = 1, fd.bound do
		local keyvs = fd.keyvs
		local x = elem_at(val, slot)
		if keyvs == nil or keyvs[slot] == nil then
			continue
		end
		local sm = mark(c.ids)
		local kref = ref(c.ids, keyvs[slot][2])
		local eb: any
		if e.s == "tab" then
			eb = body_of(c, St.lookup(e.t), x)
			if eb == nil then
				return false
			end
			if eb.n <= 1 then
				rollback(c.ids, sm)
				continue
			end
		else
			if elem_is_default(e, x, nil) then
				rollback(c.ids, sm)
				continue
			end
			eb = new_writer(c.count)
			if not write_value(c, e, eb, x, nil, fd) then
				return false
			end
		end
		w_leb(body, kref)
		w_leb(body, eb.n)
		w_sub(body, eb)
		count += 1
	end
	if count == 0 then
		rollback(c.ids, m)
		return true
	end
	w_leb(w, fref)
	w_u8(w, 16)
	local inner = new_writer(c.count)
	w_u8(inner, fd.ek)
	w_leb(inner, count)
	w_sub(inner, body)
	w_leb(w, inner.n)
	w_sub(w, inner)
	return true
end

-- A MAP (§2.8): kind 14 over element kind 13, its entries in ASCENDING KEY
-- ORDER. An empty map elides, EXCEPT when `required` or a PRESENT optional
-- forces it: then the header and the two-byte empty body `0d 00` ride.
local function write_map(c: any, fd: any, w: any, val: any, required: boolean?): boolean
	local n = if type(val) == "table" then #val else 0
	if n == 0 and not required then
		return true
	end
	header(c, w, fd.id, 14)
	local entry = St.lookup(fd.t)
	local b = new_writer(c.count)
	w_u8(b, 13)
	w_leb(b, n)
	if n > 0 then
		for _, ev in map_order(fd, val) do
			local eb = body_of(c, entry, ev)
			if eb == nil then
				return false
			end
			w_leb(b, eb.n)
			w_sub(b, eb)
		end
	end
	w_leb(w, b.n)
	w_sub(w, b)
	return true
end

-- ONE ARM's payload, the bytes under its L (§3's arm payload table)
local function arm_payload(c: any, arm: any, w: any, u: any): boolean
	local s = arm.s
	local val = u[arm.slot]
	if s == "tab" then
		return write_body(c, St.lookup(arm.t), w, val)
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		w_leb(w, if not val then 0 else c.nodes.index[val])
	elseif s == "str" and arm.wide then
		local b, n = used(u, arm.slot, true)
		if b ~= nil and n > 0 then
			if not V.wide(b, 0, n) or (arm.bound ~= nil and n // 2 > arm.bound) then
				return refuse(c, "invalid_value", arm)
			end
			w_buf(w, b, 0, n)
		end
	elseif s == "str" or s == "text" then
		local text = if val == nil then "" else val
		if not text_ok(text, if s == "str" then arm.bound else nil) then
			return refuse(c, "invalid_value", arm)
		end
		w_str(w, text)
	elseif s == "bytes" then
		local b, n = used(u, arm.slot, false)
		w_u8(w, 6) -- bytes ride as an array of u8 (§2.5)
		w_leb(w, n)
		if b ~= nil and n > 0 then
			w_buf(w, b, 0, n)
		end
	elseif s == "arr" then
		local count = live_count(arm, u, val)
		w_u8(w, arm.ek)
		w_leb(w, count)
		local lens = u[arm.slot .. "Length"]
		for i = 1, count do
			local len: number? = nil
			if type(lens) == "table" then
				len = lens[i]
			end
			if not write_value(c, arm.e, w, elem_at(val, i), len, arm) then
				return false
			end
		end
	elseif s == "union" then
		if type(val) ~= "table" or val.Type == 0 then
			w_leb(w, 0)
		else
			return write_arm_header(c, St.lookup(arm.t), w, val)
		end
	else
		return write_value(c, arm, w, val, nil, arm)
	end
	return true
end

-- AN ARM HEADER IS A FIELD HEADER (§3): the arm id reference, the arm's kind
-- byte (32 for a payload-free arm, §2.6), L, then L bytes of payload. A tag no
-- arm names has no wire identity, and the save refuses it (§5).
write_arm_header = function(c: any, un: any, w: any, u: any): boolean
	local arm = un.arms[u.Type]
	if arm == nil then
		return refuse(c, "invalid_value", nil)
	end
	w_leb(w, ref(c.ids, arm.id))
	if arm.s == "void" then
		w_u8(w, 32)
		w_leb(w, 0)
		return true
	end
	w_u8(w, arm.k)
	local b = new_writer(c.count)
	if not arm_payload(c, arm, b, u) then
		return false
	end
	w_leb(w, b.n)
	w_sub(w, b)
	return true
end

-- a scalar or an enum at its declared default (§3, internal/tablewire
-- cellIsDefault): an integer in either spelling, a float by its BITS
local function at_default(fd: any, val: any): boolean
	local s = fd.s
	if s == "bool" then
		return (val == true) == (fd.def == true)
	end
	if s == "enum" then
		return val == fd.def
	end
	return V.sameStorage(fd, val, fd.def)
end

-- ONE FIELD, elided at its declared default (§3): a pointer and a blob at
-- null, a scalar at its default, a string at its default bytes, an array when
-- empty (a fixed one when every element is at its default; a fixed array of
-- tables always rides), a map when empty, a union at None, a nested table when
-- its body is its terminator alone, the field's id rolled back with it. A
-- present optional always rides; an absent one never does (§2.3).
local function write_field(c: any, fd: any, w: any, v: any): boolean
	if not guard_holds(fd, v) then
		return true
	end
	local s = fd.s
	local val = v[fd.slot]
	-- `| required` (§2.3): the writer ALWAYS writes the field when its guard
	-- holds. A required integer whose declared range excludes its stored
	-- value has no wire value, and the save refuses, as Go's writer does.
	local required = fd.required == true
	if required and s == "int" and fd.lo ~= nil then
		local x = V.toRaw(fd, val)
		if x ~= nil and (V.rawCmp(x, fd.lo, fd.kw, fd.signed == true) < 0 or V.rawCmp(x, fd.hi, fd.kw, fd.signed == true) > 0) then
			return refuse(c, "invalid_value", fd)
		end
	end
	-- A TEXT LIST'S TOTAL (§2.13): a list whose element bytes sum past the
	-- total is refused, as a count past [..N] is
	if fd.total ~= nil and s == "arr" and type(val) == "table" then
		local spent = 0
		for i = 1, live_count(fd, v, val) do
			spent += text_elem_used(fd.e, elem_at(val, i))
		end
		if spent > fd.total then
			return refuse(c, "invalid_value", fd)
		end
	end

	if s == "ptr" or s == "sblob" or s == "bblob" then
		-- NULL IS INDEX 0 AND NULL IS ELIDED; a non-null pointer always rides
		if val == nil then
			if not required then
				return true
			end
			header(c, w, fd.id, 17)
			w_leb(w, 0)
			return true
		end
		header(c, w, fd.id, 17)
		w_leb(w, c.nodes.index[val])
		return true
	end
	if s == "map" then
		if fd.present ~= nil then
			if not v[fd.present] then
				return true
			end
			return write_map(c, fd, w, val, true)
		end
		return write_map(c, fd, w, val, required)
	end
	if fd.present ~= nil then
		if not v[fd.present] then
			return true
		end
		if s == "arr" or s == "bytes" then
			if s == "bytes" then
				local b, n = used(v, fd.slot, false)
				if n > fd.bound then
					return refuse(c, "invalid_value", fd)
				end
				header(c, w, fd.id, 14)
				w_leb(w, 1 + V.lebLen(n) + n)
				w_u8(w, 6)
				w_leb(w, n)
				if b ~= nil and n > 0 then
					w_buf(w, b, 0, n)
				end
				return true
			end
			return write_array(c, fd, w, v, val, live_count(fd, v, val))
		elseif s == "tab" then
			local fref = ref(c.ids, fd.id)
			local b = body_of(c, St.lookup(fd.t), val)
			if b == nil then
				return false
			end
			w_leb(w, fref)
			w_u8(w, 13)
			w_leb(w, b.n)
			w_sub(w, b)
			return true
		end
		header(c, w, fd.id, fd.k)
		if s == "str" and fd.wide then
			local b, n = used(v, fd.slot, true)
			if b ~= nil and (n // 2 > fd.bound or not V.wide(b, 0, n)) then
				return refuse(c, "invalid_value", fd)
			end
			w_leb(w, n)
			if b ~= nil and n > 0 then
				w_buf(w, b, 0, n)
			end
			return true
		elseif s == "union" then
			if type(val) ~= "table" or val.Type == 0 then
				w_leb(w, 0)
				return true
			end
			return write_arm_header(c, St.lookup(fd.t), w, val)
		end
		return write_value(c, fd, w, val, nil, fd)
	end
	if s == "arr" and fd.array == "keyed" then
		return write_keyed(c, fd, w, val)
	end
	if s == "str" and fd.wide then
		local b, n = used(v, fd.slot, true)
		if not required and n == 0 then
			return true -- wide text takes no specified default
		end
		if b ~= nil and (n // 2 > fd.bound or not V.wide(b, 0, n)) then
			return refuse(c, "invalid_value", fd)
		end
		header(c, w, fd.id, fd.k)
		w_leb(w, n)
		if b ~= nil and n > 0 then
			w_buf(w, b, 0, n)
		end
		return true
	end
	if s == "str" or s == "text" then
		local text = if val == nil then "" else val
		if not required and text == (fd.def or "") then
			return true
		end
		if not text_ok(text, if s == "str" then fd.bound else nil) then
			return refuse(c, "invalid_value", fd)
		end
		header(c, w, fd.id, 12)
		w_leb(w, #text)
		w_str(w, text)
		return true
	end
	if s == "bytes" then
		local b, n = used(v, fd.slot, false)
		if not required and equals_def(b, n, fd.def) then
			return true
		end
		if n > fd.bound then
			return refuse(c, "invalid_value", fd)
		end
		-- L IS THE ARRAY BODY'S LENGTH (§3): the element kind, the count's
		-- own LEB128 bytes, then the bytes
		header(c, w, fd.id, 14)
		w_leb(w, 1 + V.lebLen(n) + n)
		w_u8(w, 6)
		w_leb(w, n)
		if b ~= nil and n > 0 then
			w_buf(w, b, 0, n)
		end
		return true
	end
	if s == "arr" then
		if fd.array == "list" or fd.array == "counted" then
			local n = live_count(fd, v, val)
			if n == 0 and not required then
				return true
			end
			return write_array(c, fd, w, v, val, n)
		end
		if fd.e.s == "tab" then
			-- a fixed array of tables always rides: position is identity there
			return write_array(c, fd, w, v, val, fd.bound)
		end
		local lens = v[fd.slot .. "Length"]
		local all = true
		for i = 1, fd.bound do
			local len: number? = nil
			if type(lens) == "table" then
				len = lens[i]
			end
			if not elem_is_default(fd.e, elem_at(val, i), len) then
				all = false
				break
			end
		end
		if all and not required then
			return true
		end
		return write_array(c, fd, w, v, val, fd.bound)
	end
	if s == "union" then
		if type(val) ~= "table" or val.Type == 0 then
			if not required then
				return true -- None elides
			end
			w_leb(w, ref(c.ids, fd.id))
			w_u8(w, 15)
			w_leb(w, 0)
			return true
		end
		local un = St.lookup(fd.t)
		if un.arms[val.Type] == nil then
			return refuse(c, "invalid_value", fd)
		end
		w_leb(w, ref(c.ids, fd.id))
		w_u8(w, 15)
		return write_arm_header(c, un, w, val)
	end
	if s == "tab" then
		-- AN ELIDED FIELD COSTS NOTHING IN THE ID TABLE EITHER (§3)
		local m = mark(c.ids)
		local fref = ref(c.ids, fd.id)
		local b = body_of(c, St.lookup(fd.t), val)
		if b == nil then
			return false
		end
		if b.n <= 1 and not required then
			rollback(c.ids, m)
			return true
		end
		w_leb(w, fref)
		w_u8(w, 13)
		w_leb(w, b.n)
		w_sub(w, b)
		return true
	end
	if s ~= "bool" and s ~= "float" and s ~= "int" and s ~= "enum" then
		return refuse(c, "invalid_value", fd)
	end
	if not required and at_default(fd, val) then
		return true
	end
	header(c, w, fd.id, fd.k)
	return write_value(c, fd, w, val, nil, fd)
end

-- a body's fields in declaration order (§3)
write_fields = function(c: any, d: any, w: any, v: any): boolean
	for _, fd in d.fields do
		if not write_field(c, fd, w, v) then
			return false
		end
	end
	return true
end

-- THE SAVE (§3, §3.1, §2.14): the form byte, the root's fields, the node
-- table LAST in the root body under the reserved id at kind 12, the
-- terminator, then the id table and its count, and for a root that declares
-- integrity = "crc32c" the four-byte CRC-32C trailer.
local function walk(d: any, v: any, count: boolean): (any, any?)
	local nodes, why = number(d, v)
	if nodes == nil then
		return nil, why
	end
	local c: any = { ids = new_ids(), nodes = nodes, count = count, why = nil }
	local w = new_writer(count)
	w_u8(w, Form)
	if not write_fields(c, d, w, v) then
		return nil, c.why
	end
	-- in the ROOT body the retained tail sits BEFORE the node table (§6.6)
	if _retain ~= nil then
		_retain.tail(c, w, v, RETAIN_API)
	end
	if #nodes.list > 0 then
		w_leb(w, ref(c.ids, NodeTableId))
		w_u8(w, 12)
		local p = new_writer(count)
		w_leb(p, #nodes.list)
		for _, n in nodes.list do
			w_leb(p, ref(c.ids, n.typeId))
			if n.blob ~= nil then
				-- a BYTE BUFFER's record is the bytes themselves (§2.5); a
				-- string buffer's are UTF-8
				local len = buffer.len(n.blob)
				if n.str and not V.utf8(n.blob, 0, len) then
					return nil, { name = "invalid_value", field = n.field }
				end
				w_leb(p, len)
				w_buf(p, n.blob, 0, len)
			else
				local b = body_of(c, n.d, n.v)
				if b == nil then
					return nil, c.why
				end
				w_leb(p, b.n)
				w_sub(p, b)
			end
		end
		w_leb(w, p.n)
		w_sub(w, p)
	end
	w_u8(w, 0)
	-- THE ID TABLE IS THE LAST THING IN THE FILE: the entries, then the count
	for _, id in c.ids.list do
		w_str(w, id)
	end
	w_u32(w, #c.ids.list)
	w_u32(w, 0)
	if d.integrity ~= nil and d.integrity ~= "" then
		if not w.count then
			local sum = V.crc32c(w.b, w.n)
			reserve(w, 4)
			buffer.writeu32(w.b, w.n, sum)
		end
		w.n += 4
	end
	return w, nil
end

-- Validate enum retirement before the walk, so every form-1 writer returns
-- the standard named refusal (§2.15).
local function invalid_deprecated(d: any, v: any, seen: any): any
	if type(v) ~= "table" or seen[v] then
		return nil
	end
	seen[v] = true
	for _, fd in d.fields do
		if not guard_holds(fd, v) or (fd.present ~= nil and not v[fd.present]) then
			continue
		end
		local val = v[fd.slot]
		if fd.keyvs ~= nil and val ~= nil then
			for slot, pair in fd.keyvs do
				if pair[3] == true and not elem_is_default(fd.e, elem_at(val, slot), nil) then
					return { name = "invalid_value", field = d.name .. "." .. fd.name, variant = pair[1] }
				end
			end
		end
		local e = if fd.s == "arr" then fd.e else fd
		if e ~= nil and e.s == "enum" and val ~= nil then
			local limit = if fd.s == "arr" then live_count(fd, v, val) else 1
			for i = 1, limit do
				local ordinal = if fd.s == "arr" then elem_at(val, i) else val
				local pair = if ordinal == nil then nil else e.vs[ordinal]
				if pair ~= nil and pair[3] == true then
					return { name = "invalid_value", field = d.name .. "." .. fd.name, variant = pair[1] }
				end
			end
		elseif fd.s == "tab" then
			local reason = invalid_deprecated(St.lookup(fd.t), val, seen)
			if reason ~= nil then
				return reason
			end
		elseif fd.s == "arr" and fd.e ~= nil and fd.e.s == "tab" and type(val) == "table" then
			for i = 1, live_count(fd, v, val) do
				local reason = invalid_deprecated(St.lookup(fd.e.t), elem_at(val, i), seen)
				if reason ~= nil then
					return reason
				end
			end
		end
	end
	return nil
end

-- save is the root value's form-1 wire as a buffer of exactly its bytes, or
-- nil and the refusal { name, field } (a data cycle, a shared blob, a value
-- with no wire identity or outside its contract) (§3, §3.1, §5).
function S.save(d: any, v: any): (buffer?, any?)
	if type(v) ~= "table" then
		return nil, { name = "nil_value", field = d.name }
	end
	local invalid = invalid_deprecated(d, v, {})
	if invalid ~= nil then
		return nil, invalid
	end
	local w, why = walk(d, v, false)
	if w == nil then
		return nil, why
	end
	local out = buffer.create(w.n)
	buffer.copy(out, 0, w.b, 0, w.n)
	return out, nil
end

-- measure is the length save writes, by the same walk storing nothing, or nil
-- and the refusal (§9's measure/save split).
function S.measure(d: any, v: any): (number?, any?)
	if type(v) ~= "table" then
		return nil, { name = "nil_value", field = d.name }
	end
	local invalid = invalid_deprecated(d, v, {})
	if invalid ~= nil then
		return nil, invalid
	end
	local w, why = walk(d, v, true)
	if w == nil then
		return nil, why
	end
	return w.n, nil
end

-- number is the numbering (§3.1), shared with the message form's writer
-- (table_message.moo), so both forms number one graph one way
S.number = number

-- setRetain installs (or, with nil, clears) RETAIN-UNKNOWN's save hook
-- (table_retain.moo).
function S.setRetain(h: any)
	_retain = h
end

return table.freeze(S)
