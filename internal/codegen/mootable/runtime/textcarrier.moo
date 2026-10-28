--!strict
-- textcarrier.moo — THE BASE64 TEXT CARRIER (docs/SPEC-TABLES.md §16.8; docs/PORT-MOO.md,
-- THE LAYERS, layer 4). A text channel carries a table as the CANONICAL base64 of
-- its whole binary wire: the standard alphabet, padded, pad bits zero, and no
-- byte outside the alphabet and `=`. The form byte, the framing check and every
-- versioning guarantee ride inside, and the text is opaque.
--
-- A text that drops its CR and LF bytes into padded standard base64 that decodes,
-- but is not exactly the canonical encoding of those bytes (a pad bit set, a CR
-- or LF present), is `base64_not_canonical`; every other failure is
-- `base64_damaged`. A decoded wire the binary reader refuses keeps the binary
-- reader's own name.
--
-- It is a WRAPPER: SaveText is the base64 of the table's Save bytes and LoadText
-- is the table's strict read (§6.5) over the decoded wire, both handed in by the
-- generated module, so this file edits nothing in the wire runtime.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local T = {}

local ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

-- the symbol's value by byte, -1 for a byte outside the alphabet
local VALUE: { number } = table.create(256, -1)
for i = 1, 64 do
	VALUE[(string.byte(ALPHABET, i) :: number) + 1] = i - 1
end

-- encode(bytes): the canonical base64 of a buffer: three bytes per four
-- symbols, then `=` for each byte the last group does not fill.
function T.encode(bytes: buffer): string
	local n = buffer.len(bytes)
	local out = buffer.create((n + 2) // 3 * 4)
	local o = 0
	local i = 0
	while i + 3 <= n do
		local v = buffer.readu8(bytes, i) * 65536 + buffer.readu8(bytes, i + 1) * 256 + buffer.readu8(bytes, i + 2)
		buffer.writeu8(out, o, string.byte(ALPHABET, v // 262144 + 1) :: number)
		buffer.writeu8(out, o + 1, string.byte(ALPHABET, (v // 4096) % 64 + 1) :: number)
		buffer.writeu8(out, o + 2, string.byte(ALPHABET, (v // 64) % 64 + 1) :: number)
		buffer.writeu8(out, o + 3, string.byte(ALPHABET, v % 64 + 1) :: number)
		i += 3
		o += 4
	end
	local left = n - i
	if left > 0 then
		local v = buffer.readu8(bytes, i) * 65536
		if left == 2 then
			v += buffer.readu8(bytes, i + 1) * 256
		end
		buffer.writeu8(out, o, string.byte(ALPHABET, v // 262144 + 1) :: number)
		buffer.writeu8(out, o + 1, string.byte(ALPHABET, (v // 4096) % 64 + 1) :: number)
		if left == 2 then
			buffer.writeu8(out, o + 2, string.byte(ALPHABET, (v // 64) % 64 + 1) :: number)
		else
			buffer.writeu8(out, o + 2, 61)
		end
		buffer.writeu8(out, o + 3, 61)
	end
	return buffer.tostring(out)
end

-- decode(text): the wire bytes the text spells and nil, or (for a text that
-- decodes but is not canonical) the bytes and "base64_not_canonical", or nil
-- and "base64_damaged".
function T.decode(text: string): (buffer?, string?)
	local nonCanonical = string.find(text, "[\r\n]") ~= nil
	local s = if nonCanonical then string.gsub(text, "[\r\n]", "") else text
	local n = #s
	if n % 4 ~= 0 then
		return nil, "base64_damaged"
	end
	local pad = 0
	if n > 0 and string.byte(s, n) == 61 then
		pad = if string.byte(s, n - 1) == 61 then 2 else 1
	end
	local out = buffer.create(n // 4 * 3 - pad)
	local o = 0
	for i = 1, n, 4 do
		local c1, c2, c3, c4 = string.byte(s, i, i + 3)
		local v1 = VALUE[(c1 :: number) + 1]
		local v2 = VALUE[(c2 :: number) + 1]
		if v1 < 0 or v2 < 0 then
			return nil, "base64_damaged"
		end
		local last = i + 3 == n
		if last and pad == 2 then
			-- the last group has four spare bits; canonical leaves them zero
			if v2 % 16 ~= 0 then
				nonCanonical = true
			end
			buffer.writeu8(out, o, v1 * 4 + v2 // 16)
			o += 1
		elseif last and pad == 1 then
			local v3 = VALUE[(c3 :: number) + 1]
			if v3 < 0 then
				return nil, "base64_damaged"
			end
			-- the last group has two spare bits; canonical leaves them zero
			if v3 % 4 ~= 0 then
				nonCanonical = true
			end
			buffer.writeu8(out, o, v1 * 4 + v2 // 16)
			buffer.writeu8(out, o + 1, (v2 % 16) * 16 + v3 // 4)
			o += 2
		else
			local v3 = VALUE[(c3 :: number) + 1]
			local v4 = VALUE[(c4 :: number) + 1]
			if v3 < 0 or v4 < 0 then
				return nil, "base64_damaged"
			end
			buffer.writeu8(out, o, v1 * 4 + v2 // 16)
			buffer.writeu8(out, o + 1, (v2 % 16) * 16 + v3 // 4)
			buffer.writeu8(out, o + 2, (v3 % 4) * 64 + v4)
			o += 3
		end
	end
	if nonCanonical then
		return out, "base64_not_canonical"
	end
	return out, nil
end

-- saveText(save, value): the base64 of the bytes save(value) writes, or nil
-- and save's own refusal (§16.8)
function T.saveText(save: (any) -> (buffer?, any), value: any): (string?, any)
	local bytes, refusal = save(value)
	if bytes == nil then
		return nil, refusal
	end
	return T.encode(bytes), nil
end

-- loadText(text, readStrict, report): the value the strict read lands from the
-- decoded wire, or nil and the refusal's name. A body that does not decode, or
-- one that is not the canonical spelling of its bytes, refuses before any read;
-- the decoded wire's own refusal is the strict read's. A nil report is allowed.
function T.loadText(text: string, readStrict: (buffer, any) -> (any, string?), report: any?): (any, string?)
	local wire, damage = T.decode(text)
	if wire == nil or damage ~= nil then
		if report ~= nil then
			report.reason = damage
		end
		return nil, damage
	end
	return readStrict(wire, report)
end

return table.freeze(T)
