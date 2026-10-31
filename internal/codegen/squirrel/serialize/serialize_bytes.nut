// serialize_bytes.nut: the bytes and string family of serialize-squirrel
// (DESIGN.md (h), file layout: src/serialize_bytes.nut). A module of the
// closed loader (DESIGN.md (f)): vargv[0] is this module's exports table and
// vargv[1] the loader's require.
//
// THE WIRE (STANDARD.md, Bytes and Strings):
//
//   bytes   aligns first, then writes the block; the length is known to both
//           sides and is not transmitted. A zero count still aligns. Set
//           padding is refused as nonzero_padding before the payload's length
//           is checked.
//   string  the length in [0, bufferSize-1] as serialize_int, then the bytes
//           as serialize_bytes (which aligns). The payload is well-formed
//           UTF-8 with no zero byte: the reader refuses a zero byte as
//           embedded_nul, scanning the whole payload BEFORE it validates the
//           encoding, then malformed UTF-8 as invalid_utf8; the writer is
//           handed such text only, and raises on any other.
//
// bufferSize is in [1, 2^31) on all three streams, serialize.go's
// panicBufferSize. Every read bounds-checks the whole length before it
// allocates or copies (DESIGN.md (g), Security trumps), and a refused read
// leaves the holder or destination untouched.
local _M = vargv[0];
local require = vargv[1];

local E = require("serialize_stream").SerializeError;

local PANIC_BUFFER_SIZE = "serialize: string buffer size must be in [1,1<<31)";
local PANIC_NOT_BLOB = "serialize: serializeBytes requires a blob";
local PANIC_STRING_NUL = "serialize: a string must not contain a zero byte";
local PANIC_STRING_UTF8 = "serialize: a string must be well-formed UTF-8";

// validateBufferSize(bufferSize): serialize.go's validateBufferSize, the
// panic text verbatim (serialize.go panicBufferSize). API misuse raises in
// every stream state (DESIGN.md (g)).
local bufferSizeIsValid = function(bufferSize) {
  return typeof bufferSize == "integer" && bufferSize >= 1 && bufferSize <= 0x7fffffff;
};
local validateBufferSize = function(bufferSize) {
  if (!bufferSizeIsValid(bufferSize)) throw PANIC_BUFFER_SIZE;
};

local isBlob = function(b) {
  return typeof b == "blob" && b instanceof ::blob;
};

// checkWriteHolder(h): a write holder holds the string to consume.
local writeString = function(h) {
  if (typeof h != "table" || !("value" in h) || typeof h.value != "string") {
    throw "serialize: the string holder must be a table { value = <string> }";
  }
  return h.value;
};

// rangeBits(min, max): serialize.go BitsRequired, the width of the unsigned
// offset max - min. The string ranges here have min 0 and max below 2^31, so
// the subtraction and the shifts stay in the number model (DESIGN.md (c)).
local rangeBits = function(min, max) {
  local x = max - min;
  local bits = 0;
  while (x > 0) {
    bits++;
    x = x >>> 1;
  }
  return bits;
};

// writeRange(s, value, min, max): the ranged-int prefix, value already known
// in [min,max] (STANDARD.md, int). The whole width is checked before a bit
// moves (DESIGN.md (g), the arm's shape).
local writeRange = function(s, value, min, max) {
  local bits = rangeBits(min, max);
  if (!s._room(bits)) return false;
  if (bits > 0) s._put(value - min, bits);
  return true;
};

// readRange(s, min, max): a { ok, value } prefix read. The width is checked
// before it reads, and an offset past the range is ValueOutOfRange after its
// bits are counted, exactly as serialize.go's ReadStream.SerializeInt.
local readRange = function(s, min, max) {
  local bits = rangeBits(min, max);
  if (!s._room(bits)) return { ok = false };
  local offset = bits > 0 ? s._take(bits) : 0;
  if (offset > max - min) {
    s._fail(E.ValueOutOfRange);
    return { ok = false };
  }
  return { ok = true, value = min + offset };
};

local stringToBlob = function(str) {
  local b = blob(str.len());
  for (local i = 0; i < str.len(); i++) b[i] = str[i] & 0xff;
  return b;
};

