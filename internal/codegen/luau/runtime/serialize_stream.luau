--!strict
-- serialize_stream.luau — the three streams, WriteStream, ReadStream and
-- MeasureStream (DESIGN.md (a), (g) and (h)), wire compatible with serialize
-- (STANDARD.md, format version 1.1) and serialize.go v1.15.1.
--
-- This file holds what every stream shares and no family decides: the latch,
-- terminality, the bit accounting, the fixed-width read and measure gates,
-- align, object, Continue and Until, and the DISPATCH of every other serialize
-- method into its family module. A family module (src/serialize_<name>.luau,
-- one per card, DESIGN.md (h)) is required LAZILY, on the first call that
-- needs it, so each family card creates its own file and no two cards edit
-- one file. While a family's file is absent its methods raise
-- `serialize: <name> not implemented`.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

-- THE INTEGER GATE (DESIGN.md (c)): 64-bit values are Luau's native integer.
-- A Luau without the integer type refuses here, by name, at require time,
-- never later as a wrong value.
if typeof(integer) ~= "table" then
	error("serialize: this Luau has no integer type (typeof(integer) is not table); serialize.luau needs Luau 0.741 or later with the integer library (DESIGN.md (c))")
end

export type Holder<T> = { value: T }

-- Int128 is a 128-bit value as its two's-complement halves: lo is bits 0..63
-- and hi bits 64..127, each an integer holding the half's bit pattern, the
-- wire's own order (low half first, STANDARD.md uint128). Uint128 is the same
-- pair read unsigned (DESIGN.md (c)).
export type Int128 = { lo: integer, hi: integer }

-- The error kinds latched on a stream (DESIGN.md (a)): serialize.go's
-- ErrOverflow, ErrValueOutOfRange and ErrAlign, spelled as serialize.js's
-- SerializeError codes.
local SerializeError = table.freeze({
	Overflow = "overflow",
	ValueOutOfRange = "value_out_of_range",
	Align = "align",
})

-- Raise messages for API misuse: serialize.go panics, raised naming the rule
-- (DESIGN.md (g)). Family modules raise their own with the same prefix.
local RAISE_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes"
local RAISE_NOT_AN_OBJECT = "serialize: serializeObject requires a table with a serialize method"

-- THE FAMILY LOADER (DESIGN.md (f) and (h)). The one require in the library
-- that is not a literal: its path is always "./serialize_" .. name with name
-- a module name ([a-z][a-z0-9_]*), so it can never spell ../, an @alias or
-- a bare name, and it resolves inside the unit only (Luau's require is
-- relative to the requiring file, here the unit directory). It is reached
-- through an `any` cast because luau-analyze refuses a require it cannot
-- resolve, and a family file is absent until its card lands.
local families: { [string]: any } = {}
local requireFamily: (string) -> any = require :: any

local function family(name: string): any
	local loaded = families[name]
	if loaded ~= nil then
		return loaded
	end
	if string.match(name, "^[a-z][a-z0-9_]*$") == nil then
		error("serialize: family name " .. string.format("%q", name) .. " is not a family module name", 2)
	end
	local ok, result = pcall(function()
		return requireFamily("./serialize_" .. name)
	end)
	if not ok then
		local message = tostring(result)
		if string.find(message, 'could not resolve child component "serialize_' .. name .. '"', 1, true) then
			error("serialize: " .. name .. " not implemented (src/serialize_" .. name .. ".luau is absent)", 2)
		end
		error(result, 0)
	end
	families[name] = result
	return result
end

-- 64-bit helpers for the bit accounting below (DESIGN.md (b)).
local MASK32 = 0xffffffffi
local SHIFT32 = 32i

-- ---------------------------------------------------------------------------
-- What every stream shares.

local Shared = {}

function Shared.isWriting(self: any): boolean
	return self._kind ~= "read"
end

function Shared.isReading(self: any): boolean
	return self._kind == "read"
end

-- error returns the first error latched on the stream, or nil.
function Shared.error(self: any): string?
	return self._error
end

-- ok is true while no error is latched.
function Shared.ok(self: any): boolean
	return self._error == nil
end

function Shared.bitsProcessed(self: any): number
	return self._bits
end

-- bytesProcessed is the bits processed rounded up to the next byte
-- (STANDARD.md General Conventions, Flush): after a write, the packet size.
function Shared.bytesProcessed(self: any): number
	return (self._bits + 7) // 8
