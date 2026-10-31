--!strict
-- wirefuzz.luau — THE LUAU LEG's WIRE-FUZZ DRIVER (test/conformance/README.md,
-- "The wire-fuzz driver"; docs/PORT-LUAU.md, THE HARNESS; docs/SPEC-TABLES.md
-- §4.2, the read is the verifier).
--
-- The pinned Luau has no io library (docs/PORT-LUAU.md, THE HARNESS), so this
-- file runs in one of two shapes, chosen by the host it finds:
--
-- THE PIPE. Under the native host tools/luaufuzzhost (its __luaufuzz.read and
-- __luaufuzz.write are stdin and stdout), started by the executable driver
-- test/conformance/luau/wirefuzz-driver from the repository root, this file
-- IS the harness's one process on a pipe: it reads the roster, answers one
-- byte per entry (1 for a FORM-1, non-retaining root a unit under
-- build/conformance-luau registers, 0 for the rest), then reads mutants until
-- EOF and writes one little-endian reply per mutant, flushed before the next
-- is read. Each unit is required from its own directory, so every unit keeps
-- its own registry.
--
-- THE BATCH. Under the plain interpreter (the in-package differential,
-- internal/codegen/luautable/fuzz_test.go) the working directory is one unit
-- directory, every module is required relative to it, and inputs.luau is:
--
--   return table.freeze({
--     root = "Root",
--     modules = { "ProbeTable" },          -- every <Base>Table module
--     wire = buffer.fromstring("\x.."),   -- one mutant, or
--     wires = { buffer.fromstring(".."), ... }, -- a batch (the unit tests)
--   })
--
-- and each reply is printed as one hex line (an ERR line for a raise).
--
-- The reply is the harness's 47-byte header
--   u8 loaded; i32 unknown, kind_mismatch, widened, clamped, duplicate;
--   u8 malformed; u8 refused; i64 measure; i32 retained, retain_lost;
--   i64 saved, then that many bytes
-- with the runtime's counters and saved bytes. measure is the canonical region
-- a VARIABLE root's whole framing commands (the descriptor record walk below)
-- and -1 otherwise: a FIXED root has no region, and a wire the framing cannot
-- read whole is the LoadMeasure refusal the harness accepts on the same terms
-- (test/conformance/README.md, "The wire-fuzz driver"; §6.5).

type Unit = { S: any, load: any, save: any, retain: any?, align: number, measure: boolean? }
type Entry = { u: Unit, root: string, retain: boolean }


-- ---- LOADMEASURE (docs/SPEC-TABLES.md §6.5; internal/tablewire/extent.go) --
-- The Luau leg's load has no caller region, so this file answers the harness's
-- measure itself: a VARIABLE root's whole framing yields the canonical region
-- bytes the wire commands (the root record's layout plus every node it places,
-- the node-table directory and the blob bodies), and anything the framing
-- cannot read whole answers -1, the LoadMeasure refusal (the harness accepts
-- that on a wire whose oracle did not read whole either). A FIXED root has no
-- region and answers -1. The record layouts are the descriptor walk schema_cook.luau
-- uses (§8.1, §20.3), so this is the same C ABI model and not a second one.

local KIND_BOOL, KIND_STRING, KIND_TABLE, KIND_ARRAY = 1, 12, 13, 14
local KIND_UNION, KIND_KEYED, KIND_POINTER = 15, 16, 17
local KIND_ENUM, KIND_ESCAPE, KIND_NOPAYLOAD, KIND_WSTRING = 30, 31, 32, 33
local KIND_RESERVED_FLOAT16, KIND_TIMESTAMP = 34, 36
local NODE_FIELD_ID = 0xFFFFFFFFFFFFFFFFi
local BYTES_TYPE_ID = 0x2F2EC0474F1C4FE4i
local STRING_TYPE_ID = 0x704BE0D8FAAFFC58i

local function align_up(n: number, a: number): number
	if a <= 1 then return n end
	return math.ceil(n / a) * a
end