local BYTE_CHARS = array(256);
for (local c = 0; c < 256; c++) BYTE_CHARS[c] = c.tochar();

// sliceToString(b, lo, hi): balanced midpoint string assembly over b[lo..hi).
// Squirrel 3.2 strings are immutable and SQVM::StringCat copies both operands;
// a left-associative per-byte prefix loop copies n*(n+1)/2 bytes. Splitting at
// the midpoint bounds recursion depth to ceil(log2(n/4)) + 1 and total copied
// bytes across tree levels to O(n log n), while each intermediate SQString is
// released from SQStringTable as soon as its parent concatenation completes.
local sliceToString = null;
sliceToString = function(b, lo, hi) {
  local span = hi - lo;
  if (span <= 4) {
    if (span == 4) {
      return (BYTE_CHARS[b[lo] & 0xff] + BYTE_CHARS[b[lo + 1] & 0xff]) +
          (BYTE_CHARS[b[lo + 2] & 0xff] + BYTE_CHARS[b[lo + 3] & 0xff]);
    }
    if (span == 3) {
      return BYTE_CHARS[b[lo] & 0xff] + BYTE_CHARS[b[lo + 1] & 0xff] +
          BYTE_CHARS[b[lo + 2] & 0xff];
    }
    if (span == 2) return BYTE_CHARS[b[lo] & 0xff] + BYTE_CHARS[b[lo + 1] & 0xff];
    if (span == 1) return BYTE_CHARS[b[lo] & 0xff];
    return "";
  }
  local mid = lo + (span >> 1);
  return sliceToString(b, lo, mid) + sliceToString(b, mid, hi);
};

local blobToString = function(b) {
  return sliceToString(b, 0, b.len());
};

local hasNul = function(b) {
  for (local i = 0; i < b.len(); i++) if (b[i] == 0) return true;
  return false;
};

// utf8Valid(b): Unicode Table 3-7 well-formedness, refusing overlong forms,
// encoded surrogates, code points above 10FFFF and truncated sequences
// (STANDARD.md, string: "Readers must refuse malformed string payloads").
local utf8Valid = function(b) {
  local n = b.len();
  local i = 0;
  while (i < n) {
    local c = b[i] & 0xff;
    if (c < 0x80) {
      i++;
      continue;
    }
    if (c >= 0xc2 && c <= 0xdf) {
      if (i + 1 >= n) return false;
      local c1 = b[i + 1] & 0xff;
      if (c1 < 0x80 || c1 > 0xbf) return false;
      i += 2;
      continue;
    }
    if (c >= 0xe0 && c <= 0xef) {
      if (i + 2 >= n) return false;
      local c1 = b[i + 1] & 0xff;
      local c2 = b[i + 2] & 0xff;
      if (c1 < 0x80 || c1 > 0xbf || c2 < 0x80 || c2 > 0xbf) return false;
      if (c == 0xe0 && c1 < 0xa0) return false;
      if (c == 0xed && c1 > 0x9f) return false;
      i += 3;
      continue;
    }
    if (c >= 0xf0 && c <= 0xf4) {
      if (i + 3 >= n) return false;
      local c1 = b[i + 1] & 0xff;
      local c2 = b[i + 2] & 0xff;
      local c3 = b[i + 3] & 0xff;
      if (c1 < 0x80 || c1 > 0xbf || c2 < 0x80 || c2 > 0xbf || c3 < 0x80 || c3 > 0xbf) return false;
      if (c == 0xf0 && c1 < 0x90) return false;
      if (c == 0xf4 && c1 > 0x8f) return false;
      i += 4;
      continue;
    }
    return false;
  }
  return true;
};

// putBytes(s, data): bytes on write and measure. alignBits() is the write's
// actual padding and the measure's worst case 7 (STANDARD.md, bytes; The
// Measure Stream). count zero still aligns.
// The room for the whole operation, align and payload, is checked before the
// first bit moves (IMPLEMENTING.md U10): no align bits ahead of a payload that
// does not fit.
local putBytes = function(s, data) {
  if (!isBlob(data)) throw PANIC_NOT_BLOB;
  local align = s.alignBits();
  local n = data.len();
  if (!s._room(align + 8 * n)) return false;
  if (align > 0) s._put(0, align);
  if (n > 0) s._putBytes(data, 0, n);
  return true;
};

