--!strict
-- The Luau driver of the packet bare-integer write cases
-- (test/packet-write-bareint/cases.txt, SPEC.md §5.1 row 11): every case is
-- written through the generated Write<Type>; a refused case must return
-- false (never raise), and a written case must read back exactly. cases.luau
-- carries the shared list in as source (the interpreter has no io library).
--
-- The storage is the leg's own (docs/PORT-LUAU.md 64-BIT AND 128-BIT
-- INTEGERS): an 8- to 32-bit field holds a number; a 64-bit field holds an
-- integer, the uint64 bit pattern for U64. A value the integer cannot carry
-- (above int64's max for I64; negative or above 2^64-1 for U64) can only be
-- held as a number, and that is the value the case writes.
local serialize = require("./serialize")
local B = require("./BareInt")
local cases: { { name: string, type: string, value: string, verdict: string } } = require("./cases") :: any

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

-- integerOf parses a decimal string the 64-bit storage can carry into its
-- integer (the bit pattern, wrapping past int64's max for U64).
local function integerOf(s: string): any
	local neg = string.sub(s, 1, 1) == "-"
	local digits = if neg then string.sub(s, 2) else s
	local v = integer.create(0)
	for i = 1, #digits do
		local d = integer.create(string.byte(digits, i) - 48)
		v = integer.add(integer.mul(v, integer.create(10)), d)
	end
	if neg then
		v = integer.sub(integer.create(0), v)
	end
	return v
end

-- valueOf is the value a caller holds for s in a field of type t.
local function valueOf(t: string, s: string): any
	local neg = string.sub(s, 1, 1) == "-"
	local digits = if neg then string.sub(s, 2) else s
	if t == "I64" then
		local fits = if neg then cmpDec(digits, "9223372036854775808") <= 0 else cmpDec(digits, "9223372036854775807") <= 0
		if fits then
			return integerOf(s)
		end
		return tonumber(s)
	elseif t == "U64" then
		if not neg and cmpDec(digits, "18446744073709551615") <= 0 then
			return integerOf(s)
		end
		return tonumber(s)
	end
	return tonumber(s)
end

local function same(a: any, b: any): boolean
	if typeof(a) == "integer" and typeof(b) == "integer" then
		return not integer.lt(a, b) and not integer.gt(a, b)
	end
	return a == b
end

-- write returns the verdict and, when written, the value read back; a raise
-- is reported as its own failure, never as a refusal.
local function write(t: string, value: any, what: string): (boolean, any)
	local v: any = (B :: any)["New" .. t]()
	v.Value = value
	local ws = serialize.WriteStream.new(buffer.create(64))
	local ran, ok = pcall((B :: any)["Write" .. t], ws, v)
	if not ran then
		check(false, what .. ": the write raised (" .. tostring(ok) .. ")")
		return false, nil
	end
	if not ok then
		return false, nil
	end
	ws:flush()
	local back: any = (B :: any)["New" .. t]()
	local rs = serialize.ReadStream.new(ws:data())
	check((B :: any)["Read" .. t](rs, back), what .. ": reads back")
	return true, back.Value
end

local n = 0
for _, c in cases do
	n += 1
	local value = valueOf(c.type, c.value)
	local ok, back = write(c.type, value, c.name)
	if c.verdict == "refused" then
		check(not ok, "packet-write-bareint " .. c.name .. " refused")
	else
		check(ok, "packet-write-bareint " .. c.name .. " written")
		check(ok and same(back, value), "packet-write-bareint " .. c.name .. " reads back exactly")
	end
end
check(n > 0, "packet-write-bareint: the case list is not empty")

-- A value that is not an integer is refused at every width: a fraction, a
-- NaN, an infinity and a non-number.
local kinds = { "I8", "I16", "I32", "I64", "U8", "U16", "U32", "U64" }
local notIntegers: { [string]: any } = { fraction = 1.5, nan = 0 / 0, infinity = math.huge, text = "1" }
for _, t in kinds do
	for what, value in notIntegers do
		local ok = write(t, value, t .. " " .. what)
		check(not ok, "packet-write-bareint " .. t .. " " .. what .. " refused")
	end
end

if failed then
	error("packet-write-bareint Luau: FAILED")
end
print("packet-write-bareint Luau: " .. n .. " cases, each out-of-width and non-integer value refused, each boundary written and read back")
