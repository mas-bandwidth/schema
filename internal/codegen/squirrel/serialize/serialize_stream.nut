// serialize_stream.nut: the three streams (DESIGN.md (g)), the skeleton every
// family card fills. A module of the closed loader (DESIGN.md (f)): vargv[0]
// is this module's exports table and vargv[1] the loader's require.
//
// What lives here, IMPLEMENTED: the reader's error latch and terminality, the
// write-misuse trap (IMPLEMENTING.md U9), the bit
// accounting (bits processed, the write capacity and read length checks), the
// stream-level operations (object, align bits, bytes processed, context), and
// the dispatch of every serialize method into its family module. What does NOT
// live here: any family's logic. A family module is required LAZILY, on the
// first call that needs it (DESIGN.md (h)), and while its file is absent the
// method raises "serialize: <method> not implemented".
local _M = vargv[0];
local require = vargv[1];

// THE REFUSAL REASONS (DESIGN.md (a), (g); STANDARD.md, Refusal reasons): the
// closed list of nine, each value spelled exactly as STANDARD.md spells it, and
// serialize.go v2's Refusal sentinels. The FIRST refusal latches on the stream
// and sticks: every later call fails with that same reason (its echoes keep
// it), consumes nothing and leaves its holder and destination untouched
// (STANDARD.md, Reader Obligations: the failure is terminal). Only reset
// clears it.
local SerializeError = {
  None = null,
  PastEnd = "past_end",
  ValueOutOfRange = "value_out_of_range",
  NonzeroPadding = "nonzero_padding",
  TrailingData = "trailing_data",
  NonFiniteFloat = "non_finite_float",
  InvalidUtf8 = "invalid_utf8",
  EmbeddedNul = "embedded_nul",
  NonCanonical = "non_canonical",
  Rejected = "rejected",
};
_M.SerializeError <- SerializeError;

// REASONS: the closed list as a set of its spellings, for fail(reason) and
// refusalName (IMPLEMENTING.md U14)
local REASONS = {};
foreach (k, v in SerializeError) if (v != null) REASONS[v] <- true;

// refusalName(reason): the family spelling of a refusal reason, "none" for no
// refusal (SerializeError.None, null), and "unknown" for anything outside the
// closed list, as the reference's serialize::refusal_name (IMPLEMENTING.md
// U14). A stream's error() already returns the spelling; this names a reason
// held anywhere, null included.
local refusalName = function(reason) {
  if (reason == null) return "none";
  if (typeof reason == "string" && reason in REASONS) return reason;
  return "unknown";
};
_M.refusalName <- refusalName;

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
  serializeBytes = "bytes", serializeString = "bytes",
};
_M.FAMILY <- FAMILY;

// arm(kind, family, method): the family's arm for this stream kind, loaded on
// first use. Absent file, or a file with no such arm: the not-implemented
// error, raised in every stream state (misuse traps and missing code raises;
// only a reader latches, DESIGN.md (g)).
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

  // _fail(kind): latch kind if nothing is latched yet; always false. Only a
  // ReadStream latches: WriteStream and MeasureStream override it with the
  // trap (IMPLEMENTING.md U9: write misuse never latches), so an arm shared
  // by the write and the measure side traps wherever it would have latched.
  function _fail(kind) {
    if (_error == null) _error = kind;
    return false;
  }

  // _arm(family, method): this stream's arm of method, or the
  // not-implemented error; every serialize method dispatches through it
  function _arm(family, method) { return arm(_kind, family, method); }

  // --- the dispatch: every serialize method of serialize.go v2.0.0's
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

  // serializeEnd(): the end of the top-level object (STANDARD.md end). On a
  // write or a measure it is a no-op returning true, emitting and costing
  // nothing, whatever the stream's state (IMPLEMENTING.md U12; the stream's
  // error() and flush still tell a failed write). ReadStream overrides it with
  // the reader's end.
  function serializeEnd() { return true; }

  // serializeObject(object): object.serialize(stream) inline, no bytes of its
  // own (STANDARD.md object). On a write or a measure, anything but true from
  // the object makes the call return false and records nothing
  // (IMPLEMENTING.md U9). ReadStream overrides it with the latch.
  function serializeObject(object) {
    return object.serialize(this) == true;
  }

  // reject(): the caller's own refusal (STANDARD.md, Refusal reasons,
  // `rejected`; IMPLEMENTING.md U2). On a write or a measure it is caller
  // misuse and traps (U9: this port has no unchecked build, so it traps in
  // every run). ReadStream overrides it with the latch.
  function reject() { _trap("reject on a write or a measure stream (a refusal is read only)"); }

  // fail(reason): a refusal decided outside the stream (IMPLEMENTING.md U14).
  // On a write or a measure it answers false, records nothing and never traps:
  // it is the path every refusal takes, so a serialize function shared by the
  // read and write sides can call it (U6, U9). ReadStream overrides it.
  function fail(reason = null) { return false; }

  // _trap(what): THE TRAP (IMPLEMENTING.md U9; DESIGN.md (g)), the one
  // write-misuse mechanism: a thrown string, "serialize: write misuse: <what>".
  // It is thrown at the call, before any bit of the operation moves, and
  // nothing is recorded on the stream.
  function _trap(what) { throw "serialize: write misuse: " + what; }
}