end

function Shared.setContext(self: any, context: any)
	self._context = context
end

function Shared.context(self: any): any
	return self._context
end

-- THE LATCH (STANDARD.md, Reader Obligations, "Failure is terminal"; DESIGN.md
-- (g)): the first failure sticks, and every later call fails, consumes
-- nothing and writes nothing to its destination. _fail returns false so an
-- arm can `return s:_fail(kind)`.
function Shared._fail(self: any, kind: any): boolean
	if self._error == nil then
		self._error = kind
	end
	return false
end

function Shared._latched(self: any): boolean
	return self._error ~= nil
end

-- _available is the number of bits the stream can still move: the buffer's
-- bits less the bit index (STANDARD.md, Reader Obligations: the past-end test
-- is bit_index + n > 8 * bytes). A measure stream is unbounded.
function Shared._available(self: any): number
	return self._numBits - self._bits
end

-- _alignBits is (8 - (bit_index % 8)) % 8 (STANDARD.md General Conventions,
-- Bit index and alignment).
function Shared._alignBits(self: any): number
	return (8 - self._bits % 8) % 8
end

Shared.alignBits = Shared._alignBits

-- serializeObject runs object:serialize(stream), which returns true, or false
-- and optionally the caller's own error value (serialize.go Serializer). A
-- latched stream refuses before the object runs; a false latches the
-- caller's value, or "object" when it gives none (DESIGN.md (a)).
function Shared.serializeObject(self: any, object: any): boolean
	local method: any = if type(object) == "table" then object.serialize else nil
	if type(method) ~= "function" then
		error(RAISE_NOT_AN_OBJECT, 2)
	end
	if self._error ~= nil then
		return false
	end
	local ok, err = (method :: any)(object, self)
	if not ok then
		return self:_fail(if err ~= nil then err else "object")
	end
	return self._error == nil
end

-- ---------------------------------------------------------------------------
-- THE DISPATCH (DESIGN.md (h)). Each row is a stream method, its family, and
-- the arm suffix: the WriteStream method calls family.write<Suffix>(stream,
-- ...), the ReadStream method family.read<Suffix>(stream, ...) and the
-- MeasureStream method family.measure<Suffix>(stream, ...). Each arm does
-- the whole operation in serialize.go's order: raise on API misuse, refuse
-- on a latched stream, then the refusals and the bits.
local DISPATCH: { { string } } = {
	{ "serializeBits", "bits", "Bits" },
	{ "serializeBits64", "bits", "Bits64" },
	{ "serializeInt", "int", "Int" },
	{ "serializeInt64", "int", "Int64" },
	{ "serializeInt128", "int128", "Int128" },
	{ "serializeFixed64", "fixed", "Fixed64" },
	{ "serializeFixed128", "fixed", "Fixed128" },
	{ "serializeIntRelative", "int_relative", "IntRelative" },
	{ "serializeCompressedFloat", "compressed_float", "CompressedFloat" },
	{ "serializeCompressedFloatPrecomputed", "compressed_float", "CompressedFloatPrecomputed" },
	{ "serializeBytes", "bytes", "Bytes" },
	{ "serializeString", "bytes", "String" },
	{ "serializeWideString", "bytes", "WideString" },
}

-- THE FIXED WIDTHS (DESIGN.md (h)): methods with no parameters whose width is
-- the operation's own (STANDARD.md bits, bool, uint128, float, double). The
-- write arm is the family's, whole. The READ gate is the stream's: refuse on
-- a latched stream, refuse past the end (overflow), and only then call
-- family.read<Suffix>(stream, holder), which reads exactly that many bits
-- and stores the value. The MEASURE arm is the stream's alone: the width.
local FIXED: { { any } } = {
	{ "serializeBool", "bits", "Bool", 1 },
	{ "serializeUint8", "bits", "Uint8", 8 },
	{ "serializeUint16", "bits", "Uint16", 16 },
	{ "serializeUint32", "bits", "Uint32", 32 },
	{ "serializeUint64", "bits", "Uint64", 64 },
	{ "serializeFloat", "float", "Float", 32 },
	{ "serializeDouble", "float", "Double", 64 },
	{ "serializeUint128", "int128", "Uint128", 128 },
}

