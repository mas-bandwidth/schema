--!strict
-- table_message.moo — the moo table runtime's MESSAGE FORM (docs/SPEC-TABLES.md
-- §3.3), form byte 2: a batch of bitpacked bodies of one root under one
-- announced vocabulary. The announcement is an ordinary form-1 file whose body
-- carries the build version and the vocabulary; a batch is the form byte, the
-- count M - 1 in eight bits, and the M bodies as ONE CONTINUOUS BIT STREAM,
-- zero-padded at the end and nowhere else.
--
-- The walks are the Go reference engine's (internal/tablewire messageencode.go,
-- messagedecode.go, message.go) driven by the descriptors (schema_tables.moo)
-- and by the VOCABULARY SLOT of every field and arm, which each generated
-- <Base>Table.moo registers here (internal/codegen/mootable messageform.go).
-- The writer's shapes are read back out of the unit's own announcement, so
-- what a writer encodes with and what it announces are one parse of one set
-- of bytes; the reader decodes at the SENDER's announced shape and lands the
-- value in this build's storage (docs/PORT-MOO.md, THE VALUE MAPPING), with
-- §4's evolution counters.
--
-- moo has no float: a compressed float quantizes and dequantizes in float32
-- on bit patterns through serialize.moo's own soft-float (serialize_float),
-- the arithmetic serialize.moo's compressed float runs, so the index a message
-- carries is the index a packet carries (SPEC.md §4.3). A NaN or an infinity
-- is REFUSED on write, never clamped, and refused on read (§3.3, 2026-10-27).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local V = require("./table_values")
local Fl = require("./serialize_float")

local Int64 = V.Int64
local Uint64 = V.Uint64
local Int128 = V.Int128
local Uint128 = V.Uint128

local MS = {}

-- RETAIN-UNKNOWN's hook for the body being read (docs/SPEC-TABLES.md §6.6;
-- table_retain.moo): nil on every plain read, set once per body by LoadMessages,
-- so the plain walk pays one upvalue test where a retaining read has a hook
local _retain: any = nil

local BATCH_MAX = 256
local DEFAULT_MAX_ENTRIES = 4096 -- ir.TableVocabularyMaxEntries
local DEFAULT_MAX_BYTES = 65536 -- ir.TableVocabularyMaxBytes
local LIST_MAX = 4294967295 -- ir.TableMessageListMax
local INT32_MAX = 2147483647
local TWO32 = 4294967296
local F_ONE = 0x3F800000
local F_HALF = 0x3F000000
local F_VALUES_MAX = 0x4F7FFFFF -- 4294967040, the largest float32 below 2^32

local NODE_ID = V.NodeTableId
local VERSION_ID = V.BuildVersionId
local VOCAB_ID = V.MessageVocabularyId
local BYTES_ID = V.BytesBlobId
local STRING_ID = V.StringBlobId
local KEY_ID = V.MapKeyId

local POW2: { number } = {}
do
	local p = 1
	for i = 0, 32 do
		POW2[i] = p
		p *= 2
	end
end

-- bitsReq(min, max): bits_required(min, max), the bit length of max - min
local function bitsReq(lo: number, hi: number): number
	local n = hi - lo
	local bits = 0
	while n >= 1 do
		bits += 1
		n = n // 2
	end
	return bits
end

-- ---------------------------------------------------------------------------
-- 128-bit two's complement values as four 32-bit limbs, least significant
-- first: every integer a message moves is one of these on the way through.

local function add4(a: { number }, b: { number }): { number }
	local r = table.create(4, 0)
	local carry = 0
	for i = 1, 4 do
		local s = a[i] + b[i] + carry
		r[i] = s % TWO32
		carry = s // TWO32
	end
	return r
end

local function neg4(a: { number }): { number }
	local r = table.create(4, 0)
	local carry = 1
	for i = 1, 4 do
		local s = (4294967295 - a[i]) + carry
		r[i] = s % TWO32
		carry = s // TWO32
	end
	return r
end

local function sub4(a: { number }, b: { number }): { number }
	return add4(a, neg4(b))
end