// TRAPPED: what each reason an arm would have latched means as write misuse,
// for the trap's text (WriteStream and MeasureStream _fail)
local TRAPPED = {
  value_out_of_range = "a value outside its range or its bit width, or a string longer than bufferSize - 1",
  non_finite_float = "a float or double value that is not finite",
  past_end = "an operation that does not fit the room left in the buffer",
};
local misuse = function(kind) {
  return kind in TRAPPED ? TRAPPED[kind] : "" + kind;
};

// WriteStream(buffer): writes into buffer, a blob whose length is a multiple
// of 8 bytes (STANDARD.md General Conventions: the stream occupies whole
// 8-byte words in the writer's buffer). STRICT (DESIGN.md (g)): exactly
// serialize.go v2.0.0's bytes.
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

  // _room(bits): true when bits more fit; an operation that does not fit the
  // room left traps, whole, before its first bit (IMPLEMENTING.md U9, U10)
  function _room(bits) {
    if (_bits + bits > _capacity) _trap(misuse(SerializeError.PastEnd));
    return true;
  }

  // _fail(kind): write misuse never latches; it traps (IMPLEMENTING.md U9)
  function _fail(kind) { _trap(misuse(kind)); }

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
  // with PastEnd (STANDARD.md Reader Obligations, Reading past the end must be
  // refused, as past_end); a latched stream is false and latches nothing new
  function _room(bits) {
    if (_error != null) return false;
    if (_bits + bits > _length) return _fail(SerializeError.PastEnd);
    return true;
  }

  // serializeEnd(): the reader's end (STANDARD.md end; serialize.go
  // ReadStream.SerializeEnd). A failed stream answers with its first reason;
  // a whole byte or more left is TrailingData, checked first, so `bits 3; end`
  // over 0D 00 is trailing_data; otherwise the remaining 0-7 bits are the
  // final byte's padding, consumed, and any set bit is NonzeroPadding. The
  // bits of the last field are never examined. An accepted end leaves the
  // stream exactly consumed: 8 * bytes bits.
  function serializeEnd() {
    if (_error != null) return false;
    local remaining = _length - _bits;
    if (remaining >= 8) return _fail(SerializeError.TrailingData);
    if (remaining > 0 && _take(remaining) != 0) return _fail(SerializeError.NonzeroPadding);
    return true;
  }

  // serializeObject(object): a latched stream does not call it. Anything but
  // true from the object fails the call, and a false that latched nothing
  // latches Rejected through the same latch, so a primitive's earlier reason
  // is kept (STANDARD.md, Refusal reasons, `rejected`; IMPLEMENTING.md U2).
  function serializeObject(object) {
    if (_error != null) return false;
    local r = object.serialize(this);
    if (r != true) return _fail(SerializeError.Rejected);
    return _error == null;
  }

  // reject(): latch Rejected through the same latch as a wire refusal, so the
  // first reason wins and the stream is terminal from here on; always false
  function reject() { return _fail(SerializeError.Rejected); }

  // fail(reason): latch reason through the same latch, so the first reason
  // wins; no reason, or anything outside the closed list, latches Rejected,
  // never none (a refused stream that reported none would be relabelled by
  // the next read) and never a raise (IMPLEMENTING.md U14, U7). Always false.
  function fail(reason = null) {
    if (typeof reason == "string" && reason in REASONS) return _fail(reason);
    return _fail(SerializeError.Rejected);
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

  // a measure has no room to run out of (IMPLEMENTING.md U6), and makes every
  // other check a write makes by the same trap, latching nothing
  function _room(bits) { return true; }
  function _fail(kind) { _trap(misuse(kind)); }
  function _put(value, bits) { _bits += bits; }
  function _putBytes(data, offset, n) { _bits += 8 * n; }
}

_M.Stream <- Stream;
_M.WriteStream <- WriteStream;
_M.ReadStream <- ReadStream;
_M.MeasureStream <- MeasureStream;
