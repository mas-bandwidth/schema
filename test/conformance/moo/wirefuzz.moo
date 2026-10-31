--!strict
-- wirefuzz.moo — THE MOO LEG's WIRE-FUZZ DRIVER (test/conformance/README.md,
-- "The wire-fuzz driver"; docs/PORT-MOO.md, THE HARNESS; docs/SPEC-TABLES.md
-- §4.2, the read is the verifier).
--
-- The pinned moo CLI has no io library, so this file runs under the native
-- host tools/moofuzzhost (its __moofuzz.read and __moofuzz.write are stdin and
-- stdout), started by the executable driver test/conformance/moo/
-- wirefuzz-driver from the repository root. It IS the harness's one process
-- on a pipe: it reads the roster, answers one byte per entry, then reads
-- mutants until EOF and writes one little-endian reply per mutant, flushed
-- before the next is read. Each unit is required from its own directory under
-- the generated tree, so every unit keeps its own registry.
--
-- usage: moofuzzhost wirefuzz.moo <tables module> <generated tree>
--   <tables module>  the repo-relative module stage.pl writes (each unit's
--                    <Base>Table modules: moo cannot list a directory)
--   <generated tree> the repo-relative tree make/moo-conformance.mk writes
--
-- THE ROSTER. A FORM-1 entry is driven through the unit's table_load.moo
-- (loadTolerant) and table_save.moo; with RETAIN set, through table_retain.moo
-- (LoadRetain, SaveRetain) over one store at the harness's two capacities. A
-- FORM-2 entry is a MESSAGE batch, driven through table_message.moo
-- (LoadMessages, SaveMessages) against the vocabulary the unit's own
-- announcement (TableMessageAnnouncement) reads into, for a FIXED-CLASS root
-- only: the message form is the fixed table's (docs/SPEC-TABLES.md §3.3), so a
-- variable-class root's form-2 entry answers 0 by name. Every entry answered 0
-- is NOTED (__moofuzz.note, the file MOOFUZZ_NOTE names), and make/moo-fuzz.mk
-- holds the list to test/conformance/moo/wire-fuzz-absent.txt.
--
-- The reply is the harness's 47-byte header
--   u8 loaded; i32 unknown, kind_mismatch, widened, clamped, duplicate;
--   u8 malformed; u8 refused; i64 measure; i32 retained, retain_lost;
--   i64 saved, then that many bytes
-- with the runtime's counters and saved bytes.

local args = { ... }
local tablesModule: string = args[1] or ""
local generated: string = args[2] or "build/conformance-moo"

-- require resolves relative to this file, test/conformance/moo/
local ROOT = "../../../"
local load = require :: any

-- ---- LOADMEASURE (docs/SPEC-TABLES.md §6.5; internal/tablewire/extent.go) --
-- The moo leg's load has no caller region, so this file answers the
-- harness's measure itself, as internal/tablewire FileRegionMeasure states
-- it: a VARIABLE root's whole framing yields the canonical region bytes the
-- wire commands, and a framing that cannot be read at all answers -1. The
-- record, map-entry and list-element layouts are the cooked form's
-- (table_cookread.moo C.layout, emitted from ir.RecordLayout), so this is the
-- one C ABI model and not a second one.

local KIND_STRING, KIND_TABLE, KIND_ARRAY, KIND_UNION, KIND_KEYED, KIND_POINTER = 12, 13, 14, 15, 16, 17

type Ctx = { S: any, V: any, C: any, ids: { string }, at: number }

local function newReader(b: buffer, at: number, e: number): any
	return { b = b, at = at, e = e }
end

local function has(r: any, n: number): boolean
	return n <= r.e - r.at
end

-- a failed LEB leaves the cursor where it was and answers nil
local function leb(c: Ctx, r: any): number?
	local v, at = c.V.leb(r.b, r.at, r.e)
	if v == nil then
		return nil
	end
	r.at = at
	return v
end

local function u8(r: any): number
	local v = buffer.readu8(r.b, r.at)
	r.at += 1
	return v
end

local function idOf(c: Ctx, ref: number): string?
	if ref < 1 or ref > #c.ids then
		return nil
	end
	return c.ids[ref]
end

