// serialize_bitpacker.nut: the bit writer and bit reader, the wire's bottom
// layer and the family card of serialize-squirrel-bitpacker (DESIGN.md (h)).
// A module of the closed loader (DESIGN.md (f)): vargv[0] is this module's
// exports table and vargv[1] the loader's require. It is a pure port of
// serialize.go v2.0.0's bitpacker.go: the same bytes, bit for bit.
//
// THE WIRE (STANDARD.md, General Conventions; DESIGN.md (b)). Bits are packed
// least-significant-bit first into a 64-bit scratch word. A value crossing the
// word boundary is split: its low part completes the current word and its
// remainder begins the next. Each full word is stored as 8 little-endian bytes,
// composed by hand because readn/writen are the host's order (DESIGN.md (d)).
// The final partial word is flushed; the meaningful length is the bits written
// rounded up to a byte, and the bits past it are zero.
//
// THE NUMBER MODEL (DESIGN.md (c)). The scratch is two 32-bit lanes rather
// than one signed SQInteger, so no shift ever overflows a signed 64-bit value:
// every shift count stays in [0,31] and every intermediate stays under 2^63.
// The bytes emitted are the same as serialize.go's 64-bit scratch.
local _M = vargv[0];
local require = vargv[1];

// panic text, verbatim from serialize.go's serialize.go (DESIGN.md (g): API
// misuse raises serialize.go's panic text).
local PANIC_BITS_RANGE = "serialize: bits must be in [1,32]";
local PANIC_WRITE_OVERFLOW = "serialize: bit writer overflow";
local PANIC_READ_OVERFLOW = "serialize: bit reader would read past the end of the buffer";
local PANIC_NOT_ALIGNED = "serialize: byte array serialization requires byte alignment";
local PANIC_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes";

class BitWriter {
  _buffer = null;     // the caller's blob, written in place
  _numBits = 0;       // the buffer's capacity in bits
  _bitsWritten = 0;
  _wordIndex = 0;     // the next 8-byte word goes at _wordIndex * 8
  _scratchBits = 0;   // bits held in the scratch, in [0,63]
  _lo = 0;            // the scratch's low 32 bits
  _hi = 0;            // the scratch's high 32 bits

  // BitWriter(buffer): a writer over buffer, whose length is a multiple of 8
  // bytes (STANDARD.md, General Conventions: the stream occupies whole 8-byte
  // words in the writer's buffer; serialize.go panicBufferBytes).
  constructor(buffer) {
    if (typeof buffer != "blob" || buffer.len() % 8 != 0) throw PANIC_BUFFER_BYTES;
    _buffer = buffer;
    _numBits = buffer.len() * 8;
  }

  // writeBits(value, bits): the low `bits` bits of value, bits in [1,32]
  // (DESIGN.md (h); serialize.go panicBitsRange). Bits above the width are
  // ignored, as serialize.go masks them. A past-end write is API misuse here
  // and raises, exactly as Go's BitWriter does (panicWriteOverflow).
  function writeBits(value, bits) {
    if (bits < 1 || bits > 32) throw PANIC_BITS_RANGE;
    if (_bitsWritten + bits > _numBits) throw PANIC_WRITE_OVERFLOW;
    _writeBits(value, bits);
  }

  // _writeBits is writeBits after its checks: place `bits` low bits of value
  // at the current scratch position, LSB first, splitting across the scratch
  // word and the next one when it crosses bit 64 (STANDARD.md, General
  // Conventions; DESIGN.md (b)).
  function _writeBits(value, bits) {
    value = value & (bits == 32 ? 0xffffffff : (1 << bits) - 1);
    local s = _scratchBits;
    if (s < 32) {
      // the value starts in the low lane; it may spill into the high lane but,
      // with bits <= 32 and s <= 31, never past bit 64
      _lo = _lo | ((value << s) & 0xffffffff);
      if (s + bits > 32) _hi = _hi | (value >>> (32 - s));
      _scratchBits = s + bits;
    } else {
      local at = s - 32; // the position in the high lane, in [0,31]
      if (at + bits < 32) {
        _hi = _hi | ((value << at) & 0xffffffff);
        _scratchBits = s + bits;
      } else {
        // the scratch fills: store the word, and the bits past bit 64 begin
        // the next word's low lane
        _hi = _hi | ((value << at) & 0xffffffff);
        local spill = value >>> (32 - at); // a shift in [1,32]; a full dword is 0
        _flushWord();
        _lo = spill & 0xffffffff;
        _scratchBits = at + bits - 32;
      }
    }
    _bitsWritten += bits;
  }

  // _flushWord writes the scratch as 8 little-endian bytes, composed by hand
  // (DESIGN.md (d): readn/writen are the host's order), and clears it. Bytes
  // past the meaningful length are zero because the scratch is zero there
  // (STANDARD.md, General Conventions, Flush; trailing bits).
  function _flushWord() {
    local at = _wordIndex * 8;
    _buffer[at] = _lo & 0xff;
    _buffer[at + 1] = (_lo >>> 8) & 0xff;
    _buffer[at + 2] = (_lo >>> 16) & 0xff;
    _buffer[at + 3] = (_lo >>> 24) & 0xff;
    _buffer[at + 4] = _hi & 0xff;
    _buffer[at + 5] = (_hi >>> 8) & 0xff;
    _buffer[at + 6] = (_hi >>> 16) & 0xff;
    _buffer[at + 7] = (_hi >>> 24) & 0xff;
    _wordIndex++;
    _lo = 0;
    _hi = 0;
    _scratchBits = 0;
  }

