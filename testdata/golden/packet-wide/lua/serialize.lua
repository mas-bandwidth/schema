--[[
    serialize.lua: the entry

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    A bitpacking serializer, wire compatible with the C++ serialize library and
    serialize.go: the same values produce the same bytes. The wire format is
    STANDARD.md (vendored verbatim), format version 2.0. DESIGN.md is the
    design; its section (a) lists every name this table carries.

        local serialize = require("serialize")

    Loading (DESIGN.md (f)): every file of this library is required by its
    basename on the consumer's package.path; nothing is global and no native
    module is loaded. Family functions resolve LAZILY: until a family's file
    exists, calling one raises "serialize: <name> not implemented".
]]

local stream = require("serialize_stream")
local lazy = stream.lazy

local M = {}

-- The refusal reasons a stream latches (STANDARD.md, "Refusal reasons").
M.Refusal = stream.Refusal

-- The refusal-name function (IMPLEMENTING.md U14): nil is "none", a reason
-- from the closed list is its own name.
M.refusalName = stream.refusalName

-- The three streams (DESIGN.md (a), (g)): serialize_stream.lua.
M.WriteStream = stream.WriteStream
M.ReadStream = stream.ReadStream
M.MeasureStream = stream.MeasureStream

-- The bitpacker (serialize_bitpacker.lua, card serialize-lua-bitpacker).
M.BitWriter = { new = lazy("bitpacker", "new", "BitWriter", "BitWriter.new") }
M.BitReader = { new = lazy("bitpacker", "new", "BitReader", "BitReader.new") }

-- serialize_bits.lua (card serialize-lua-bits).
M.bitsRequired = lazy("bits", "bitsRequired")
M.bitsRequired64 = lazy("bits", "bitsRequired64")
M.signedToUnsigned = lazy("bits", "signedToUnsigned")
M.unsignedToSigned = lazy("bits", "unsignedToSigned")

-- serialize_int128.lua (card serialize-lua-int).
M.bitsRequired128 = lazy("int128", "bitsRequired128")

-- serialize_compressed_float.lua (card serialize-lua-compressed-float).
M.compressedFloatParams = lazy("compressed_float", "compressedFloatParams")

-- The 128-bit helpers: serialize.go's Uint128 and Int128 methods, as functions
-- over { lo = <low 64>, hi = <high 64> } (DESIGN.md (a), (c)).
local UINT128 = {
  "from64", "uint64", "int128", "isZero", "len", "cmp", "add", "sub", "mul",
  "divMod", "div", "mod", "neg", "bnot", "band", "bor", "bxor", "lsh", "rsh",
}
local INT128 = {
  "from64", "int64", "uint128", "isNegative", "cmp", "add", "sub", "mul",
  "divMod", "div", "mod", "neg", "bnot", "band", "bor", "bxor", "lsh", "rsh",
}
M.Uint128 = {}
for _, name in ipairs(UINT128) do
  M.Uint128[name] = lazy("int128", name, "Uint128", "Uint128." .. name)
end
M.Int128 = {}
for _, name in ipairs(INT128) do
  M.Int128[name] = lazy("int128", name, "Int128", "Int128." .. name)
end

-- serialize_copy_string (IMPLEMENTING.md U15): copyString(source, destSize)
-- returns the bounded copy of source for a string field whose buffer_size is
-- destSize. Lua strings are byte strings, so the rule is applied to source's
-- bytes whatever they hold: the bytes before the first NUL; if more than
-- destSize - 1, the first destSize - 1 of them, then bytes dropped from the end
-- while the first byte left out is a continuation byte (10xxxxxx). For
-- well-formed UTF-8 that is the longest prefix that fits and does not end
-- inside a character, which serializeString with bufferSize = destSize
-- accepts. destSize below 1 (or not an integer), and a source that is not a
-- string, are write misuse: the trap, error() (U9).
function M.copyString(source, destSize)
  if type(source) ~= "string" then
    error("serialize: copyString takes a string source (IMPLEMENTING.md U9, U15)", 2)
  end
  if math.type(destSize) ~= "integer" or destSize < 1 then
    error("serialize: copyString destSize must be an integer >= 1 (IMPLEMENTING.md U9, U15)", 2)
  end
  local nul = string.find(source, "\0", 1, true)
  local n = nul and nul - 1 or #source
  if n <= destSize - 1 then
    return string.sub(source, 1, n)
  end
  n = destSize - 1
  while n > 0 and (string.byte(source, n + 1) & 0xC0) == 0x80 do
    n = n - 1
  end
  return string.sub(source, 1, n)
end

-- serialize.go Continue: one continuation bit; false as soon as the stream has
-- failed, so a sentinel-driven loop always terminates on hostile data.
function M.Continue(s, more)
  if not s:serializeBool(more) then
    return false
  end
  return more.value == true
end

-- serialize.go Until: the inverse polarity, a termination bit.
function M.Until(s, done)
  if not s:serializeBool(done) then
    return false
  end
  return done.value ~= true
end

return M
