--!strict
-- The Luau leg of test/packet-void (SPEC §4.8): answers every case of
-- testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
-- line each, for tools/casegate. The pins are the Go oracle's bytes, never
-- this writer's. The interpreter has no io library, so pins.luau carries
-- cases.txt and every <name>.bin in as hex (make/luau-packet.mk writes it).
local serialize = require("./serialize")
local V: any = require("./Void")
local pins: { [string]: string } = require("./pins") :: any

local TYPES: { [string]: boolean } = { Mixed = true, Late = true, Tags = true, Single = true, Empty = true, Frame = true, Batch = true }

local function unhex(s: string): buffer
	local n = #s // 2
	local b = buffer.create(n)
	for i = 0, n - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(s, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
end

local function hex(b: buffer): string
	local out = {}
	for i = 0, buffer.len(b) - 1 do
		table.insert(out, string.format("%02x", buffer.readu8(b, i)))
	end
	return table.concat(out)
end

-- same compares the value's wire-meaningful fields: the selected arm and the
-- counted elements; an unselected arm and storage past a count are
-- unspecified (SPEC §4.8).
local function sameArm(a: any, b: any): boolean
	if a.EntriesCount ~= b.EntriesCount or a.Marker ~= b.Marker then
		return false
	end
	for i = 1, a.EntriesCount do
		if a.Entries[i].Retries ~= b.Entries[i].Retries then
			return false
		end
	end
	return true
end

local function sameMixed(a: any, b: any): boolean
	if a.Type ~= b.Type then
		return false
	end
	return a.Type ~= V.MixedType.Payload or sameArm(a.Payload, b.Payload)
end

local function same(typeName: string, a: any, b: any): boolean
	if typeName == "Mixed" then
		return sameMixed(a, b)
	elseif typeName == "Late" then
		if a.Type ~= b.Type then
			return false
		end
		return a.Type ~= V.LateType.Payload or sameArm(a.Payload, b.Payload)
	elseif typeName == "Frame" then
		return a.Lead == b.Lead and a.Tail == b.Tail and sameMixed(a.Choice, b.Choice)
	elseif typeName == "Batch" then
		if a.ValuesCount ~= b.ValuesCount then
			return false
		end
		for i = 1, a.ValuesCount do
			if not sameMixed(a.Values[i], b.Values[i]) then
				return false
			end
		end
		return true
	end
	return a.Type == b.Type
end

local function mixed(tag: number): any
	local v = V.NewMixed()
	v.Type = tag
	return v
end

-- The value of each accept case, by name, and its type; nil for a name this
-- driver does not know, which is FAILED and never written with another
-- case's value.
local function build(name: string): (any, string)
	if name == "mixed-none" then
		return mixed(V.MixedType.None), "Mixed"
	elseif name == "mixed-ping" then
		return mixed(V.MixedType.Ping), "Mixed"
	elseif name == "mixed-payload" then
		return mixed(V.MixedType.Payload), "Mixed"
	elseif name == "late-payload" or name == "late-pong" then
		local v = V.NewLate()
		v.Type = if name == "late-payload" then V.LateType.Payload else V.LateType.Pong
		return v, "Late"
	elseif name == "tags-none" or name == "tags-ping" or name == "tags-pong" then
		local v = V.NewTags()
		local tags: { [string]: number } = { ["tags-none"] = V.TagsType.None, ["tags-ping"] = V.TagsType.Ping, ["tags-pong"] = V.TagsType.Pong }
		v.Type = tags[name]
		return v, "Tags"
	elseif name == "single-none" or name == "single-ack" then
		local v = V.NewSingle()
		v.Type = if name == "single-ack" then V.SingleType.Ack else V.SingleType.None
		return v, "Single"
	elseif name == "empty-none" then
		return V.NewEmpty(), "Empty"
	elseif name == "frame-ping" then
		local v = V.NewFrame()
		v.Lead = 5
		v.Choice = mixed(V.MixedType.Ping)
		v.Tail = 6
		return v, "Frame"
	elseif name == "batch-empty" or name == "batch-ping" or name == "batch-ping-payload" then
		local v = V.NewBatch()
		if name ~= "batch-empty" then
			v.Values[1] = mixed(V.MixedType.Ping)
			v.ValuesCount = 1
		end
		if name == "batch-ping-payload" then
			v.Values[2] = mixed(V.MixedType.Payload)
			v.ValuesCount = 2
		end
		return v, "Batch"
	end
	return nil, ""
end

-- dirtyArm fills the storage a read reuses in place, so a read that does not
-- start the payload arm at its declared defaults shows.
local function dirtyArm(arm: any)
	arm.EntriesCount = 2
	arm.Entries[1].Retries = 9
	arm.Entries[2].Retries = 9
	arm.Marker = 0
end

local function fresh(typeName: string): any
	local v = V["New" .. typeName]()
	if typeName == "Mixed" or typeName == "Late" then
		dirtyArm(v.Payload)
	elseif typeName == "Batch" then
		dirtyArm(v.Values[1].Payload)
		dirtyArm(v.Values[2].Payload)
	end
	return v
end

local function write(typeName: string, value: any): (buffer?, number)
	local w = serialize.WriteStream.new(buffer.create(256))
	if not V["Write" .. typeName](w, value) then
		return nil, 0
	end
	local bits = w:bitsProcessed()
	w:flush()
	return w:data(), bits
end

-- The refuse-write cases: the type's default value with the one change the
-- case names. Luau's own refusal is the writer returning false.
local function buildRefusal(name: string): (any, string)
	if name == "tags-write-tag-3" then
		local v = V.NewTags()
		v.Type = 3
		return v, "Tags"
	elseif name == "empty-write-tag-1" then
		local v = V.NewEmpty()
		v.Type = 1
		return v, "Empty"
	elseif string.sub(name, 1, 12) == "mixed-write-" then
		local v = V.NewMixed()
		if name == "mixed-write-tag-negative" then
			v.Type = -1
		elseif name == "mixed-write-tag-text" then
			v.Type = "ping"
		elseif name == "mixed-write-tag-3" then
			v.Type = 3
		elseif name == "mixed-write-payload-type" then
			v.Type = V.MixedType.Payload
			v.Payload = 1
		elseif name == "mixed-write-marker-8" then
			v.Type = V.MixedType.Payload
			v.Payload.Marker = 8
		else
			return nil, ""
		end
		return v, "Mixed"
	end
	return nil, ""
end

local function refuseWrite(name: string, typeName: string): string?
	local value, built = buildRefusal(name)
	if value == nil then
		return "unknown refuse-write case"
	end
	if built ~= typeName then
		return "case type " .. typeName .. ", the driver builds " .. built
	end
	local w = serialize.WriteStream.new(buffer.create(256))
	local ok, res = pcall(V["Write" .. typeName], w, value)
	if not ok then
		return "write raised: " .. tostring(res)
	end
	if res ~= false then
		return "write accepted"
	end
	return nil
end

-- selectPayload reads mixed-ping's pin, then selects the payload arm the Luau
-- API's way: the union keeps its Payload object, so selecting is setting the
-- tag, and the arm must still carry its declared defaults.
local function selectPayload(): (buffer?, string | number)
	local pinHex = pins["mixed-ping.bin"]
	if pinHex == nil then
		return nil, "the pin mixed-ping.bin was not carried in"
	end
	local v = V.NewMixed()
	if not V.ReadMixed(serialize.ReadStream.new(unhex(pinHex)), v) then
		return nil, "read mixed-ping refused"
	end
	if v.Type ~= V.MixedType.Ping then
		return nil, "read mixed-ping: tag " .. tostring(v.Type)
	end
	v.Type = V.MixedType.Payload
	local got, gotBits = write("Mixed", v)
	if got == nil then
		return nil, "write refused"
	end
	return got, gotBits
end

local function answer(name: string, typeName: string, verdict: string, bits: string): string?
	if not TYPES[typeName] then
		return "unknown type " .. typeName
	end
	if verdict == "refuse-write" then
		return refuseWrite(name, typeName)
	end
	local pinHex = pins[name .. ".bin"]
	if pinHex == nil then
		return "the pin " .. name .. ".bin was not carried in"
	end
	local pin = unhex(pinHex)
	if verdict == "select" then
		if name ~= "mixed-ping-select-payload" then
			return "unknown select case"
		end
		local got, gotBits = selectPayload()
		if got == nil then
			return tostring(gotBits)
		end
		if hex(got) ~= pinHex then
			return "write " .. hex(got) .. ", pin " .. pinHex
		end
		if gotBits ~= tonumber(bits) then
			return "write has " .. tostring(gotBits) .. " bits, want " .. bits
		end
		return nil
	end
	if verdict == "refuse" then
		if V["Read" .. typeName](serialize.ReadStream.new(pin), fresh(typeName)) then
			return "read accepted"
		end
		return nil
	end
	if verdict ~= "accept" then
		return "verdict " .. verdict
	end

	local value, built = build(name)
	if value == nil then
		return "unknown accept case"
	end
	if built ~= typeName then
		return "case type " .. typeName .. ", the driver builds " .. built
	end
	local want = tonumber(bits)

	local got, gotBits = write(typeName, value)
	if got == nil then
		return "write refused"
	end
	if hex(got) ~= pinHex then
		return "write " .. hex(got) .. ", pin " .. pinHex
	end
	if gotBits ~= want then
		return "write has " .. tostring(gotBits) .. " bits, want " .. bits
	end

	local back = fresh(typeName)
	if not V["Read" .. typeName](serialize.ReadStream.new(pin), back) then
		return "read refused"
	end
	if not same(typeName, back, value) then
		return "read value differs"
	end
	local again = write(typeName, back)
	if again == nil or hex(again) ~= pinHex then
		return "write what was read differs"
	end

	-- The body read stops before the end check, so its cursor is the
	-- object's bit count.
	local body = serialize.ReadStream.new(pin)
	if not V["Read" .. typeName .. "Body"](body, fresh(typeName)) then
		return "body read refused"
	end
	if body:bitsProcessed() ~= want then
		return "read has " .. tostring(body:bitsProcessed()) .. " bits, want " .. bits
	end
	return nil
end

local casesHex = pins["cases.txt"]
if casesHex == nil then
	error("packet-void Luau: cases.txt was not carried in")
end
for line in string.gmatch(buffer.tostring(unhex(casesHex)), "[^\n]+") do
	local fields: { string } = {}
	for f in string.gmatch(line, "%S+") do
		table.insert(fields, f)
	end
	if #fields > 0 and string.sub(fields[1], 1, 1) ~= "#" then
		if #fields < 4 then
			print("case " .. fields[1] .. " FAILED: a case line needs <name> <type> <verdict> <bits>")
		else
			local ok, why = pcall(answer, fields[1], fields[2], fields[3], fields[4])
			local reason: string? = if ok then why else "raised: " .. tostring(why)
			if reason then
				print("case " .. fields[1] .. " FAILED: " .. reason)
			else
				print("case " .. fields[1] .. " ok")
			end
		end
	end
end
