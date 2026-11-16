--!strict
-- The moo driver of the packet bare-integer write cases
-- (test/packet-write-bareint/cases.txt, SPEC.md §5.1 row 11): every case is
-- written through the generated Write<Type>; a refused case must return
-- false (never raise), and a written case must read back exactly. The driver
-- hands cases.txt in as inputs.cases (the moo CLI has no io).
--
-- The storage is the leg's own (docs/PORT-MOO.md, THE VALUE MAPPING): an 8-
-- to 32-bit field holds a moo number; int64 holds a serialize.Int64 and
-- uint64 a serialize.Uint64. A 64-bit value its class cannot carry is held in
-- the narrowest storage that can: 2^63 as a Uint64, 2^64 as a Uint128, a
-- negative as a number. That is the value the case writes.
local serialize = require("./serialize")
local B = require("./BareInt")
local C = require("./Carriers")
local inputs = require("./inputs") :: any

local failed = false
local function check(ok: boolean, what: string)
	if not ok then
		failed = true
		print("FAILED: " .. what)
	end
end

-- cmpDec compares two decimal strings of non-negative integers.
local function cmpDec(a: string, b: string): number
	if #a ~= #b then
		return if #a < #b then -1 else 1
	end
	if a == b then
		return 0
	end
	return if a < b then -1 else 1
end

-- valueOf is the value a caller holds for s in a field of type t.
local function valueOf(t: string, s: string): any
	local neg = string.sub(s, 1, 1) == "-"
	local digits = if neg then string.sub(s, 2) else s
	if t == "I64" then
		if neg then
			if cmpDec(digits, "9223372036854775808") <= 0 then
				return serialize.Int64.parse(s)
			end
			return serialize.Int128.parse(s)
		end
		if cmpDec(digits, "9223372036854775807") <= 0 then
			return serialize.Int64.parse(s)
		end
		return serialize.Uint64.parse(s)
	elseif t == "U64" then
		if neg then
			return tonumber(s)
		end
		if cmpDec(digits, "18446744073709551615") <= 0 then
			return serialize.Uint64.parse(s)
		end
		return serialize.Uint128.parse(s)
	end
	return tonumber(s)
end

local function same(t: string, a: any, b: any): boolean
	if t == "I64" then
		return serialize.Int64.is(a) and serialize.Int64.is(b) and serialize.Int64.cmp(a, b) == 0
	elseif t == "U64" then
		return serialize.Uint64.is(a) and serialize.Uint64.is(b) and serialize.Uint64.cmp(a, b) == 0
	end
	return a == b
end

-- write runs one Write and returns its verdict and, when written, the value
-- read back through readInto; a raise is its own failure, never a refusal.
local function write(m: any, name: string, v: any, what: string): (boolean, any)
	local ws = serialize.WriteStream.new(buffer.create(64))
	local ran, ok = pcall(m["Write" .. name], ws, v)
	if not ran then
		check(false, what .. ": the write raised (" .. tostring(ok) .. ")")
		return false, nil
	end
	if not ok then
		return false, nil
	end
	ws:flush()
	local back: any = m["New" .. name]()
	local rs = serialize.ReadStream.new(ws:data())
	check(m["Read" .. name](rs, back), what .. ": reads back")
	return true, back
end

local function writeBare(t: string, value: any, what: string): (boolean, any)
	local v: any = (B :: any)["New" .. t]()
	v.Value = value
	local ok, back = write(B, t, v, what)
	return ok, if ok then back.Value else nil
end

local n = 0
for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
	local words = {}
	for word in string.gmatch(line, "%S+") do
		table.insert(words, word)
	end
	if #words == 4 and string.sub(words[1], 1, 1) ~= "#" then
		n += 1
		local name, t, text, verdict = words[1], words[2], words[3], words[4]
		local value = valueOf(t, text)
		local ok, back = writeBare(t, value, name)
		if verdict == "refused" then
			check(not ok, "packet-write-bareint " .. name .. " refused")
		else
			check(ok, "packet-write-bareint " .. name .. " written")
			check(ok and same(t, back, value), "packet-write-bareint " .. name .. " reads back exactly")
		end
	end
end
check(n > 0, "packet-write-bareint: the case list is not empty")

-- A value that is not an integer is refused at every width: a fraction, a
-- string and a boolean.
local kinds = { "I8", "I16", "I32", "I64", "U8", "U16", "U32", "U64" }
local notIntegers: { [string]: any } = { fraction = 1.5, text = "1", boolean = true }
for _, t in kinds do
	for what, value in notIntegers do
		local ok = writeBare(t, value, t .. " " .. what)
		check(not ok, "packet-write-bareint " .. t .. " " .. what .. " refused")
	end
end

-- The carriers: a fixed array element, a counted array element and a union
-- arm's payload refuse an out-of-width value and write each boundary.
local function elements(pair1: number, list1: number): any
	local e: any = C.NewElements()
	e.Pair[1] = pair1
	e.Pair[2] = 0
	e.ListCount = 1
	e.List[1] = list1
	return e
end

local function carrier(what: string, name: string, v: any, verdict: string)
	local ok = write(C, name, v, what)
	if verdict == "refused" then
		check(not ok, "packet-write-bareint " .. what .. " refused")
	else
		check(ok, "packet-write-bareint " .. what .. " written")
	end
end

carrier("fixed-element-int8-max", "Elements", elements(127, 0), "written")
carrier("fixed-element-int8-above-max", "Elements", elements(128, 0), "refused")
carrier("fixed-element-int8-below-min", "Elements", elements(-129, 0), "refused")
carrier("counted-element-uint16-max", "Elements", elements(0, 65535), "written")
carrier("counted-element-uint16-above-max", "Elements", elements(0, 65536), "refused")
carrier("counted-element-uint16-negative", "Elements", elements(0, -1), "refused")

local function armSmall(value: number): any
	local a: any = C.NewArm()
	a.Type = 1
	a.Small.Value = value
	return a
end
local function armElements(pair1: number): any
	local a: any = C.NewArm()
	a.Type = 2
	a.Elements = elements(pair1, 0)
	return a
end
carrier("arm-int8-min", "Arm", armSmall(-128), "written")
carrier("arm-int8-above-max", "Arm", armSmall(128), "refused")
carrier("arm-element-int8-above-max", "Arm", armElements(128), "refused")

if failed then
	error("packet-write-bareint moo: FAILED")
end
print("packet-write-bareint moo: " .. n .. " cases, each out-of-width and non-integer value refused, each boundary written and read back, the carriers held")
