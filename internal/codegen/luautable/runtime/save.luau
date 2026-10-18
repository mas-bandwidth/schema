--!strict
-- save.luau — the VARIABLE table wire's SAVE walk (docs/SPEC-TABLES.md §3,
-- §3.1), driven by the descriptors schema_tables.luau registers: one walk per
-- table, byte for byte against the reference (internal/tablewire/encode.go and
-- the JavaScript leg internal/codegen/jstable/variableruntime.go). The form
-- byte, the root's fields, elision at the declared default, and the node table
-- for pointers in first-visit order. Every rule lives once here; the port of
-- the reference engine is the authority.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local St = require("./schema_tables")

local S = {}

local Form = 1
local NodeTableId = 0xffffffffffffffffi

local StringBlobId: integer = 0x704be0d8faaffc58i
local BytesBlobId: integer = 0x2f2ec0474f1c4fe4i

local writeFields: any
local writeField: any
local writeElement: any
local writeArmHeader: any
local writeKeyed: any
local sameBytes: any

-- ---- THE WRITER: a growable byte list, LEB128 offsets and lengths (§3) -----

local Writer = {}
Writer.__index = Writer

function Writer.new()
	local self = { b = {} }
	return setmetatable(self, Writer)
end

function Writer:len()
	return #self.b
end

function Writer:u8(v)
	table.insert(self.b, v % 256)
end

function Writer:u16(v)
	local v = math.floor(v) % 65536
	table.insert(self.b, v % 256)
	table.insert(self.b, math.floor(v / 256) % 256)
end

function Writer:u32(v)
	local v = math.floor(v) % 4294967296
	for i = 1, 4 do
		table.insert(self.b, v % 256)
		v = math.floor(v / 256)
	end
end

