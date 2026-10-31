--!strict
-- load.moo — the moo table runtime's VARIABLE FORM (form 1) load: the
-- tolerant walk, its report, and the strict read (docs/SPEC-TABLES.md §3,
-- §3.1, §4, §6.5). A port of the Luau leg's load.luau (the dev branch) onto
-- moo's values (table_values.moo; docs/PORT-MOO.md), with main's rules: a
-- non-finite float is refused on its raw bits by every read (§3.3, the Go
-- engine's NonFiniteRefusal), and the walk is the Go reference engine's
-- (internal/tablewire decode.go, decodenodes.go).
--
-- The load is DRIVEN BY THE DESCRIPTOR (docs/SPEC-TABLES.md §8.1).
--
-- THE REPORT (docs/SPEC-TABLES.md §4): the six counters and the verdict. The
-- caller owns it; the codec never makes one.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local V = require("./table_values")

local L = {}

local NodeTableId = V.NodeTableId
local BuildVersionId = V.BuildVersionId
local MessageVocabularyId = V.MessageVocabularyId
local StringBlobId = V.StringBlobId
local BytesBlobId = V.BytesBlobId
local MapKeyId = V.MapKeyId

local function new_reader(bytes: buffer, at: number, e: number): any
	return { b = bytes, at = at, e = e, bad = false }
end

-- a FAILED LEB LEAVES THE CURSOR WHERE IT STARTED (docs/SPEC-TABLES.md §3)
local function r_leb(r: any): number
	local v, at = V.leb(r.b, r.at, r.e)
	if v == nil then
		r.bad = true
		return 0
	end
	r.at = at
	return v
end

local function r_u8(r: any): number
	if r.at >= r.e then
		r.bad = true
		return 0
	end
	local v = buffer.readu8(r.b, r.at)
	r.at += 1
	return v
end

-- skipping a field the reader cannot name (docs/SPEC-TABLES.md §3)
local function skip(r: any, kind: number): boolean
	local w = V.width(kind)
	if w > 0 then
		if r.at + w > r.e then
			return false
		end
		r.at += w
		return true
	end
	if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then
			return false
		end
		r.at += n
		return true
	elseif kind == 15 then
		local arm = r_leb(r)
		if r.bad then
			return false
		end
		if arm ~= 0 then
			if r.at + 1 > r.e then
				return false
			end
			r_u8(r)
			local n = r_leb(r)
			if r.bad or r.at + n > r.e then
				return false
			end
			r.at += n
		end
		return true
	elseif kind == 17 or kind == 30 then
		r_leb(r)
		return not r.bad
	end
	return false
end

-- THE ID TABLE AND THE ROOT BODY'S FRAMING (docs/SPEC-TABLES.md §3): a form
-- byte this reader does not know REFUSES; damage is malformed. The count is
-- the unsigned u64 at the file's end; a high half past zero is huge and never
-- frames.
local function open(bytes: buffer, report: any): any
	local len = buffer.len(bytes)
	if len < 1 then
		report.malformed = true
		return nil
	end
	if buffer.readu8(bytes, 0) ~= 1 then
		report.refused += 1
		return nil
	end
	if len < 9 then
		report.malformed = true
		return nil
	end
	local lo = buffer.readu32(bytes, len - 8)
	local hi = buffer.readu32(bytes, len - 4)
	if hi ~= 0 or lo > (len - 9) // 8 then
		report.malformed = true
		return nil
	end
	local count = lo
	local endpos = len - 8 - count * 8
	local ids: { string } = table.create(count) :: { string }
	local seen: { [string]: boolean } = {}
	for i = 0, count - 1 do
		local id = buffer.readstring(bytes, endpos + i * 8, 8)
		if seen[id] then
			report.malformed = true
			return nil
		end
		seen[id] = true
		ids[i + 1] = id
	end
	return { table = ids, e = endpos }
end

-- the root body's reserved node-table field (docs/SPEC-TABLES.md §3.1); the
-- numbering is whole or it is nothing
local function node_table_scan(bytes: buffer, ids: { string }, endpos: number, report: any): any
	local w = new_reader(bytes, 1, endpos)
	local payload: buffer? = nil
	local present = false
	local bad = false
	while not bad do
		local ref = r_leb(w)
		if w.bad or ref == 0 or ref > #ids or w.at >= w.e then
			break
		end
		local id = ids[ref]
		local kind = r_u8(w)
		if id == NodeTableId then
			present = true
			if kind ~= 12 then
				bad = true
				break
			end
			local n = r_leb(w)
			if w.bad or w.at + n > w.e then
				bad = true
				break
			end
			local p = buffer.create(n)
			buffer.copy(p, 0, bytes, w.at, n)
			payload = p
			w.at += n
		elseif not skip(w, kind) then
			break
		end
	end
	if present and bad then
		report.malformed = true
		return nil
	end
	if not present or payload == nil then
		return {}
	end
	local pb = payload :: buffer
	local rec = new_reader(pb, 0, buffer.len(pb))
	local count = r_leb(rec)
	if rec.bad then
		report.malformed = true
		return nil
	end
	local raw = {}
	while rec.at < rec.e do
		local tref = r_leb(rec)
		if rec.bad or tref == 0 or tref > #ids then
			report.malformed = true
			return nil
		end
		local tlen = r_leb(rec)
		if rec.bad or rec.at + tlen > rec.e then
			report.malformed = true
			return nil
		end
		local body = buffer.create(tlen)
		buffer.copy(body, 0, pb, rec.at, tlen)
		raw[#raw + 1] = { tid = ids[tref], body = body }
		rec.at += tlen
	end
	if count ~= #raw then
		report.malformed = true
		return nil
	end
	return raw
end

-- THE LINK: each descriptor learns its fields by id, once (§3, §8.1).
local byIdCache: { [any]: { [string]: any } } = {}
local typeCache: { [any]: { [string]: any } } = {}

local function fields_by_id(desc: any): { [string]: any }
	local m = byIdCache[desc]
	if m == nil then
		local built: { [string]: any } = {}
		for _, f in desc.fields do
			built[f.id] = f
		end
		byIdCache[desc] = built
		return built
	end
	return m
end

local function place_types(desc: any): { [string]: any }
	local m = typeCache[desc]
	if m == nil then
		local built: { [string]: any } = {}
		if desc.place ~= nil then
			for _, name in desc.place.tables do
				local d = S.lookup(name)
				built[d.id] = d
			end
		end
		typeCache[desc] = built
		return built
	end
	return m
end

local _table: { string } = {}
local _pending: { any } = {}
local _nodes: { any } = {}
local _rootValue: any = nil
local _rootDesc: any = nil
local _report: any = nil
-- RETAIN-UNKNOWN's hook (docs/SPEC-TABLES.md §6.6; table_retain.moo): nil on
-- every plain read, so nothing below reads a byte or moves a counter
-- differently because retention is on. A retaining read sets it for the one
-- walk and clears it after.
local _retain: any = nil
local _numberingGood: boolean = true
-- the builder-path storage refusal (Go CountRefusal): a collection count past
-- the int32 cap discards the partial value and is NOT a malformed event
local _countRefused: boolean = false

-- A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (docs/SPEC-TABLES.md §3.3;
-- Go's NonFiniteRefusal): the walk goes on, so the framing verdict is the one
-- a finite wire gets, and the read answers the refusal at its end, naming the
-- first such field as <Owner>.<field>.
local function non_finite(report: any, named: any)
	report.nonFinite = (report.nonFinite or 0) + 1
	if report.nonFiniteField == nil then
		if named ~= nil and named.owner ~= nil then
			report.nonFiniteField = named.owner .. "." .. named.name
		else
			report.nonFiniteField = ""
		end
	end
end

L.nonFinite = non_finite

-- scalar_at reads one scalar of wire kind `kind` for the field (or element)
-- fd: the raw value, widened into fd's kind (§4), clamped to its declared
-- range (lo/hi, raw, in the kind's domain) and bits(N)'s ceiling, then held as
-- fd's storage (cls). nil is framing damage. named is the field a refusal
-- names.
local function scalar_at(r: any, fd: any, kind: number, report: any, named: any): any
	local width = V.width(kind)
	if width == 0 or r.at + width > r.e then
		report.malformed = true
		return nil
	end
	local b, at = r.b, r.at
	r.at = at + width
	if kind == 1 then
		-- a bool byte past 1 is malformed, and non-zero is still true (§3)
		local x = buffer.readu8(b, at)
		if x > 1 then
			report.malformed = true
		end
		return x ~= 0
	elseif kind == 10 then
		local bits = buffer.readu32(b, at)
		if not V.finite32(bits) then
			non_finite(report, named)
			return fd.def or 0
		end
		if fd.k == 11 then
			-- f32 WIDENED into f64 (§4): every finite float32 is exact, and a
			-- float64's declared range clamps nothing on this wire
			local lo, hi = V.f32to64(bits)
			return V.Uint64.new(lo, hi)
		end
		if fd.flo ~= nil then
			local lo, hi = V.f32to64(bits)
			if V.cmp64(lo, hi, fd.flo[1], fd.flo[2]) < 0 then
				report.clamped += 1
				return fd.flo32
			elseif V.cmp64(lo, hi, fd.fhi[1], fd.fhi[2]) > 0 then
				report.clamped += 1
				return fd.fhi32
			elseif bits == 0x80000000 then
				return 0 -- a -0 lands as +0, uncounted (§3.3)
			end
		end
		return bits
	elseif kind == 11 then
		local lo = buffer.readu32(b, at)
		local hi = buffer.readu32(b, at + 4)
		if not V.finite64(lo, hi) then
			non_finite(report, named)
			return fd.def or V.Uint64.new(0, 0)
		end
		return V.Uint64.new(lo, hi)
	end
	local x = V.readRaw(b, at, kind)
	local kw = fd.kw or width
	local signed = if fd.signed == nil then V.signedKind(kind) else fd.signed
	if width ~= kw then
		x = V.widen(x, kw, signed)
	end
	local lo, hi = fd.lo, fd.hi
	if lo ~= nil then
		if V.rawCmp(x, lo, kw, signed) < 0 then
			x = lo
			report.clamped += 1
		elseif V.rawCmp(x, hi, kw, signed) > 0 then
			x = hi
			report.clamped += 1
		end
	end
	if fd.bits ~= nil and fd.bits < kw * 8 then
		local cap = V.bitsCap(fd.bits, kw)
		if V.rawCmp(x, cap, kw, false) > 0 then
			x = cap
			report.clamped += 1
		end
	end
	if fd.cls == nil then
		return x
	end
	return V.toStorage(fd, x)
end

local read_body: any
local read_field: any
local read_element: any
local read_union: any

-- an enum reference (§3, §2.15): 0 is None, a reference past the id table is
-- damage, a retired variant reads the field's default, and a variant this
-- enum cannot name reads None and counts unknown. The value is the variant's
-- number.
local function enum_read(fd: any, r: any, report: any): (any, boolean)
	local ref = r_leb(r)
	if r.bad then
		report.malformed = true
		return nil, false
	end
	if ref == 0 then
		return 0, true
	end
	if ref > #_table then
		report.malformed = true
		return nil, false
	end
	local id = _table[ref]
	for i, v in fd.vs do
		if v[2] == id then
			if v[3] == true then
				return fd.def or 0, true
			end
			return i, true
		end
	end
	report.unknown += 1
	if _retain ~= nil then
		_retain.lost(report) -- an enum variant this build cannot name (§6.6)
	end
	return 0, true
end

-- a wide string or bytes value lands in its buffer with its used length
-- beside it (docs/PORT-MOO.md; SPEC §4.7)
local function store_buffer(v: any, slot: string, src: buffer, at: number, nbytes: number, length: number)
	local dst = v[slot]
	if type(dst) ~= "buffer" or buffer.len(dst) < nbytes then
		dst = buffer.create(nbytes)
		v[slot] = dst
	end
	buffer.fill(dst, 0, 0)
	buffer.copy(dst, 0, src, at, nbytes)
	v[slot .. "Length"] = length
end

-- the key value of one entry as the order rules compare it (§2.8): a string
-- key its moo string, an integer key its storage
local function key_value(keyfd: any, vals: any): any
	if keyfd == nil then
		return nil
	end
	return vals[keyfd.slot]
end

-- the ascending order of two keys: -1/0/1 (§2.8)
local function key_compare(keyfd: any, a: any, b: any): number
	if type(a) == "string" and type(b) == "string" then
		return if a < b then -1 elseif a > b then 1 else 0
	end
	if type(a) == "boolean" then
		return if a == b then 0 elseif a then 1 else -1
	end
	if type(a) == "number" and type(b) == "number" then
		return if a < b then -1 elseif a > b then 1 else 0
	end
	local C = if keyfd.cls == "I64" then V.Int64 elseif keyfd.cls == "U64" then V.Uint64 elseif keyfd.cls == "I128" then V.Int128 else V.Uint128
	return C.cmp(a, b)
end

-- KEYS NEVER CLAMP (§2.8): a string key past the declaration's bound drops
-- its whole entry and counts clamped; an integer key always fits.
local function key_fits(keyfd: any, key: any): boolean
	if keyfd == nil or type(key) ~= "string" then
		return true
	end
	local bound = keyfd.bound
	if bound == nil or bound == 0 then
		return true
	end
	return #key <= bound
end

-- THE KEY SCAN (§2.8; internal/tablewire mapKey): the entry's headers are
-- scanned for the key's id, the LAST occurrence winning. A string key is read
-- WHOLE, never clamped; an integer key lands at its declared range. Returns
-- the key, whether the LAST key kind is foreign, whether a key widened, and
-- whether the scan was whole.
local function scan_key(keyfd: any, body: any, entry: any): (any, boolean, boolean, boolean)
	local scan = new_reader(body.b, body.at, body.e)
	local keyvals: any = entry.new()
	local keyVal: any = key_value(keyfd, keyvals)
	local kindBad = false
	local entryWidened = false
	local quiet = { malformed = false, clamped = 0, nonFinite = 0 }
	while true do
		local ref = r_leb(scan)
		if scan.bad then
			return keyVal, kindBad, entryWidened, false
		end
		if ref == 0 then
			return keyVal, kindBad, entryWidened, scan.at == scan.e
		end
		if ref > #_table or scan.at >= scan.e then
			return keyVal, kindBad, entryWidened, false
		end
		local id = _table[ref]
		local kind = r_u8(scan)
		if id == MapKeyId and keyfd ~= nil then
			if kind ~= keyfd.k and V.widens(kind, keyfd.k) then
				entryWidened = true
				local x = scalar_at(scan, keyfd, kind, quiet, keyfd)
				if x == nil then
					return keyVal, kindBad, entryWidened, false
				end
				keyvals[keyfd.slot] = x
				keyVal = key_value(keyfd, keyvals)
			else
				kindBad = kind ~= keyfd.k
				if kindBad then
					if not skip(scan, kind) then
						return keyVal, kindBad, entryWidened, false
					end
				elseif keyfd.s == "str" or keyfd.s == "text" then
					local n = r_leb(scan)
					if scan.bad or scan.at + n > scan.e then
						return keyVal, kindBad, entryWidened, false
					end
					if not V.utf8(scan.b, scan.at, n) then
						return keyVal, kindBad, entryWidened, false
					end
					keyVal = buffer.readstring(scan.b, scan.at, n)
					scan.at += n
				else
					local x = scalar_at(scan, keyfd, keyfd.k, quiet, keyfd)
					if x == nil then
						return keyVal, kindBad, entryWidened, false
					end
					keyvals[keyfd.slot] = x
					keyVal = key_value(keyfd, keyvals)
				end
			end
		elseif not skip(scan, kind) then
			return keyVal, kindBad, entryWidened, false
		end
	end
end

local function read_map(fd: any, r: any, report: any, v: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then
		report.malformed = true
		return false
	end
	local endpos = r.at + n
	local landed: { any } = {}
	if n >= 2 then
		-- THE HEADER IS READ AS AN ARRAY'S IS (array_body): its count is one
		-- LEB128 bounded by the enclosing body, not by this field's L, as the
		-- reference's and the engine's map readers read it
		local head = new_reader(r.b, r.at, r.e)
		local ek = r_u8(head)
		local count = r_leb(head)
		local sub = new_reader(r.b, if head.at > endpos then endpos else head.at, endpos)
		if head.bad then
			report.malformed = true
			r.at = endpos
			return true
		end
		if ek ~= 13 then
			report.kindMismatch += 1
			r.at = endpos
			return true
		end
		if _retain ~= nil and v ~= nil and fd.slot ~= nil then
			-- THE READ COMMITS TO REPLACE THE MAP HERE (§6.6): every body the
			-- earlier occurrence built goes with it, moving neither counter
			_retain.supersede(v, fd.slot, fd)
		end
		-- THE STORAGE CAP (§2.9, §2.2; internal/tablewire collectionCap)
		if count > 0x7FFFFFFF then
			_countRefused = true
			report.malformed = true
			report.reason = "count_refusal"
			r.at = endpos
			return true
		end
		local entry = S.lookup(fd.t)
		local entryFields = fields_by_id(entry)
		local keyfd = entryFields[MapKeyId]
		local widened = false
		local lastKey: any = nil
		local hasLast = false
		for _ = 1, count do
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then
				report.malformed = true
				break
			end
			local body = new_reader(sub.b, sub.at, sub.at + elemLen)
			sub.at += elemLen
			-- THE KEY BEFORE THE SLOT (§2.8): scan it before the entry body is
			-- counted, exactly as mapField does
			local keyVal, kindBad, entryWidened, keyOk = scan_key(keyfd, body, entry)
			if entryWidened and not widened then
				widened = true
				report.widened += 1
			end
			if kindBad then
				report.kindMismatch += 1
				landed = {}
				break
			end
			if not keyOk then
				report.malformed = true
				break
			end
			if not key_fits(keyfd, keyVal) then
				report.clamped += 1
			else
				local order = -1
				if hasLast then
					order = key_compare(keyfd, lastKey, keyVal)
				end
				if order > 0 then
					report.malformed = true
					break
				end
				-- the entry's body is read WHOLE now the key has chosen its
				-- place; a duplicate replaces the entry it repeats, whole
				local vals: any = entry.new()
				while true do
					local ref = r_leb(body)
					if body.bad then
						report.malformed = true
						break
					end
					if ref == 0 then
						break
					end
					if ref > #_table then
						report.malformed = true
						break
					end
					local id = _table[ref]
					local kind = r_u8(body)
					-- THE RESERVED IDS IN A NESTED BODY ARE MALFORMED (§3.1)
					if id == BuildVersionId or id == MessageVocabularyId or id == NodeTableId then
						report.malformed = true
						break
					end
					local efd = entryFields[id]
					if efd == nil then
						report.unknown += 1
						local from = body.at
						if not skip(body, kind) then
							report.malformed = true
							break
						end
						if _retain ~= nil then
							_retain.capture(vals, entry, id, kind, body.b, from, body.at, _table, report)
						end
					elseif id == MapKeyId and keyfd ~= nil then
						if kind ~= keyfd.k then
							if V.widens(kind, keyfd.k) then
								local x = scalar_at(body, keyfd, kind, report, keyfd)
								if x == nil then
									break
								end
								vals[keyfd.slot] = x
							else
								report.kindMismatch += 1
								if not skip(body, kind) then
									report.malformed = true
									break
								end
							end
						elseif not read_field(keyfd, body, vals) then
							break
						end
					elseif kind ~= efd.k then
						if V.widens(kind, efd.k) then
							local x = scalar_at(body, efd, kind, report, efd)
							if x == nil then
								break
							end
							if efd.slot ~= nil then
								vals[efd.slot] = x
							end
							report.widened += 1
						else
							report.kindMismatch += 1
							if not skip(body, kind) then
								report.malformed = true
								break
							end
						end
					elseif not read_field(efd, body, vals) then
						break
					end
				end
				if _retain ~= nil and v ~= nil and fd.slot ~= nil then
					_retain.builtMap(v, fd.slot, entry, vals)
				end
				if order == 0 then
					report.duplicate += 1
					if _retain ~= nil then
						_retain.forgetBody(entry, landed[#landed]) -- last wins WHOLE (§2.8, §6.6)
					end
					landed[#landed] = vals
				else
					landed[#landed + 1] = vals
					lastKey = key_value(keyfd, vals)
					hasLast = true
				end
			end
		end
	end
	if v ~= nil and fd.slot ~= nil then
		v[fd.slot] = landed
	end
	r.at = endpos
	return true
end

-- AN ENUM-KEYED ARRAY (§2.4, §3.2): its own kind 16, each element a key
-- reference and then its framed payload. Key variant k lands at index k.
local function read_keyed(fd: any, r: any, report: any, v: any): boolean
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then
		report.malformed = true
		return false
	end
	local endpos = r.at + n
	local dst: any = nil
	if v ~= nil and fd.slot ~= nil then
		dst = v[fd.slot]
	end
	if n >= 2 then
		-- the header's count is bounded by the enclosing body, as an
		-- array's is (array_body) and as the reference's keyed reader reads it
		local head = new_reader(r.b, r.at, r.e)
		local ek = r_u8(head)
		local count = r_leb(head)
		local sub = new_reader(r.b, if head.at > endpos then endpos else head.at, endpos)
		if head.bad then
			report.malformed = true
			r.at = endpos
			return true
		end
		if ek ~= fd.ek then
			if not V.widens(ek, fd.ek) then
				report.kindMismatch += 1
				r.at = endpos
				return true
			end
			report.widened += 1
		end
		local e = fd.e
		for _ = 1, count do
			local key = r_leb(sub)
			if sub.bad then
				report.malformed = true
				break
			end
			if key == 0 or key > #_table then
				report.malformed = true
				break
			end
			local at = 0
			if fd.keyvs ~= nil then
				local keyID = _table[key]
				for i, kv in fd.keyvs do
					if kv[2] == keyID then
						at = i
						break
					end
				end
			end
			local elemLen = r_leb(sub)
			if sub.bad or sub.at + elemLen > sub.e then
				report.malformed = true
				break
			end
			if at == 0 then
				report.unknown += 1
				if _retain ~= nil then
					_retain.lost(report) -- a keyed slot this build cannot name (§6.6)
				end
				sub.at += elemLen
				continue
			end
			local payload = new_reader(sub.b, sub.at, sub.at + elemLen)
			sub.at += elemLen
			local x: any = nil
			if e.s == "tab" then
				local d = S.lookup(e.t)
				local inst = d.new()
				if _retain ~= nil and dst ~= nil and type(dst) == "table" then
					_retain.forgetBody(d, dst[at]) -- the slot written again (§3.2, §6.6)
				end
				read_body(d, payload, inst, true)
				if payload.at ~= payload.e then
					-- A SLOT WHOSE BODY DID NOT SPEND ITS L RESETS TO ITS DECLARED
					-- DEFAULTS (§3.2, §5)
					report.malformed = true
					if _retain ~= nil then
						_retain.refill(d, inst) -- damage is not a later occurrence (§6.6)
					else
						inst = d.new()
					end
				end
				x = inst
			elseif e.s == "enum" then
				x = enum_read(e, payload, report)
			else
				x = scalar_at(payload, e, ek, report, fd)
			end
			if dst ~= nil and x ~= nil then
				if type(dst) == "buffer" then
					buffer.writeu8(dst, at - 1, x)
				else
					dst[at] = x
				end
			end
		end
	end
	r.at = endpos
	return true
end

-- the bytes one text-list element contributes to its total (§2.13): a plain
-- element's byte length, or an optional element's value when present
local function text_elem_bytes(e: any, x: any): number
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
		local vf = S.lookup(e.t).fields[1]
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

-- the body after its L: the element kind, the count, the elements (§2.9,
-- §3). It answers whether the body SELECTED this array, the engine's
-- `selected` (internal/tablewire decode.go, arrayBody): false for a foreign
-- element kind, a damaged count and a count past the list cap.
local function array_body(fd: any, sub: any, report: any, v: any, headerEnd: number?): boolean
	local head = new_reader(sub.b, sub.at, headerEnd or sub.e)
	local ek = r_u8(head)
	local count = r_leb(head)
	sub.at = if head.at > sub.e then sub.e else head.at
	if head.bad then
		report.malformed = true
		if fd.present ~= nil and v ~= nil then
			v[fd.present] = true
		end
		return false
	end
	if ek ~= fd.ek then
		if not V.widens(ek, fd.ek) then
			report.kindMismatch += 1
			return false
		end
		report.widened += 1
	end
	if _retain ~= nil and v ~= nil and fd.slot ~= nil and fd.slot ~= "" then
		-- THE READ COMMITS TO REPLACE HERE (§6.6): the records under this
		-- field go with the value it is about to lose
		_retain.supersede(v, fd.slot, fd)
	end
	local keep = count
	if fd.array == "list" then
		if count > 0x7FFFFFFF then
			_countRefused = true
			report.malformed = true
			report.reason = "count_refusal"
			return false
		end
	elseif fd.bound ~= nil and count > fd.bound then
		keep = fd.bound
		report.clamped += 1
	end
	local slot = fd.slot
	local holder: any = nil
	if v ~= nil and slot ~= nil and slot ~= "" then
		holder = v
	end
	if fd.s == "bytes" then
		-- bytes(N) rides as an array of u8 (§2.5)
		local have = keep
		if sub.at + have > sub.e then
			have = sub.e - sub.at
			report.malformed = true
		end
		if holder ~= nil then
			store_buffer(holder, slot, sub.b, sub.at, have, have)
		end
		sub.at += have
		return true
	end
	local e = fd.e
	local dst: any
	if holder == nil then
		dst = {}
	elseif fd.array == "list" then
		dst = {}
		holder[slot] = dst
		if holder[slot .. "Length"] ~= nil then
			holder[slot .. "Length"] = {}
		end
	else
		dst = holder[slot]
		if dst == nil then
			dst = {}
			holder[slot] = dst
		end
	end
	local lens: any = nil
	if holder ~= nil then
		lens = holder[slot .. "Length"]
	end
	local got = 0
	local spent = 0
	for i = 1, keep do
		if sub.at >= sub.e then
			report.malformed = true
			break
		end
		if not read_element(fd, sub, e, dst, i, report, ek, lens) then
			break
		end
		-- A TEXT LIST'S TOTAL (§2.13): the first element that would carry the
		-- running sum past the total is dropped with every later element,
		-- clamped counted once
		if fd.total ~= nil then
			spent += text_elem_bytes(e, dst[i])
			if spent > fd.total then
				report.clamped += 1
				if fd.array == "list" then
					dst[i] = nil
				end
				got = i - 1
				break
			end
		end
		got = i
	end
	if fd.array == "counted" and holder ~= nil then
		holder[slot .. "Count"] = got
	end
	return true
end

-- AN ARRAY FIELD (§2.9, §3): the L, then the body, which bounds the elements;
-- the header is read against the PARENT buffer (§2.9).
local function read_array(fd: any, r: any, report: any, v: any): (boolean, boolean)
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then
		report.malformed = true
		return false, false
	end
	local endpos = r.at + n
	local selected = true
	if n >= 2 then
		selected = array_body(fd, new_reader(r.b, r.at, endpos), report, v, r.e)
	end
	r.at = endpos
	return true, selected
end

read_element = function(fd: any, r: any, e: any, dst: any, i: number, report: any, ek: number, lens: any): boolean
	local s = e.s
	if s == "ptr" or s == "sblob" or s == "bblob" then
		local index = r_leb(r)
		if r.bad then
			report.malformed = true
			return false
		end
		-- a null in a list is false, so the list is never cut short; a fixed
		-- or counted array stays nil-filled
		local null: any = nil
		if fd.array == "list" then
			null = false
		end
		dst[i] = null
		_pending[#_pending + 1] = { holder = dst, key = i, index = index, d = e, null = null }
		return true
	elseif s == "enum" then
		local x, ok = enum_read(e, r, report)
		if not ok then
			return false
		end
		dst[i] = x
		return true
	elseif s == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then
			report.malformed = true
			return false
		end
		local sub = new_reader(r.b, r.at, r.at + n)
		local d = S.lookup(e.t)
		local inst = d.new()
		read_body(d, sub, inst, true)
		if sub.at ~= sub.e then
			report.malformed = true
			if _retain ~= nil then
				_retain.refill(d, inst) -- damage is not a later occurrence (§6.6)
			else
				inst = d.new()
			end
		end
		r.at += n
		dst[i] = inst
		return true
	elseif s == "union" then
		-- THE ELEMENT IS RESET THE MOMENT IT IS REACHED (§3, §4); a list's
		-- failed element stays absent
		if fd.array ~= "list" then
			dst[i] = S.lookup(e.t).new()
		end
		local x, ok = read_union(e, r, report, dst, i)
		if not ok then
			return false
		end
		dst[i] = x
		return true
	elseif s == "str" or s == "text" then
		-- a text element is kind 12 (33 wide): its own L, then L bytes (§2.13, §3)
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then
			report.malformed = true
			return false
		end
		if e.wide then
			if n % 2 ~= 0 or not V.wide(r.b, r.at, n) then
				report.malformed = true
				r.at += n
				return true
			end
			local units = n // 2
			if e.bound ~= nil and units > e.bound then
				units = V.clampWide(r.b, r.at, e.bound)
				report.clamped += 1
			end
			local b = dst[i]
			if type(b) ~= "buffer" or buffer.len(b) < units * 2 then
				b = buffer.create(units * 2)
				dst[i] = b
			end
			buffer.fill(b, 0, 0)
			buffer.copy(b, 0, r.b, r.at, units * 2)
			if lens ~= nil then
				lens[i] = units
			end
			r.at += n
			return true
		end
		if not V.utf8(r.b, r.at, n) then
			-- the element does not land: the list keeps the prefix (§2.13, §3)
			report.malformed = true
			r.at += n
			return false
		end
		local keep = n
		if s == "str" and e.bound ~= nil and e.bound > 0 and keep > e.bound then
			keep = V.clampUtf8(r.b, r.at, e.bound)
			report.clamped += 1
		end
		dst[i] = buffer.readstring(r.b, r.at, keep)
		r.at += n
		return true
	end
	local x = scalar_at(r, e, ek, report, fd)
	if x == nil then
		return false
	end
	if type(dst) == "buffer" then
		buffer.writeu8(dst, i - 1, x)
	else
		dst[i] = x
	end
	return true
end

-- AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6, §3; docs/PORTING.md M18):
-- an arm's payload is exactly the bytes a field of its type puts after its own
-- framing prefix, so the arm is read by read_field, the field's own reader,
-- with the arm's L standing in for the field's length. What this function
-- adds is FRAMING ALONE, never a payload reader of its own:
--
--   - a length-shaped arm (a string, text, bytes, an array, a table) is read
--     by read_field from the arm's L, over a reader that ends where the arm's
--     payload ends, so its L is the field's L and every rule after it is the
--     field's;
--   - a fixed-width arm's L must be its kind's width, and a reference-shaped
--     arm's L must be exactly the reference it frames: either is the arm's own
--     framing damage, the union reading None and the parent reading on past L
--     (internal/tablewire decode.go, arm);
--   - a pointer arm's framing is checked BEFORE its reference is registered,
--     because the engine checks the frame before it resolves the cell, so a
--     damaged arm cannot produce a pointee-kind mismatch.
--
-- lpos is where the arm's L begins and sub the reader over its payload. The
-- answer is true when the arm's value is whole: read_field's third answer says
-- a payload was damaged inside its own L, and its second that an array carried
-- a foreign element kind; either reads None, and the slot keeps nothing.
local function read_arm(arm: any, r: any, lpos: number, sub: any, u: any, holder: any, key: any): boolean
	local s = arm.s
	if s == "bool" or s == "int" or s == "float" then
		local w = V.width(arm.k)
		if w > 0 and sub.e - sub.at ~= w then
			_report.malformed = true
			return false
		end
		return (read_field(arm, sub, u))
	end
	if s == "ptr" or s == "sblob" or s == "bblob" or s == "enum" then
		if s ~= "enum" then
			local framing = new_reader(sub.b, sub.at, sub.e)
			r_leb(framing)
			if framing.bad or framing.at ~= framing.e then
				_report.malformed = true
				return false
			end
		end
		local ok = read_field(arm, sub, u)
		if ok and sub.at ~= sub.e then
			_report.malformed = true
			return false
		end
		return ok
	end
	local ok, selected, intact = read_field(arm, new_reader(r.b, lpos, sub.e), u)
	if s == "tab" and _retain ~= nil and holder ~= nil and u[arm.slot] ~= nil then
		-- the occurrence's own body, whatever becomes of it (§6.6)
		_retain.builtArm(holder, key, S.lookup(arm.t), u[arm.slot])
	end
	if ok and selected ~= false and intact ~= false then
		return true
	end
	u[arm.slot] = nil
	return false
end

-- AN ARM HEADER IS A FIELD HEADER (§3, §2.6): the arm id reference, the
-- arm's kind byte, L, then L bytes of payload. An arm this reader cannot name
-- is unknown and the union reads None; a retyped arm is a kind mismatch.
read_union = function(fd: any, r: any, report: any, holder: any, key: any): (any, boolean)
	local un = S.lookup(fd.t)
	local u = un.new()
	local ref = r_leb(r)
	if r.bad then
		report.malformed = true
		return u, false
	end
	if ref == 0 then
		return u, true
	end
	if ref > #_table then
		report.malformed = true
		return u, false
	end
	local armID = _table[ref]
	local kind = r_u8(r)
	local lpos = r.at
	local n = r_leb(r)
	if r.bad or r.at + n > r.e then
		report.malformed = true
		return u, false
	end
	local sub = new_reader(r.b, r.at, r.at + n)
	local arm: any = nil
	for _, a in un.arms do
		if a.id == armID then
			arm = a
			break
		end
	end
	local want = if arm == nil then 0 elseif arm.s == "void" then 32 else arm.k
	if arm == nil then
		report.unknown += 1
		if _retain ~= nil then
			_retain.lost(report) -- a union arm this build cannot name (§6.6)
		end
	elseif kind ~= want then
		if V.widens(kind, want) then
			if n == V.width(kind) then
				local x = scalar_at(sub, arm, kind, report, arm)
				if x ~= nil then
					report.widened += 1
					u[arm.slot] = x
					u.Type = arm.tag
				end
			else
				report.malformed = true
			end
		else
			report.kindMismatch += 1
		end
	elseif arm.s == "void" then
		if n ~= 0 then
			report.malformed = true
		else
			u.Type = arm.tag
		end
	elseif read_arm(arm, r, lpos, sub, u, holder, key) then
		u.Type = arm.tag
	end
	r.at += n
	return u, true
end

-- ONE FIELD (§3): every value read is stored in its slot in the shape
-- docs/PORT-MOO.md names; a string clamps at a whole code point and a wide
-- string at a whole code unit.
-- It answers (ok, selected, intact): ok false stops the body; selected false
-- is an array of a FOREIGN ELEMENT KIND (§3); intact false is a payload
-- damaged inside its own L, which a field answers with its declared default
-- and a union arm (read_arm) with None.
read_field = function(fd: any, r: any, v: any): ...any
	local s = fd.s
	if s == "bool" or s == "int" or s == "float" then
		local x = scalar_at(r, fd, fd.k, _report, fd)
		if x == nil then
			return false
		end
		if fd.slot ~= nil and v ~= nil then
			v[fd.slot] = x
		end
		return true
	elseif s == "str" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then
			_report.malformed = true
			return false
		end
		if fd.wide then
			if n % 2 ~= 0 or not V.wide(r.b, r.at, n) then
				_report.malformed = true
				r.at += n
				return true, nil, false
			end
			local units = n // 2
			if fd.bound ~= nil and units > fd.bound then
				units = V.clampWide(r.b, r.at, fd.bound)
				_report.clamped += 1
			end
			if fd.slot ~= nil and v ~= nil then
				store_buffer(v, fd.slot, r.b, r.at, units * 2, units)
			end
			r.at += n
			return true
		end
		if not V.utf8(r.b, r.at, n) then
			_report.malformed = true
			r.at += n
			return true, nil, false
		end
		local keep = n
		if fd.bound ~= nil and n > fd.bound then
			keep = V.clampUtf8(r.b, r.at, fd.bound)
			_report.clamped += 1
		end
		if fd.slot ~= nil and v ~= nil then
			v[fd.slot] = buffer.readstring(r.b, r.at, keep)
		end
		r.at += n
		return true
	elseif s == "text" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then
			_report.malformed = true
			return false
		end
		if not V.utf8(r.b, r.at, n) then
			_report.malformed = true
			r.at += n
			return true, nil, false
		end
		if fd.slot ~= nil and v ~= nil then
			v[fd.slot] = buffer.readstring(r.b, r.at, n)
		end
		r.at += n
		return true
	elseif s == "enum" then
		local x, ok = enum_read(fd, r, _report)
		if not ok then
			return false
		end
		if fd.slot ~= nil and v ~= nil then
			v[fd.slot] = x
		end
		return true
	elseif s == "ptr" or s == "sblob" or s == "bblob" then
		local index = r_leb(r)
		if r.bad then
			_report.malformed = true
			return false
		end
		_pending[#_pending + 1] = { holder = v, key = fd.slot, index = index, d = fd }
		return true
	elseif s == "tab" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then
			_report.malformed = true
			return false
		end
		local sub = new_reader(r.b, r.at, r.at + n)
		local d = S.lookup(fd.t)
		local inst = d.new()
		if fd.slot ~= nil and v ~= nil then
			if _retain ~= nil then
				-- a second occurrence replaces the body whole: what the first
				-- retained goes with it, moving neither counter (§6.6)
				_retain.forgetBody(d, v[fd.slot])
			end
			v[fd.slot] = inst
		end
		read_body(d, sub, inst, true)
		if sub.at ~= sub.e then
			-- A NESTED BODY THAT DID NOT SPEND ITS L FALLS BACK ON ITS DECLARED
			-- DEFAULTS (§3, §5)
			_report.malformed = true
			if _retain ~= nil then
				_retain.refill(d, inst) -- damage is not a later occurrence (§6.6)
			else
				inst = d.new()
			end
			if fd.slot ~= nil and v ~= nil then
				v[fd.slot] = inst
			end
			r.at += n
			return true, nil, false
		end
		r.at += n
		return true
	elseif s == "union" then
		if _retain ~= nil and fd.slot ~= nil and v ~= nil then
			-- the union written again, the same arm or another (§6.6)
			_retain.supersede(v, fd.slot, fd)
		end
		local x, ok = read_union(fd, r, _report, v, fd.slot)
		if not ok then
			return false
		end
		if fd.slot ~= nil and v ~= nil then
			v[fd.slot] = x
		end
		return true
	elseif s == "bytes" or s == "arr" then
		if s == "arr" and fd.array == "keyed" then
			return read_keyed(fd, r, _report, v)
		end
		return read_array(fd, r, _report, v)
	elseif s == "map" then
		return read_map(fd, r, _report, v)
	end
	_report.malformed = true
	return false
end

-- RESOLUTION (§3.1): 0 is null, 1 is the root, k + 1 is record k. A record of
-- another kind than the slot declares is a kind mismatch, an index past the
-- numbering is malformed, and a numbering that could not be read resolves
-- nothing. A blob node is owned by one slot: every later slot naming it counts
-- blob_shared (§2.5).
local function resolve(report: any)
	if not _numberingGood then
		return
	end
	local landed: { [any]: boolean } = {}
	for _, p in _pending do
		local value: any = nil
		if p.index ~= 0 then
			if p.index == 1 then
				if p.d.s == "ptr" and p.d.t == _rootDesc.name then
					value = _rootValue
				else
					report.kindMismatch += 1
				end
			elseif p.index - 2 >= #_nodes then
				report.malformed = true
			else
				local n = _nodes[p.index - 1]
				if n ~= nil and n.kind ~= "x" then
					if p.d.s == "sblob" then
						if n.kind == "s" then
							if landed[n] then
								report.blobShared = (report.blobShared or 0) + 1
							end
							landed[n] = true
							value = n.blob
						else
							report.kindMismatch += 1
						end
					elseif p.d.s == "bblob" then
						if n.kind == "b" then
							if landed[n] then
								report.blobShared = (report.blobShared or 0) + 1
							end
							landed[n] = true
							value = n.blob
						else
							report.kindMismatch += 1
						end
					elseif n.kind == "t" and n.desc.name == p.d.t then
						value = n.v
					else
						report.kindMismatch += 1
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

-- a field's guard holds when every (bool slot, value) pair matches (§2.3, §3)
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

read_body = function(desc: any, r: any, v: any, nested: boolean): boolean
	local byId = fields_by_id(desc)
	local seen: { [string]: boolean } = {}
	while true do
		local ref = r_leb(r)
		if r.bad then
			_report.malformed = true
			return false
		end
		if ref == 0 then
			-- `| required` (§2.3): a body that ends without a required field
			-- whose guard holds is missing_required, naming the field
			local missing = {}
			for _, fd in desc.fields do
				if fd.required == true and not seen[fd.id] and guard_holds(fd, v) then
					missing[#missing + 1] = fd.name
				end
			end
			if #missing > 0 then
				_report.missingRequired = (_report.missingRequired or 0) + 1
				if _report.missingName == nil then
					_report.missingName = table.concat(missing, ", ")
				end
				return false
			end
			return true
		end
		if ref > #_table or r.at >= r.e then
			_report.malformed = true
			return false
		end
		local kind = r_u8(r)
		local id = _table[ref]
		if id == BuildVersionId or id == MessageVocabularyId then
			_report.malformed = true
			return false
		end
		local handled = false
		if id == NodeTableId then
			if nested then
				_report.malformed = true
				return false
			end
			if desc.variable then
				if not skip(r, kind) then
					_report.malformed = true
					return false
				end
				handled = true
			end
		end
		if not handled then
			local fd = byId[id]
			if fd == nil then
				_report.unknown += 1
				local from = r.at
				if not skip(r, kind) then
					_report.malformed = true
					return false
				end
				if _retain ~= nil then
					_retain.capture(v, desc, id, kind, r.b, from, r.at, _table, _report)
				end
			elseif kind ~= fd.k then
				if V.widens(kind, fd.k) then
					local x = scalar_at(r, fd, kind, _report, fd)
					if x == nil then
						return false
					end
					if fd.slot ~= nil and v ~= nil then
						v[fd.slot] = x
					end
					if fd.present ~= nil and v ~= nil then
						v[fd.present] = true
					end
					seen[id] = true
					_report.widened += 1
				else
					_report.kindMismatch += 1
					if not skip(r, kind) then
						_report.malformed = true
						return false
					end
				end
			else
				local ok, selected = read_field(fd, r, v)
				if not ok then
					return false
				end
				seen[id] = true
				-- ?T: a field that rode is present (§2.3), except an optional
				-- array whose body carries a FOREIGN ELEMENT KIND
				if fd.present ~= nil and v ~= nil and selected ~= false then
					v[fd.present] = true
				end
			end
		end
	end
end

-- a report at rest (docs/SPEC-TABLES.md §4)
local function reset(report: any)
	report.malformed = false
	report.retained = 0
	report.retainLost = 0
	report.refused = 0
	report.unknown = 0
	report.kindMismatch = 0
	report.clamped = 0
	report.widened = 0
	report.duplicate = 0
	report.missingRequired = 0
	report.nonFinite = 0
	report.blobShared = 0
	report.missingName = nil
	report.nonFiniteField = nil
	report.reason = nil
	report.field = nil
	report.unknownField = nil
	report.duplicateField = nil
end

-- loadTolerant loads a form-1 wire at the descriptor named root, fills report
-- and returns the root value only if the wire is whole. Tolerant evolution
-- is not damage (§4): malformed input returns nil, as on every read form.
-- A non-finite float also refuses it whole (§3.3):
-- nil, with report.reason "non_finite_float: <Owner>.<field>".
function L.loadTolerant(root: string, bytes: buffer, report: any): any
	if report.missingRequired == nil then
		report.missingRequired = 0
	end
	if report.blobShared == nil then
		report.blobShared = 0
	end
	if report.retained == nil then
		report.retained = 0
	end
	if report.retainLost == nil then
		report.retainLost = 0
	end
	local prior = report.nonFiniteField
	report.nonFiniteField = nil
	report.nonFinite = 0
	local o = open(bytes, report)
	if o == nil then
		return nil
	end
	_table = o.table
	_report = report
	_countRefused = false
	_pending = {}
	_nodes = {}
	local desc = S.lookup(root)
	_rootDesc = desc
	local v = desc.new()
	_rootValue = v
	-- ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST ENTRY IS
	-- MALFORMED (§3): the engine reads nothing when bytes follow it
	local walk = new_reader(bytes, 1, o.e)
	while true do
		local ref = r_leb(walk)
		if walk.bad then
			break
		end
		if ref == 0 then
			if walk.at ~= o.e then
				report.malformed = true
				return nil
			end
			break
		end
		if ref > #_table or walk.at >= walk.e then
			break
		end
		local kind = r_u8(walk)
		if not skip(walk, kind) then
			break
		end
	end
	local raw: any = {}
	if desc.variable then
		raw = node_table_scan(bytes, o.table, o.e, report)
	end
	-- §3.1: a node table that failed to read whole is nothing
	_numberingGood = raw ~= nil
	if raw ~= nil then
		local place = place_types(desc)
		for _, rec in raw do
			local tid = rec.tid
			if tid == StringBlobId and desc.place.str then
				if not V.utf8(rec.body, 0, buffer.len(rec.body)) then
					-- §3.1: a refused record KEEPS ITS INDEX as a placeholder
					report.malformed = true
					_nodes[#_nodes + 1] = { kind = "x" }
				else
					_nodes[#_nodes + 1] = { kind = "s", blob = rec.body }
				end
			elseif tid == BytesBlobId and desc.place.bytes then
				_nodes[#_nodes + 1] = { kind = "b", blob = rec.body }
			else
				local nc = place[tid]
				if nc == nil then
					report.unknown += 1
					if _retain ~= nil then
						_retain.lost(report) -- a node record of a type this build cannot place (§6.6)
					end
					_nodes[#_nodes + 1] = { kind = "x" }
				else
					_nodes[#_nodes + 1] = { kind = "t", desc = nc, v = nc.new(), body = rec.body }
				end
			end
		end
		for _, n in _nodes do
			if n.kind == "t" then
				read_body(n.desc, new_reader(n.body, 0, buffer.len(n.body)), n.v, true)
			end
		end
	end
	read_body(desc, new_reader(bytes, 1, o.e), v, false)
	resolve(report)
	if report.nonFiniteField ~= nil then
		report.reason = "non_finite_float: " .. report.nonFiniteField
		return nil
	end
	report.nonFiniteField = prior
	if report.malformed then
		return nil
	end
	return v
end

-- the counters that refuse, in TableReport.Err()'s order (§6.5), each with
-- its §11 name
local REFUSING = { "missingRequired", "malformed", "blobShared", "clamped", "kindMismatch" }
local NAMES: { [string]: string } = {
	missingRequired = "missing_required",
	malformed = "malformed",
	blobShared = "blob_shared",
	clamped = "clamped",
	kindMismatch = "kind_mismatch",
}

local function fresh(): any
	local r = {}
	reset(r)
	return r
end

local function moved(report: any, k: string): boolean
	if k == "malformed" then
		return report.malformed == true
	end
	return (report[k] or 0) > 0
end

-- §2.14's order: the trailer is checked before any field is decoded. A file
-- too short to hold one, or whose last four bytes are not the CRC-32C of the
-- bytes before them, is integrity_mismatch; otherwise the trailer comes off.
local function integrityOpen(b: buffer): buffer?
	local n = buffer.len(b)
	if n < 5 or buffer.readu32(b, n - 4) ~= V.crc32c(b, n - 4) then
		return nil
	end
	local out = buffer.create(n - 4)
	buffer.copy(out, 0, b, 0, n - 4)
	return out
end

L.integrityOpen = integrityOpen

-- the end of one payload of `kind` at `at`, or nil when it does not fit
local function stepOver(b: buffer, at: number, e: number, kind: number): number?
	local r = new_reader(b, at, e)
	if not skip(r, kind) then
		return nil
	end
	return r.at
end

-- one body's field headers in wire order, each { s, e, ps, id, kind }, and
-- where its terminator sits; nil when the framing does not walk to one
local function walkBody(b: buffer, at: number, e: number, ids: { string }): any
	local fields = {}
	local p = at
	while true do
		local ref, q = V.leb(b, p, e)
		if ref == nil then
			return nil
		end
		if ref == 0 then
			return { fields = fields, term = p }
		end
		if ref > #ids or q >= e then
			return nil
		end
		local kind = buffer.readu8(b, q)
		local stop = stepOver(b, q + 1, e, kind)
		if stop == nil then
			return nil
		end
		fields[#fields + 1] = { s = p, e = stop, ps = q + 1, id = ids[ref], kind = kind }
		p = stop
	end
end

-- the id table and the root body's span, as open reads them; nil when the
-- count does not frame
local function frame(b: buffer): any
	local len = buffer.len(b)
	if len < 9 then
		return nil
	end
	local lo = buffer.readu32(b, len - 8)
	local hi = buffer.readu32(b, len - 4)
	if hi ~= 0 or lo > (len - 9) // 8 then
		return nil
	end
	local endpos = len - 8 - lo * 8
	local ids: { string } = table.create(lo) :: { string }
	for i = 0, lo - 1 do
		ids[i + 1] = buffer.readstring(b, endpos + i * 8, 8)
	end
	return { ids = ids, e = endpos, tail = buffer.readstring(b, endpos, len - endpos) }
end

local function bytesOf(b: buffer, s: number, e: number): string
	return buffer.readstring(b, s, e - s)
end

local function fieldById(fields: { any }, id: string): any
	for _, f in fields do
		if f.id == id then
			return f
		end
	end
	return nil
end

-- THE WITNESS: the first field the walk saw move a counter (§11, a refusal
-- names the field). The file is rebuilt with only a PREFIX of a body's fields
-- (the id table unchanged) and loaded again into a fresh report; the first
-- prefix whose last field moves the counter names that field, descending
-- into an inline table and into a union's selected arm. An id the reader
-- cannot name is spelled #<hex>; a counter no field holds names none.
local function witness(root: string, b: buffer, k: string): string?
	local fr = frame(b)
	if fr == nil then
		return nil
	end
	local ids = fr.ids
	local desc = S.lookup(root)
	local top = walkBody(b, 1, fr.e, ids)
	if top == nil then
		return nil
	end

	local function probe(wire: string): boolean
		local rep = fresh()
		L.loadTolerant(root, buffer.fromstring(wire), rep)
		return moved(rep, k)
	end

	local refine: (fd: any, seg: any, rebuild: (string) -> string) -> string?
	local function isolate(fields: { any }, f: any, rebuild: (string) -> string): string?
		for i = 1, #f.fields do
			local parts = {}
			for j = 1, i do
				local s = f.fields[j]
				parts[j] = bytesOf(b, s.s, s.e)
			end
			if probe(rebuild(table.concat(parts) .. "\0")) then
				local seg = f.fields[i]
				local fd = fieldById(fields, seg.id)
				if fd == nil then
					return V.idHex(seg.id)
				end
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
			local n, bs = V.leb(b, seg.ps, seg.e)
			if n == nil then
				return fd.name
			end
			local inner = walkBody(b, bs, bs + n, ids)
			if inner == nil then
				return fd.name
			end
			local head = bytesOf(b, seg.s, seg.ps)
			return isolate(S.lookup(fd.t).fields, inner, function(body: string): string
				return rebuild(head .. V.lebString(#body) .. body)
			end) or fd.name
		elseif fd.s == "union" and seg.kind == 15 and fd.t ~= nil then
			local ref = V.leb(b, seg.ps, seg.e)
			if ref == nil or ref == 0 or ref > #ids then
				return fd.name
			end
			local arm = fieldById(S.lookup(fd.t).arms, ids[ref])
			if arm == nil then
				return V.idHex(ids[ref])
			end
			return arm.name
		end
		return fd.name
	end

	local function wireOf(body: string): string
		return "\1" .. body .. fr.tail
	end

	-- the node records (§3.1), read before the root body: the node table
	-- field is kept and every other root field dropped, and records past the
	-- one under test are emptied in place, so every pointer still resolves
	local nodeSeg: any = nil
	for _, s in top.fields do
		if s.id == NodeTableId then
			nodeSeg = s
		end
	end
	if desc.variable and nodeSeg ~= nil and nodeSeg.kind == 12 then
		local n, ps = V.leb(b, nodeSeg.ps, nodeSeg.e)
		local recs: { any } = {}
		local ok = n ~= nil
		local count = 0
		if n ~= nil then
			local stop = ps + n
			local c, p = V.leb(b, ps, stop)
			ok = c ~= nil
			count = c or 0
			while ok and p < stop do
				local tref, q = V.leb(b, p, stop)
				if tref == nil or tref == 0 or tref > #ids then
					ok = false
					break
				end
				local tlen, r = V.leb(b, q, stop)
				if tlen == nil or r + tlen > stop then
					ok = false
					break
				end
				recs[#recs + 1] = { tref = tref, tid = ids[tref], s = r, e = r + tlen }
				p = r + tlen
			end
			ok = ok and count == #recs
		end
		if ok then
			local head = bytesOf(b, nodeSeg.s, nodeSeg.ps)
			local function empty(rec: any): string
				if rec.tid == StringBlobId or rec.tid == BytesBlobId then
					return ""
				end
				return "\0"
			end
			local function nodeWire(bodyOf: (number, any) -> string): string
				local parts = { V.lebString(#recs) }
				for i, rec in recs do
					local body = bodyOf(i, rec)
					parts[#parts + 1] = V.lebString(rec.tref) .. V.lebString(#body) .. body
				end
				local payload = table.concat(parts)
				return wireOf(head .. V.lebString(#payload) .. payload .. "\0")
			end
			local placed: { [string]: any } = {}
			for _, name in desc.place.tables do
				local d = S.lookup(name)
				placed[d.id] = d
			end
			for upto = 1, #recs + 1 do
				local j = upto - 1
				local wire = nodeWire(function(i: number, rec: any): string
					if i <= j then
						return bytesOf(b, rec.s, rec.e)
					end
					return empty(rec)
				end)
				if probe(wire) then
					if j == 0 then
						if k == "unknown" then
							for _, rec in recs do
								local blob = (rec.tid == StringBlobId and desc.place.str) or (rec.tid == BytesBlobId and desc.place.bytes)
								if not blob and placed[rec.tid] == nil then
									return V.idHex(rec.tid)
								end
							end
						end
						return nil
					end
					local rec = recs[j]
					local rd = placed[rec.tid]
					if rd == nil then
						return nil
					end
					local body = walkBody(b, rec.s, rec.e, ids)
					if body == nil then
						return nil
					end
					return isolate(rd.fields, body, function(one: string): string
						return nodeWire(function(i: number, r: any): string
							if i < j then
								return bytesOf(b, r.s, r.e)
							end
							if i == j then
								return one
							end
							return empty(r)
						end)
					end)
				end
			end
		end
	end

	if desc.variable and nodeSeg ~= nil and probe(wireOf(bytesOf(b, nodeSeg.s, nodeSeg.e) .. "\0")) then
		return nil
	end
	local others = {}
	for _, s in top.fields do
		if not (desc.variable and s == nodeSeg) then
			others[#others + 1] = s
		end
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
			if fd == nil then
				return V.idHex(seg.id)
			end
			return refine(fd, seg, function(one: string): string
				local out = {}
				local t = 0
				for _, s in top.fields do
					if desc.variable and s == nodeSeg then
						out[#out + 1] = bytesOf(b, s.s, s.e)
					elseif t < i then
						t += 1
						out[#out + 1] = if t == i then one else bytesOf(b, s.s, s.e)
					end
				end
				return wireOf(table.concat(out) .. "\0")
			end) or fd.name
		end
	end
	return nil
end

-- THE VARIABLE FORM'S STRICT READ (§6.5): the one answer for form 1, over
-- the tolerant walk. It resets the caller's report, applies the file's own
-- framing (integrity, the id table and the root body's terminator), then the
-- non-finite refusal, then the refusing counters in TableReport.Err()'s
-- order, so a wire always answers one name.
function L.ReadStrict(root: string, bytes: buffer?, report: any?): any
	local rep: any = report or {}
	reset(rep)
	if bytes == nil or buffer.len(bytes) < 1 then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local b = bytes :: buffer
	local form = buffer.readu8(b, 0)
	if form == 2 then
		rep.reason = "message_form_as_file"
		return nil
	end
	if form ~= 1 then
		rep.reason = "newer_form"
		return nil
	end
	local d = S.lookup(root)
	if d.integrity ~= nil and d.integrity ~= "" then
		local body = integrityOpen(b)
		if body == nil then
			rep.malformed = true
			rep.reason = "integrity_mismatch"
			return nil
		end
		b = body
	end
	local fr = frame(b)
	local top: any = if fr ~= nil then walkBody(b, 1, fr.e, fr.ids) else nil
	if fr == nil or (top ~= nil and top.term + 1 ~= fr.e) then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local v = L.loadTolerant(root, b, rep)
	if rep.nonFinite > 0 then
		rep.field = rep.nonFiniteField
		return nil
	end
	if _countRefused then
		rep.reason = "count_refusal"
		return nil
	end
	if v == nil and rep.malformed then
		rep.reason = "wire_damaged"
		return nil
	end
	for _, k in REFUSING do
		if moved(rep, k) then
			if k == "missingRequired" then
				rep.reason = "missing_required: " .. (rep.missingName or "")
				rep.field = rep.missingName
			else
				rep.reason = NAMES[k]
				rep.field = witness(root, b, k)
			end
			return nil
		end
	end
	if rep.unknown > 0 then
		rep.unknownField = witness(root, b, "unknown")
	end
	if rep.duplicate > 0 then
		rep.duplicateField = witness(root, b, "duplicate")
	end
	return v
end

-- setRetain installs (or, with nil, clears) RETAIN-UNKNOWN's hook for the
-- next walk (table_retain.moo, docs/SPEC-TABLES.md §6.6).
function L.setRetain(h: any)
	_retain = h
end

-- resetReport is the report at rest, shared with refusal.moo's fixed path.
function L.resetReport(report: any)
	reset(report)
end

-- newReport is a fresh report at rest.
function L.newReport(): any
	return fresh()
end

-- THE PLAIN NAME IS THE STRICT READ (§6.5): no recovered value comes back
-- under it.
function L.load(root: string, bytes: buffer, report: any): any
	return L.ReadStrict(root, bytes, report)
end

return table.freeze(L)
