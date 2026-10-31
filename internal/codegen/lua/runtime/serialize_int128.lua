--[[
    serialize_int128.lua: the 128-bit integers (DESIGN.md (h))

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    This family owns the serializeInt128 and serializeUint128 arms and the
    two-lane Uint128/Int128 helpers serialize.go's int128.go provides (DESIGN.md
    (a), (c)). A 128-bit value is { lo = <low 64>, hi = <high 64> }, each lane
    an integer, two's complement across the pair; arithmetic wraps modulo 2^128.
    The arms follow STANDARD.md, "int128 (ranged)" and "serialize_uint128": the
    offset and span arithmetic is unsigned, so a range wider than 2^127 is exact.
]]

local stream = require("serialize_stream")

local VALUE_OUT_OF_RANGE = stream.Refusal.ValueOutOfRange
local PAST_END = stream.Refusal.PastEnd
local PANIC_MIN_MAX = "serialize: min must not be greater than max"

local math_type = math.type

local function bitlen(n)
  local c = 0
  while n > 0 do
    n = n >> 1
    c = c + 1
  end
  return c
end

local function len64(n)
  if n == 0 then return 0 end
  if n < 0 then return 64 end
  return bitlen(n)
end

--------------------------------------------------------------------------------
-- Uint128: the unsigned two-lane pair (serialize.go int128.go, Uint128).
--------------------------------------------------------------------------------

local U = {}

function U.from64(x)
  return { lo = x, hi = 0 }
end

function U.uint64(v)
  return v.lo
end

function U.int128(v)
  return { lo = v.lo, hi = v.hi }
end

function U.isZero(v)
  return v.lo == 0 and v.hi == 0
end

function U.len(v)
  if v.hi ~= 0 then return 64 + len64(v.hi) end
  return len64(v.lo)
end

function U.cmp(a, b)
  if a.hi ~= b.hi then
    if math.ult(b.hi, a.hi) then return 1 end
    return -1
  end
  if a.lo ~= b.lo then
    if math.ult(b.lo, a.lo) then return 1 end
    return -1
  end
  return 0
end

function U.add(a, b)
  local lo = a.lo + b.lo
  local carry = 0
  if math.ult(lo, a.lo) then carry = 1 end
  local hi = a.hi + b.hi + carry
  return { lo = lo & 0xffffffffffffffff, hi = hi & 0xffffffffffffffff }
end

function U.sub(a, b)
  local lo = a.lo - b.lo
  local borrow = 0
  if math.ult(a.lo, b.lo) then borrow = 1 end
  local hi = a.hi - b.hi - borrow
  return { lo = lo & 0xffffffffffffffff, hi = hi & 0xffffffffffffffff }
end

-- mul64x64(a, b): the exact 128-bit product of two uint64 patterns, as low,
-- high (serialize.go uses math/bits.Mul64).
local function mul64x64(a, b)
  local a0 = a & 0xffff; local a1 = (a >> 16) & 0xffff
  local a2 = (a >> 32) & 0xffff; local a3 = (a >> 48) & 0xffff
  local b0 = b & 0xffff; local b1 = (b >> 16) & 0xffff
  local b2 = (b >> 32) & 0xffff; local b3 = (b >> 48) & 0xffff
  local c0 = a0 * b0
  local c1 = a0 * b1 + a1 * b0
  local c2 = a0 * b2 + a1 * b1 + a2 * b0
  local c3 = a0 * b3 + a1 * b2 + a2 * b1 + a3 * b0
  local c4 = a1 * b3 + a2 * b2 + a3 * b1
  local c5 = a2 * b3 + a3 * b2
  local c6 = a3 * b3
  c1 = c1 + (c0 >> 16); c0 = c0 & 0xffff
  c2 = c2 + (c1 >> 16); c1 = c1 & 0xffff
  c3 = c3 + (c2 >> 16); c2 = c2 & 0xffff
  c4 = c4 + (c3 >> 16); c3 = c3 & 0xffff
  c5 = c5 + (c4 >> 16); c4 = c4 & 0xffff
  c6 = c6 + (c5 >> 16); c5 = c5 & 0xffff
  local c7 = c6 >> 16; c6 = c6 & 0xffff
  local lo = c0 | (c1 << 16) | (c2 << 32) | (c3 << 48)
  local hi = c4 | (c5 << 16) | (c6 << 32) | (c7 << 48)
  return lo, hi
end

function U.mul(a, b)
  local lo, hi = mul64x64(a.lo, b.lo)
  hi = hi + a.lo * b.hi + a.hi * b.lo
  return { lo = lo, hi = hi & 0xffffffffffffffff }
end

function U.lsh(v, n)
  n = n % 128
  if n == 0 then return { lo = v.lo, hi = v.hi } end
  if n < 64 then
    return {
      lo = (v.lo << n) & 0xffffffffffffffff,
      hi = ((v.hi << n) | (v.lo >> (64 - n))) & 0xffffffffffffffff,
    }
  end
  return { lo = 0, hi = (v.lo << (n - 64)) & 0xffffffffffffffff }