-- the reader's skip of a field it cannot name (internal/tablewire decode.go skip)
local function skip(c: Ctx, r: any, kind: number): boolean
	local w = c.V.width(kind)
	if w > 0 then
		if not has(r, w) then
			return false
		end
		r.at += w
		return true
	end
	if kind == KIND_POINTER or kind == 30 then
		return leb(c, r) ~= nil
	end
	if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
		local n = leb(c, r)
		if n == nil or not has(r, n) then
			return false
		end
		r.at += n
		return true
	end
	if kind == KIND_UNION then
		local arm = leb(c, r)
		if arm == nil then
			return false
		end
		if arm == 0 then
			return true
		end
		if not has(r, 1) then
			return false
		end
		r.at += 1
		local n = leb(c, r)
		if n == nil or not has(r, n) then
			return false
		end
		r.at += n
		return true
	end
	return false
end

local function extentBody(c: Ctx, r: any): any
	local n = leb(c, r)
	if n == nil or n > r.e - r.at then
		r.at = r.e
		return nil
	end
	local body = newReader(r.b, r.at, r.at + n)
	r.at += n
	return body
end

local function alignUp(n: number, a: number): number
	return (n + a - 1) // a * a
end

local extentRecord: (c: Ctx, d: any, r: any) -> boolean
local extentField: (c: Ctx, f: any, piece: any, r: any) -> boolean

local function extentUnion(c: Ctx, r: any, un: any): boolean
	local ref = leb(c, r)
	if ref == nil then
		r.at = r.e
		return true
	end
	if ref == 0 then
		return true
	end
	local id = idOf(c, ref)
	if id == nil or not has(r, 1) then
		r.at = r.e
		return true
	end
	u8(r)
	local body = extentBody(c, r)
	if body == nil then
		return true
	end
	local layout = c.C.unionLayout(un.name)
	for i, arm in un.arms do
		if arm.id ~= id or arm.s == "void" or arm.s == "ptr" then
			continue
		end
		if arm.s == "union" and arm.array == nil then
			return extentUnion(c, body, c.S.lookup(arm.t))
		end
		return extentField(c, arm, layout.arms[i], body)
	end
	return true
end

extentRecord = function(c: Ctx, d: any, r: any): boolean
	local layout = c.C.layout(d.name)
	while true do
		local ref = leb(c, r)
		if ref == nil or ref == 0 then
			return true
		end
		local id = idOf(c, ref)
		if id == nil or not has(r, 1) then
			return true
		end
		local kind = u8(r)
		local f, piece = nil, nil
		for i, candidate in d.fields do
			if candidate.id == id then
				f, piece = candidate, layout.fields[i]
				break
			end
		end
		if f == nil then
			if not skip(c, r, kind) then
				return true
			end
			continue
		end
		if kind == KIND_UNION and f.array == nil and f.s == "union" then
			if not extentUnion(c, r, c.S.lookup(f.t)) then
				return false
			end
			continue
		end
		if kind ~= KIND_TABLE and kind ~= KIND_ARRAY and kind ~= KIND_KEYED then
			if not skip(c, r, kind) then
				return true
			end
			continue
		end
		local body = extentBody(c, r)
		if body == nil then
			return true
		end
		if kind ~= f.k then
			continue
		end
		if not extentField(c, f, piece, body) then
			return false
		end
	end
end

extentField = function(c: Ctx, f: any, piece: any, r: any): boolean
	if f.s == "ptr" or f.s == "sblob" or f.s == "bblob" or f.s == "text" then
		return true
	end
	if f.s == "map" or f.s == "arr" then
		if not has(r, 2) then
			return true
		end
		local kind = u8(r)
		local expected = if f.s == "map" then KIND_TABLE else f.ek
		if kind ~= expected and not c.V.widens(kind, expected) then
			return true
		end
		local count = leb(c, r)
		if count == nil then
			return true
		end
		if f.s == "map" or f.array == "list" then
			if count > 0x7fffffff then
				return false
			end
			local floor = c.V.width(kind)
			if kind == KIND_POINTER then
				floor = 1
			elseif kind == KIND_TABLE then
				floor = 2
			end
			if floor < 1 then
				floor = 1
			end
			if count > (r.e - r.at) // floor then
				return false
			end
			c.at = alignUp(c.at, piece.ealign)
			c.at += count * piece.elem
		end
		if kind ~= KIND_TABLE and kind ~= KIND_UNION then
			return true
		end
		local i = 0
		while i < count and has(r, 1) do
			i += 1
			if f.keyvs ~= nil and leb(c, r) == nil then
				return true
			end
			if kind == KIND_UNION then
				local e = f.e
				if e ~= nil and e.s == "union" then
					if not extentUnion(c, r, c.S.lookup(e.t)) then
						return false
					end
				end
				continue
			end
			local body = extentBody(c, r)
			if body == nil then
				return true
			end
			local st: any = nil
			if f.s == "map" then
				st = c.S.lookup(f.t)
			elseif f.e ~= nil and f.e.s == "tab" then
				st = c.S.lookup(f.e.t)
			end
			if st ~= nil and not extentRecord(c, st, body) then
				return false
			end
		end
		return true
	end
	if f.s == "tab" then
		return extentRecord(c, c.S.lookup(f.t), r)
	end
	return true
