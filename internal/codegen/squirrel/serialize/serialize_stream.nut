// serialize_stream.nut: the three streams (DESIGN.md (g)), the skeleton every
// family card fills. A module of the closed loader (DESIGN.md (f)): vargv[0]
// is this module's exports table and vargv[1] the loader's require.
//
// What lives here, IMPLEMENTED: the error latch and terminality, the bit
// accounting (bits processed, the write capacity and read length checks), the
// stream-level operations (object, align bits, bytes processed, context), and
// the dispatch of every serialize method into its family module. What does NOT
// live here: any family's logic. A family module is required LAZILY, on the
// first call that needs it (DESIGN.md (h)), and while its file is absent the
// method raises "serialize: <method> not implemented".
local _M = vargv[0];
local require = vargv[1];

// THE ERROR KINDS (DESIGN.md (a), (g)): serialize.go's ErrOverflow,
// ErrValueOutOfRange and ErrAlign. The first error latches on the stream and
// sticks: every later call fails, consumes nothing and leaves its holder and
// destination untouched (STANDARD.md, Reader Obligations: the failure is
// terminal). Only reset clears it.
local SerializeError = {
  None = null,
  Overflow = "overflow",
  ValueOutOfRange = "value_out_of_range",
  Align = "align",
};
_M.SerializeError <- SerializeError;

// the family of every serialize method (DESIGN.md (h)): the method's arm lives
// in src/serialize_<family>.nut as <module>.write / .read / .measure [method]
local FAMILY = {
  serializeBits = "bits", serializeBits64 = "bits", serializeBool = "bits",
  serializeUint8 = "bits", serializeUint16 = "bits", serializeUint32 = "bits",
  serializeUint64 = "bits", serializeAlign = "bits",
  serializeInt = "int", serializeInt64 = "int",
  serializeInt128 = "int128", serializeUint128 = "int128",
  serializeFixed64 = "fixed", serializeFixed128 = "fixed",
  serializeIntRelative = "int_relative",
  serializeFloat = "float", serializeDouble = "float",
  serializeCompressedFloat = "compressed_float",
  serializeCompressedFloatPrecomputed = "compressed_float",
  serializeBytes = "bytes", serializeString = "bytes", serializeWideString = "bytes",
};
_M.FAMILY <- FAMILY;

// arm(kind, family, method): the family's arm for this stream kind, loaded on
// first use. Absent file, or a file with no such arm: the not-implemented
// error, raised in every stream state (API misuse and missing code raise; only
// data latches, DESIGN.md (g)).
local arm = function(kind, family, method) {
  local m = require("serialize_" + family, method);
  if (!(kind in m) || !(method in m[kind])) {
    throw "serialize: " + method + " not implemented (serialize_" + family + ".nut has no " + kind + " arm)";
  }
  return m[kind][method];
};

// a blob reports typeof "blob" (its _typeof), and is an instance of ::blob
local isBlob = function(b) { return typeof b == "blob" && b instanceof ::blob; };

// The surface every stream shares. A serialize method returns true on success
// and false on failure (the latched kind is error()); a holder is a table
// { value = ... }, consumed on write, assigned on a successful read and left
// unmodified on a failed one, ignored on measure (DESIGN.md (a)).
class Stream {
  _kind = null;     // "write", "read" or "measure": the arm table a call dispatches to
  _error = null;    // the latched SerializeError kind, null while healthy
  _bits = 0;        // bits written, read or measured (the bit index, STANDARD.md General Conventions)
  _context = null;

  function isWriting() { return _kind != "read"; }
  function isReading() { return _kind == "read"; }
  function error() { return _error; }
  function ok() { return _error == null; }
  function setContext(c) { _context = c; }
  function context() { return _context; }

  // the bit index and the meaningful length, rounded up to a byte (STANDARD.md
  // General Conventions)
  function bitsProcessed() { return _bits; }
  function bytesProcessed() { return (_bits + 7) / 8; }

  // (8 - (bit_index % 8)) % 8 (STANDARD.md General Conventions, Bit index and
  // alignment); MeasureStream overrides it with the worst case, 7
  function alignBits() { return (8 - _bits % 8) % 8; }

  // --- the primitives the family arms use (DESIGN.md (h)) ---------------