local function kind_width(kind: number): number
	if kind == KIND_BOOL or kind == 2 or kind == 6 or kind == 20 or kind == 25 then return 1 end
	if kind == 3 or kind == 7 or kind == 21 or kind == 26 then return 2 end
	if kind == 4 or kind == 8 or kind == 10 or kind == KIND_POINTER or kind == 22 or kind == 27 then return 4 end
	if kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == KIND_TIMESTAMP then return 8 end
	if kind == 18 or kind == 19 or kind == 24 or kind == 29 then return 16 end
	return 0
end

local function kind_widens(kind: number, declared: number): boolean
	if declared == 3 or declared == 4 or declared == 5 then return kind >= 2 and kind < declared end
	if declared == 7 or declared == 8 or declared == 9 then return kind >= 6 and kind < declared end
	if declared == 18 then return kind >= 2 and kind <= 5 end
	if declared == 19 then return kind >= 6 and kind <= 9 end
	if declared == 11 then return kind == 10 end
	return false
end

local lookup_S: any = nil
local record_layouts: { [any]: any } = {}
local union_layouts: { [any]: any } = {}
-- the five layout walks call each other, so they live on one table and each
-- is defined below as a field of it
local layout = {}

local function storage_width(n: number): number
	if n <= 0xff then return 1 end
	if n <= 0xffff then return 2 end
	if n <= 0xffffffff then return 4 end
	return 8
end