local function class(kind: string): any
	local c: any = {}
	c.__index = c
	for k, v in Shared :: any do
		c[k] = v
	end
	for _, row in DISPATCH do
		local method, name, arm = row[1], row[2], kind .. row[3]
		c[method] = function(self: any, ...: any): boolean
			return family(name)[arm](self, ...)
		end
	end
	return c
end

-- ---------------------------------------------------------------------------
-- WriteStream (serialize.go WriteStream; DESIGN.md (g): the writer is STRICT).

local WriteStream = class("write")

for _, row in FIXED do
	local method, name, arm = row[1], row[2], "write" .. row[3]
	WriteStream[method] = function(self: any, holder: any): boolean
		return family(name)[arm](self, holder)
	end
end

-- new creates a write stream over buffer, whose size must be a multiple of 8
-- bytes: the writer stores 8-byte words (STANDARD.md General Conventions).
function WriteStream.new(buf: buffer): any
	local self = setmetatable({ _kind = "write", _context = nil }, WriteStream)
	self:reset(buf)
	return self
end

-- reset points the stream at a buffer and clears all write state, including
-- a latched error. The context is kept.
function WriteStream.reset(self: any, buf: buffer)
	local n = buffer.len(buf)
	if n % 8 ~= 0 then
		error(RAISE_BUFFER_BYTES, 2)
	end
	self._data = buf
	self._numBits = n * 8
	self._bits = 0
	self._packer = nil
	self._error = nil
end

function WriteStream.bitsAvailable(self: any): number
	return self._numBits - self._bits
end

-- writer is the bit writer, created on the first bit the stream writes
-- (serialize_bitpacker.luau, BitWriter).
local function writer(self: any): any
	local p = self._packer
	if p == nil then
		p = family("bitpacker").BitWriter.new(self._data)
		self._packer = p
	end
	return p
end

-- _writeBits packs bits in [1,32] of value, a number already masked to bits
-- and already checked against _available (STANDARD.md bits; DESIGN.md (b)).
function WriteStream._writeBits(self: any, value: number, bits: number)
	writer(self):writeBits(value, bits)
	self._bits += bits
end

-- _writeBits64 packs bits in [1,64] of value, an integer already masked to
-- bits and already checked: for bits > 32 the low 32 bits first as a 32-bit
-- group, then the high bits - 32 (STANDARD.md bits).
function WriteStream._writeBits64(self: any, value: integer, bits: number)
	if bits <= 32 then
		self:_writeBits(integer.tonumber(value), bits)
		return
	end
	self:_writeBits(integer.tonumber(integer.band(value, MASK32)), 32)
	self:_writeBits(integer.tonumber(integer.rshift(value, SHIFT32)), bits - 32)
end

-- _writeBytes copies count bytes of src from offset into an ALIGNED stream,
-- already checked against _available (STANDARD.md bytes).
function WriteStream._writeBytes(self: any, src: buffer, offset: number, count: number)
	if count == 0 then
		return
	end
	writer(self):writeBytes(src, offset, count)
	self._bits += count * 8
end

-- serializeAlign pads with zero bits to the next byte boundary; an aligned
-- stream writes nothing (STANDARD.md align).
function WriteStream.serializeAlign(self: any): boolean
	if self._error ~= nil then
		return false
	end
	local n = self:_alignBits()
	if n == 0 then
		return true
	end
	if self._bits + n > self._numBits then
		return self:_fail(SerializeError.Overflow)
	end
	self:_writeBits(0, n)
	return true
end

-- flush writes out the partially filled scratch word (STANDARD.md General
-- Conventions, Flush). The flush ends the write.
function WriteStream.flush(self: any)
	if self._packer ~= nil then
		self._packer:flushBits()
	end
end

-- data returns the written bytes, bytesProcessed() of them, as a fresh buffer:
-- Luau buffers have no slices (DESIGN.md (d)). Call flush first.
function WriteStream.data(self: any): buffer
	local n = self:bytesProcessed()
	local out = buffer.create(n)
	if n > 0 then
		buffer.copy(out, 0, self._data, 0, n)
	end
	return out
end

-- ---------------------------------------------------------------------------
-- ReadStream (serialize.go ReadStream; DESIGN.md (g): hostile bytes never
-- raise, no read goes past the data's end, a refusal leaves its destination
-- unwritten and the stream terminal).

local ReadStream = class("read")