end

function U.rsh(v, n)
  n = n % 128
  if n == 0 then return { lo = v.lo, hi = v.hi } end
  if n < 64 then
    return {
      lo = ((v.lo >> n) | (v.hi << (64 - n))) & 0xffffffffffffffff,
      hi = (v.hi >> n) & 0xffffffffffffffff,
    }
  end
  return { lo = (v.hi >> (n - 64)) & 0xffffffffffffffff, hi = 0 }
end

function U.bnot(v)
  return { lo = ~v.lo, hi = ~v.hi }
end

function U.band(a, b)
  return { lo = a.lo & b.lo, hi = a.hi & b.hi }
end

function U.bor(a, b)
  return { lo = a.lo | b.lo, hi = a.hi | b.hi }
end

function U.bxor(a, b)
  return { lo = a.lo ~ b.lo, hi = a.hi ~ b.hi }
end

function U.neg(v)
  return U.sub({ lo = 0, hi = 0 }, v)
end

function U.divMod(x, y)
  if U.isZero(y) then return { lo = 0, hi = 0 }, { lo = 0, hi = 0 } end
  local quotient = { lo = 0, hi = 0 }
  local remainder = { lo = 0, hi = 0 }
  local one = { lo = 1, hi = 0 }
  for i = U.len(x) - 1, 0, -1 do
    remainder = U.lsh(remainder, 1)
    remainder.lo = remainder.lo | (U.rsh(x, i).lo & 1)
    if U.cmp(remainder, y) >= 0 then
      remainder = U.sub(remainder, y)
      quotient = U.bor(quotient, U.lsh(one, i))
    end
  end
  return quotient, remainder
end

function U.div(x, y)
  local q = U.divMod(x, y)
  return q
end

function U.mod(x, y)
  local _, r = U.divMod(x, y)
  return r
end

--------------------------------------------------------------------------------
-- Int128: the signed two-lane pair (serialize.go int128.go, Int128).
--------------------------------------------------------------------------------

local I = {}

function I.from64(x)
  local hi = 0
  if x < 0 then hi = 0xffffffffffffffff end
  return { lo = x, hi = hi }
end

function I.int64(v)
  return v.lo
end

function I.uint128(v)
  return { lo = v.lo, hi = v.hi }
end

function I.isNegative(v)
  return (v.hi >> 63) ~= 0
end

function I.cmp(a, b)
  if a.hi ~= b.hi then
    if a.hi < b.hi then return -1 end
    return 1
  end
  if a.lo ~= b.lo then
    if math.ult(a.lo, b.lo) then return -1 end
    return 1
  end
  return 0
end

function I.add(a, b)
  local r = U.add(a, b)
  return { lo = r.lo, hi = r.hi }
end

function I.sub(a, b)
  local r = U.sub(a, b)
  return { lo = r.lo, hi = r.hi }
end

function I.mul(a, b)
  local r = U.mul(a, b)
  return { lo = r.lo, hi = r.hi }
end

function I.divMod(x, y)
  local q, r = U.divMod(I.uint128(x), I.uint128(y))
  if I.isNegative(x) ~= I.isNegative(y) then
    q = U.neg(q)
  end
  if I.isNegative(x) then
    r = U.neg(r)
  end
  return { lo = q.lo, hi = q.hi }, { lo = r.lo, hi = r.hi }
end

function I.div(x, y)
  local q = I.divMod(x, y)
  return q
end

function I.mod(x, y)
  local _, r = I.divMod(x, y)
  return r
end

function I.neg(v)
  local r = U.neg(v)
  return { lo = r.lo, hi = r.hi }
end

function I.bnot(v)
  return { lo = ~v.lo, hi = ~v.hi }
end

function I.band(a, b)
  return { lo = a.lo & b.lo, hi = a.hi & b.hi }
end

function I.bor(a, b)
  return { lo = a.lo | b.lo, hi = a.hi | b.hi }
end

function I.bxor(a, b)
  return { lo = a.lo ~ b.lo, hi = a.hi ~ b.hi }
end

function I.lsh(v, n)
  local r = U.lsh(v, n)
  return { lo = r.lo, hi = r.hi }
end

function I.rsh(v, n)
  if n >= 128 then
    if I.isNegative(v) then
      return { lo = 0xffffffffffffffff, hi = 0xffffffffffffffff }
    end
    return { lo = 0, hi = 0 }
  end
  local r = U.rsh(v, n)
  if I.isNegative(v) and n > 0 then
    local ones = U.lsh({ lo = 0xffffffffffffffff, hi = 0xffffffffffffffff }, 128 - n)
    r = U.bor(r, ones)
  end
  return { lo = r.lo, hi = r.hi }
end

--------------------------------------------------------------------------------
-- bitsRequired128 and the arms.
--------------------------------------------------------------------------------

local function bitsRequired128(min, max)
  if min.lo == max.lo and min.hi == max.hi then return 0 end
  return U.len(U.sub(max, min))