  // writeAlign pads with zero bits to the next byte boundary, writing nothing
  // when already aligned (STANDARD.md, align; General Conventions, Bit index
  // and alignment).
  function writeAlign() {
    local remainder = _bitsWritten % 8;
    if (remainder != 0) writeBits(0, 8 - remainder);
  }

  // writeBytes(data, offset, n): n bytes of data from offset, on a byte
  // aligned stream (STANDARD.md, bytes). Unaligned is API misuse, and a write
  // past the buffer is the writer's overflow, both as serialize.go's
  // writeBytes (panicNotAligned, panicWriteOverflow). The total is checked
  // before any byte moves.
  function writeBytes(data, offset, n) {
    if (_bitsWritten % 8 != 0) throw PANIC_NOT_ALIGNED;
    if (_bitsWritten + n * 8 > _numBits) throw PANIC_WRITE_OVERFLOW;
    for (local i = 0; i < n; i++) _writeBits(data[offset + i], 8);
  }

  // flushBits writes the partial scratch word out; it ends the write
  // (STANDARD.md, General Conventions, Flush; serialize.go FlushBits).
  function flushBits() {
    if (_scratchBits != 0) _flushWord();
  }

  // alignBits: (8 - bit_index % 8) % 8 (STANDARD.md, General Conventions).
  function alignBits() { return (8 - _bitsWritten % 8) % 8; }

  function bitsWritten() { return _bitsWritten; }

  function bitsAvailable() { return _numBits - _bitsWritten; }

  // bytesWritten: the meaningful length, bits written rounded up to a byte
  // (STANDARD.md, General Conventions, Flush).
  function bytesWritten() { return (_bitsWritten + 7) / 8; }

  // data: the first bytesWritten bytes the writer wrote, as a blob. Call
  // flushBits first, as serialize.go's Data requires.
  function data() {
    local n = bytesWritten();
    local out = blob(n);
    for (local i = 0; i < n; i++) out[i] = _buffer[i];
    return out;
  }
}

class BitReader {
  _data = null;     // the caller's blob, read only
  _numBits = 0;     // the data's length in bits
  _bitsRead = 0;

  // BitReader(data): a reader over data of any length (DESIGN.md (h); any
  // buffer size is supported, serialize.go NewBitReader).
  constructor(data) {
    if (typeof data != "blob") throw "serialize: bit reader data must be a blob";
    _data = data;
    _numBits = data.len() * 8;
  }

  // wouldReadPastEnd(bits): bit_index + bits > 8 * bytes (STANDARD.md, Reader
  // Obligations: Reading past the end must fail).
  function wouldReadPastEnd(bits) { return _bitsRead + bits > _numBits; }

  // readBits(bits): `bits` bits in [1,32] from the stream, LSB first, masking
  // the high bits. A past-end read is refused BEFORE any byte is read
  // (STANDARD.md, Reader Obligations) and raises serialize.go's
  // panicReadOverflow; bits outside [1,32] raise panicBitsRange. No byte past
  // the data's end is ever read: only the bytes the field actually occupies
  // are composed (DESIGN.md (d)).
  function readBits(bits) {
    if (bits < 1 || bits > 32) throw PANIC_BITS_RANGE;
    if (_bitsRead + bits > _numBits) throw PANIC_READ_OVERFLOW;
    local byteIndex = _bitsRead >> 3;
    local offset = _bitsRead & 7;
    local need = (offset + bits + 7) >> 3; // at most 5 bytes for [1,32]
    local window = 0;
    for (local i = 0; i < need; i++) window = window | (_data[byteIndex + i] << (8 * i));
    local value = (window >>> offset) & (bits == 32 ? 0xffffffff : (1 << bits) - 1);
    _bitsRead += bits;
    return value;
  }

  // readAlign skips to the next byte boundary and returns true when every
  // padding bit is zero, false when one is not; the padding is consumed
  // either way (STANDARD.md, align; Reader Obligations, Refusal rules are
  // part of the format; serialize.go ReadAlign).
  function readAlign() {
    local remainder = _bitsRead % 8;
    if (remainder != 0) {
      if (readBits(8 - remainder) != 0) return false;
    }
    return true;
  }

  // readBytes(dest, offset, n): n bytes into dest from offset, on a byte
  // aligned stream. Unaligned is API misuse and a read past the data's end is
  // refused before any byte is copied, so dest is untouched on refusal
  // (STANDARD.md, bytes; Reader Obligations; serialize.go ReadBytes).
  function readBytes(dest, offset, n) {
    if (_bitsRead % 8 != 0) throw PANIC_NOT_ALIGNED;
    if (_bitsRead + n * 8 > _numBits) throw PANIC_READ_OVERFLOW;
    local byteIndex = _bitsRead >> 3;
    for (local i = 0; i < n; i++) dest[offset + i] = _data[byteIndex + i];
    _bitsRead += n * 8;
  }

  // alignBits: (8 - bit_index % 8) % 8 (STANDARD.md, General Conventions).
  function alignBits() { return (8 - _bitsRead % 8) % 8; }

  function bitsRead() { return _bitsRead; }

  function bitsRemaining() { return _numBits - _bitsRead; }
}

_M.BitWriter <- BitWriter;
_M.BitReader <- BitReader;
