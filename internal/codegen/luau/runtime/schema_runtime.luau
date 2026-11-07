--!strict
-- schema_runtime.luau — the Luau target's shared runtime (docs/PORT-LUAU.md,
-- THE RUNTIME LAYOUT). Emitted beside every generated module and required as
-- require("./schema_runtime"). It holds what every surface shares: the
-- integer gate, the Int128 pair and its writer-contract checks, and the
-- array helper. The type wire's codec is the vendored serialize.luau beside
-- it (serialize*.luau).
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

-- THE INTEGER GATE (docs/PORT-LUAU.md, 64-BIT AND 128-BIT INTEGERS): a 64-bit
-- field stores as Luau's native integer. A Luau without the integer type
-- (an older release, or a host that has not enabled it) refuses here, by
-- name, at require time — never later as a wrong value.
if typeof(integer) ~= "table" then
	error("schema: this Luau has no integer type (typeof(integer) is not table); the schema Luau target needs Luau 0.741 or later with the integer library (docs/PORT-LUAU.md)")
end

-- Int128 is a 128-bit value as its two's-complement halves: lo is bits 0..63
-- and hi bits 64..127, each an integer holding the half's bit pattern. It is
-- the wire's own order (low half first, docs/SPEC-TABLES.md §3).
export type Int128 = { lo: integer, hi: integer }

local R = {}

-- int128 builds an Int128 from its two halves.
function R.int128(lo: integer, hi: integer): Int128
	return { lo = lo, hi = hi }
end

-- isInt128 is true when x is an Int128 pair: a table whose lo and hi are
-- integers. A generated writer asks it before a 128-bit value rides, so a
-- value of the wrong shape is a refused write, never a runtime raise.
function R.isInt128(x: any): boolean
	return type(x) == "table" and typeof(x.lo) == "integer" and typeof(x.hi) == "integer"
end

-- compare128 orders two Int128 pairs: -1, 0 or 1. The high halves compare
-- signed (or unsigned), the low halves always unsigned.
local function compare128(a: Int128, b: Int128, unsigned: boolean): number
	if a.hi ~= b.hi then
		local less = if unsigned then integer.ult(a.hi, b.hi) else integer.lt(a.hi, b.hi)
		return if less then -1 else 1
	end
	if a.lo == b.lo then
		return 0
	end
	return if integer.ult(a.lo, b.lo) then -1 else 1
end

-- int128Within is true when x is an Int128 pair in [lo, hi], compared as
-- signed 128-bit values, or unsigned when unsigned is set: the writer
-- contract of a ranged 128-bit field (SPEC §5).
function R.int128Within(x: any, lo: Int128, hi: Int128, unsigned: boolean): boolean
	if not R.isInt128(x) then
		return false
	end
	return compare128(x, lo, unsigned) >= 0 and compare128(x, hi, unsigned) <= 0
end

-- array builds a fixed-size array of fresh elements (1-based, n of them):
-- the storage principle, every element exists at construction (SPEC §6.1).
function R.array<T>(n: number, make: () -> T): { T }
	local a = table.create(n)
	for i = 1, n do
		a[i] = make()
	end
	return a
end

local f32scratch = buffer.create(4)

-- fround rounds a number to the nearest float32, the way a float32 field's
-- value is stored before it is written (docs/PORT-LUAU.md, BYTE BUFFERS).
function R.fround(x: number): number
	buffer.writef32(f32scratch, 0, x)
	return buffer.readf32(f32scratch, 0)
end

return table.freeze(R)
