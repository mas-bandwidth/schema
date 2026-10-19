--!strict
-- refusal.luau — THE STRICT READ AND ITS NAMED REFUSALS (docs/SPEC-TABLES.md
-- §6.5, §11, §2.14; docs/PORT-LUAU.md, THE CARD MAP). Emitted beside the
-- unit's <Base>Table.luau modules by internal/codegen/luautable, which embeds
-- runtime/*.luau by glob, and required as require("./refusal").
--
-- ReadStrict answers the root or refuses with ONE name, the set §6.5 states
-- and Go's TableReport.Err() answers: wire_damaged (the file's own framing),
-- malformed, clamped and kind_mismatch, each with the first field the walk saw
-- move it; a form byte the root's own form does not read (message_form_as_file,
-- newer_form, previous_form, unknown_form); integrity_mismatch; and the fixed reader's own
-- names for a form-3 file. It does NOT refuse unknown, widened or duplicate
-- alone (§6.5: unknown is evolution, widened decoded exactly; a repeated map
-- key is last-wins, §2.8, §4): those stay counted on the report, with the
-- field each first moved on.
--
-- It is a WRAPPER over load.luau's tolerant walk and edits nothing in it: the
-- counters are the load's own report (§4).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local L = require("./load")
local F = require("./fixed")

local R = {}

local FORM_VARIABLE = 1
local FORM_MESSAGE = 2
local FORM_FIXED = 3

-- the reserved ids the framing walk below needs (docs/SPEC-TABLES.md §3.1,
-- §5), in load.luau's spelling
local NodeTableId: integer = -1i
local StringBlobId: integer = 0x704be0d8faaffc58i -- FNV-1a 64 of "string"
local BytesBlobId: integer = 0x2f2ec0474f1c4fe4i  -- FNV-1a 64 of "bytes"

-- the counters that refuse, in TableReport.Err()'s order (docs/SPEC-TABLES.md
-- §6.5), each with its §11 name; load.luau spells kind_mismatch kindMismatch
local REFUSING = { "missingRequired", "malformed", "clamped", "kindMismatch", "nonFinite" }
local NAMES: { [string]: string } = {
	missingRequired = "missing_required",
	malformed = "malformed",
	clamped = "clamped",
	kindMismatch = "kind_mismatch",
	nonFinite = "non_finite",
}

-- a report at rest, in load.luau's shape (docs/SPEC-TABLES.md §4)
local function fresh(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, missingRequired = 0, nonFinite = 0 }
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
		L.load(root, buffer.fromstring(wire), rep)
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
	report.missingRequired = 0
	report.missingName = nil
	report.nonFinite = 0
	report.reason = nil
	report.field = nil
	report.unknownField = nil
	report.duplicateField = nil
end

-- the fixed reader (docs/SPEC-TABLES.md §3.4): one record, through the
-- emitted layout, its own refusals by name
local function readFixed(T: any, root: string, bytes: buffer, report: any): any
	local d = T[root .. "Descriptor"]
	local fixed = { hash = T[root .. "FixedHash"], recordBytes = T[root .. "FixedRecordBytes"], layout = T[root .. "FixedLayout"] }
	local ok, out = pcall(F.load, d, fixed, bytes)
	if not ok then
		if type(out) == "table" and (out :: any).name ~= nil then
			report.reason = (out :: any).name
			return nil
		end
		error(out, 0)
	end
	if #out ~= 1 then
		report.malformed = true
		report.reason = "wire_damaged"
		return nil
	end
	return out[1]
end

-- ReadStrict(T, root, bytes, report): the root value or nil, with
-- report.reason the name of the first refusal (§6.5's vocabulary, the same
-- spellings in every port) and report.field the first field the walk saw move
-- that counter (nil for a form byte, a trailer, or damage no field holds). T
-- is the <Base>Table module that declares root; report is the caller's.
--
-- NEITHER CLASS BORROWS THE OTHER'S FORM (docs/SPEC-TABLES.md, The three
-- wires; §3, §3.4, §6.5): a reader emitted for one class refuses the other's
-- form BY NAME, and a form byte's refusal says which direction. A DECLARED
-- FIXED root is read the way Go's <T>FixedLoad and fixed.luau's own reader
-- answer it: form 3 goes to its fixed reader (unknown_form when this build
-- emits no fixed layout for it), form 1 is previous_form ("A form-1 file
-- handed to a fixed root is previous_form — a NAMED REFUSAL, never a slow
-- read down the variable wire"), form 2 message_form_as_file, any other byte
-- newer_form. A VARIABLE root reads form 1, and form 3 or any other byte it
-- does not carry is newer_form, form 2 message_form_as_file. The form byte is
-- answered before anything behind it, so a one-byte file names its form. A
-- form byte the root does not read moves no counter (§3, §6.5).
function R.ReadStrict(T: any, root: string, bytes: buffer?, report: any?): any
	local rep: any = report or {}
	reset(rep)
	local d = S.lookup(root)
	if bytes == nil or buffer.len(bytes) < 1 then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local b = bytes :: buffer
	local form = buffer.readu8(b, 0)
	if form == FORM_MESSAGE then rep.reason = "message_form_as_file"; return nil end
	if d.fixed then
		-- the fixed reader's form registry (docs/SPEC-TABLES.md §3, §3.4,
		-- The three wires): 1 is behind it, previous_form
		if form == FORM_VARIABLE then rep.reason = "previous_form"; return nil end
		if form ~= FORM_FIXED then rep.reason = "newer_form"; return nil end
		if T == nil or T[root .. "FixedLayout"] == nil then rep.reason = "unknown_form"; return nil end
		return readFixed(T, root, b, rep)
	end
	if form ~= FORM_VARIABLE then rep.reason = "newer_form"; return nil end
	if d.integrity ~= nil and d.integrity ~= "" then
		local body = integrityOpen(b)
		if body == nil then rep.malformed = true; rep.reason = "integrity_mismatch"; return nil end
		b = body
	end
	-- damage first, before the load runs: the file's own framing, as Go's
	-- tableOpen answers it (docs/SPEC-TABLES.md §3, §6.5) — too short for the
	-- id table, an id table whose count does not frame (load.luau's open reads
	-- a count past 2^63 as negative and walks past the buffer), and a root
	-- body whose terminator comes before its own end: wire_damaged, no field
	local fr = frame(b)
	local top = fr ~= nil and walkBody(b, 1, fr.e, fr.ids) or nil
	if fr == nil or (top ~= nil and top.term + 1 ~= fr.e) then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local v = L.load(root, b, rep)
	-- the load answers nil only for the framing it refuses itself (a
	-- repeated id in the id table): the same damage
	if v == nil and rep.malformed then
		rep.reason = "wire_damaged"
		return nil
	end
	-- then the counters in Err()'s order, so one wire always answers one name
	-- (§6.5, §11)
	for _, k in REFUSING do
		if moved(rep, k) then
			if k == "missingRequired" then
				-- Go's TableMissingRequired text (docs/SPEC-TABLES.md §2.3)
				rep.reason = "missing_required: " .. (rep.missingName or "")
				rep.field = rep.missingName
			else
				rep.reason = NAMES[k]
				rep.field = witness(root, b, k)
			end
			return nil
		end
	end
	-- the fields of the counters that do not refuse, for a caller that asks
	if rep.unknown > 0 then rep.unknownField = witness(root, b, "unknown") end
	if rep.duplicate > 0 then rep.duplicateField = witness(root, b, "duplicate") end
	return v
end

-- the names this file answers, for a caller that wants the spelling rather
-- than the reason string (docs/SPEC-TABLES.md §11, §6.5)
R.ReadStrictNames = table.freeze({
	message_form_as_file = true, newer_form = true, previous_form = true, unknown_form = true,
	integrity_mismatch = true, wire_damaged = true,
	missing_required = true, malformed = true, clamped = true, kind_mismatch = true, non_finite = true,
	-- the fixed reader's own, answered through fixed.luau for a form-3 file
	-- (§3.4, §6.5)
	layout_malformed = true, layout_record_too_large = true, layout_newer = true,
	layout_kind_invalid = true,
	plan_too_large = true, count_over_bound = true,
})

return table.freeze(R)
