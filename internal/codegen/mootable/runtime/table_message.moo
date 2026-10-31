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
-- THE FORM IS FIXED TABLES ONLY (§3.3): a root that is not a fixed table is
-- refused by name, and a fixed root's closure holds no pointer, blob, map,
-- unbounded array or guarded branch, so a body is its fields alone.
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

local MS = {}

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
	return k == 0 or (k >= 1 and k <= 17) or (k >= 18 and k <= 29) or (k >= 30 and k <= 32) or k == 36
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
	if kind == 12 then
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

-- a file's id table holds at most 4096 entries: a count past it is malformed
-- whatever the buffer would hold (docs/SPEC-TABLES.md §3, THE DISTINCTNESS
-- CHECK; docs/LAYERS/1-framing.md F6)
local ID_TABLE_BOUND = 4096

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
	if hi ~= 0 or lo > ID_TABLE_BOUND or lo > (len - 9) // 8 then
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
			elseif kind == 12 or kind == 13 or kind == 14 or kind == 15 or kind == 16 or kind == 31 then
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

local function liveCount(fd: any, holder: any, val: any): number
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
			local probe: any = { w = newWriter(true), refBits = c.refBits, why = nil, missing = false }
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
		if val == nil or val == "" then
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
		if fd.array == "counted" then
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
		if not encodeField(c, d, k, fd, v) then
			return false
		end
	end
	put(c.w, 0, c.refBits)
	return true
end

-- THE MESSAGE FORM IS FIXED TABLES ONLY (§3.3): a root that is not a fixed
-- table is refused by name before a byte is read or written
local function messageRoot(root: string): any
	local d = S.lookup(root)
	if not (d.table and d.fixed) then
		error("schema: " .. root .. " is not a fixed table: the message form is fixed tables only (docs/SPEC-TABLES.md §3.3)")
	end
	return d
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
	local d = messageRoot(root)
	local c: any = { w = newWriter(measuring), refBits = home.refBits, why = nil, missing = false }
	put(c.w, n - 1, 8)
	for _, v in values do
		if not encodeBody(c, d, v) then
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


local function decodeElement(t: any, fd: any, shape: any, dst: any, i: number, keep: boolean): boolean
	local e = fd.e
	local ek = shape.elem
	local inner = shape.inner or {}
	if ek == 13 then
		local d = S.lookup(e.t)
		local inst = d.new()
		if keep then
			dst[i] = inst
		end
		return decodeBody(t, d, inst)
	elseif ek == 15 then
		local u, ok = decodeArm(t, e)
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
	if n > fd.bound then
		kept = fd.bound
		t.report.clamped += 1
	end
	local dst = holder[fd.slot]
	local walk = n
	if run >= 0 and walk > kept then
		walk = kept
	end
	for i = 1, walk do
		if not decodeElement(t, fd, shape, dst, i, i <= kept) then
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
			keep = false
		elseif at > fd.bound then
			t.report.clamped += 1
			keep = false
		end
		if not decodeElement(t, fd, shape, if keep then dst else {}, if keep then at else 1, keep) then
			return false
		end
	end
	return true
end

-- one field's payload under its announced entry (field)
local function decodeField(t: any, fd: any, entry: any, holder: any): boolean
	local s = fd.s
	if s == "arr" and fd.array == "keyed" then
		return decodeKeyed(t, fd, entry, holder)
	elseif s == "str" or s == "bytes" then
		return decodeText(t, fd, entry, holder)
	elseif s == "arr" then
		return decodeArray(t, fd, entry, holder)
	elseif s == "union" then
		local u, ok = decodeArm(t, fd)
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
decodeArm = function(t: any, fd: any): (any, boolean)
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
		u[arm.slot] = inst
		return u, decodeBody(t, d, inst)
	end
	return u, decodeField(t, arm, entry, u)
end

-- one body: fields until the zero reference (body)
decodeBody = function(t: any, d: any, v: any): boolean
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
			if not skipEntry(t, entry.kind, entry.shape) then
				return dmal(t)
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
				t.report.widened += 1
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

-- LoadMessages decodes a batch of root against the announced vocabulary v
-- (DecodeMessages) and answers how many bodies it delivered and the values.
-- -1 with report.reason is a refusal (newer_form, no_vocabulary,
-- integrity_mismatch, non_finite_float); a count above capacity is
-- batch_too_large and returns the wire's M. Damage anywhere discards the
-- whole batch: zero bodies, no values, with report.malformed set (§4).
function MS.LoadMessages(v: any, root: string, data: buffer, capacity: number, report: any?): (number, { any })
	local d = messageRoot(root)
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
	local t: any = { r = r, v = v, report = rep, indexBits = 0 }
	local values = {}
	local got = count
	for i = 1, count do
		local value = d.new()
		local bok = decodeBody(t, d, value)
		if not bok then
			rep.malformed = true
			got = i - 1
			break
		end
		values[i] = value
	end
	if rep.nonFinite > 0 then
		rep.reason = "non_finite_float: " .. tostring(rep.nonFiniteField)
		rep.field = rep.nonFiniteField
		return 0, {}
	end
	if got == count and (not ralign(r) or r.off ~= r.n) then
		rep.malformed = true
	end
	if rep.malformed then
		return 0, {}
	end
	return got, values
end

return table.freeze(MS)