function layout.value_piece(e: any): any
	local s = e.s
	if s == "ptr" or s == "sblob" or s == "bblob" then return { size = 8, align = 8 } end
	if s == "bool" then return { size = 1, align = 1 } end
	if s == "float" then return e.f32 and { size = 4, align = 4 } or { size = 8, align = 8 } end
	if s == "int" then local w = e.kw or 4; return { size = w, align = w } end
	if s == "enum" then local w = storage_width(#(e.vs or {})); return { size = w, align = w } end
	if s == "tab" then
		local l = layout.record_layout(lookup_S.lookup(e.t))
		return { size = l.size, align = l.align }
	end
	if s == "union" then
		local u = layout.union_layout(lookup_S.lookup(e.t))
		return { size = u.size, align = u.align }
	end
	if s == "str" or s == "bytes" or s == "text" or s == "arr" or s == "map" then
		local pieces = layout.field_pieces(e)
		local at, a = 0, 1
		for _, p in pieces do
			if p.align > a then a = p.align end
			at = align_up(at, p.align) + p.size
		end
		return { size = at, align = a }
	end
	return { size = 1, align = 1 }
end

function layout.field_pieces(fd: any): { any }
	local out: { any } = {}
	local s = fd.s
	if s == "map" then
		out[1] = { size = 16, align = 8 }
	elseif s == "ptr" or s == "sblob" or s == "bblob" or s == "text" then
		out[1] = { size = 8, align = 8 }
	elseif s == "str" then
		if fd.wide then
			out[1] = { size = ((fd.bound or 0) + 1) * 2, align = 2 }
		else
			out[1] = { size = (fd.bound or 0) + 1, align = 1 }
		end
		out[2] = { size = 4, align = 4 }
	elseif s == "bytes" then
		out[1] = { size = fd.bound or 0, align = 1 }
		out[2] = { size = 4, align = 4 }
	elseif fd.array == "keyed" then
		local e = layout.value_piece(fd.e)
		out[1] = { size = e.size * (fd.bound or #(fd.keyvs or {})), align = e.align }
	elseif fd.array == "fixed" or fd.array == "counted" then
		local e = layout.value_piece(fd.e)
		out[1] = { size = e.size * (fd.bound or 0), align = e.align }
		if fd.array == "counted" then out[2] = { size = 4, align = 4 } end
	elseif fd.array == "list" then
		out[1] = { size = 16, align = 8 }
	else
		out[1] = layout.value_piece(fd)
	end
	if fd.present ~= nil then out[#out + 1] = { size = 1, align = 1 } end
	return out
end

function layout.record_layout(d: any): any
	local got = record_layouts[d]
	if got ~= nil then return got end
	local offset, maxAlign = 0, 1
	local fields: { any } = {}
	for _, fd in d.fields do
		local pieces = layout.field_pieces(fd)
		local start, falign = -1, 1
		for _, p in pieces do
			if p.align > maxAlign then maxAlign = p.align end
			if p.align > falign then falign = p.align end
			offset = align_up(offset, p.align)
			if start < 0 then start = offset end
			offset = offset + p.size
		end
		fields[#fields + 1] = { fd = fd, at = start, size = offset - start, align = falign }
	end
	local size = align_up(offset, maxAlign)
	if size == 0 then size = 1 end
	local l = { size = size, align = maxAlign, fields = fields }
	record_layouts[d] = l
	return l
end

function layout.arm_layout(arm: any): any
	if arm.s == "void" then return { size = 0, align = 1 } end
	local offset, maxAlign = 0, 1
	for _, p in layout.field_pieces(arm) do
		if p.align > maxAlign then maxAlign = p.align end
		offset = align_up(offset, p.align) + p.size
	end
	return { size = align_up(offset, maxAlign), align = maxAlign }
end

function layout.union_layout(u: any): any
	local got = union_layouts[u]
	if got ~= nil then return got end
	local tag = storage_width(#u.arms)
	local align = tag
	local armAlign, armSize = 1, 0
	for _, arm in u.arms do
		local a = layout.arm_layout(arm)
		if a.align > armAlign then armAlign = a.align end
		if a.size > armSize then armSize = a.size end
	end
	if armAlign > align then align = armAlign end
	local armOffset = align_up(tag, armAlign)
	local size = align_up(armOffset + armSize, align)
	local z = { size = size, align = align, tag = tag, armOffset = armOffset }
	union_layouts[u] = z
	return z
end

local function new_reader(b: buffer, at: number, e: number): any
	return { b = b, at = at, e = e }
end

local function r_has(r: any, n: number): boolean
	return r.at + n <= r.e
end

local function r_u8(r: any): number?
	if r.at >= r.e then return nil end
	local v = buffer.readu8(r.b, r.at)
	r.at = r.at + 1
	return v
end

-- canonical LEB128, the load's rule (§3): ten bytes at most, the tenth only 0
-- or 1, no needless terminal zero
local function r_leb(r: any): number?
	local start = r.at
	local v, s = 0, 0
	for i = 0, 9 do
		if r.at >= r.e then r.at = start; return nil end
		local x = buffer.readu8(r.b, r.at)
		r.at = r.at + 1
		if i == 9 and x > 1 then r.at = start; return nil end
		v = v + (x % 0x80) * (2 ^ s)
		if x < 0x80 then
			if i > 0 and x == 0 then r.at = start; return nil end
			return v
		end
		s = s + 7
	end
	r.at = start
	return nil
end

local function r_skip(r: any, kind: number): boolean
	if kind == KIND_POINTER or kind == KIND_ENUM then
		return r_leb(r) ~= nil
	end
	local w = kind_width(kind)
	if w > 0 then
		if not r_has(r, w) then return false end
		r.at = r.at + w
		return true
	end
	if kind == KIND_STRING or kind == KIND_TABLE or kind == KIND_ARRAY or kind == KIND_KEYED
		or kind == KIND_ESCAPE or kind == KIND_NOPAYLOAD or kind == KIND_WSTRING then
		local n = r_leb(r)
		if n == nil or not r_has(r, n) then return false end
		r.at = r.at + n
		return true
	end
	if kind == KIND_RESERVED_FLOAT16 then return false end
	if kind == KIND_UNION then
		local arm = r_leb(r)
		if arm == nil then return false end
		if arm == 0 then return true end
		if not r_has(r, 1) then return false end
		r.at = r.at + 1
		local n = r_leb(r)
		if n == nil or not r_has(r, n) then return false end
		r.at = r.at + n
		return true
	end
	return false
end

local function extent_body(r: any): any
	local n = r_leb(r)
	if n == nil then r.at = r.e; return nil end
	if n > r.e - r.at then r.at = r.e; return nil end
	local sub = new_reader(r.b, r.at, r.at + n)
	r.at = r.at + n
	return sub
end

local function id_at(ids: { integer }, ref: number): integer?
	if ref < 1 or ref > #ids then return nil end
	return ids[ref]
end

local extent_record: (any, any, { integer }, any) -> boolean = function(_S: any, _st: any, _ids: { integer }, _r: any): boolean return true end
local extent_field: (any, any, any, any) -> boolean = function(_S: any, _ids: any, _f: any, _r: any): boolean return true end

local current_scan: any = { at = 0 }

local function extent_union(S: any, ids: { integer }, r: any, un: any): boolean
	local ref = r_leb(r)
	if ref == nil or ref == 0 then return true end
	local id = id_at(ids, ref)
	if id == nil or not r_has(r, 1) then return true end
	r_u8(r)
	local body = extent_body(r)
	if body == nil then return true end
	for _, arm in un.arms do
		if arm.id == id and arm.s ~= "ptr" then
			if arm.s == "union" and arm.array == nil then
				local a = new_reader(body.b, body.at, body.e)
				return extent_union(S, ids, a, S.lookup(arm.t))
			end
			return extent_field(S, ids, arm, body)
		end
	end
	return true
end

local function field_with_id(st: any, id: integer): any
	for _, fd in st.fields do
		if fd.id == id then return fd end
	end
	return nil
end

extent_record = function(S: any, st: any, ids: { integer }, r: any): boolean
	while true do
		local ref = r_leb(r)
		if ref == nil or ref == 0 then return true end
		local id = id_at(ids, ref)
		if id == nil or not r_has(r, 1) then return true end
		local kind = r_u8(r) :: number
		local f = field_with_id(st, id)
		if f == nil then
			if not r_skip(r, kind) then return true end
		elseif kind == KIND_UNION and f.array == nil and f.s == "union" then
			if not extent_union(S, ids, r, S.lookup(f.t)) then return false end
		elseif kind ~= KIND_TABLE and kind ~= KIND_ARRAY and kind ~= KIND_KEYED then
			if not r_skip(r, kind) then return true end
		else
			local body = extent_body(r)
			if body == nil then return true end
			if kind == f.k then
				if not extent_field(S, ids, f, body) then return false end
			end
		end
	end
end

extent_field = function(S: any, ids: { integer }, f: any, r: any): boolean
	if f.s == "ptr" or f.s == "sblob" or f.s == "bblob" then return true end
	local scan = current_scan
	if f.s == "map" or f.s == "arr" then
		if not r_has(r, 2) then return true end
		local kind = r_u8(r) :: number
		local expected = f.ek
		if f.s == "map" then expected = KIND_TABLE end
		if kind ~= expected and not kind_widens(kind, expected) then return true end
		local count = r_leb(r)
		if count == nil then return true end
		if f.s == "map" or f.array == "list" then
			local size, align
			if f.s == "map" then
				local l = layout.record_layout(S.lookup(f.t))
				size, align = l.size, l.align
			else
				local p = layout.value_piece(f.e)
				size, align = p.size, p.align
			end
			if count > 0x7fffffff then return false end
			local floor = kind_width(kind)
			if kind == KIND_POINTER then floor = 1 elseif kind == KIND_TABLE then floor = 2 end
			if floor < 1 then floor = 1 end
			if count > math.floor((r.e - r.at) / floor) then return false end
			scan.at = align_up(scan.at, align)
			scan.at = scan.at + count * size
		end
		if kind ~= KIND_TABLE and kind ~= KIND_UNION then return true end
		local keyed = f.keyvs ~= nil
		for _ = 1, count do
			if not r_has(r, 1) then break end
			if keyed and r_leb(r) == nil then return true end
			if kind == KIND_UNION then
				-- a POSITIONAL array's element union is f.e's descriptor; a
				-- map's entry is f.t (and its element kind is kind 13, so this
				-- arm is the array's)
				local uname = if f.s == "map" then f.t elseif f.e ~= nil then f.e.t else nil
				if uname == nil then return true end
				if not extent_union(S, ids, r, S.lookup(uname)) then return false end
			else
				local body = extent_body(r)
				if body == nil then return true end
				local st = nil
				if f.s == "map" then
					st = S.lookup(f.t)
				elseif f.e ~= nil and f.e.s == "tab" then
					st = S.lookup(f.e.t)
				end
				if st ~= nil and not extent_record(S, st, ids, body) then return false end
			end
		end
		return true
	end
	if f.s == "tab" then return extent_record(S, S.lookup(f.t), ids, r) end
	return true
end

-- node_table is internal/tablewire/nodetable.go's nodeTableBytes: the root
-- body's fields are walked to the end and the LAST node-table field is the
-- one that frames, exactly as there; a node field
-- of another kind, or one whose length runs past the body, is unframed.
local function node_table(body: any, ids: { integer }): (any?, boolean)
	local r = new_reader(body.b, body.at, body.e)
	local payload: any? = nil
	while true do
		local ref = r_leb(r)
		if ref == nil or ref == 0 then return payload, true end
		local id = id_at(ids, ref)
		if id == nil or not r_has(r, 1) then return payload, true end
		local kind = r_u8(r) :: number
		if id == NODE_FIELD_ID then
			if kind ~= KIND_STRING then return nil, false end
			local n = r_leb(r)
			if n == nil or not r_has(r, n) then return nil, false end
			payload = new_reader(r.b, r.at, r.at + n)
			r.at = r.at + n
		elseif not r_skip(r, kind) then
			return payload, true
		end
	end
end

local function scan_records(payload: any, ids: { integer }): (any?, boolean)
	local declared = r_leb(payload)
	if declared == nil then return nil, false end
	local out = {}
	while payload.at < payload.e do
		local ref = r_leb(payload)
		if ref == nil or ref == 0 then return nil, false end
		local id = id_at(ids, ref)
		if id == nil then return nil, false end
		local n = r_leb(payload)
		if n == nil or n > payload.e - payload.at then return nil, false end
		out[#out + 1] = { typeId = id, at = payload.at, e = payload.at + n }
		payload.at = payload.at + n
	end
	if declared ~= #out then return nil, false end
	return out, true
end

-- region_measure answers the canonical region bytes for a whole form-1 wire,
-- or nil when the framing cannot be read whole (a LoadMeasure refusal).
local function region_measure(S: any, desc: any, data: buffer, align: number): number?
	lookup_S = S
	if desc.variable ~= true then return -1 end
	local len = buffer.len(data)
	if len < 9 or buffer.readu8(data, 0) ~= 1 then return nil end
	local n = integer.tonumber(buffer.readinteger(data, len - 8))
	if n < 0 or n > math.floor(len / 8) then return nil end
	local span = n * 8 + 8
	if span + 1 > len then return nil end
	local first = len - span
	local ids: { integer } = {}
	local seen: { [integer]: boolean } = {}
	for i = 0, n - 1 do
		local id = buffer.readinteger(data, first + i * 8)
		if seen[id] then return nil end
		seen[id] = true
		ids[i + 1] = id
	end
	local body = new_reader(data, 1, first)
	local round = function(x: number): number return math.ceil(x / align) * align end
	local function size_of(st: any, at: number, e: number): (number?, boolean)
		current_scan = { at = 0 }
		if not extent_record(S, st, ids, new_reader(data, at, e)) then return nil, false end
		return round(round(layout.record_layout(st).size) + current_scan.at), true
	end
	local total, ok = size_of(desc, body.at, body.e)
	if not ok or total == nil then return nil end
	local payload, framed = node_table(body, ids)
	if payload == nil then
		if not framed then return nil end
		return total + 16
	end
	local records, scanned = scan_records(payload, ids)
	if records == nil or not scanned then return nil end
	local nodes: { [integer]: any } = {}
	for _, name in desc.place.tables do
		nodes[S.lookup(name).id] = S.lookup(name)
	end
	for _, rec in records do
		local st = nodes[rec.typeId]
		if st ~= nil then
			local got, good = size_of(st, rec.at, rec.e)
			if not good or got == nil then return nil end
			total = total + got
		elseif rec.typeId == BYTES_TYPE_ID and desc.place.bytes then
			total = total + round(8 + (rec.e - rec.at))
		elseif rec.typeId == STRING_TYPE_ID and desc.place.str then
			total = total + round(8 + (rec.e - rec.at) + 1)
		end
	end
	return total + (#records + 1) * 16
end

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

-- answer reads one mutant through the unit's tolerant load and writes back what
-- it decoded (test/conformance/README.md, "The wire-fuzz driver"; §4.2).
-- the retention arm's two stores, the harness's own
local RETAIN_CAPACITY = 1048576
local RETAIN_ID_CAPACITY = RETAIN_CAPACITY // 8

local function answer(u: Unit, root: string, wire: buffer, retain: boolean?): buffer
	local S, load, save = u.S, u.load, u.save
	-- docs/SPEC-TABLES.md §4: the six counters and the verdict
	local report = {
		malformed = false,
		refused = 0,
		unknown = 0,
		kindMismatch = 0,
		clamped = 0,
		widened = 0,
		duplicate = 0,
	}
	local desc = S.lookup(root)
	-- LoadMeasure (§6.5) COMES FIRST for a VARIABLE root: the framing states
	-- the region, and a wire it cannot read whole is a LoadMeasure refusal —
	-- there is no region to load into, so the leg returns NO ROOT and never
	-- saves (test/conformance/README.md, "The wire-fuzz driver"). A FIXED
	-- root has no region and always answers -1 there.
	local measure = -1
	local measureRefused = false
	if u.measure and desc ~= nil and desc.variable == true then
		measure = region_measure(S, desc, wire, u.align) or -1
		measureRefused = measure < 0
	end
	-- THE LEG'S LOAD IS THE TOLERANT READ (test/conformance/README.md, "The
	-- wire-fuzz driver"): it recovers the value and counts the report, and the
	-- plain name (the STRICT read, §6.5) is the caller's and not this pipe's.
	local v = nil
	-- THE RETENTION ARM (--retain; docs/SPEC-TABLES.md §6.6) reads and writes
	-- the same mutant through the retaining pair, LoadRetainTolerant then
-- SaveRetain (the tolerant read, as the plain arm's loadTolerant),
	-- with the harness's two stores (test/conformance/harness/wirefuzz.go,
	-- retainFuzzCapacity and retainFuzzIdCapacity), and one report across the
	-- pair: retain_lost at the end is what the load could not keep plus what
	-- the save could not place.
	local kept: any = nil
	if retain then
		(report :: any).retained = 0;
		(report :: any).retainLost = 0
	end
	if not measureRefused then
		if retain then
			v, kept = (u.retain :: any).LoadRetainTolerant(root, wire, report, RETAIN_CAPACITY, RETAIN_ID_CAPACITY)
		else
			v = load.loadTolerant(root, wire, report)
		end
	end
	-- A FIXED root always loads (test/conformance/README.md, "The wire-fuzz
	-- driver"): its Load fills a value and reports, so a refused or damaged
	-- wire answers the root at its defaults.
	if v == nil and desc ~= nil and not desc.variable then
		v = desc.new()
	end
	local saved = nil
	if v ~= nil then
		if retain then
			saved = (u.retain :: any).SaveRetain(root, v, kept, report)
		else
			saved = save.save(desc, v)
		end
	end
	local n = 47 + (if saved ~= nil then buffer.len(saved) else 0)
	local b = buffer.create(n)
	buffer.writeu8(b, 0, if v ~= nil then 1 else 0)
	buffer.writei32(b, 1, report.unknown or 0)
	buffer.writei32(b, 5, report.kindMismatch or 0)
	buffer.writei32(b, 9, report.widened or 0)
	buffer.writei32(b, 13, report.clamped or 0)
	buffer.writei32(b, 17, report.duplicate or 0)
	buffer.writeu8(b, 21, if report.malformed then 1 else 0)
	buffer.writeu8(b, 22, if (report.refused or 0) > 0 then 1 else 0)
	buffer.writeinteger(b, 23, integer.create(measure))
	buffer.writei32(b, 31, if retain then (report :: any).retained or 0 else 0)
	buffer.writei32(b, 35, if retain then (report :: any).retainLost or 0 else 0)
	if saved == nil then
		buffer.writeinteger(b, 39, -1i)
	else
		buffer.writeinteger(b, 39, integer.create(buffer.len(saved)))
		buffer.copy(b, 47, saved, 0, buffer.len(saved))
	end
	return b
end


-- THE PIPE (test/conformance/README.md, "The wire-fuzz driver": the stream is
-- little-endian throughout, the roster once, then one reply per mutant).
local function pipe(host: any)
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
	local units: { [string]: { string } } = require("../../../build/luau-driver/units") :: any
	local loaded: { [string]: Unit | false } = {}
	-- one unit, required from build/conformance-luau/<key>/; a unit the
	-- target refused has no directory, and every root of it answers 0. Not
	-- every schema file of a unit has a table module (examples-wide's
	-- WideText.schema declares none), so a module the unit did not emit is
	-- skipped, as the conformance driver skips it; a module that is there and
	-- raises still refuses the unit.
	local function unit(key: string): Unit?
		local have = loaded[key]
		if have == nil then
			local base = "../../../build/conformance-luau/" .. key .. "/"
			local ok, got = pcall(function(): Unit
				local S = require(base .. "schema_tables")
				lookup_S = S
				local align = 8
				for _, name in units[key] or {} do
					-- a closure, not pcall(require, ...): require resolves
					-- ./ and ../ against the chunk that calls it
					local okm, mod = pcall(function(): any return require(base .. name) end)
					if not okm then
						if string.find(tostring(mod), "no module at", 1, true) then
							continue
						end
						error(mod, 0)
					end
					for k, v in mod do
						if type(k) == "string" and string.sub(k, -10) == "Descriptor" and type(v) == "table" and v.fields ~= nil and v.id ~= nil then
							local a = layout.record_layout(v).align
							if a > align then align = a end
						end
					end
				end
				return { S = S, load = require(base .. "schema_load"), save = require(base .. "schema_save"), retain = require(base .. "schema_retain"), align = align, measure = true }
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
	local count = u32()
	assert(count ~= nil, "wirefuzz: no roster")
	local roster: { Entry | false } = {}
	local reply = buffer.create(count)
	for i = 1, count do
		local key = str()
		local root = str()
		local fb = readExact(2)
		assert(fb ~= nil, "wirefuzz: the roster ended inside an entry")
		local form, retain = buffer.readu8(fb, 0), buffer.readu8(fb, 1)
		local entry: Entry | false = false
		-- A FORM-2 entry is the message batch reader, which this leg does not
		-- drive, so it answers 0 (test/conformance/README.md, "The wire-fuzz
		-- driver"). A RETAIN entry is the retention arm, driven through
		-- schema_retain.luau's LoadRetain and SaveRetain.
		-- Every entry answered 0 is NOTED by name (__luaufuzz.note, the file
		-- LUAUFUZZ_NOTE names): make/luau-fuzz.mk holds the list to
		-- test/conformance/luau/wire-fuzz-absent.txt, so a root that drops out
		-- is a reviewed change and never a quieter run.
		local why = ""
		if form ~= 1 then
			why = "message"
		else
			local u = unit(key)
			if u == nil then
				why = "refused"
			elseif not pcall(u.S.lookup, root) then
				why = "no-codec"
			else
				entry = { u = u, root = root, retain = retain ~= 0 }
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
		host.write(answer(entry.u, entry.root, wire, entry.retain))
	end
end

local host = (_G :: any).__luaufuzz
if host ~= nil and host.read ~= nil then
	pipe(host)
else
	local inputs = require("./inputs")
	for _, name in inputs.modules or {} do
		require("./" .. name)
	end
	local u: Unit = { S = require("./schema_tables"), load = require("./schema_load"), save = require("./schema_save"), align = 8 }
	local root: string = inputs.root
	if inputs.wires ~= nil then
		-- the unit test's batch: one line per mutant, an ERR line for a raise
		for _, wire in inputs.wires do
			local ok, res = pcall(answer, u, root, wire)
			if ok then
				print(hex(res))
			else
				print("ERR")
			end
		end
	else
		print(hex(answer(u, root, inputs.wire)))
	end
end