  // _fail(kind): latch kind if nothing is latched yet; always false
  function _fail(kind) {
    if (_error == null) _error = kind;
    return false;
  }

  // _arm(family, method): this stream's arm of method, or the
  // not-implemented error; every serialize method dispatches through it
  function _arm(family, method) { return arm(_kind, family, method); }

  // --- the dispatch: every serialize method of serialize.go v1.15.1's
  // Stream, in serialize.js's spelling (DESIGN.md (a)) -------------------
  function serializeBits(h, bits) { return _arm("bits", "serializeBits")(this, h, bits); }
  function serializeBits64(h, bits) { return _arm("bits", "serializeBits64")(this, h, bits); }
  function serializeBool(h) { return _arm("bits", "serializeBool")(this, h); }
  function serializeUint8(h) { return _arm("bits", "serializeUint8")(this, h); }
  function serializeUint16(h) { return _arm("bits", "serializeUint16")(this, h); }
  function serializeUint32(h) { return _arm("bits", "serializeUint32")(this, h); }
  function serializeUint64(h) { return _arm("bits", "serializeUint64")(this, h); }
  function serializeAlign() { return _arm("bits", "serializeAlign")(this); }
  function serializeInt(h, min, max) { return _arm("int", "serializeInt")(this, h, min, max); }
  function serializeInt64(h, min, max) { return _arm("int", "serializeInt64")(this, h, min, max); }
  function serializeInt128(h, min, max) { return _arm("int128", "serializeInt128")(this, h, min, max); }
  function serializeUint128(h) { return _arm("int128", "serializeUint128")(this, h); }
  function serializeFixed64(h, integerBits, fractionBits, min, max) {
    return _arm("fixed", "serializeFixed64")(this, h, integerBits, fractionBits, min, max);
  }
  function serializeFixed128(h, integerBits, fractionBits, min, max) {
    return _arm("fixed", "serializeFixed128")(this, h, integerBits, fractionBits, min, max);
  }
  function serializeIntRelative(previous, h) {
    return _arm("int_relative", "serializeIntRelative")(this, previous, h);
  }
  function serializeFloat(h) { return _arm("float", "serializeFloat")(this, h); }
  function serializeDouble(h) { return _arm("float", "serializeDouble")(this, h); }
  function serializeCompressedFloat(h, min, max, resolution) {
    return _arm("compressed_float", "serializeCompressedFloat")(this, h, min, max, resolution);
  }
  function serializeCompressedFloatPrecomputed(h, maxIntegerValue, bits, delta, min) {
    return _arm("compressed_float", "serializeCompressedFloatPrecomputed")(this, h, maxIntegerValue, bits, delta, min);
  }
  function serializeBytes(data) { return _arm("bytes", "serializeBytes")(this, data); }
  function serializeString(h, bufferSize) { return _arm("bytes", "serializeString")(this, h, bufferSize); }
  function serializeWideString(h, bufferSize) { return _arm("bytes", "serializeWideString")(this, h, bufferSize); }

  // serializeObject(object): object.serialize(stream) inline, no bytes of its
  // own (STANDARD.md object). A latched stream does not call it. Anything but
  // true from the object fails the call; serialize.go latches the object's
  // own error, and a Squirrel serializer returns a boolean, so a false that
  // latched nothing latches ValueOutOfRange: the object refused its own value
  // (DESIGN.md (a), the one deviation of the error model).
  function serializeObject(object) {
    if (_error != null) return false;
    local r = object.serialize(this);
    if (r != true) return _fail(SerializeError.ValueOutOfRange);
    return _error == null;
  }
}

// WriteStream(buffer): writes into buffer, a blob whose length is a multiple
// of 8 bytes (STANDARD.md General Conventions: the stream occupies whole
// 8-byte words in the writer's buffer). STRICT (DESIGN.md (g)): exactly
// serialize.go v1.15.1's bytes.
class WriteStream extends Stream {
  _buffer = null;
  _capacity = 0;    // bits the buffer holds
  _writer = null;   // the BitWriter, attached on the first bit written

  constructor(buffer) {
    _kind = "write";
    reset(buffer);
  }