end

-- the reserved node-table field's payload (internal/tablewire nodetable.go
-- nodeTableBytes): present, and framed or not
local function nodeTable(c: Ctx, body: any): (any?, boolean, boolean)
	local r = newReader(body.b, body.at, body.e)
	local payload: any? = nil
	local present = false
	while true do
		local ref = leb(c, r)
		if ref == nil or ref == 0 then
			return payload, present, true
		end
		local id = idOf(c, ref)
		if id == nil or not has(r, 1) then
			return payload, present, true
		end
		local kind = u8(r)
		if id == c.V.NodeTableId then
			present = true
			if kind ~= KIND_STRING then
				return nil, present, false
			end
			local n = leb(c, r)
			if n == nil or not has(r, n) then
				return nil, present, false
			end
			payload = newReader(r.b, r.at, r.at + n)
			r.at += n
		elseif not skip(c, r, kind) then
			return payload, present, true
		end
	end
end

-- the records of the numbering (nodetable.go scanNodeRecords), whole or nil
local function scanRecords(c: Ctx, payload: any): { any }?
	local declared = leb(c, payload)
	if declared == nil then
		return nil
	end
	local out = {}
	while payload.at < payload.e do
		local ref = leb(c, payload)
		if ref == nil or ref == 0 or ref > #c.ids then
			return nil
		end
		local n = leb(c, payload)
		if n == nil or n > payload.e - payload.at then
			return nil
		end
		out[#out + 1] = { typeId = c.ids[ref], at = payload.at, e = payload.at + n }
		payload.at += n
	end
	if declared ~= #out then
		return nil
	end
	return out
end

type Unit = { S: any, V: any, C: any, L: any, W: any, R: any, M: any, align: number, announcement: buffer? }

-- regionMeasure answers the canonical region bytes for a whole form-1 wire,
-- -1 for a framing that cannot be read (FileRegionMeasure's refusal and its
-- unread trailer alike).
local function regionMeasure(u: Unit, desc: any, data: buffer): number
	local len = buffer.len(data)
	if len < 9 or buffer.readu8(data, 0) ~= 1 then
		return -1
	end
	local lo, hi = buffer.readu32(data, len - 8), buffer.readu32(data, len - 4)
	if hi ~= 0 or lo > len // 8 then
		return -1
	end
	local span = lo * 8 + 8
	if span + 1 > len then
		return -1
	end
	local first = len - span
	local ids: { string } = table.create(lo) :: { string }
	local seen: { [string]: boolean } = {}
	for i = 0, lo - 1 do
		local id = buffer.readstring(data, first + i * 8, 8)
		if seen[id] then
			return -1
		end
		seen[id] = true
		ids[i + 1] = id
	end
	local c: Ctx = { S = u.S, V = u.V, C = u.C, ids = ids, at = 0 }
	local align = u.align
	local function size(d: any, at: number, e: number): number?
		c.at = 0
		if not extentRecord(c, d, newReader(data, at, e)) then
			return nil
		end
		return alignUp(alignUp(u.C.layout(d.name).size, align) + c.at, align)
	end
	local body = newReader(data, 1, first)
	local total = size(desc, body.at, body.e)
	if total == nil then
		return -1
	end
	local payload, present, framed = nodeTable(c, body)
	if present and not framed then
		return -1
	end
	local records: { any } = {}
	if present and payload ~= nil then
		local got = scanRecords(c, payload)
		if got == nil then
			return -1
		end
		records = got
	end
	local nodes: { [string]: any } = {}
	for _, name in desc.place.tables do
		local nd = u.S.lookup(name)
		nodes[nd.id] = nd
	end
	for _, rec in records do
		local nd = nodes[rec.typeId]
		if nd ~= nil then
			local n = size(nd, rec.at, rec.e)
			if n == nil then
				return -1
			end
			total += n
		elseif rec.typeId == u.V.BytesBlobId and desc.place.bytes then
			total += alignUp(8 + (rec.e - rec.at), align)
		elseif rec.typeId == u.V.StringBlobId and desc.place.str then
			total += alignUp(9 + (rec.e - rec.at), align)
		end
	end
	return total + (#records + 1) * 16
end

-- ---- THE ANSWER (test/conformance/README.md, "The wire-fuzz driver"; §4.2)

-- the retention arm's two stores, the harness's own
-- (test/conformance/harness/wirefuzz.go, retainFuzzCapacity, retainFuzzIdCapacity)
local RETAIN_CAPACITY = 1048576
local RETAIN_ID_CAPACITY = RETAIN_CAPACITY // 8

-- the most bodies a message batch carries (an 8-bit count, docs/SPEC-TABLES.md §3.3)
local BATCH = 256

type Entry = { u: Unit, root: string, retain: boolean, message: boolean }

local function count(x: any): number
	if type(x) == "number" then
		return x
	end
	return 0
end

local function answer(e: Entry, wire: buffer): buffer
	local u, root = e.u, e.root
	local desc = u.S.lookup(root)
	local report = u.L.newReport()
	local measure = -1
	local measureRefused = false
	local v: any = nil
	local saved: buffer? = nil
	if e.message then
		-- A FIXED-CLASS root's batch (§3.3): body one is the answer, and the
		-- value as far as it decoded stands when the batch is damaged inside it
		local vocabulary = u.M.NewVocabulary()
		if not u.M.AnnounceRead(vocabulary, u.announcement, {}) then
			error("wirefuzz: the unit's own announcement was refused", 0)
		end
		local n, values = u.M.LoadMessages(vocabulary, root, wire, BATCH, report)
		if n >= 1 then
			v = values[1]
		else
			v = values[1] or desc.new()
		end
		saved = u.M.SaveMessages(root, { v }, {})
	else
		-- LoadMeasure (§6.5) COMES FIRST for a VARIABLE root: a wire it cannot
		-- read is a LoadMeasure refusal, there is no region to load into, so
		-- the leg returns NO ROOT and never saves
		if desc.variable then
			measure = regionMeasure(u, desc, wire)
			measureRefused = measure < 0
		end
		local kept: any = nil
		if not measureRefused then
			if e.retain then
				kept = u.R.newStore(RETAIN_CAPACITY, RETAIN_ID_CAPACITY)
				v = u.R.LoadRetain(root, wire, report, kept)
			else
				v = u.L.loadTolerant(root, wire, report)
			end
		end
		-- A FIXED root always loads: a refused or damaged wire answers the
		-- root at its defaults
		if v == nil and not desc.variable then
			v = desc.new()
		end
		if v ~= nil then
			if e.retain then
				saved = u.R.SaveRetain(root, v, kept, report)
			else
				saved = u.W.save(desc, v)
			end
		end
	end
	local refused = report.refused == true or count(report.refused) > 0 or count(report.nonFinite) > 0
	local n = 47 + (if saved ~= nil then buffer.len(saved) else 0)
	local b = buffer.create(n)
	buffer.writeu8(b, 0, if v ~= nil then 1 else 0)
	buffer.writei32(b, 1, count(report.unknown))
	buffer.writei32(b, 5, count(report.kindMismatch))
	buffer.writei32(b, 9, count(report.widened))
	buffer.writei32(b, 13, count(report.clamped))
	buffer.writei32(b, 17, count(report.duplicate))
	buffer.writeu8(b, 21, if report.malformed then 1 else 0)
	buffer.writeu8(b, 22, if refused then 1 else 0)
	-- an i64 as two little-endian halves (a moo number is Q48.16)
	if measure < 0 then
		buffer.writeu32(b, 23, 0xffffffff)
		buffer.writeu32(b, 27, 0xffffffff)
	else
		buffer.writeu32(b, 23, measure % 0x100000000)
		buffer.writeu32(b, 27, measure // 0x100000000)
	end
	buffer.writei32(b, 31, if e.retain then count(report.retained) else 0)
	buffer.writei32(b, 35, if e.retain then count(report.retainLost) else 0)
	if saved == nil then
		buffer.writeu32(b, 39, 0xffffffff)
		buffer.writeu32(b, 43, 0xffffffff)
	else
		buffer.writeu32(b, 39, buffer.len(saved))
		buffer.writeu32(b, 43, 0)
		buffer.copy(b, 47, saved, 0, buffer.len(saved))
	end
	return b
end

-- ---- THE UNITS: one each, required from its own directory, loaded once

local tableModules: { [string]: { string } } = load(ROOT .. tablesModule)
local loaded: { [string]: Unit | false } = {}

local function unit(key: string): Unit?
	local have = loaded[key]
	if have == nil then
		local base = ROOT .. generated .. "/" .. key .. "/"
		local ok, got = pcall(function(): Unit
			local S = load(base .. "schema_tables")
			local C = load(base .. "table_cookread")
			local align = 8
			local announcement: buffer? = nil
			for _, name in tableModules[key] or {} do
				local mod = load(base .. name)
				for k, d in mod do
					if type(k) == "string" and string.sub(k, -10) == "Descriptor" and type(d) == "table" and d.fields ~= nil then
						local l = C.layout(d.name)
						if l ~= nil and l.align > align then
							align = l.align
						end
					end
				end
				if announcement == nil and mod.TableMessageAnnouncement ~= nil then
					announcement = mod.TableMessageAnnouncement
				end
			end
			local M: any = nil
			if announcement ~= nil then
				M = load(base .. "table_message")
			end
			return {
				S = S,
				V = load(base .. "table_values"),
				C = C,
				L = load(base .. "table_load"),
				W = load(base .. "table_save"),
				R = load(base .. "table_retain"),
				M = M,
				align = align,
				announcement = announcement,
			}
		end)
		local next: Unit | false = false
		if ok then
			next = got :: Unit
		end
		loaded[key] = next
		have = next
	end
	if have == false or have == nil then
		return nil
	end
	return have
end

-- ---- THE PIPE (little-endian throughout: the roster once, then one reply a mutant)

local host = (_G :: any).__moofuzz
if host == nil or host.read == nil then
	error("wirefuzz: run under tools/moofuzzhost (test/conformance/moo/wirefuzz-driver)", 0)
end

local function readExact(n: number): buffer?
	local b: buffer = host.read(n)
	if buffer.len(b) < n then
		return nil
	end
	return b
end

local function u32(): number?
	local b = readExact(4)
	return if b ~= nil then buffer.readu32(b, 0) else nil
end

local function str(): string
	local nb = readExact(2)
	assert(nb ~= nil, "wirefuzz: the roster ended inside a name")
	local b = readExact(buffer.readu16(nb, 0))
	assert(b ~= nil, "wirefuzz: the roster ended inside a name")
	return buffer.tostring(b)
end

local total = u32()
assert(total ~= nil, "wirefuzz: no roster")
local roster: { Entry | false } = {}
local reply = buffer.create(total)
for i = 1, total do
	local key = str()
	local root = str()
	local fb = readExact(2)
	assert(fb ~= nil, "wirefuzz: the roster ended inside an entry")
	local form, retain = buffer.readu8(fb, 0), buffer.readu8(fb, 1)
	local entry: Entry | false = false
	local why = ""
	local u = unit(key)
	if u == nil then
		why = "refused"
	else
		local ok, d = pcall(u.S.lookup, root)
		if not ok or d == nil then
			why = "no-codec"
		elseif form == 2 and d.variable then
			-- the message form is the FIXED table's (docs/SPEC-TABLES.md §3.3)
			why = "variable-message"
		elseif form == 2 and (u.M == nil or u.announcement == nil) then
			why = "no-message-codec"
		elseif form ~= 1 and form ~= 2 then
			why = "form"
		else
			entry = { u = u, root = root, retain = retain ~= 0, message = form == 2 }
		end
	end
	if not entry then
		host.note(key .. "." .. root .. " form=" .. tostring(form) .. " " .. why)
	end
	roster[i] = entry
	buffer.writeu8(reply, i - 1, if entry then 1 else 0)
end
host.write(reply)

while true do
	local index = u32()
	if index == nil then
		break -- the harness closed the stream
	end
	local n = u32()
	assert(n ~= nil, "wirefuzz: the stream ended inside a mutant header")
	local wire = readExact(n)
	assert(wire ~= nil, "wirefuzz: the stream ended inside a mutant")
	local entry = roster[index + 1]
	assert(entry, "wirefuzz: a mutant for a root this leg answered 0")
	host.write(answer(entry, wire))
end
