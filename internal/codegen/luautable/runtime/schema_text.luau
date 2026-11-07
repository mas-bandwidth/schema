--!strict
-- schema_text.luau — THE BASE64 TEXT CARRIER (docs/SPEC-TABLES.md §16.8; docs/PORT-
-- LUAU.md, THE CARD MAP). A text channel carries a table as the padded
-- standard base64 of its whole binary wire: form byte, framing check and
-- every versioning guarantee ride inside, and the text is opaque. A body that
-- does not decode is `base64_damaged`; a decoded wire the binary reader
-- refuses keeps the binary reader's own name (a form this build does not
-- carry, wire_damaged, truncated, or the report's reason).
--
-- It is a WRAPPER over schema_save.luau and schema_refusal.luau and edits nothing in them:
-- SaveText is the base64 of Save's bytes, and LoadText is ReadStrict (§6.5)
-- over the decoded wire, so a tolerant load's recovered value never comes
-- back with a nil verdict.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local Save = require("./schema_save")
local Load = require("./schema_load")
local Refusal = require("./schema_refusal")

local T = {}

-- THE STANDARD ALPHABET, padded (§16.8; RFC 4648).
local ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
local VALUE: { [string]: number } = {}
for i = 1, #ALPHABET do VALUE[string.sub(ALPHABET, i, i)] = i - 1 end

-- the report at rest, as schema_refusal.luau resets it (§4)
local function reset(report: any)
	report.malformed = false
	report.refused = 0
	report.unknown = 0
	report.kindMismatch = 0
	report.clamped = 0
	report.widened = 0
	report.duplicate = 0
	report.reason = nil
	report.field = nil
	report.unknownField = nil
	report.duplicateField = nil
end