end

local function checkPair(v)
  return type(v) == "table"
    and math_type(v.lo) == "integer" and math_type(v.hi) == "integer"
end

local function pairMisuse(name)
  error("serialize: " .. name .. " takes a { lo, hi } value", 0)
end

local function writeGroups128(s, offset, numBits)
  if numBits <= 64 then
    return s:_writeBits(offset.lo, numBits)
  end
  if not s:_writeBits(offset.lo, 64) then return false end
  return s:_writeBits(offset.hi, numBits - 64)
end

local function readGroups128(s, numBits)
  if numBits <= 64 then
    local v = s:_readBits(numBits)
    if v == nil then return nil end
    return { lo = v, hi = 0 }
  end
  local lo = s:_readBits(64)
  if lo == nil then return nil end
  local hi = s:_readBits(numBits - 64)
  if hi == nil then return nil end
  return { lo = lo, hi = hi }
end

local int128 = {
  -- A declaration check returns the rule's message, or nil (serialize_stream.lua,
  -- M.method; IMPLEMENTING.md U16).
  check = function(min, max)
    if not (checkPair(min) and checkPair(max)) then
      return "serialize: serializeInt128 takes a { lo, hi } value"
    end
    if I.cmp(min, max) > 0 then return PANIC_MIN_MAX end
    return nil
  end,
  checkValue = function(value)
    if not checkPair(value) then pairMisuse("serializeInt128") end
  end,
  -- STANDARD.md, int128 (ranged): the offset value - min in the unsigned
  -- 128-bit domain, in bits_required128(min, max) bits, split into 32-bit
  -- groups from least significant upward (serialize.go WriteStream.SerializeInt128).
  write = function(s, value, min, max)
    if I.cmp(value, min) < 0 or I.cmp(value, max) > 0 then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    local numBits = bitsRequired128(min, max)
    if numBits == 0 then return true end
    if s._bits + numBits > s._numBits then
      return s:_fail(PAST_END)
    end
    local offset = U.sub(value, min)
    return writeGroups128(s, offset, numBits)
  end,
  -- STANDARD.md, int128 (ranged): the reader checks the decoded offset against
  -- max - min in the unsigned domain before forming the value — reject, never
  -- clamp (serialize.go ReadStream.SerializeInt128).
  read = function(s, min, max)
    local numBits = bitsRequired128(min, max)
    if numBits == 0 then return true, { lo = min.lo, hi = min.hi } end
    local offset = readGroups128(s, numBits)
    if offset == nil then return false end
    local span = U.sub(max, min)
    if U.cmp(offset, span) > 0 then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    local value = U.add(offset, min)
    return true, { lo = value.lo, hi = value.hi }
  end,
  measure = function(s, value, min, max)
    if I.cmp(value, min) < 0 or I.cmp(value, max) > 0 then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    return s:_measure(bitsRequired128(min, max))
  end,
}

local uint128 = {
  checkValue = function(value)
    if not checkPair(value) then pairMisuse("serializeUint128") end
  end,
  -- STANDARD.md, serialize_uint128: the low 64-bit half first, then the high
  -- half, each half as 64 bits (serialize.go WriteStream.SerializeUint128).
  write = function(s, value)
    if s._bits + 128 > s._numBits then
      return s:_fail(PAST_END)
    end
    if not s:_writeBits(value.lo, 64) then return false end
    return s:_writeBits(value.hi, 64)
  end,
  read = function(s)
    local lo = s:_readBits(64)
    if lo == nil then return false end
    local hi = s:_readBits(64)
    if hi == nil then return false end
    return true, { lo = lo, hi = hi }
  end,
  measure = function(s, _value)
    return s:_measure(128)
  end,
}

local M = {}
M.methods = { serializeInt128 = int128, serializeUint128 = uint128 }
M.api = {
  bitsRequired128 = function(min, max) return bitsRequired128(min, max) end,
  Uint128 = {
    from64 = U.from64,
    uint64 = U.uint64,
    int128 = U.int128,
    isZero = U.isZero,
    len = U.len,
    cmp = U.cmp,
    add = U.add,
    sub = U.sub,
    mul = U.mul,
    divMod = U.divMod,
    div = U.div,
    mod = U.mod,
    neg = U.neg,
    bnot = U.bnot,
    band = U.band,
    bor = U.bor,
    bxor = U.bxor,
    lsh = U.lsh,
    rsh = U.rsh,
  },
  Int128 = {
    from64 = I.from64,
    int64 = I.int64,
    uint128 = I.uint128,
    isNegative = I.isNegative,
    cmp = I.cmp,
    add = I.add,
    sub = I.sub,
    mul = I.mul,
    divMod = I.divMod,
    div = I.div,
    mod = I.mod,
    neg = I.neg,
    bnot = I.bnot,
    band = I.band,
    bor = I.bor,
    bxor = I.bxor,
    lsh = I.lsh,
    rsh = I.rsh,
  },
}
return M
