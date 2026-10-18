--!strict
-- refusal.luau — §6.5's STRICT read and the NAMED refusals it raises
-- (docs/SPEC-TABLES.md §6.5, §3, §2.14, §11; docs/PORT-LUAU.md, THE CARD MAP).
-- It wraps the landed tolerant load (load.luau) and adds nothing to it: the
-- form byte and §2.14's integrity trailer are refused before any field is
-- decoded, and a report that carries any of clamped, unknown, kind_mismatch,
-- duplicate or malformed is refused naming the counter and the first root
-- field that raised it.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local L = require("./load")

local M = {}

-- THE REASONS this read answers (docs/SPEC-TABLES.md §6.5's
-- TableRefuseReason spellings, §2.14). The counters' own reasons are the
-- report's counter names: malformed, kind_mismatch, clamped, duplicate,
-- unknown.
M.TableFormPrevious = "previous_form"
M.TableFormMessageAsFile = "message_form_as_file"
M.TableFormNewer = "newer_form"
M.TableFormUnknown = "unknown_form"
M.TableIntegrityMismatch = "integrity_mismatch"

local NodeTableId: integer = -1i -- 0xffffffffffffffff (docs/SPEC-TABLES.md §3.1)

-- THE FORM BYTE (docs/SPEC-TABLES.md §3, §6.5's previous_form and
-- unknown_form rows). This call reads the VARIABLE form (1), which every
-- table's file may be, a `fixed table`'s included (docs/PORT-LUAU.md, Tables:
-- "for `table` and `fixed table` alike"). The fixed form (3) at a root that
-- does not declare `fixed` is previous_form, as the card names it; at a
-- `fixed table` root it is that root's own form, read by the fixed form's
-- runtime and not by this call, so it is unknown_form: a form byte THIS CALL
-- does not carry. Form 2 handed to a file root is message_form_as_file; a
-- byte past the registry is newer_form; 0 names no form and is unknown_form.
-- (A form-1 file at the FIXED reader is previous_form: that refusal is the
-- fixed form's read, not this one.)
local function formRefusal(desc: any, form: number): string?
	if form == 1 then return nil end
	if form == 3 then
		if desc.fixed then return M.TableFormUnknown end
		return M.TableFormPrevious
	end
	if form == 2 then return M.TableFormMessageAsFile end
	if form == 0 then return M.TableFormUnknown end
	return M.TableFormNewer
end

-- CRC-32C, the Castagnoli polynomial reflected (0x82F63B78), initial value
-- and final XOR 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14).
local crcTable: { number } = {}
for i = 0, 255 do
	local c = i
	for _ = 1, 8 do
		if bit32.band(c, 1) ~= 0 then
			c = bit32.bxor(bit32.rshift(c, 1), 0x82F63B78)
		else
			c = bit32.rshift(c, 1)
		end
	end
	crcTable[i + 1] = c
end

local function crc32c(b: buffer, n: number): number
	local crc = 0xFFFFFFFF
	for i = 0, n - 1 do
		local x = bit32.band(bit32.bxor(crc, buffer.readu8(b, i)), 0xFF)
		crc = bit32.bxor(crcTable[x + 1], bit32.rshift(crc, 8))
	end
	return bit32.bxor(crc, 0xFFFFFFFF)
end

-- THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14): read after the form
-- byte and before any field. A form-1 file shorter than five bytes cannot
-- hold it, and a trailer that is not the CRC-32C of the bytes before it is
-- integrity_mismatch. The trailer is then removed and the rest is §3's file.
local function stripTrailer(wire: buffer): buffer?
	local n = buffer.len(wire)
	if n < 5 then return nil end
	if buffer.readu32(wire, n - 4) ~= crc32c(wire, n - 4) then return nil end
	local body = buffer.create(n - 4)
	buffer.copy(body, 0, wire, 0, n - 4)
	return body
end