-- SAVE — the base64 of the whole wire (§16.8). The alphabet is standard and
-- the output is padded: three input bytes per four characters, then `=` for
-- the one or two bytes the last group does not fill.
local function encode(bytes: buffer): string
	local n = buffer.len(bytes)
	local parts: { string } = {}
	local i = 0
	while i + 3 <= n do
		local a = buffer.readu8(bytes, i)
		local b = buffer.readu8(bytes, i + 1)
		local c = buffer.readu8(bytes, i + 2)
		parts[#parts + 1] = string.sub(ALPHABET, math.floor(a / 4) + 1, math.floor(a / 4) + 1)
		parts[#parts + 1] = string.sub(ALPHABET, (a % 4) * 16 + math.floor(b / 16) + 1, (a % 4) * 16 + math.floor(b / 16) + 1)
		parts[#parts + 1] = string.sub(ALPHABET, (b % 16) * 4 + math.floor(c / 64) + 1, (b % 16) * 4 + math.floor(c / 64) + 1)
		parts[#parts + 1] = string.sub(ALPHABET, (c % 64) + 1, (c % 64) + 1)
		i = i + 3
	end
	local rem = n - i
	if rem > 0 then
		local a = buffer.readu8(bytes, i)
		local b = 0
		if rem > 1 then b = buffer.readu8(bytes, i + 1) end
		parts[#parts + 1] = string.sub(ALPHABET, math.floor(a / 4) + 1, math.floor(a / 4) + 1)
		parts[#parts + 1] = string.sub(ALPHABET, (a % 4) * 16 + math.floor(b / 16) + 1, (a % 4) * 16 + math.floor(b / 16) + 1)
		if rem > 1 then
			parts[#parts + 1] = string.sub(ALPHABET, (b % 16) * 4 + 1, (b % 16) * 4 + 1)
		end
		local pad = (3 - rem) % 3
		if pad > 0 then parts[#parts + 1] = string.rep("=", pad) end
	end
	return table.concat(parts)
end

-- LOAD — the wire bytes the text spells, or nil and the refusal name for a
-- body that does not (§16.8). Padding is required. The text is CANONICAL
-- (§16.8): the standard alphabet, padded, pad bits zero, and no whitespace or
-- newline, so a body that has a CR or LF, or a non-zero unused pad bit, but
-- still decodes is `base64_not_canonical`; every other failure — a byte
-- outside the alphabet and `=`, a missing, extra or misplaced `=`, or a
-- length that does not decode — is `base64_damaged`.
local function decode(text: string): (buffer?, string?)
	local broken = string.find(text, "[\r\n]") ~= nil
	local s = string.gsub(text, "[\r\n]", "")
	local n = #s
	if n % 4 ~= 0 then return nil, "base64_damaged" end
	if n == 0 then
		if broken then return buffer.create(0), "base64_not_canonical" end
		return buffer.create(0), nil
	end
	local lastPad = 0
	if string.sub(s, n, n) == "=" then
		lastPad = 1
		if string.sub(s, n - 1, n - 1) == "=" then lastPad = 2 end
	end
	-- dropping CR and LF leaves canonical text only when the text had none
	local nonCanonical = broken
	local out = buffer.create(n / 4 * 3 - lastPad)
	local o = 0
	for i = 1, n, 4 do
		local last = i + 3 == n
		local c1 = string.sub(s, i, i)
		local c2 = string.sub(s, i + 1, i + 1)
		local c3 = string.sub(s, i + 2, i + 2)
		local c4 = string.sub(s, i + 3, i + 3)
		local v1 = VALUE[c1]
		local v2 = VALUE[c2]
		if v1 == nil or v2 == nil then return nil, "base64_damaged" end
		if last and lastPad == 2 then
			if c3 ~= "=" or c4 ~= "=" then return nil, "base64_damaged" end
			-- the last quantum has four spare bits; canonical leaves them zero
			if v2 % 16 ~= 0 then nonCanonical = true end
			buffer.writeu8(out, o, v1 * 4 + math.floor(v2 / 16))
			o = o + 1
		elseif last and lastPad == 1 then
			if c4 ~= "=" then return nil, "base64_damaged" end
			local v3 = VALUE[c3]
			if v3 == nil then return nil, "base64_damaged" end
			-- the last quantum has two spare bits; canonical leaves them zero
			if v3 % 4 ~= 0 then nonCanonical = true end
			buffer.writeu8(out, o, v1 * 4 + math.floor(v2 / 16))
			buffer.writeu8(out, o + 1, (v2 % 16) * 16 + math.floor(v3 / 4))
			o = o + 2
		else
			local v3 = VALUE[c3]
			local v4 = VALUE[c4]
			if v3 == nil or v4 == nil then return nil, "base64_damaged" end
			buffer.writeu8(out, o, v1 * 4 + math.floor(v2 / 16))
			buffer.writeu8(out, o + 1, (v2 % 16) * 16 + math.floor(v3 / 4))
			buffer.writeu8(out, o + 2, (v3 % 4) * 64 + v4)
			o = o + 3
		end
	end
	if nonCanonical then return out, "base64_not_canonical" end
	return out, nil
end

-- SaveText returns the base64 of `d`'s value `v` — the whole wire Save writes
-- (§16.8), padded on write — or nil when Save refuses.
function T.SaveText(d: any, v: any): (string?, any?)
	local bytes, reason = Save.save(d, v)
	if bytes == nil then return nil, reason end
	return encode(bytes), nil
end

-- STRICT OVER THE WIRE `<Name>Save` WRITES (§16.8, §6.5). A fixed table's
-- binary Save is still the variable form's form-1 writer (docs/SPEC-TABLES.md,
-- THE FIRST BYTE: the form-1 machinery a fixed table carries until form 3
-- lands), exactly as Go's <Name>LoadText reads it through <Name>Load, so this
-- path names form 1 and not the fixed form's previous_form. Every refusing
-- counter is TableReport.Err's, in its order, and damage the framing walk
-- refuses itself is wire_damaged.
local REFUSING = { "malformed", "clamped", "kindMismatch" }
local NAMES: { [string]: string } = {
	malformed = "malformed",
	clamped = "clamped",
	kindMismatch = "kind_mismatch",
}

local function strict_variable(root: string, wire: buffer, rep: any): any
	reset(rep)
	if buffer.len(wire) < 1 then
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local form = buffer.readu8(wire, 0)
	if form == 2 then rep.reason = "message_form_as_file"; return nil end
	if form ~= 1 then rep.reason = "newer_form"; return nil end
	local v = Load.load(root, wire, rep)
	if v == nil then
		if rep.reason == nil then rep.reason = "wire_damaged" end
		return nil
	end
	for _, k in REFUSING do
		local moved = k == "malformed" and rep.malformed == true or (k ~= "malformed" and rep[k] > 0)
		if moved then
			rep.reason = NAMES[k]
			return nil
		end
	end
	return v
end

-- LoadText reads one text into the root of the table module `module` and
-- returns the value, as ReadStrict does (§6.5, §16.8). A body that does not
-- decode is `base64_damaged`; a body that decodes but is not the canonical
-- spelling of its bytes is `base64_not_canonical`; the decoded wire's refusal
-- is the binary reader's own name. A nil report is allowed; a supplied one
-- receives THIS read's report, so earlier counts neither decide nor survive.
function T.LoadText(module: any, root: string, text: string, report: any?): any
	local rep: any = report or {}
	local wire, damage = decode(text)
	if wire == nil or damage ~= nil then
		reset(rep)
		rep.reason = damage or "base64_damaged"
		return nil
	end
	local d = S.lookup(root)
	if d.fixed then
		return strict_variable(root, wire, rep)
	end
	return Refusal.ReadStrict(module, root, wire, rep)
end

return table.freeze(T)
