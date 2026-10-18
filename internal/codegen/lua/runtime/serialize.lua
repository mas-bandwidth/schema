--[[
    serialize.lua: the entry

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    A bitpacking serializer, wire compatible with the C++ serialize library and
    serialize.go: the same values produce the same bytes. The wire format is
    STANDARD.md (vendored verbatim), format version 1.1. DESIGN.md is the
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

-- The error kinds latched on a stream (DESIGN.md (a)).
M.SerializeError = stream.SerializeError

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