function Writer:u64Bytes(v: integer)
	-- v is a Luau integer (64-bit, two's complement); its eight bytes LE
	local x = v
	for i = 1, 8 do
		table.insert(self.b, tonumber(integer.band(x, 255i)))
		x = integer.rshift(x, 8i)
	end
end

function Writer:leb(v)
	local n = math.floor(v)
	while n >= 128 do
		table.insert(self.b, (n % 128) + 128)
		n = math.floor(n / 128)
	end
	table.insert(self.b, n)
end

function Writer:raw(bytes)
	if typeof(bytes) == "string" then
		for i = 1, bytes:len() do
			table.insert(self.b, bytes:byte(i))
		end
	else
		for i = 0, buffer.len(bytes) - 1 do
			table.insert(self.b, buffer.readu8(bytes, i))
		end
	end
end

function Writer:bytes()
	return buffer.fromstring(table.concat(self.b, ""))
end

local function blen(x)
	if typeof(x) == "string" then
		return x:len()
	end
	return buffer.len(x)
end

-- ---- IDS: intern ids in first-use order (§3), with mark/rollback for elision

local Ids = {}
Ids.__index = Ids

function Ids.new()
	return setmetatable({ list = {}, index = {} }, Ids)
end

function Ids:ref(id)
	local r = self.index[id]
	if r ~= nil then
		return r
	end
	table.insert(self.list, id)
	local n = #self.list
	self.index[id] = n
	return n
end

function Ids:mark()
	return #self.list
end

function Ids:rollback(m)
	while #self.list > m do
		local id = table.remove(self.list)
		self.index[id] = nil
	end
end

-- ---- FLOAT BIT SURGERY (docs/SPEC-TABLES.md §3; NarrowF32) --------------

local scratch = buffer.create(8)

local function narrowF32(v)
	if v ~= v then
		buffer.writef64(scratch, 0, v)
		local lo = buffer.readu32(scratch, 0)
		local hi = buffer.readu32(scratch, 4)
		local payload = ((hi % 1048576) * 8 + math.floor(lo / 536870912)) % 8388608
		if payload == 0 then
			payload = 4194304
		end
		local sign = math.floor(hi / 2147483648) * 2147483648
		return sign + 2139095040 + payload
	end
	buffer.writef32(scratch, 0, v)
	return buffer.readu32(scratch, 0)
end

local function fround(v)
	buffer.writef32(scratch, 0, v)
	return buffer.readf32(scratch, 0)
end

-- ---- THE NUMBERING: first-visit pre-order over pointer edges (§3.1) ------

local arrCount

local function edges(d, v, out)
	for _, fd in d.fields do
		local val = v[fd.slot]
		if fd.s == "sblob" or fd.s == "bblob" then
			if val ~= nil then
				table.insert(out, { blob = true, fd = fd, holder = v, val = val })
			end
		elseif fd.s == "ptr" then
			if val ~= nil then
				table.insert(out, { blob = false, d = St.lookup(fd.t), val = val })
			end
		elseif fd.s == "tab" then
			edges(St.lookup(fd.t), val, out)
		elseif fd.s == "arr" then
			local n = arrCount(fd, val)
			if fd.e.s == "ptr" then
				for i = 1, n do
					if val[i] ~= nil then
						table.insert(out, { blob = false, d = St.lookup(fd.e.t), val = val[i] })
					end
				end
			elseif fd.e.s == "tab" then
				for i = 1, n do
					edges(St.lookup(fd.e.t), val[i], out)
				end
			end
		end
	end
	return out
end

arrCount = function(fd, val)
	if fd.array == "list" then
		return #val
	end
	return math.min(#val, fd.bound)
end

local function blobNode(nodes, holder, fd, val)
	local typeId = fd.s == "sblob" and StringBlobId or BytesBlobId
	if fd.s == "sblob" and type(val) == "string" then
		local m = nodes.slots[holder]
		if m == nil then
			m = {}
			nodes.slots[holder] = m
		end
		if m[fd.slot] == nil then
			table.insert(nodes.list, { typeId = typeId, body = val, v = nil })
			m[fd.slot] = #nodes.list + 1
		end
		return
	end
	if nodes.index[val] ~= nil then
		return
	end
	table.insert(nodes.list, { typeId = typeId, body = val, v = nil })
	nodes.index[val] = #nodes.list + 1
end

local function number(d, root)
	local nodes = { list = {}, index = {}, slots = {} }
	nodes.index[root] = 1
	local open = { [root] = true }
	local stack = { { v = root, edges = edges(d, root, {}), i = 0 } }
	while #stack > 0 do
		local top = stack[#stack]
		if top.i >= #top.edges then
			open[top.v] = nil
			table.remove(stack)
		else
			top.i = top.i + 1
			local e = top.edges[top.i]
			if e.blob then
				blobNode(nodes, e.holder, e.fd, e.val)
			elseif open[e.val] then
				return nil
			elseif nodes.index[e.val] == nil then
				table.insert(nodes.list, { typeId = e.d.id, d = e.d, v = e.val })
				nodes.index[e.val] = #nodes.list + 1
				open[e.val] = true
				table.insert(stack, { v = e.val, edges = edges(e.d, e.val, {}), i = 0 })
			end
		end
	end
	return nodes
end

-- ---- INT AND ENUM WRITING -------------------------------------------------

local function writeInt(w, width, v)
	if width == 1 then
		w:u8(v)
	elseif width == 2 then
		w:u16(v)
	elseif width == 4 then
		w:u32(v)
	else
		w:u64Bytes(v)
	end
end

local function enumRef(e, ids, val)
	if val == 0 then
		return 0
	end
	local pair = e.vs[val]
	if pair ~= nil then
		return ids:ref(pair[2])
	end
	return -1
end

local function header(w, ids, fd, kind)
	w:leb(ids:ref(fd.id))
	w:u8(kind)
end

-- ---- THE SAVE WALK (docs/SPEC-TABLES.md §3) ----------------------------

local function writeBody(d, w, ids, nodes, v, path)
	if not writeFields(d, w, ids, nodes, v, path) then
		return false
	end
	w:u8(0)
	return true
end

writeFields = function(d, w, ids, nodes, v, path)
	for _, fd in d.fields do
		if not writeField(fd, w, ids, nodes, v, path) then
			return false
		end
	end
	return true
end

writeField = function(fd, w, ids, nodes, v, path)
	local val = v[fd.slot]
	if fd.s == "bool" then
		if val ~= fd.def then
			header(w, ids, fd, 1)
			w:u8(val and 1 or 0)
		end
		return true
	end
	if fd.s == "int" then
		if fd.dom == "int128" then
			local def = fd.def
			if val.lo ~= def.lo or val.hi ~= def.hi then
				header(w, ids, fd, fd.k)
				w:u64Bytes(val.lo)
				w:u64Bytes(val.hi)
			end
			return true
		end
		if val ~= fd.def then
			header(w, ids, fd, fd.k)
			writeInt(w, fd.kw, val)
		end
		return true
	end
	if fd.s == "float" then
		local diff
		if fd.f32 then
			diff = fround(val) ~= fround(fd.def)
		else
			diff = val ~= fd.def
		end
		if diff then
			header(w, ids, fd, fd.k)
			if fd.f32 then
				w:u32(narrowF32(val))
			else
				buffer.writef64(scratch, 0, val)
				for i = 1, 8 do
					table.insert(w.b, buffer.readu8(scratch, i - 1))
				end
			end
		end
		return true
	end
	if fd.s == "str" then
		if val ~= fd.def then
			header(w, ids, fd, 12)
			w:leb(blen(val))
			w:raw(val)
		end
		return true
	end
	if fd.s == "bytes" then
		if not sameBytes(val, fd.def) then
			header(w, ids, fd, 14)
			local body = Writer.new()
			body:u8(6)
			body:leb(blen(val))
			body:raw(val)
			w:leb(body:len())
			w:raw(body:bytes())
		end
		return true
	end
	if fd.s == "enum" then
		if val ~= fd.def then
			local ref = ids:ref(fd.id)
			local vref = enumRef(fd, ids, val)
			if vref < 0 then
				return false
			end
			w:leb(ref)
			w:u8(30)
			w:leb(vref)
		end
		return true
	end
	if fd.s == "ptr" then
		if val ~= nil then
			header(w, ids, fd, 17)
			w:leb(nodes.index[val])
		end
		return true
	end
	if fd.s == "sblob" or fd.s == "bblob" then
		if val ~= nil then
			local index
			if fd.s == "sblob" and type(val) == "string" then
				index = nodes.slots[v][fd.slot]
			else
				index = nodes.index[val]
			end
			header(w, ids, fd, 17)
			w:leb(index)
		end
		return true
	end
	if fd.s == "union" then
		if val == nil or val.Type == 0 then
			return true
		end
		local un = St.lookup(fd.t)
		if val.Type > #un.arms then
			return false
		end
		w:leb(ids:ref(fd.id))
		w:u8(15)
		return writeArmHeader(un, val.Type, w, ids, nodes, val, path)
	end
	if fd.s == "tab" then
		local mark = ids:mark()
		local ref = ids:ref(fd.id)
		local body = Writer.new()
		local sub = St.lookup(fd.t)
		table.insert(path, { o = fd.o, i = 0 })
		local ok = writeBody(sub, body, ids, nodes, val, path)
		table.remove(path)
		if not ok then
			return false
		end
		if body:len() <= 1 then
			ids:rollback(mark)
			return true
		end
		w:leb(ref)
		w:u8(13)
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if fd.s == "arr" then
		if fd.array == "keyed" then
			return writeKeyed(fd, w, ids, nodes, val, path)
		end
		local n = arrCount(fd, val)
		if n == 0 then
			return true
		end
		w:leb(ids:ref(fd.id))
		w:u8(14)
		local body = Writer.new()
		body:u8(fd.ek)
		body:leb(n)
		for i = 1, n do
			if not writeElement(fd, body, ids, nodes, val[i], path, i) then
				return false
			end
		end
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	return false
end

sameBytes = function(a, b)
	if blen(a) ~= blen(b) then
		return false
	end
	for i = 0, blen(a) - 1 do
		local ab = typeof(a) == "string" and a:byte(i + 1) or buffer.readu8(a, i)
		local bb = typeof(b) == "string" and b:byte(i + 1) or buffer.readu8(b, i)
		if ab ~= bb then
			return false
		end
	end
	return true
end

writeElement = function(fd, w, ids, nodes, val, path, i)
	local e = fd.e
	if e.s == "bool" then
		w:u8(val and 1 or 0)
		return true
	end
	if e.s == "int" then
		if e.dom == "int128" then
			w:u64Bytes(val.lo)
			w:u64Bytes(val.hi)
		else
			writeInt(w, e.kw, val)
		end
		return true
	end
	if e.s == "float" then
		if e.f32 then
			w:u32(narrowF32(val))
		else
			buffer.writef64(scratch, 0, val)
			for k = 1, 8 do
				table.insert(w.b, buffer.readu8(scratch, k - 1))
			end
		end
		return true
	end
	if e.s == "enum" then
		local vref = enumRef(e, ids, val)
		if vref < 0 then
			return false
		end
		w:leb(vref)
		return true
	end
	if e.s == "ptr" then
		w:leb(val == nil and 0 or nodes.index[val])
		return true
	end
	if e.s == "tab" then
		local body = Writer.new()
		local sub = St.lookup(e.t)
		table.insert(path, { o = fd.o, i = i })
		local ok = writeBody(sub, body, ids, nodes, val, path)
		table.remove(path)
		if not ok then
			return false
		end
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if e.s == "union" then
		if val.Type == 0 then
			w:leb(0)
			return true
		end
		local un = St.lookup(e.t)
		if val.Type > #un.arms then
			return false
		end
		return writeArmHeader(un, val.Type, w, ids, nodes, val, path)
	end
	return false
end

writeArmHeader = function(un, tag, w, ids, nodes, v, path)
	local arm = un.arms[tag]
	w:leb(ids:ref(arm.id))
	w:u8(arm.k)
	local body = Writer.new()
	if arm.s == "void" then
		w:leb(0)
		return true
	end
	if arm.s == "tab" then
		local sub = St.lookup(arm.t)
		if not writeBody(sub, body, ids, nodes, v[arm.slot], path) then
			return false
		end
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "ptr" then
		local val = v[arm.slot]
		w:leb(val == nil and 0 or nodes.index[val])
		return true
	end
	if arm.s == "bytes" then
		local val = v[arm.slot]
		body:u8(6)
		body:leb(blen(val))
		body:raw(val)
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "arr" then
		local n = arrCount(arm, v[arm.slot])
		body:u8(arm.ek)
		body:leb(n)
		for i = 1, n do
			if not writeElement(arm, body, ids, nodes, v[arm.slot][i], path, i) then
				return false
			end
		end
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "union" then
		local val = v[arm.slot]
		if val.Type == 0 then
			body:leb(0)
		else
			local inner = St.lookup(arm.t)
			if val.Type > #inner.arms then
				return false
			end
			if not writeArmHeader(inner, val.Type, body, ids, nodes, val, path) then
				return false
			end
		end
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "int" then
		local val = v[arm.slot]
		if arm.dom == "int128" then
			body:u64Bytes(val.lo)
			body:u64Bytes(val.hi)
		else
			writeInt(body, arm.kw, val)
		end
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "float" then
		if arm.f32 then
			body:u32(narrowF32(v[arm.slot]))
		else
			buffer.writef64(scratch, 0, v[arm.slot])
			for k = 1, 8 do
				table.insert(body.b, buffer.readu8(scratch, k - 1))
			end
		end
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "str" then
		body:raw(v[arm.slot])
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "bool" then
		body:u8(v[arm.slot] and 1 or 0)
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	if arm.s == "enum" then
		local vref = enumRef(arm, ids, v[arm.slot])
		if vref < 0 then
			return false
		end
		body:leb(vref)
		w:leb(body:len())
		w:raw(body:bytes())
		return true
	end
	return false
end

writeKeyed = function(fd, w, ids, nodes, v, path)
	local mark = ids:mark()
	local ref = ids:ref(fd.id)
	local body = Writer.new()
	local pairs = 0
	for slot = 1, #v do
		if v[slot] == nil then
			continue
		end
		local keyID = fd.e.vs[slot]
		if keyID == nil then
			return false
		end
		local slotMark = ids:mark()
		local keyRef = ids:ref(keyID[2])
		local elem: any
		local elemBytes
		if fd.ek == 13 then
			local b = Writer.new()
			local sub = St.lookup(fd.e.t)
			if not writeBody(sub, b, ids, nodes, v[slot], path) then
				return false
			end
			if b:len() <= 1 then
				ids:rollback(slotMark)
				continue
			end
			elemBytes = b:bytes()
		else
			local eb = Writer.new()
			if not writeElement(fd, eb, ids, nodes, v[slot], path, slot) then
				return false
			end
			elemBytes = eb:bytes()
		end
		body:leb(keyRef)
		body:leb(blen(elemBytes))
		body:raw(elemBytes)
		pairs = pairs + 1
	end
	if pairs == 0 then
		ids:rollback(mark)
		return true
	end
	w:leb(ref)
	w:u8(16)
	local inner = Writer.new()
	inner:u8(fd.ek)
	inner:leb(pairs)
	inner:raw(body:bytes())
	w:leb(inner:len())
	w:raw(inner:bytes())
	return true
end

-- ---- THE SAVE (docs/SPEC-TABLES.md §3, §3.1) --------------------------

function S.save(d, v)
	local nodes = number(d, v)
	if nodes == nil then
		return nil
	end
	local w = Writer.new()
	w:u8(Form)
	local ids = Ids.new()
	local path = {}
	if not writeFields(d, w, ids, nodes, v, path) then
		return nil
	end
	if #nodes.list > 0 then
		w:leb(ids:ref(NodeTableId))
		w:u8(12)
		local sub = Writer.new()
		sub:leb(#nodes.list)
		for _, n in nodes.list do
			sub:leb(ids:ref(n.typeId))
			if n.v == nil then
				sub:leb(blen(n.body))
				sub:raw(n.body)
			else
				local body = Writer.new()
				if not writeBody(n.d, body, ids, nodes, n.v, path) then
					return nil
				end
				sub:leb(body:len())
				sub:raw(body:bytes())
			end
		end
		w:leb(sub:len())
		w:raw(sub:bytes())
	end
	w:u8(0)
	for _, id in ids.list do
		w:u64Bytes(id)
	end
	w:u64Bytes(integer.fromstring(tostring(#ids.list)))
	return { bytes = w:bytes(), measure = w:len() }
end

return S