  function reset(buffer) {
    if (!isBlob(buffer) || buffer.len() % 8 != 0) {
      throw "serialize: bit writer buffer size must be a multiple of 8 bytes (a blob; serialize.go panicBufferBytes)";
    }
    _buffer = buffer;
    _capacity = buffer.len() * 8;
    _writer = null;
    _bits = 0;
    _error = null;
  }

  // _room(bits): true when bits more fit; a full buffer latches Overflow
  // (serialize.go WriteStream: a message that does not fit is a runtime
  // condition); a latched stream is false and latches nothing new
  function _room(bits) {
    if (_error != null) return false;
    if (_bits + bits > _capacity) return _fail(SerializeError.Overflow);
    return true;
  }

  // _put(value, bits): one group of bits in [1,32], after _room
  function _put(value, bits) {
    if (_writer == null) _writer = require("serialize_bitpacker", "BitWriter").BitWriter(_buffer);
    _writer.writeBits(value, bits);
    _bits += bits;
  }

  // _putBytes(data, offset, n): n bytes of blob data from offset, after
  // _room(8 * n), on a byte-aligned stream (STANDARD.md bytes)
  function _putBytes(data, offset, n) {
    if (_bits % 8 != 0) throw "serialize: byte array serialization requires byte alignment (serialize.go panicNotAligned)";
    if (_writer == null) _writer = require("serialize_bitpacker", "BitWriter").BitWriter(_buffer);
    _writer.writeBytes(data, offset, n);
    _bits += 8 * n;
  }

  // flush(): write out the partially filled scratch word (STANDARD.md General
  // Conventions, Flush); the flush ends the write
  function flush() {
    if (_writer != null) _writer.flushBits();
  }

  // data(): the written bytes, bytesProcessed() of them, after flush()
  function data() {
    if (_writer == null) return ::blob(0);
    return _writer.data();
  }
}

// ReadStream(data): reads the blob data. Hostile bytes never raise, and no
// read goes past the data's end (STANDARD.md Reader Obligations).
class ReadStream extends Stream {
  _data = null;
  _length = 0;      // bits the data holds: 8 * bytes
  _reader = null;   // the BitReader, attached on the first bit read

  constructor(data) {
    _kind = "read";
    reset(data);
  }

  function reset(data) {
    if (!isBlob(data)) throw "serialize: a read stream reads a blob";
    _data = data;
    _length = data.len() * 8;
    _reader = null;
    _bits = 0;
    _error = null;
  }

  // _room(bits): the reader's test, bit_index + n > 8 * bytes fails the read
  // with Overflow (STANDARD.md Reader Obligations, Reading past the end must
  // fail); a latched stream is false and latches nothing new
  function _room(bits) {
    if (_error != null) return false;
    if (_bits + bits > _length) return _fail(SerializeError.Overflow);
    return true;
  }

  // _take(bits): one group of bits in [1,32], after _room
  function _take(bits) {
    if (_reader == null) _reader = require("serialize_bitpacker", "BitReader").BitReader(_data);
    local v = _reader.readBits(bits);
    _bits += bits;
    return v;
  }

  // _takeBytes(dest, offset, n): n bytes into blob dest from offset, after
  // _room(8 * n), on a byte-aligned stream
  function _takeBytes(dest, offset, n) {
    if (_bits % 8 != 0) throw "serialize: byte array serialization requires byte alignment (serialize.go panicNotAligned)";
    if (_reader == null) _reader = require("serialize_bitpacker", "BitReader").BitReader(_data);
    _reader.readBytes(dest, offset, n);
    _bits += 8 * n;
  }
}

// MeasureStream(): a conservative bound, never bytes (STANDARD.md The Measure
// Stream): exact width for every operation but the alignment-performing ones,
// which charge the worst case of 7 bits, and it refuses nothing at runtime.
class MeasureStream extends Stream {
  constructor() {
    _kind = "measure";
    reset();
  }

  function reset() {
    _bits = 0;
    _error = null;
  }

  function alignBits() { return 7; }

  function _room(bits) { return _error == null; }
  function _put(value, bits) { _bits += bits; }
  function _putBytes(data, offset, n) { _bits += 8 * n; }
}

_M.Stream <- Stream;
_M.WriteStream <- WriteStream;
_M.ReadStream <- ReadStream;
_M.MeasureStream <- MeasureStream;