local function freshReport(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end

-- the counters this read refuses, damage first and evolution last; the
-- first one a report carries is the reason (docs/SPEC-TABLES.md §6.5)
local function refusingCounter(rep: any): string?
	if rep.malformed then return "malformed" end
	if rep.kindMismatch > 0 then return "kind_mismatch" end
	if rep.clamped > 0 then return "clamped" end
	if rep.duplicate > 0 then return "duplicate" end
	if rep.unknown > 0 then return "unknown" end
	return nil
end

local function raises(rep: any, counter: string): boolean
	if counter == "malformed" then return rep.malformed end
	if counter == "kind_mismatch" then return rep.kindMismatch > 0 end
	return rep[counter] > 0
end

local function r_leb(b: buffer, at: number, e: number): (number?, number)
	local v = 0
	local s = 0
	for i = 0, 9 do
		if at >= e then return nil, at end
		local x = buffer.readu8(b, at)
		at = at + 1
		v = v + (x % 0x80) * (2 ^ s)
		if x < 0x80 then return v, at end
		s = s + 7
	end
	return nil, at
end

-- a field's payload length by its kind's framing (docs/SPEC-TABLES.md §3), the
-- same rule load.luau's skip keeps: the attribution walk below steps over a
-- field without decoding it
local function payloadEnd(b: buffer, at: number, e: number, kind: number): number?
	local w = 0
	if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then w = 1
	elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then w = 2
	elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then w = 4
	elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then w = 8
	elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then w = 16
	elseif kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
		local n, p = r_leb(b, at, e)
		if n == nil then return nil end
		at, w = p, n
	elseif kind == 15 then
		local arm, p = r_leb(b, at, e)
		if arm == nil then return nil end
		if arm == 0 then return p end
		p = p + 1
		local n, q = r_leb(b, p, e)
		if n == nil then return nil end
		at, w = q, n
	elseif kind == 17 or kind == 30 then
		local n, p = r_leb(b, at, e)
		if n == nil then return nil end
		return p
	else
		return nil
	end
	if at + w > e then return nil end
	return at + w
end

-- the name of a root field a header names: its declared name, or the wire id
-- when this build cannot name it (an unknown field)
local function nameOf(desc: any, id: integer): string
	for _, f in desc.fields do
		if f.id == id then return desc.name .. "." .. f.name end
	end
	return string.format("%s.#%016x", desc.name, id)
end

-- THE FIRST FIELD IT SAW (docs/SPEC-TABLES.md §6.5, §11): the root body's
-- field headers are walked in wire order and each is loaded ALONE — the form
-- byte, the node table (§3.1) when there is one, that one field, the
-- terminator and the file's own id table — under the tolerant load; the first
-- whose solo load raises the refusing counter is named. The node table alone
-- raising it names the node table; damage no field reproduces names none.
local function firstField(desc: any, body: buffer, counter: string): string?
	local n = buffer.len(body)
	if n < 9 then return nil end
	local count = integer.tonumber(buffer.readinteger(body, n - 8))
	if count > math.floor((n - 9) / 8) then return nil end
	local tableAt = n - 8 - count * 8
	local fields: { { id: integer, from: number, to: number } } = {}
	local nodeFrom, nodeTo = 0, 0
	local at = 1
	while at < tableAt do
		local from = at
		local ref, p = r_leb(body, at, tableAt)
		if ref == nil or ref == 0 or ref > count or p >= tableAt then break end
		local kind = buffer.readu8(body, p)
		local stop = payloadEnd(body, p + 1, tableAt, kind)
		if stop == nil then break end
		local id = buffer.readinteger(body, tableAt + (ref - 1) * 8)
		if id == NodeTableId then
			nodeFrom, nodeTo = from, stop
		else
			fields[#fields + 1] = { id = id, from = from, to = stop }
		end
		at = stop
	end
	local tail = n - tableAt
	local function solo(from: number, to: number): any
		local w = buffer.create(1 + (nodeTo - nodeFrom) + (to - from) + 1 + tail)
		buffer.writeu8(w, 0, 1)
		local o = 1
		buffer.copy(w, o, body, nodeFrom, nodeTo - nodeFrom)
		o = o + (nodeTo - nodeFrom)
		buffer.copy(w, o, body, from, to - from)
		o = o + (to - from)
		buffer.writeu8(w, o, 0)
		buffer.copy(w, o + 1, body, tableAt, tail)
		local rep = freshReport()
		L.load(desc.name, w, rep)
		return rep
	end
	if nodeTo > nodeFrom and raises(solo(0, 0), counter) then
		return desc.name .. ".(node table)"
	end
	for _, f in fields do
		if raises(solo(f.from, f.to), counter) then return nameOf(desc, f.id) end
	end
	return nil
end

-- TableReadStrict is the STRICT read (docs/SPEC-TABLES.md §6.5): one call
-- that answers the root, or nil and ONE named reason. A counter's refusal
-- also answers the first root field that raised it ("<Table>.<field>", nil
-- for damage no field reproduces). It does not refuse `widened` alone.
-- Order (§2.14): the form byte, then the integrity trailer where the root
-- declares `integrity = "crc32c"`, then the tolerant load and its report.
function M.TableReadStrict(wire: buffer, root: string): (any, string?, string?)
	if buffer.len(wire) < 1 then
		return nil, "malformed", nil
	end
	local desc = S.lookup(root)
	local refused = formRefusal(desc, buffer.readu8(wire, 0))
	if refused ~= nil then
		return nil, refused, nil
	end
	local body = wire
	if desc.integrity == "crc32c" then
		local stripped = stripTrailer(wire)
		if stripped == nil then
			return nil, M.TableIntegrityMismatch, nil
		end
		body = stripped
	end
	local report = freshReport()
	local value = L.load(root, body, report)
	if report.refused > 0 then
		return nil, M.TableFormUnknown, nil
	end
	local counter = refusingCounter(report)
	if counter == nil then
		return value, nil, nil
	end
	return nil, counter, firstField(desc, body, counter)
end

return table.freeze(M)