for _, row in FIXED do
	local method, name, arm, width = row[1], row[2], "read" .. row[3], row[4]
	ReadStream[method] = function(self: any, holder: any): boolean
		if self._error ~= nil then
			return false
		end
		if self._bits + width > self._numBits then
			return self:_fail(SerializeError.Overflow)
		end
		return family(name)[arm](self, holder)
	end
end

-- new creates a read stream over data, any length.
function ReadStream.new(data: buffer): any
	local self = setmetatable({ _kind = "read", _context = nil }, ReadStream)
	self:reset(data)
	return self
end

-- reset points the stream at data and clears all read state, including a
-- latched error (STANDARD.md, Reader Obligations: a failure persists until the
-- stream is re-initialized). The context is kept.
function ReadStream.reset(self: any, data: buffer)
	self._data = data
	self._numBits = buffer.len(data) * 8
	self._bits = 0
	self._packer = nil
	self._error = nil
end

local function reader(self: any): any
	local p = self._packer
	if p == nil then
		p = family("bitpacker").BitReader.new(self._data)
		self._packer = p
	end
	return p
end

-- _readBits reads bits in [1,32] as a number, already checked against
-- _available.
function ReadStream._readBits(self: any, bits: number): number
	local v = reader(self):readBits(bits)
	self._bits += bits
	return v
end

-- _readBits64 reads bits in [1,64] as an integer, already checked: the low 32
-- bits first, then the high bits - 32 (STANDARD.md bits).
function ReadStream._readBits64(self: any, bits: number): integer
	if bits <= 32 then
		return integer.create(self:_readBits(bits))
	end
	local lo = integer.create(self:_readBits(32))
	local hi = integer.create(self:_readBits(bits - 32))
	return integer.bor(lo, integer.lshift(hi, SHIFT32))
end

-- _readBytes copies count bytes from an ALIGNED stream into dst at offset,
-- already checked against _available.
function ReadStream._readBytes(self: any, dst: buffer, offset: number, count: number)
	if count == 0 then
		return
	end
	reader(self):readBytes(dst, offset, count)
	self._bits += count * 8
end

-- serializeAlign skips to the next byte boundary, refusing nonzero padding
-- with Align (STANDARD.md align). A latched stream refuses even when aligned
-- (STANDARD.md, Reader Obligations: zero-bit reads included).
function ReadStream.serializeAlign(self: any): boolean
	if self._error ~= nil then
		return false
	end
	local n = self:_alignBits()
	if n == 0 then
		return true
	end
	if self._bits + n > self._numBits then
		return self:_fail(SerializeError.Overflow)
	end
	if self:_readBits(n) ~= 0 then
		return self:_fail(SerializeError.Align)
	end
	return true
end

-- ---------------------------------------------------------------------------
-- MeasureStream (serialize.go MeasureStream): counts the bits a write would
-- take, writing nothing. Every align counts as the worst case 7 bits.

local MeasureStream = class("measure")

for _, row in FIXED do
	local method, width = row[1], row[4]
	MeasureStream[method] = function(self: any, _holder: any): boolean
		return self:_measure(width)
	end
end

function MeasureStream.new(): any
	local self = setmetatable({ _kind = "measure", _context = nil }, MeasureStream)
	self:reset()
	return self
end

-- reset clears the measured count and a latched error. The context is kept.
function MeasureStream.reset(self: any)
	self._numBits = math.huge
	self._bits = 0
	self._error = nil
end

-- _measure adds bits to the count, refusing on a latched stream.
function MeasureStream._measure(self: any, bits: number): boolean
	if self._error ~= nil then
		return false
	end
	self._bits += bits
	return true
end

function MeasureStream.alignBits(_self: any): number
	return 7
end

function MeasureStream.serializeAlign(self: any): boolean
	return self:_measure(7)
end

-- ---------------------------------------------------------------------------
-- Continue and Until (serialize.go): sentinel-driven loops that stop as soon
-- as the stream has an error, so they always terminate on hostile data.

local function Continue(stream: any, more: Holder<boolean>): boolean
	if not stream:serializeBool(more) then
		return false
	end
	return more.value
end

local function Until(stream: any, done: Holder<boolean>): boolean
	if not stream:serializeBool(done) then
		return false
	end
	return not done.value
end

return table.freeze({
	SerializeError = SerializeError,
	WriteStream = table.freeze(WriteStream),
	ReadStream = table.freeze(ReadStream),
	MeasureStream = table.freeze(MeasureStream),
	Continue = Continue,
	Until = Until,
	family = family,
})