-- limbs(x, signed): a raw value (a number or a wide class value) as four
-- limbs, sign- or zero-extended
local function limbs(x: any, signed: boolean): { number }
	if type(x) == "number" then
		local m = if x < 0 then -x else x
		local r = { m % TWO32, m // TWO32, 0, 0 }
		if x < 0 then
			return neg4(r)
		end
		return r
	end
	local n = #x
	local fill = 0
	if signed and x[n] >= 2147483648 then
		fill = 4294967295
	end
	local r = table.create(4, fill)
	for i = 1, n do
		r[i] = x[i]
	end
	return r
end

-- the low `bits` of a four-limb value, the rest zeroed
local function lowBits(a: { number }, bits: number): { number }
	local r = table.create(4, 0)
	for i = 1, 4 do
		local lo = (i - 1) * 32
		if bits >= lo + 32 then
			r[i] = a[i]
		elseif bits > lo then
			r[i] = a[i] % POW2[bits - lo]
		end
	end
	return r
end

-- sign-extend from `bits`
local function signExtend(a: { number }, bits: number): { number }
	if bits <= 0 or bits >= 128 then
		return a
	end
	local r = lowBits(a, bits)
	local li = (bits - 1) // 32 + 1
	local bit = (bits - 1) % 32
	if (r[li] // POW2[bit]) % 2 == 1 then
		if bit < 31 then
			r[li] += 4294967296 - POW2[bit + 1]
		end
		for i = li + 1, 4 do
			r[i] = 4294967295
		end
	end
	return r
end

-- ---------------------------------------------------------------------------
-- The bit stream: bit i is bit i%8 of byte i/8, low bit first (§3.3, the
-- packet wire's own rule), relative to the batch's byte 1.

local function newWriter(measuring: boolean): any
	return { b = if measuring then nil else buffer.create(64), n = 0, measuring = measuring }
end

local function ensure(w: any, bits: number)
	local need = (bits + 7) // 8
	local cap = buffer.len(w.b)
	if need <= cap then
		return
	end
	while cap < need do
		cap *= 2
	end
	local nb = buffer.create(cap)
	buffer.copy(nb, 0, w.b, 0, buffer.len(w.b))
	w.b = nb
end

-- put writes the low n (<= 32) bits of v
local function put(w: any, v: number, n: number)
	if n <= 0 then
		return
	end
	local at = w.n
	w.n = at + n
	if w.measuring then
		return
	end
	ensure(w, w.n)
	local done = 0
	while done < n do
		local byteAt = at // 8
		local sh = at % 8
		local take = math.min(8 - sh, n - done)
		local chunk = (v // POW2[done]) % POW2[take]
		local old = buffer.readu8(w.b, byteAt)
		local mask = (POW2[take] - 1) * POW2[sh]
		buffer.writeu8(w.b, byteAt, old - bit32.band(old, mask) + chunk * POW2[sh])
		at += take
		done += take
	end
end

-- putLimbs writes the low n (<= 128) bits of a four-limb value
local function putLimbs(w: any, a: { number }, n: number)
	local i = 1
	while n > 0 do
		local take = math.min(32, n)
		put(w, a[i] % POW2[take], take)
		n -= take
		i += 1
	end
end

local function align(w: any)
	put(w, 0, (8 - w.n % 8) % 8)
end

local function putBytes(w: any, src: buffer, at: number, n: number)
	-- after an align: whole bytes
	if w.measuring then
		w.n += n * 8
		return
	end
	if w.n % 8 == 0 then
		ensure(w, w.n + n * 8)
		buffer.copy(w.b, w.n // 8, src, at, n)
		w.n += n * 8
		return
	end
	for i = 0, n - 1 do
		put(w, buffer.readu8(src, at + i), 8)
	end
end

-- truncate rewinds to n bits, zeroing what the rewound bits held, so the pad
-- a batch ends in is zero
local function truncate(w: any, n: number)
	if not w.measuring then
		local stop = (w.n + 7) // 8
		local first = n // 8
		if n % 8 ~= 0 then
			local old = buffer.readu8(w.b, first)
			buffer.writeu8(w.b, first, old % POW2[n % 8])
			first += 1
		end
		for i = first, stop - 1 do
			buffer.writeu8(w.b, i, 0)
		end
	end
	w.n = n
end

local function newReader(b: buffer, base: number, bits: number): any
	return { b = b, base = base, off = 0, n = bits }
end

local function has(r: any, n: number): boolean
	return n >= 0 and r.off + n <= r.n
end

-- get reads n (<= 32) bits
local function get(r: any, n: number): (number, boolean)
	if n < 0 or n > 32 or not has(r, n) then
		return 0, false
	end
	local v = 0
	local got = 0
	local at = r.off
	while got < n do
		local byteAt = r.base + at // 8
		local sh = at % 8
		local take = math.min(8 - sh, n - got)
		local chunk = (buffer.readu8(r.b, byteAt) // POW2[sh]) % POW2[take]
		v += chunk * POW2[got]
		got += take
		at += take
	end
	r.off = at
	return v, true
end

-- getLimbs reads n (<= 128) bits as four limbs
local function getLimbs(r: any, n: number): ({ number }, boolean)
	local out = table.create(4, 0)
	local i = 1
	while n > 0 do
		local take = math.min(32, n)
		local v, ok = get(r, take)
		if not ok then
			return out, false
		end
		out[i] = v
		n -= take
		i += 1
	end
	return out, true
end

local function skipBits(r: any, n: number): boolean
	if not has(r, n) then
		return false
	end
	r.off += n
	return true
end

local function ralign(r: any): boolean
	while r.off % 8 ~= 0 do
		local bit, ok = get(r, 1)
		if not ok or bit ~= 0 then
			return false
		end
	end
	return true
end

local function left(r: any): number
	return r.n - r.off
end

-- ---------------------------------------------------------------------------
-- THE ANNOUNCEMENT: its form-1 framing, and the vocabulary's entries with
-- their shapes (ir.DecodeShape's grammar, every width checked once, here).

-- leb64 reads one canonical LEB128 of up to 64 bits as four limbs
local function leb64(b: buffer, at: number, e: number): ({ number }?, number)
	local v = { 0, 0, 0, 0 }
	for i = 0, 9 do
		if at + i >= e then
			return nil, at
		end
		local x = buffer.readu8(b, at + i)
		if i == 9 and x > 1 then
			return nil, at
		end
		local low = x % 128
		local pos = i * 7
		-- place 7 bits at pos across the two low limbs
		for k = 0, 6 do
			if (low // POW2[k]) % 2 == 1 then
				local p = pos + k
				if p < 32 then
					v[1] += POW2[p]
				elseif p < 64 then
					v[2] += POW2[p - 32]
				end
			end
		end
		if x < 128 then
			if i > 0 and x == 0 then
				return nil, at
			end
			return v, at + i + 1
		end
	end
	return nil, at
end

local function kindWidth(k: number): number
	return V.width(k)
end

local function integerKind(k: number): boolean
	return (k >= 2 and k <= 9) or k == 18 or k == 19
end

local function fixedKind(k: number): boolean
	return k >= 20 and k <= 29
end

local function knownKind(k: number): boolean
	return k == 0 or (k >= 1 and k <= 17) or (k >= 18 and k <= 29) or (k >= 30 and k <= 33) or k == 36
end

-- the compressed float's derivation (ir.TableMessageQuantization): the step
-- count and delta in float32, nil for a non-conforming triple
local function quantization(qmin: number, qmax: number, qres: number): (number?, number)
	if not Fl.lt(qmin, qmax) or not Fl.lt(0, qres) then
		return nil, 0
	end
	local delta = Fl.fsub(qmax, qmin)
	local values = Fl.fdiv(delta, qres)
	if not Fl.isFinite(delta) or not Fl.isFinite(values) then
		return nil, 0
	end
	if not Fl.le(F_ONE, values) then
		values = F_ONE
	elseif Fl.lt(F_VALUES_MAX, values) then
		values = F_VALUES_MAX
	end
	return Fl.ceilToInt(values), delta
end

local readShape: any

readShape = function(b: buffer, at: number, e: number, kind: number): (any, number?)
	local s: any = { packing = 0, bits = 0, base = nil, min = 0, max = 0, elem = 0, inner = nil }
	if integerKind(kind) or fixedKind(kind) or kind == 10 then
		if at >= e then
			return s, nil
		end
		s.packing = buffer.readu8(b, at)
		at += 1
		if s.packing == 0 then
			return s, at
		elseif s.packing == 1 and kind ~= 10 then
			local n
			n, at = V.leb(b, at, e)
			if n == nil or n > kindWidth(kind) * 8 then
				return s, nil
			end
			s.bits = n
			if kind == 18 or kind == 19 or fixedKind(kind) then
				if at + 16 > e then
					return s, nil
				end
				s.base = { buffer.readu32(b, at), buffer.readu32(b, at + 4), buffer.readu32(b, at + 8), buffer.readu32(b, at + 12) }
				at += 16
			else
				local z
				z, at = leb64(b, at, e)
				if z == nil then
					return s, nil
				end
				if V.signedKind(kind) then
					-- zigzag: (v >> 1) ^ -(v & 1), over 64 bits
					local odd = z[1] % 2 == 1
					local hi, lo = z[2], z[1]
					local shifted = { (lo // 2) + (hi % 2) * 2147483648, hi // 2, 0, 0 }
					if odd then
						shifted = { 4294967295 - shifted[1], 4294967295 - shifted[2], 4294967295, 4294967295 }
					end
					s.base = shifted
				else
					s.base = z
				end
			end
			return s, at
		elseif s.packing == 2 and kind == 10 then
			if at + 12 > e then
				return s, nil
			end
			s.qmin = buffer.readu32(b, at)
			s.qmax = buffer.readu32(b, at + 4)
			s.qres = buffer.readu32(b, at + 8)
			at += 12
			local count, delta = quantization(s.qmin, s.qmax, s.qres)
			if count == nil then
				return s, nil
			end
			s.count = count
			s.delta = delta
			s.bits = bitsReq(0, count)
			return s, at
		end
		return s, nil
	end
	if kind == 12 or kind == 33 then
		local n
		n, at = V.leb(b, at, e)
		if n == nil or n > INT32_MAX then
			return s, nil
		end
		s.max = n
	elseif kind == 14 or kind == 16 then
		if kind == 14 then
			local n
			n, at = V.leb(b, at, e)
			if n == nil or n > LIST_MAX then
				return s, nil
			end
			s.min = n
		end
		local n
		n, at = V.leb(b, at, e)
		if n == nil or n > LIST_MAX or n < s.min then
			return s, nil
		end
		s.max = n
		if at >= e then
			return s, nil
		end
		s.elem = buffer.readu8(b, at)
		at += 1
		if not knownKind(s.elem) then
			return s, nil
		end
		local inner, next = readShape(b, at, e, s.elem)
		if next == nil then
			return s, nil
		end
		s.inner = inner
		at = next
	end
	return s, at
end

-- valueBits (ir.TableMessageValueBits): one value's width under a shape, -1
-- where the kind's payload is not one fixed-width value
local function valueBits(kind: number, s: any): number
	if kind == 1 then
		return 1
	elseif kind == 11 or kind == 36 then
		return 64
	elseif kind == 10 then
		return if s.packing == 2 then s.bits else 32
	elseif integerKind(kind) or fixedKind(kind) then
		return if s.packing == 1 then s.bits else kindWidth(kind) * 8
	end
	return -1
end

local function countBits(s: any): number
	return bitsReq(s.min, s.max)
end

local function aligns(kind: number, s: any): boolean
	return kind == 12 or (kind == 14 and s.elem == 6)
end

-- MS.NewVocabulary is one direction's vocabulary, unannounced.
function MS.NewVocabulary(): any
	return { entries = {}, count = 0, refBits = 1, buildVersion = nil, announced = false, refused = false, maxBytes = 0, maxEntries = 0 }
end

local function resetReport(report: any)
	report.malformed = false
	report.refused = false
	report.reason = nil
	report.field = nil
	report.unknown = 0
	report.kindMismatch = 0
	report.widened = 0
	report.clamped = 0
	report.duplicate = 0
	report.nonFinite = 0
	report.nonFiniteField = nil
end

MS.resetReport = resetReport

local function refuse(report: any, why: string): boolean
	report.refused = true
	report.reason = why
	return false
end

local function announceRead(v: any, data: buffer, report: any): boolean
	local len = buffer.len(data)
	if len < 1 then
		report.malformed = true
		return false
	end
	local form = buffer.readu8(data, 0)
	if form ~= 1 then
		return refuse(report, if form == 2 then "message_form_as_file" else "newer_form")
	end
	if len < 9 then
		report.malformed = true
		return false
	end
	local lo = buffer.readu32(data, len - 8)
	local hi = buffer.readu32(data, len - 4)
	if hi ~= 0 or lo > (len - 9) // 8 then
		report.malformed = true
		return false
	end
	local start = len - 8 - lo * 8
	local ids = table.create(lo)
	for i = 0, lo - 1 do
		ids[i + 1] = buffer.readstring(data, start + i * 8, 8)
	end
	-- the body's extent: a terminated body must end exactly at the id table
	local at = 1
	local versions, vocabs = 0, 0
	local version: string? = nil
	local words: buffer? = nil
	local maxBytes = if v.maxBytes ~= nil and v.maxBytes > 0 then v.maxBytes else DEFAULT_MAX_BYTES
	while true do
		local ref, next = V.leb(data, at, start)
		if ref == nil then
			report.malformed = true
			return false
		end
		at = next
		if ref == 0 then
			if at ~= start then
				report.malformed = true
				return false
			end
			break
		end
		if ref > #ids or at >= start then
			report.malformed = true
			return false
		end
		local kind = buffer.readu8(data, at)
		at += 1
		local id = ids[ref]
		if id == VERSION_ID then
			if kind ~= 9 or at + 8 > start then
				report.malformed = true
				return false
			end
			version = buffer.readstring(data, at, 8)
			at += 8
			versions += 1
		elseif id == VOCAB_ID then
			local n
			n, at = V.leb(data, at, start)
			if kind ~= 14 or n == nil or at + n > start then
				report.malformed = true
				return false
			end
			local bodyAt, bodyEnd = at, at + n
			at = bodyEnd
			if bodyAt >= bodyEnd or buffer.readu8(data, bodyAt) ~= 6 then
				report.malformed = true
				return false
			end
			local m, wordsAt = V.leb(data, bodyAt + 1, bodyEnd)
			if m == nil or wordsAt + m ~= bodyEnd then
				report.malformed = true
				return false
			end
			if m > maxBytes then
				v.buildVersion = version
				return refuse(report, "vocabulary_too_large")
			end
			local wb = buffer.create(m)
			buffer.copy(wb, 0, data, wordsAt, m)
			words = wb
			vocabs += 1
		else
			report.unknown += 1
			local w = V.width(kind)
			if w > 0 then
				if at + w > start then
					report.malformed = true
					return false
				end
				at += w
			elseif kind == 12 or kind == 13 or kind == 14 or kind == 15 or kind == 16 or kind == 31 or kind == 33 then
				local n
				n, at = V.leb(data, at, start)
				if n == nil or at + n > start then
					report.malformed = true
					return false
				end
				at += n
			elseif kind == 17 or kind == 30 then
				local n
				n, at = V.leb(data, at, start)
				if n == nil then
					report.malformed = true
					return false
				end
			elseif kind ~= 0 and kind ~= 32 then
				report.malformed = true
				return false
			end
		end
	end
	v.buildVersion = version
	if versions ~= 1 or vocabs ~= 1 or words == nil then
		report.malformed = true
		return false
	end
	local wb = words :: buffer
	local we = buffer.len(wb)
	local entries = {}
	local seen: { [string]: boolean } = {}
	local nodeTables = 0
	local p = 0
	while p < we do
		if p + 9 > we then
			report.malformed = true
			return false
		end
		local id = buffer.readstring(wb, p, 8)
		local kind = buffer.readu8(wb, p + 8)
		if id == VERSION_ID or id == VOCAB_ID or not knownKind(kind) then
			report.malformed = true
			return false
		end
		if id == NODE_ID then
			nodeTables += 1
			if nodeTables > 1 then
				report.malformed = true
				return false
			end
		end
		local shape, next = readShape(wb, p + 9, we, kind)
		if next == nil then
			report.malformed = true
			return false
		end
		local key = buffer.readstring(wb, p, next - p)
		if seen[key] then
			report.malformed = true
			return false
		end
		seen[key] = true
		entries[#entries + 1] = { id = id, kind = kind, shape = shape }
		p = next
	end
	local maxEntries = if v.maxEntries ~= nil and v.maxEntries > 0 then v.maxEntries else DEFAULT_MAX_ENTRIES
	if #entries > maxEntries then
		return refuse(report, "vocabulary_too_large")
	end
	v.entries = entries
	v.count = #entries
	v.refBits = if #entries <= 0 then 1 else bitsReq(0, #entries)
	v.announced = true
	return true
end

-- AnnounceRead resolves one announcement into the caller's vocabulary
-- (§3.3: the announcement reads tolerantly with exactly two strict checks; a
-- refusal is terminal, and a second announcement is second_announcement).
function MS.AnnounceRead(v: any, data: buffer, report: any?): boolean
	local rep: any = report or {}
	resetReport(rep)
	if v.announced or v.refused then
		return refuse(rep, "second_announcement")
	end
	local ok = announceRead(v, data, rep)
	if not v.announced then
		v.refused = true
	end
	return ok
end

-- ---------------------------------------------------------------------------
-- THIS BUILD's vocabulary and slots, registered by the generated modules.

local home: any = nil
local homeBytes: string? = nil
local slotsByName: { [string]: { number } } = {}
local nameSlot: { [string]: number } = {}

-- unit registers the unit's own announcement (a compile-time constant) and
-- returns it as a buffer; every table module of the unit carries the same
-- bytes, and a second, different unit in one program is refused.
function MS.unit(bytes: string): buffer
	if homeBytes ~= nil then
		if homeBytes ~= bytes then
			error("schema: two units' table modules are loaded in one program; the message form has one vocabulary a program")
		end
		return buffer.fromstring(bytes)
	end
	local v = MS.NewVocabulary()
	local rep: any = {}
	resetReport(rep)
	homeBytes = bytes
	if not announceRead(v, buffer.fromstring(bytes), rep) then
		-- a unit whose closure announces a kind the message form does not
		-- spell: the Go engine refuses that announcement too, and this build's
		-- writer has no vocabulary to write with, which writeBatch names
		return buffer.fromstring(bytes)
	end
	home = v
	for i, e in v.entries do
		if e.kind == 0 then
			nameSlot[e.id] = i
		end
	end
	return buffer.fromstring(bytes)
end

-- slots registers one descriptor's fields (a union's arms) as vocabulary
-- slots, in declared order
function MS.slots(name: string, list: { number })
	slotsByName[name] = table.freeze(list)
end

-- the slot of this build's field or arm k of descriptor d
local function slotOf(d: any, k: number): number
	local l = slotsByName[d.name]
	if l == nil then
		return 0
	end
	return l[k] or 0
end

-- ---------------------------------------------------------------------------
-- Shared facts of a field.

local fieldMaps: { [any]: any } = {}
local function fieldsById(d: any): { [string]: any }
	local m = fieldMaps[d]
	if m == nil then
		local built: { [string]: any } = {}
		for i, f in d.fields do
			built[f.id] = { fd = f, k = i }
		end
		fieldMaps[d] = built
		return built
	end
	return m
end

local function guardHolds(fd: any, v: any): boolean
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

local function liveCount(fd: any, holder: any, val: any): number
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

local function elemAt(val: any, i: number): any
	if type(val) == "buffer" then
		return buffer.readu8(val, i - 1)
	end
	if type(val) ~= "table" then
		return nil
	end
	return val[i]
end

-- a scalar storage value at its declared default (internal/tablewire
-- cellIsDefault): a ranged float32 compares by value, so -0 is +0 there; an
-- unranged float by its bits
local function atDefault(fd: any, val: any): boolean
	local s = fd.s
	if s == "bool" then
		return (val == true) == (fd.def == true)
	end
	if s == "enum" then
		return val == (fd.def or 0)
	end
	if s == "float" and fd.f32 and fd.flo ~= nil and type(val) == "number" and type(fd.def) == "number" then
		local a = if val == 0x80000000 then 0 else val
		local b = if fd.def == 0x80000000 then 0 else fd.def
		return a == b
	end
	return V.sameStorage(fd, val, fd.def)
end

local function ownerName(named: any): string
	if named ~= nil and named.owner ~= nil and named.name ~= nil then
		return named.owner .. "." .. named.name
	end
	return "?"
end

-- ---------------------------------------------------------------------------
-- THE WRITER (internal/tablewire messageencode.go).

local encodeBody: any
local encodeArm: any

local function wref(c: any, slot: number)
	if slot == 0 then
		c.missing = true
	end
	put(c.w, slot, c.refBits)
end

local function wname(c: any, id: string)
	local slot = nameSlot[id]
	if slot == nil then
		c.missing = true
		slot = 0
	end
	put(c.w, slot, c.refBits)
end

local function fail(c: any, name: string, named: any): boolean
	if c.why == nil then
		c.why = { name = name, field = if named ~= nil then ownerName(named) else nil }
	end
	return false
end

-- one value at its MINE shape: bool, a float's bits or its quantized index,
-- an integer raw or as value - base (encodeBitValue)
local function encodeValue(c: any, e: any, kind: number, shape: any, val: any, named: any): boolean
	local w = c.w
	if kind == 1 then
		put(w, if val == true then 1 else 0, 1)
		return true
	elseif kind == 11 then
		if not Uint64.is(val) then
			return fail(c, "invalid_value", named)
		end
		if not V.finite64(val[1], val[2]) then
			return fail(c, "non_finite_float", named)
		end
		put(w, val[1], 32)
		put(w, val[2], 32)
		return true
	elseif kind == 10 then
		if type(val) ~= "number" or val ~= math.floor(val) or val < 0 or val > 4294967295 then
			return fail(c, "invalid_value", named)
		end
		if not V.finite32(val) then
			return fail(c, "non_finite_float", named)
		end
		if shape.packing == 2 then
			-- THE PACKET WIRE'S RULE, IN FLOAT32 (SPEC.md §4.3)
			local normalized = Fl.fdiv(Fl.fsub(val, shape.qmin), shape.delta)
			if not Fl.le(0, normalized) then
				normalized = 0
			elseif not Fl.le(normalized, F_ONE) then
				normalized = F_ONE
			end
			local scaled = Fl.fmul(normalized, Fl.fromUint(shape.count))
			local index = Fl.floorToInt(Fl.fadd(scaled, F_HALF))
			if index > shape.count then
				index = shape.count
			end
			put(w, index, shape.bits)
			return true
		end
		put(w, val, 32)
		return true
	end
	local bits = valueBits(kind, shape)
	if bits <= 0 then
		return true
	end
	local raw = V.toRaw(e, val)
	if raw == nil then
		return fail(c, "invalid_value", named)
	end
	local x = limbs(raw, e.signed == true)
	if shape.packing == 1 and shape.base ~= nil then
		x = sub4(x, shape.base)
	end
	putLimbs(w, x, bits)
	return true
end

local function encodeEnum(c: any, e: any, val: any, named: any): boolean
	if val == nil or val == 0 then
		put(c.w, 0, c.refBits)
		return true
	end
	local pair = if type(val) == "number" then e.vs[val] else nil
	if pair == nil or pair[3] == true then
		return fail(c, "invalid_value", named)
	end
	wname(c, pair[2])
	return true
end

local function encodeText(c: any, fd: any, shape: any, kind: number, holder: any, val: any, named: any): boolean
	local w = c.w
	if kind == 33 then
		local units = holder[fd.slot .. "Length"] or 0
		if type(val) ~= "buffer" or units * 2 > buffer.len(val) then
			return fail(c, "invalid_value", named)
		end
		put(w, units, bitsReq(0, shape.max))
		for i = 0, units - 1 do
			put(w, buffer.readu16(val, i * 2), 16)
		end
		return true
	end
	local s = if val == nil then "" else val
	if type(s) ~= "string" or not V.utf8String(s) then
		return fail(c, "invalid_value", named)
	end
	put(w, #s, bitsReq(0, shape.max))
	align(w)
	if #s > 0 then
		putBytes(w, buffer.fromstring(s), 0, #s)
	end
	return true
end

local function encodeBytes(c: any, fd: any, shape: any, holder: any, val: any): boolean
	local w = c.w
	local n = holder[fd.slot .. "Length"] or 0
	if type(val) ~= "buffer" or n > buffer.len(val) then
		return fail(c, "invalid_value", fd)
	end
	put(w, n, countBits(shape))
	align(w)
	putBytes(w, val, 0, n)
	return true
end

local function encodeElement(c: any, fd: any, shape: any, x: any): boolean
	local e = fd.e
	local inner = shape.inner or {}
	local ek = shape.elem
	if ek == 13 then
		return encodeBody(c, S.lookup(e.t), x)
	elseif ek == 17 then
		put(c.w, if not x then 0 else c.nodes.index[x], c.indexBits)
		return true
	elseif ek == 30 then
		return encodeEnum(c, e, x, fd)
	elseif ek == 15 then
		if type(x) ~= "table" or x.Type == 0 then
			put(c.w, 0, c.refBits)
			return true
		end
		return encodeArm(c, S.lookup(e.t), x, fd)
	end
	return encodeValue(c, e, ek, inner, x, fd)
end

local function encodeArray(c: any, fd: any, entry: any, holder: any, val: any, count: number): boolean
	local shape = entry.shape
	local bits = countBits(shape)
	if bits > 0 then
		put(c.w, count - shape.min, bits)
	end
	if aligns(entry.kind, shape) then
		align(c.w)
	end
	for i = 1, count do
		if not encodeElement(c, fd, shape, elemAt(val, i)) then
			return false
		end
	end
	return true
end

local function elemDefault(e: any, x: any): boolean
	local s = e.s
	if s == "bool" then
		return x == false or x == nil
	elseif s == "int" or s == "float" then
		return atDefault(e, x)
	elseif s == "enum" then
		return x == 0 or x == nil
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		return not x
	elseif s == "union" then
		return type(x) ~= "table" or x.Type == 0
	end
	return false
end

local function encodeKeyed(c: any, fd: any, entry: any, val: any): boolean
	local shape = entry.shape
	local e = fd.e
	local present = {}
	for slot = 1, fd.bound do
		local x = elemAt(val, slot)
		local pair = fd.keyvs[slot]
		if pair == nil then
			continue
		end
		if pair[3] == true and not elemDefault(e, x) and e.s ~= "tab" then
			return fail(c, "invalid_value", fd)
		end
		if shape.elem == 13 then
			local probe: any = { w = newWriter(true), refBits = c.refBits, indexBits = c.indexBits, nodes = c.nodes, why = nil, missing = false }
			if not encodeBody(probe, S.lookup(e.t), x) then
				c.why = probe.why
				return false
			end
			if probe.w.n <= c.refBits then
				continue
			end
		elseif elemDefault(e, x) then
			continue
		end
		if pair[3] == true then
			return fail(c, "invalid_value", fd)
		end
		present[#present + 1] = slot
	end
	if #present == 0 then
		return true
	end
	wref(c, entry.slot)
	put(c.w, #present, bitsReq(0, shape.max))
	for _, slot in present do
		wname(c, fd.keyvs[slot][2])
		if not encodeElement(c, fd, shape, elemAt(val, slot)) then
			return false
		end
	end
	return true
end

local function keyLess(keyfd: any, a: any, b: any): boolean
	local ka, kb = a[keyfd.slot], b[keyfd.slot]
	if type(ka) == "string" and type(kb) == "string" then
		return ka < kb
	end
	if type(ka) == "boolean" then
		return (not ka) and kb
	end
	if type(ka) == "number" and type(kb) == "number" then
		return ka < kb
	end
	local C = if keyfd.cls == "I64" then Int64 elseif keyfd.cls == "U64" then Uint64 elseif keyfd.cls == "I128" then Int128 else Uint128
	local ok, r = pcall(C.cmp, ka, kb)
	return ok and r < 0
end

local function encodeMap(c: any, fd: any, entry: any, val: any): boolean
	if type(val) ~= "table" or #val == 0 then
		return true
	end
	local ed = S.lookup(fd.t)
	local keyfd = nil
	for _, f in ed.fields do
		if f.id == KEY_ID then
			keyfd = f
		end
	end
	local order = table.clone(val)
	if keyfd ~= nil then
		table.sort(order, function(a: any, b: any): boolean
			return keyLess(keyfd, a, b)
		end)
	end
	wref(c, entry.slot)
	put(c.w, #order, countBits(entry.shape))
	for _, ev in order do
		if not encodeBody(c, ed, ev) then
			return false
		end
	end
	return true
end

-- the entry this build announces for slot k
local function mineEntry(slot: number): any
	local e = home.entries[slot]
	if e == nil then
		return { slot = 0, kind = 0, shape = { packing = 0, bits = 0, min = 0, max = 0, elem = 0 } }
	end
	return { slot = slot, kind = e.kind, shape = e.shape }
end

-- one field (encodeBitField): the elisions are the variable form's
local function encodeField(c: any, d: any, k: number, fd: any, v: any): boolean
	local entry = mineEntry(slotOf(d, k))
	local shape = entry.shape
	local s = fd.s
	local val = v[fd.slot]
	local w = c.w
	if s == "ptr" or s == "sblob" or s == "bblob" then
		if val == nil then
			return true
		end
		wref(c, entry.slot)
		put(w, c.nodes.index[val], c.indexBits)
		return true
	end
	if s == "map" then
		return encodeMap(c, fd, entry, val)
	end
	if fd.present ~= nil then
		if not v[fd.present] then
			return true
		end
		wref(c, entry.slot)
		if s == "arr" then
			local count = if fd.array == "fixed" then fd.bound else liveCount(fd, v, val)
			return encodeArray(c, fd, entry, v, val, count)
		elseif s == "tab" then
			return encodeBody(c, S.lookup(fd.t), val)
		elseif s == "enum" then
			return encodeEnum(c, fd, val, fd)
		end
		-- every other present optional rides the engine's value path, which
		-- spells a string, bytes or union kind as no payload: the reference
		-- alone (internal/tablewire encodeBitField)
		return encodeValue(c, fd, entry.kind, shape, val, fd)
	end
	if s == "arr" and fd.array == "keyed" then
		return encodeKeyed(c, fd, entry, val)
	end
	if s == "str" then
		if fd.wide then
			if (v[fd.slot .. "Length"] or 0) == 0 then
				return true
			end
		elseif val == nil or val == "" then
			return true
		end
		wref(c, entry.slot)
		return encodeText(c, fd, shape, entry.kind, v, val, fd)
	end
	if s == "text" then
		return fail(c, "invalid_value", fd) -- unbounded text: the form spells no capacity for it
	end
	if s == "bytes" then
		if (v[fd.slot .. "Length"] or 0) == 0 then
			return true
		end
		wref(c, entry.slot)
		return encodeBytes(c, fd, shape, v, val)
	end
	if s == "arr" then
		if fd.e.s == "str" or fd.e.s == "text" then
			return fail(c, "invalid_value", fd) -- a text list: no element kind 12 rides this form
		end
		if fd.array == "counted" or fd.array == "list" then
			local n = liveCount(fd, v, val)
			if n == 0 then
				return true
			end
			wref(c, entry.slot)
			return encodeArray(c, fd, entry, v, val, n)
		end
		if fd.e.s ~= "tab" then
			local rides = false
			for i = 1, fd.bound do
				if not elemDefault(fd.e, elemAt(val, i)) then
					rides = true
					break
				end
			end
			if not rides then
				return true
			end
		end
		wref(c, entry.slot)
		return encodeArray(c, fd, entry, v, val, fd.bound)
	end
	if s == "union" then
		if type(val) ~= "table" or val.Type == 0 then
			return true
		end
		local un = S.lookup(fd.t)
		if un.arms[val.Type] == nil then
			return fail(c, "invalid_value", fd)
		end
		wref(c, entry.slot)
		return encodeArm(c, un, val, fd)
	end
	if s == "tab" then
		local start = w.n
		wref(c, entry.slot)
		if not encodeBody(c, S.lookup(fd.t), val) then
			return false
		end
		if w.n - start == 2 * c.refBits then
			truncate(w, start) -- reference plus terminator: an all-default body elides
		end
		return true
	end
	if s == "enum" then
		if atDefault(fd, val) then
			return true
		end
		wref(c, entry.slot)
		return encodeEnum(c, fd, val, fd)
	end
	if s ~= "bool" and s ~= "int" and s ~= "float" then
		return fail(c, "invalid_value", fd)
	end
	if atDefault(fd, val) then
		return true
	end
	wref(c, entry.slot)
	return encodeValue(c, fd, entry.kind, shape, val, fd)
end

-- one union ARM: its name's reference, then the payload a field of the arm's
-- type would carry (encodeBitArm)
encodeArm = function(c: any, un: any, u: any, named: any): boolean
	local tag = u.Type
	local arm = un.arms[tag]
	if arm == nil then
		return fail(c, "invalid_value", named)
	end
	local entry = mineEntry(slotOf(un, tag))
	wref(c, entry.slot)
	local s = arm.s
	if s == "void" then
		return true
	end
	local val = u[arm.slot]
	local shape = entry.shape
	if s == "tab" then
		return encodeBody(c, S.lookup(arm.t), val)
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		put(c.w, if not val then 0 else c.nodes.index[val], c.indexBits)
		return true
	elseif s == "str" then
		return encodeText(c, arm, shape, entry.kind, u, val, arm)
	elseif s == "bytes" then
		return encodeBytes(c, arm, shape, u, val)
	elseif s == "arr" then
		local count = if arm.array == "fixed" then arm.bound else liveCount(arm, u, val)
		return encodeArray(c, arm, entry, u, val, count)
	elseif s == "union" then
		if type(val) ~= "table" or val.Type == 0 then
			put(c.w, 0, c.refBits)
			return true
		end
		return encodeArm(c, S.lookup(arm.t), val, arm)
	elseif s == "enum" then
		return encodeEnum(c, arm, val, arm)
	elseif s == "text" then
		return fail(c, "invalid_value", arm)
	end
	return encodeValue(c, arm, entry.kind, shape, val, arm)
end

encodeBody = function(c: any, d: any, v: any): boolean
	if type(v) ~= "table" then
		return fail(c, "invalid_value", nil)
	end
	for k, fd in d.fields do
		if guardHolds(fd, v) then
			if not encodeField(c, d, k, fd, v) then
				return false
			end
		end
	end
	put(c.w, 0, c.refBits)
	return true
end

-- the numbering is the variable form's (table_save.moo): one walk, one order
local Save = require("./table_save")

local function encodeRoot(c: any, d: any, v: any): boolean
	local nodes, why = Save.number(d, v)
	if nodes == nil then
		c.why = why
		return false
	end
	c.nodes = nodes
	c.indexBits = bitsReq(0, 1)
	if #nodes.list > 0 then
		c.indexBits = bitsReq(0, #nodes.list + 1)
		wname(c, NODE_ID)
		put(c.w, #nodes.list, 32)
		for _, n in nodes.list do
			wname(c, n.typeId)
			if n.blob ~= nil then
				local len = buffer.len(n.blob)
				if n.str and not V.utf8(n.blob, 0, len) then
					return fail(c, "invalid_value", nil)
				end
				put(c.w, len, 32)
				align(c.w)
				putBytes(c.w, n.blob, 0, len)
			elseif not encodeBody(c, n.d, n.v) then
				return false
			end
		end
	end
	return encodeBody(c, d, v)
end

local function writeBatch(root: string, values: { any }, measuring: boolean, report: any): (any, number)
	resetReport(report)
	local n = #values
	if n < 1 or n > BATCH_MAX then
		refuse(report, "batch_too_large")
		return nil, -1
	end
	if home == nil then
		error("schema: this build has no message vocabulary: no unit is loaded, or its announcement carries a kind the message form does not spell (docs/SPEC-TABLES.md §3.3)")
	end
	local d = S.lookup(root)
	local c: any = { w = newWriter(measuring), refBits = home.refBits, indexBits = 1, nodes = nil, why = nil, missing = false }
	put(c.w, n - 1, 8)
	for _, v in values do
		if not encodeRoot(c, d, v) then
			local why = c.why or { name = "invalid_value" }
			report.refused = true
			report.reason = why.name
			report.field = why.field
			return nil, -1
		end
	end
	if c.missing then
		error("schema: the unit's vocabulary names no slot for an entry this walk reached (a compiler defect, docs/SPEC-TABLES.md §3.3)")
	end
	align(c.w)
	local size = 1 + c.w.n // 8
	if d.integrity ~= nil and d.integrity ~= "" then
		size += 4
	end
	return c.w, size
end

-- MeasureMessages is the size SaveMessages writes for values (a batch of one
-- root), or -1 with report.reason the refusal.
function MS.MeasureMessages(root: string, values: { any }, report: any?): number
	local rep: any = report or {}
	local _, size = writeBatch(root, values, true, rep)
	return size
end

-- SaveMessages writes the batch (§3.3): the form byte, M - 1, the bodies as one
-- bit stream padded to a byte, and for a root that declares integrity the
-- CRC-32C trailer (§2.14). nil and -1 with report.reason on a refusal.
function MS.SaveMessages(root: string, values: { any }, report: any?): (buffer?, number)
	local rep: any = report or {}
	local w, size = writeBatch(root, values, false, rep)
	if w == nil then
		return nil, -1
	end
	local out = buffer.create(size)
	buffer.writeu8(out, 0, 2)
	local stream = w.n // 8
	buffer.copy(out, 1, w.b, 0, stream)
	local d = S.lookup(root)
	if d.integrity ~= nil and d.integrity ~= "" then
		buffer.writeu32(out, 1 + stream, V.crc32c(out, 1 + stream))
	end
	return out, size
end

-- ---------------------------------------------------------------------------
-- THE READER (internal/tablewire messagedecode.go): it decodes at the SENDER's
-- announced shape. Damage is terminal for the batch: the bodies before it
-- stand, one malformed counts, and nothing after it is read.

local decodeBody: any
local skipEntry: any
local decodeArm: any

local function dmal(t: any): boolean
	t.report.malformed = true
	return false
end

local function entryAt(t: any, ref: number): any
	if ref == 0 or ref > t.v.count then
		return nil
	end
	return t.v.entries[ref]
end

local function reserved(id: string): boolean
	return id == NODE_ID or id == VERSION_ID or id == VOCAB_ID
end

local function reference(t: any): (number, boolean)
	return get(t.r, t.v.refBits)
end

-- a reference used as a NAME must name a kind-0, unreserved entry
local function nameAt(t: any, ref: number): any
	local e = entryAt(t, ref)
	if e == nil or reserved(e.id) or e.kind ~= 0 then
		t.report.malformed = true
		return nil
	end
	return e
end

local function armAt(t: any, ref: number): any
	local e = entryAt(t, ref)
	if e == nil or reserved(e.id) or e.kind == 0 then
		t.report.malformed = true
		return nil
	end
	return e
end

local function index(t: any): (number, boolean)
	if t.indexBits <= 0 then
		return 0, false
	end
	return get(t.r, t.indexBits)
end

-- the bits one element of an array occupies where that is one number, -1
-- where each must be walked (elementRunBits)
local function runBits(kind: number, elem: number, inner: any): number
	if kind ~= 14 then
		return -1
	end
	if elem == 13 or elem == 15 or elem == 30 or elem == 17 then
		return -1
	end
	if elem == 0 or elem == 32 then
		return 0
	end
	return valueBits(elem, inner or {})
end

local function skipRun(r: any, n: number, width: number): boolean
	if width == 0 then
		return true
	end
	return skipBits(r, n * width)
end

local function skipBody(t: any): boolean
	while true do
		local ref, ok = reference(t)
		if not ok then
			return false
		end
		if ref == 0 then
			return true
		end
		local e = entryAt(t, ref)
		if e == nil or not skipEntry(t, e.kind, e.shape) then
			return false
		end
	end
end

skipEntry = function(t: any, kind: number, shape: any): boolean
	local r = t.r
	if kind == 0 or kind == 32 then
		return true
	elseif kind == 30 then
		local ref, ok = reference(t)
		if not ok then
			return false
		end
		if ref == 0 then
			return true
		end
		return nameAt(t, ref) ~= nil
	elseif kind == 15 then
		local ref, ok = reference(t)
		if not ok then
			return false
		end
		if ref == 0 then
			return true
		end
		local arm = armAt(t, ref)
		if arm == nil then
			return false
		end
		return skipEntry(t, arm.kind, arm.shape)
	elseif kind == 13 then
		return skipBody(t)
	elseif kind == 17 then
		local _, ok = index(t)
		return ok
	elseif kind == 12 then
		local n, ok = get(r, bitsReq(0, shape.max))
		if not ok or not ralign(r) then
			return false
		end
		return skipBits(r, n * 8)
	elseif kind == 33 then
		local n, ok = get(r, bitsReq(0, shape.max))
		if not ok then
			return false
		end
		return skipBits(r, n * 16)
	elseif kind == 31 then
		if not ralign(r) then
			return false
		end
		local n, ok = get(r, 32)
		if not ok then
			return false
		end
		return skipBits(r, n * 8)
	elseif kind == 14 or kind == 16 then
		local n = shape.min
		local width = countBits(shape)
		if kind == 16 then
			width, n = bitsReq(0, shape.max), 0
		end
		if width > 0 then
			local raw, ok = get(r, width)
			if not ok then
				return false
			end
			n = raw
			if kind == 14 then
				n += shape.min
			end
		end
		if aligns(kind, shape) and not ralign(r) then
			return false
		end
		local inner = shape.inner or {}
		local run = runBits(kind, shape.elem, inner)
		if run >= 0 then
			return skipRun(r, n, run)
		end
		for i = 1, n do
			local at = r.off
			if kind == 16 then
				local _, ok = reference(t)
				if not ok then
					return false
				end
			end
			if not skipEntry(t, shape.elem, inner) then
				return false
			end
			if r.off == at then
				if n - i > left(r) then
					t.report.malformed = true
					return false
				end
				return true
			end
		end
		return true
	end
	local width = valueBits(kind, shape)
	if width < 0 then
		return false
	end
	return skipBits(r, width)
end

-- MESSAGE WIDENING (§3.3, §4): a lower kind on the same ladder, or, where
-- both are arrays, a lower element kind
local function widens(theirs: any, kind: number, elem: number): boolean
	if theirs.shape.elem == elem then
		return V.widens(theirs.kind, kind)
	end
	if theirs.kind == kind and kind == 14 then
		return V.widens(theirs.shape.elem, elem)
	end
	return false
end

-- one integer decoded at the announced shape and landed in fd's kind and
-- storage: ranged values are offset + base, raw ones sign-extend by this
-- build's signedness; the declared range and bits(N) clamp (scalar)
local function decodeInteger(t: any, fd: any, kind: number, shape: any): (any, boolean)
	local width = valueBits(kind, shape)
	if width < 0 then
		return nil, true
	end
	local x, ok = getLimbs(t.r, width)
	if not ok then
		return nil, false
	end
	local mykw = fd.kw or kindWidth(kind)
	local signed = fd.signed == true
	if kindWidth(kind) == 16 or fixedKind(kind) then
		-- the 128-bit and fixed family: a big value, then the field's clamp
		if shape.packing == 1 then
			x = add4(x, shape.base or { 0, 0, 0, 0 })
		elseif kindWidth(kind) < 16 then
			x = if V.signedKind(kind) then signExtend(x, width) else x
		end
	else
		if shape.packing == 1 then
			x = lowBits(add4(x, shape.base or { 0, 0, 0, 0 }), 64)
		elseif signed and width < 64 and width > 0 then
			x = signExtend(x, width)
		end
		x = if signed then signExtend(x, 64) else lowBits(x, 64)
	end
	-- the value in this build's kind domain
	local C = V.kindClass(mykw, signed)
	local value: any
	if mykw == 16 then
		value = (C :: any).new(x[1], x[2], x[3], x[4])
	else
		local W = if signed then Int64 else Uint64
		value = W.new(x[1], x[2])
	end
	local lo, hi = fd.lo, fd.hi
	if lo ~= nil then
		local Cmp = if mykw == 16 then C else (if signed then Int64 else Uint64)
		if Cmp.cmp(value, lo) < 0 then
			value = lo
			t.report.clamped += 1
		elseif Cmp.cmp(value, hi) > 0 then
			value = hi
			t.report.clamped += 1
		end
	end
	if fd.bits ~= nil and fd.bits < 64 then
		local cap = Uint64.sub(Uint64.lsh(Uint64.fromNumber(1), fd.bits), Uint64.fromNumber(1))
		local uv = if type(value) == "number" then value else (if Int64.is(value) then Int64.toUint64(value) else value)
		if Uint64.cmp(uv, cap) > 0 then
			value = cap
			t.report.clamped += 1
		end
	end
	-- narrow to the kind's raw domain
	local raw: any = value
	if mykw <= 4 then
		if type(value) ~= "number" then
			local l = limbs(value, signed)
			if not signed then
				local m = l[1] % POW2[mykw * 8]
				raw = m
			else
				local n = (if Int64.is(value) then Int64 else Uint64).tryNumber(value)
				raw = n or 0
			end
		end
	elseif mykw == 8 then
		if type(value) == "number" then
			raw = (if signed then Int64 else Uint64).fromNumber(value)
		end
	end
	if fd.cls == nil then
		return raw, true
	end
	return V.toStorage(fd, raw), true
end

-- one float32 at the announced shape: a quantized index dequantized, or the
-- raw bits (refused when not finite); landed in fd (widened into f64, or
-- clamped to fd's range with -0 as +0)
local function decodeFloat32(t: any, fd: any, shape: any, named: any): (any, boolean)
	local bits: number
	if shape.packing == 2 then
		local index, ok = get(t.r, shape.bits)
		if not ok or shape.count == nil or index > shape.count then
			return nil, dmal(t)
		end
		local normalized = Fl.fdiv(Fl.fromUint(index), Fl.fromUint(shape.count))
		bits = Fl.fadd(Fl.fmul(normalized, shape.delta), shape.qmin)
	else
		local raw, ok = get(t.r, 32)
		if not ok then
			return nil, dmal(t)
		end
		if not V.finite32(raw) then
			t.report.nonFinite += 1
			if t.report.nonFiniteField == nil then
				t.report.nonFiniteField = ownerName(named)
			end
			return fd.def, true
		end
		bits = raw
	end
	if fd.f32 == false then
		local lo, hi = V.f32to64(bits)
		return Uint64.new(lo, hi), true
	end
	if fd.flo ~= nil then
		local lo, hi = V.f32to64(bits)
		if V.cmp64(lo, hi, fd.flo[1], fd.flo[2]) < 0 then
			t.report.clamped += 1
			return fd.flo32, true
		elseif V.cmp64(lo, hi, fd.fhi[1], fd.fhi[2]) > 0 then
			t.report.clamped += 1
			return fd.fhi32, true
		elseif bits == 0x80000000 then
			return 0, true
		end
	end
	return bits, true
end

-- one scalar of announced kind and shape, as fd's storage (scalar)
local function decodeScalar(t: any, fd: any, kind: number, shape: any, named: any): (any, boolean)
	if kind == 1 then
		local v, ok = get(t.r, 1)
		if not ok then
			return nil, dmal(t)
		end
		return v ~= 0, true
	elseif kind == 11 then
		local lo, ok1 = get(t.r, 32)
		local hi, ok2 = get(t.r, 32)
		if not ok1 or not ok2 then
			return nil, dmal(t)
		end
		if not V.finite64(lo, hi) then
			t.report.nonFinite += 1
			if t.report.nonFiniteField == nil then
				t.report.nonFiniteField = ownerName(named)
			end
			return fd.def, true
		end
		return Uint64.new(lo, hi), true
	elseif kind == 10 then
		return decodeFloat32(t, fd, shape, named)
	end
	local v, ok = decodeInteger(t, fd, kind, shape)
	if not ok then
		return nil, dmal(t)
	end
	return v, true
end

local function decodeEnum(t: any, e: any, element: boolean): (any, boolean)
	local ref, ok = reference(t)
	if not ok then
		return nil, dmal(t)
	end
	if ref == 0 then
		return 0, true
	end
	local en = nameAt(t, ref)
	if en == nil then
		return nil, false
	end
	for i, pair in e.vs do
		if pair[2] == en.id then
			if pair[3] == true then
				return if element then 0 else (e.def or 0), true
			end
			return i, true
		end
	end
	t.report.unknown += 1
	if _retain ~= nil then
		_retain.lost(t.report) -- an enum variant this build cannot name (§6.6)
	end
	return 0, true
end

-- string(N) / bytes(N): the length at its width, the align, the bytes; a
-- payload past this build's bound keeps what fits and counts clamped
local function decodeText(t: any, fd: any, entry: any, holder: any): boolean
	local r = t.r
	local shape = entry.shape
	local isBytes = fd.s == "bytes"
	local width = if isBytes then countBits(shape) else bitsReq(0, shape.max)
	local n, ok = get(r, width)
	if not ok or not ralign(r) or not has(r, n * 8) then
		return dmal(t)
	end
	local at = r.base + r.off // 8
	r.off += n * 8
	if not isBytes and not V.utf8(r.b, at, n) then
		return dmal(t)
	end
	local keep = n
	if fd.bound ~= nil and keep > fd.bound then
		keep = if isBytes then fd.bound else V.clampUtf8(r.b, at, fd.bound)
		t.report.clamped += 1
	end
	if isBytes then
		local dst = holder[fd.slot]
		if type(dst) ~= "buffer" or buffer.len(dst) < keep then
			dst = buffer.create(math.max(keep, fd.bound or keep))
			holder[fd.slot] = dst
		end
		buffer.fill(dst, 0, 0)
		buffer.copy(dst, 0, r.b, at, keep)
		holder[fd.slot .. "Length"] = keep
	else
		holder[fd.slot] = buffer.readstring(r.b, at, keep)
	end
	return true
end

-- wstring(N): the length, NO align, sixteen bits a unit
local function decodeWide(t: any, fd: any, entry: any, holder: any): boolean
	local r = t.r
	local n, ok = get(r, bitsReq(0, entry.shape.max))
	if not ok or not has(r, n * 16) then
		return dmal(t)
	end
	local units = buffer.create(n * 2)
	for i = 0, n - 1 do
		local u = get(r, 16)
		buffer.writeu16(units, i * 2, u)
	end
	if not V.wide(units, 0, n * 2) then
		return dmal(t)
	end
	local keep = n
	if fd.bound ~= nil and keep > fd.bound then
		keep = V.clampWide(units, 0, fd.bound)
		t.report.clamped += 1
	end
	local dst = holder[fd.slot]
	if type(dst) ~= "buffer" or buffer.len(dst) < keep * 2 then
		dst = buffer.create(math.max(keep, fd.bound or keep) * 2)
		holder[fd.slot] = dst
	end
	buffer.fill(dst, 0, 0)
	buffer.copy(dst, 0, units, 0, keep * 2)
	holder[fd.slot .. "Length"] = keep
	return true
end

-- a pointer (or blob) index lands through resolution after the body
local function pend(t: any, holder: any, key: any, idx: number, d: any, null: any)
	t.pending[#t.pending + 1] = { holder = holder, key = key, index = idx, d = d, null = null }
end

local function decodeElement(t: any, fd: any, shape: any, dst: any, i: number, keep: boolean): boolean
	local e = fd.e
	local ek = shape.elem
	local inner = shape.inner or {}
	if ek == 13 then
		local d = S.lookup(e.t)
		local inst = d.new()
		if keep then
			if _retain ~= nil then
				_retain.forgetBody(d, dst[i]) -- the element's earlier body goes with it (§6.6)
			end
			dst[i] = inst
		end
		return decodeBody(t, d, inst)
	elseif ek == 17 then
		local idx, ok = index(t)
		if not ok then
			return dmal(t)
		end
		if keep then
			local null: any = nil
			if fd.array == "list" then
				null = false
			end
			dst[i] = null
			pend(t, dst, i, idx, e, null)
		end
		return true
	elseif ek == 15 then
		local u, ok = decodeArm(t, e, dst, i)
		if keep then
			dst[i] = u
		end
		return ok
	elseif ek == 30 then
		local x, ok = decodeEnum(t, e, true)
		if not ok then
			return false
		end
		if keep then
			dst[i] = x
		end
		return true
	end
	local x, ok = decodeScalar(t, e, ek, inner, fd)
	if not ok then
		return false
	end
	if keep and x ~= nil then
		if type(dst) == "buffer" then
			buffer.writeu8(dst, i - 1, x)
		else
			dst[i] = x
		end
	end
	return true
end

-- AN ELEMENT THE READER IS DROPPING (an unknown keyed slot's, a key past this
-- build's slots, an element past this build's bound) is DECODED into a sink,
-- because a bit stream has to be walked past: nothing in it is a field of this
-- region, so it runs with RETENTION OFF and the slot or element that was
-- excluded took everything under it with it (§6.6). Only the callers of a
-- dropped element come here, so the plain walk pays nothing per element.
local function decodeDropped(t: any, fd: any, shape: any, dst: any, i: number): boolean
	local was = _retain
	_retain = nil
	local done = decodeElement(t, fd, shape, dst, i, false)
	_retain = was
	return done
end

-- a positional array (array): the count, the align, the elements; a count past
-- this build's bound keeps the first N and counts clamped
local function decodeArray(t: any, fd: any, entry: any, holder: any): boolean
	local r = t.r
	local shape = entry.shape
	local n = shape.min
	local bits = countBits(shape)
	if bits > 0 then
		local raw, ok = get(r, bits)
		if not ok then
			return dmal(t)
		end
		n = raw + shape.min
	end
	if aligns(entry.kind, shape) and not ralign(r) then
		return dmal(t)
	end
	local inner = shape.inner or {}
	local run = runBits(entry.kind, shape.elem, inner)
	local kept = n
	local dst: any
	if fd.array == "list" then
		if n > INT32_MAX or n > left(r) / math.max(run, 1) then
			return dmal(t)
		end
		if _retain ~= nil then
			-- A SECOND OCCURRENCE OF A COLLECTION REPLACES IT WHOLE, and it takes
			-- its discard HERE, past the count and the align (§6.6)
			_retain.supersede(holder, fd.slot, fd)
		end
		dst = {}
		holder[fd.slot] = dst
	else
		if n > fd.bound then
			kept = fd.bound
			t.report.clamped += 1
		end
		if _retain ~= nil then
			_retain.supersede(holder, fd.slot, fd)
		end
		dst = holder[fd.slot]
	end
	local walk = n
	if run >= 0 and walk > kept then
		walk = kept
	end
	for i = 1, walk do
		local elemOk: boolean
		if i <= kept or _retain == nil then
			elemOk = decodeElement(t, fd, shape, dst, i, i <= kept)
		else
			elemOk = decodeDropped(t, fd, shape, dst, i)
		end
		if not elemOk then
			return false
		end
	end
	if walk < n and not skipRun(r, n - walk, run) then
		return dmal(t)
	end
	if fd.array == "counted" then
		holder[fd.slot .. "Count"] = kept
	end
	if fd.present ~= nil then
		holder[fd.present] = true
	end
	return true
end

-- an enum-keyed array (keyed): the present-slot count, then (key, element)
local function decodeKeyed(t: any, fd: any, entry: any, holder: any): boolean
	local shape = entry.shape
	local n, ok = get(t.r, bitsReq(0, shape.max))
	if not ok then
		return dmal(t)
	end
	local dst = holder[fd.slot]
	for _ = 1, n do
		local ref, rok = reference(t)
		if not rok then
			return dmal(t)
		end
		local key = nameAt(t, ref)
		if key == nil then
			return false
		end
		local at = 0
		for i, kv in fd.keyvs do
			if kv[2] == key.id then
				at = i
				break
			end
		end
		local keep = true
		if at == 0 then
			t.report.unknown += 1
			if _retain ~= nil then
				_retain.lost(t.report) -- a keyed slot this build cannot name (§6.6)
			end
			keep = false
		elseif at > fd.bound then
			t.report.clamped += 1
			keep = false
		end
		local elemOk: boolean
		if keep or _retain == nil then
			elemOk = decodeElement(t, fd, shape, if keep then dst else {}, if keep then at else 1, keep)
		else
			elemOk = decodeDropped(t, fd, shape, {}, 1)
		end
		if not elemOk then
			return false
		end
	end
	return true
end

-- the key one map entry carries, scanned ahead of the body (mapEntryKey)
local function scanKey(t: any, keyfd: any): (any, boolean, boolean, boolean)
	local r = t.r
	local saved = r.off
	local quiet = { malformed = false, clamped = 0, unknown = 0, nonFinite = 0, kindMismatch = 0 }
	local scan = { r = r, v = t.v, report = quiet, indexBits = t.indexBits }
	local key: any = nil
	local bad, widened = false, false
	local function done(whole: boolean): (any, boolean, boolean, boolean)
		r.off = saved
		return key, bad, widened, whole
	end
	while true do
		local ref, ok = reference(scan)
		if not ok then
			return done(false)
		end
		if ref == 0 then
			return done(true)
		end
		local e = entryAt(scan, ref)
		if e == nil or reserved(e.id) then
			return done(false)
		end
		if e.id ~= KEY_ID or keyfd == nil then
			if not skipEntry(scan, e.kind, e.shape) then
				return done(false)
			end
			continue
		end
		local want = keyfd.k
		bad = e.kind ~= want and not V.widens(e.kind, want)
		widened = widened or (e.kind ~= want and not bad)
		if bad then
			if not skipEntry(scan, e.kind, e.shape) then
				return done(false)
			end
			continue
		end
		if keyfd.s == "str" or keyfd.s == "text" then
			local n, nok = get(r, bitsReq(0, e.shape.max))
			if not nok or not ralign(r) or not has(r, n * 8) then
				return done(false)
			end
			local at = r.base + r.off // 8
			r.off += n * 8
			if not V.utf8(r.b, at, n) then
				return done(false)
			end
			key = buffer.readstring(r.b, at, n)
		else
			local x, xok = decodeScalar(scan, keyfd, e.kind, e.shape, keyfd)
			if not xok then
				return done(false)
			end
			key = x
		end
	end
end

local function keyCompare(keyfd: any, a: any, b: any): number
	if type(a) == "string" and type(b) == "string" then
		return if a < b then -1 elseif a > b then 1 else 0
	end
	if type(a) == "number" and type(b) == "number" then
		return if a < b then -1 elseif a > b then 1 else 0
	end
	if type(a) == "boolean" then
		return if a == b then 0 elseif a then 1 else -1
	end
	local C = if keyfd.cls == "I64" then Int64 elseif keyfd.cls == "U64" then Uint64 elseif keyfd.cls == "I128" then Int128 else Uint128
	return C.cmp(a, b)
end

local function decodeMap(t: any, fd: any, entry: any, holder: any): boolean
	local n, ok = get(t.r, countBits(entry.shape))
	if not ok then
		return dmal(t)
	end
	n += entry.shape.min
	if n > INT32_MAX then
		return dmal(t)
	end
	local ed = S.lookup(fd.t)
	local keyfd = fieldsById(ed)[KEY_ID]
	keyfd = if keyfd ~= nil then keyfd.fd else nil
	local landed: { any } = {}
	if _retain ~= nil then
		-- A SECOND OCCURRENCE OF A MAP REPLACES IT WHOLE (§6.6)
		_retain.supersede(holder, fd.slot, fd)
	end
	holder[fd.slot] = landed
	local last: any = nil
	local hasLast = false
	local keyWidened = false
	local i = 1
	while i <= n do
		local key, bad, widened, whole = scanKey(t, keyfd)
		if not whole then
			return dmal(t)
		end
		if widened and not keyWidened then
			keyWidened = true
			t.report.widened += 1
		end
		if bad then
			holder[fd.slot] = {}
			t.report.kindMismatch += 1
			while i <= n do
				if not skipBody(t) then
					return dmal(t)
				end
				i += 1
			end
			return true
		end
		if keyfd ~= nil and type(key) == "string" and keyfd.bound ~= nil and keyfd.bound > 0 and #key > keyfd.bound then
			t.report.clamped += 1
			if not skipBody(t) then
				return dmal(t)
			end
		else
			local order = -1
			if hasLast then
				order = keyCompare(keyfd, last, key)
			end
			if order > 0 then
				return dmal(t)
			end
			local vals = ed.new()
			t.mapKey = true
			local bok = decodeBody(t, ed, vals)
			t.mapKey = false
			if _retain ~= nil then
				_retain.builtMap(holder, fd.slot, ed, vals)
			end
			if not bok then
				return false
			end
			if order == 0 then
				if _retain ~= nil then
					_retain.forgetBody(ed, landed[#landed]) -- last wins WHOLE (§6.6)
				end
				landed[#landed] = vals
				t.report.duplicate += 1
			else
				landed[#landed + 1] = vals
			end
			last = if keyfd ~= nil then vals[keyfd.slot] else nil
			hasLast = true
		end
		i += 1
	end
	return true
end

-- one field's payload under its announced entry (field)
local function decodeField(t: any, fd: any, entry: any, holder: any): boolean
	local s = fd.s
	if s == "map" then
		return decodeMap(t, fd, entry, holder)
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		local idx, ok = index(t)
		if not ok then
			return dmal(t)
		end
		pend(t, holder, fd.slot, idx, fd, nil)
		return true
	elseif s == "arr" and fd.array == "keyed" then
		return decodeKeyed(t, fd, entry, holder)
	elseif s == "str" and fd.wide then
		return decodeWide(t, fd, entry, holder)
	elseif s == "str" or s == "bytes" then
		return decodeText(t, fd, entry, holder)
	elseif s == "arr" then
		return decodeArray(t, fd, entry, holder)
	elseif s == "union" then
		if _retain ~= nil then
			_retain.supersede(holder, fd.slot, fd) -- the union written again (§6.6)
		end
		local u, ok = decodeArm(t, fd, holder, fd.slot)
		holder[fd.slot] = u
		return ok
	elseif s == "enum" then
		local x, ok = decodeEnum(t, fd, false)
		if not ok then
			return false
		end
		holder[fd.slot] = x
		return true
	elseif s == "tab" then
		local d = S.lookup(fd.t)
		local inst = d.new()
		if _retain ~= nil then
			_retain.forgetBody(d, holder[fd.slot]) -- a second occurrence replaces the body whole (§6.6)
		end
		holder[fd.slot] = inst
		return decodeBody(t, d, inst)
	elseif s == "text" then
		return dmal(t)
	end
	local x, ok = decodeScalar(t, fd, entry.kind, entry.shape, fd)
	if not ok then
		return false
	end
	holder[fd.slot] = x
	return true
end

-- one union value (unionCell): the arm's reference, then the payload a field
-- of the arm's type carries; an arm this build cannot name reads None and
-- counts unknown, a retyped one kind_mismatch
decodeArm = function(t: any, fd: any, holder: any, key: any): (any, boolean)
	local un = S.lookup(fd.t)
	local u = un.new()
	local ref, ok = reference(t)
	if not ok then
		return u, dmal(t)
	end
	if ref == 0 then
		return u, true
	end
	local entry = armAt(t, ref)
	if entry == nil then
		return u, false
	end
	local tag = 0
	for i, a in un.arms do
		if a.id == entry.id then
			tag = i
			break
		end
	end
	if tag == 0 then
		t.report.unknown += 1
		if _retain ~= nil then
			_retain.lost(t.report) -- a union arm this build cannot name (§6.6)
		end
		return u, skipEntry(t, entry.kind, entry.shape)
	end
	local arm = un.arms[tag]
	local mine = mineEntry(slotOf(un, tag))
	if entry.kind ~= mine.kind or entry.shape.elem ~= mine.shape.elem then
		if not widens(entry, mine.kind, mine.shape.elem) then
			t.report.kindMismatch += 1
			return u, skipEntry(t, entry.kind, entry.shape)
		end
		t.report.widened += 1
	end
	u.Type = tag
	if arm.s == "void" then
		return u, true
	elseif arm.s == "tab" then
		local d = S.lookup(arm.t)
		local inst = d.new()
		if _retain ~= nil and holder ~= nil then
			_retain.builtArm(holder, key, d, inst) -- the occurrence's own body (§6.6)
		end
		u[arm.slot] = inst
		return u, decodeBody(t, d, inst)
	end
	return u, decodeField(t, arm, entry, u)
end

-- one body: fields until the zero reference (body)
decodeBody = function(t: any, d: any, v: any): boolean
	local mapKey = t.mapKey
	t.mapKey = false
	local byId = fieldsById(d)
	while true do
		local ref, ok = reference(t)
		if not ok then
			return dmal(t)
		end
		if ref == 0 then
			return true
		end
		local entry = entryAt(t, ref)
		if entry == nil or reserved(entry.id) then
			return dmal(t)
		end
		local hit = byId[entry.id]
		if hit == nil then
			t.report.unknown += 1
			-- THE SKIP RUNS FIRST AND THE CAPTURE SECOND, over the same bits: the
			-- plain read's own verdict on an unknown entry is the skip's
			local at = t.r.off
			if not skipEntry(t, entry.kind, entry.shape) then
				return dmal(t)
			end
			if _retain ~= nil then
				_retain.captureMessage(t, v, d, entry, at, t.r.off)
			end
			continue
		end
		local fd = hit.fd
		local mine = mineEntry(slotOf(d, hit.k))
		if entry.kind ~= mine.kind or entry.shape.elem ~= mine.shape.elem then
			if widens(entry, mine.kind, mine.shape.elem) then
				if not decodeField(t, fd, entry, v) then
					return false
				end
				if mapKey and entry.id == KEY_ID then
					-- the MAP counts one widened, at the key (§2.8, §4)
				else
					t.report.widened += 1
				end
				if fd.present ~= nil and fd.s ~= "arr" then
					v[fd.present] = true
				end
				continue
			end
			t.report.kindMismatch += 1
			if not skipEntry(t, entry.kind, entry.shape) then
				return dmal(t)
			end
			continue
		end
		if not decodeField(t, fd, entry, v) then
			return false
		end
		if fd.present ~= nil and fd.s ~= "arr" then
			v[fd.present] = true
		end
	end
end

-- resolution (§3.1): 0 is null, 1 the root, k + 1 record k
local function resolve(t: any)
	if not t.good then
		return
	end
	local landed: { [any]: boolean } = {}
	for _, p in t.pending do
		local value: any = nil
		if p.index ~= 0 then
			if p.index == 1 then
				if p.d.s == "ptr" and p.d.t == t.rootDesc.name then
					value = t.root
				else
					t.report.kindMismatch += 1
				end
			elseif p.index - 2 >= #t.nodes then
				t.report.malformed = true
			else
				local n = t.nodes[p.index - 1]
				if n.kind == "s" or n.kind == "b" then
					if (p.d.s == "sblob" and n.kind == "s") or (p.d.s == "bblob" and n.kind == "b") then
						if landed[n] then
							t.report.blobShared = (t.report.blobShared or 0) + 1
						end
						landed[n] = true
						value = n.blob
					else
						t.report.kindMismatch += 1
					end
				elseif n.kind == "t" then
					if p.d.s == "ptr" and n.desc.name == p.d.t then
						value = n.v
					else
						t.report.kindMismatch += 1
					end
				end
			end
		end
		if value == nil then
			value = p.null
		end
		if p.holder ~= nil and p.key ~= nil then
			p.holder[p.key] = value
		end
	end
end

-- one root body: a variable root's NODE TABLE first (root, nodeTable)
local function decodeRoot(t: any, d: any, v: any): boolean
	t.pending = {}
	t.nodes = {}
	t.root = v
	t.rootDesc = d
	t.good = true
	if not d.variable then
		t.indexBits = 0
		return decodeBody(t, d, v)
	end
	t.indexBits = bitsReq(0, 1)
	local r = t.r
	local at = r.off
	local ref, ok = reference(t)
	if not ok then
		return dmal(t)
	end
	local e = entryAt(t, ref)
	if e == nil and ref ~= 0 then
		return dmal(t)
	end
	if e ~= nil and e.id == NODE_ID then
		local count, cok = get(r, 32)
		if not cok then
			return dmal(t)
		end
		t.indexBits = bitsReq(0, count + 1)
		local placed: { [string]: any } = {}
		for _, name in d.place.tables do
			local pd = S.lookup(name)
			placed[pd.id] = pd
		end
		local recs: { any } = {}
		for _ = 1, count do
			local tref, tok = reference(t)
			if not tok then
				return dmal(t)
			end
			local te = nameAt(t, tref)
			if te == nil then
				return false
			end
			if te.id == BYTES_ID or te.id == STRING_ID then
				local n, nok = get(r, 32)
				if not nok or not ralign(r) or not has(r, n * 8) then
					return dmal(t)
				end
				local blob = buffer.create(n)
				buffer.copy(blob, 0, r.b, r.base + r.off // 8, n)
				r.off += n * 8
				recs[#recs + 1] = { id = te.id, blob = blob }
			else
				local s = r.off
				if not skipBody(t) then
					return dmal(t)
				end
				recs[#recs + 1] = { id = te.id, s = s, e = r.off }
			end
		end
		local stop = r.off
		for i, rec in recs do
			if rec.blob ~= nil then
				local str = rec.id == STRING_ID
				if (str and not d.place.str) or (not str and not d.place.bytes) then
					t.report.unknown += 1
					if _retain ~= nil then
						_retain.lost(t.report) -- a blob this root reaches no edge to (§6.6)
					end
					t.nodes[i] = { kind = "x" }
				else
					if str and not V.utf8(rec.blob, 0, buffer.len(rec.blob)) then
						return dmal(t)
					end
					t.nodes[i] = { kind = if str then "s" else "b", blob = rec.blob }
				end
			else
				local pd = placed[rec.id]
				if pd == nil then
					t.report.unknown += 1
					if _retain ~= nil then
						_retain.lost(t.report) -- a node record of a type this build cannot place (§6.6)
					end
					t.nodes[i] = { kind = "x" }
				else
					t.nodes[i] = { kind = "t", desc = pd, v = pd.new() }
				end
			end
		end
		for i, rec in recs do
			local n = t.nodes[i]
			if n.kind == "t" then
				r.off = rec.s
				local sn = r.n
				r.n = rec.e
				local bok = decodeBody(t, n.desc, n.v)
				r.n = sn
				if not bok then
					return false
				end
			end
		end
		r.off = stop
	else
		r.off = at
	end
	return decodeBody(t, d, v)
end

-- LoadMessages decodes a batch of root against the announced vocabulary v
-- (DecodeMessages) and answers how many bodies it delivered and the values.
-- -1 with report.reason is a refusal (newer_form, no_vocabulary,
-- integrity_mismatch, non_finite_float); a count above capacity is
-- batch_too_large and returns the wire's M; damage inside body k delivers
-- bodies 1 to k - 1 with report.malformed set.
function MS.LoadMessages(v: any, root: string, data: buffer, capacity: number, report: any?, hooks: { any }?): (number, { any })
	local rep: any = report or {}
	resetReport(rep)
	local len = buffer.len(data)
	if len < 1 then
		rep.malformed = true
		return 0, {}
	end
	if buffer.readu8(data, 0) ~= 2 then
		refuse(rep, "newer_form")
		return -1, {}
	end
	local d = S.lookup(root)
	if d.integrity ~= nil and d.integrity ~= "" then
		local n = len
		if n < 6 or buffer.readu32(data, n - 4) ~= V.crc32c(data, n - 4) then
			rep.malformed = true
			rep.reason = "integrity_mismatch"
			return -1, {}
		end
		local body = buffer.create(n - 4)
		buffer.copy(body, 0, data, 0, n - 4)
		data = body
		len = n - 4
	end
	if v == nil or not v.announced then
		refuse(rep, "no_vocabulary")
		return -1, {}
	end
	if home == nil then
		error("schema: this build has no message vocabulary: its announcement carries a kind the message form does not spell (docs/SPEC-TABLES.md §3.3)")
	end
	local r = newReader(data, 1, (len - 1) * 8)
	local raw, ok = get(r, 8)
	if not ok then
		rep.malformed = true
		return 0, {}
	end
	local count = raw + 1
	if count > capacity then
		refuse(rep, "batch_too_large")
		return count, {}
	end
	local t: any = { r = r, v = v, report = rep, indexBits = 0, mapKey = false }
	local values = {}
	local got = count
	for i = 1, count do
		local value = d.new()
		_retain = if hooks ~= nil then hooks[i] else nil
		local bok = decodeRoot(t, d, value)
		resolve(t)
		if not bok then
			rep.malformed = true
			got = i - 1
			break
		end
		values[i] = value
	end
	_retain = nil
	if rep.nonFinite > 0 then
		rep.reason = "non_finite_float: " .. tostring(rep.nonFiniteField)
		rep.field = rep.nonFiniteField
		return 0, {}
	end
	if got == count and (not ralign(r) or r.off ~= r.n) then
		rep.malformed = true
	end
	return got, values
end

-- THE BIT STREAM'S OWN PRIMITIVES, for the retaining walk (table_retain.moo):
-- it re-reads the bits an unknown entry's skip delimited and writes them in
-- the variable form's spelling, so it reads with the very reader the plain
-- walk does and states no second width (§3.3, §6.6)
MS.bitio = table.freeze({
	get = get,
	getLimbs = getLimbs,
	ralign = ralign,
	has = has,
	reference = reference,
	entryAt = entryAt,
	reserved = reserved,
	bitsReq = bitsReq,
	valueBits = valueBits,
	countBits = countBits,
	aligns = aligns,
	add4 = add4,
	lowBits = lowBits,
	signExtend = signExtend,
	left = left,
})

return table.freeze(MS)