// readBytes(s, data): aligns (verifying zero padding), bounds-checks the whole
// count, then fills the caller's blob (STANDARD.md, bytes; Reader Obligations).
// A destination that is not a blob is a bad declaration on read: it refuses
// ValueOutOfRange after the failure state, never a raise (IMPLEMENTING.md U7).
local readBytes = function(s, data) {
  if (!isBlob(data)) return s._fail(E.ValueOutOfRange);
  local align = s.alignBits();
  if (align > 0) {
    if (!s._room(align)) return false;
    local pad = s._take(align);
    if (pad != 0) return s._fail(E.NonzeroPadding);
  }
  local n = data.len();
  if (!s._room(8 * n)) return false;
  if (n > 0) s._takeBytes(data, 0, n);
  return true;
};

// putString(s, h, bufferSize): the length prefix, the align, then the bytes
// (STANDARD.md, string). The writer is handed well-formed UTF-8 with no zero
// byte that fits bufferSize - 1, as it is handed an int in its range
// (STANDARD.md, string): anything else is write misuse and traps, on the
// write and the measure stream, latching nothing; so does a write that does
// not fit the room left (IMPLEMENTING.md U6, U9, U10; DESIGN.md (g)).
local putString = function(s, h, bufferSize) {
  validateBufferSize(bufferSize);
  local value = writeString(h);
  local bytes = stringToBlob(value);
  if (hasNul(bytes)) throw PANIC_STRING_NUL;
  if (!utf8Valid(bytes)) throw PANIC_STRING_UTF8;
  local length = value.len();
  if (length >= bufferSize) return s._fail(E.ValueOutOfRange); // the trap (U9)
  // the whole operation's room first, the length field, the align after it
  // and the payload, so nothing is written ahead of a payload that does not
  // fit (IMPLEMENTING.md U10); a measure's room is unbounded
  local lengthBits = rangeBits(0, bufferSize - 1);
  local align = (8 - (s.bitsProcessed() + lengthBits) % 8) % 8;
  if (!s._room(lengthBits + align + 8 * length)) return false;
  if (!writeRange(s, length, 0, bufferSize - 1)) return false;
  align = s.alignBits();
  if (align > 0) s._put(0, align);
  if (length > 0) s._putBytes(bytes, 0, length);
  return true;
};

// readString(s, h, bufferSize): the length prefix, the align, the whole length
// bounds check, the copy, and only then the NUL and UTF-8 refusals, so the
// holder is untouched on failure (STANDARD.md, string; Reader Obligations).
// A buffer size outside [1, 2^31) or a holder that is not a table with a
// value field is a bad declaration on read: it refuses ValueOutOfRange after
// the failure state, before any bit moves, never a raise (IMPLEMENTING.md U7,
// U16).
local readString = function(s, h, bufferSize) {
  if (s.error() != null) return false;
  if (!bufferSizeIsValid(bufferSize) || typeof h != "table" || !("value" in h)) return s._fail(E.ValueOutOfRange);
  local r = readRange(s, 0, bufferSize - 1);
  if (!r.ok) return false;
  local length = r.value;
  local align = s.alignBits();
  if (align > 0) {
    if (!s._room(align)) return false;
    local pad = s._take(align);
    if (pad != 0) return s._fail(E.NonzeroPadding);
  }
  if (!s._room(8 * length)) return false;
  local data = blob(length);
  if (length > 0) s._takeBytes(data, 0, length);
  if (hasNul(data)) return s._fail(E.EmbeddedNul);
  if (!utf8Valid(data)) return s._fail(E.InvalidUtf8);
  h.value = blobToString(data);
  return true;
};

_M.write <- {
  serializeBytes = putBytes,
  serializeString = putString,
};

_M.read <- {
  serializeBytes = readBytes,
  serializeString = readString,
};

_M.measure <- {
  serializeBytes = putBytes,
  serializeString = putString,
};
