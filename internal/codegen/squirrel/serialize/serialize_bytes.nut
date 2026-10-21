// serialize_bytes.nut: the bytes, string and wstring family of
// serialize-squirrel (DESIGN.md (h), file layout: src/serialize_bytes.nut).
// A module of the closed loader (DESIGN.md (f)): vargv[0] is this module's
// exports table and vargv[1] the loader's require.
//
// THE WIRE (STANDARD.md, Bytes and Strings):
//
//   bytes   aligns first, then writes the block; the length is known to both
//           sides and is not transmitted. A zero count still aligns.
//   string  the length in [0, bufferSize-1] as serialize_int, then the bytes
//           as serialize_bytes (which aligns). The reader refuses malformed
//           UTF-8 and an interior NUL with ValueOutOfRange.
//   wstring the length in UTF-16 code units in [0, bufferSize-1], then each
//           unit as a 32-bit group; no alignment anywhere. Astral code points
//           are split into surrogate pairs on write and recombined on read;
//           the reader refuses a group above 0xFFFF, an unpaired surrogate
//           and a zero group.
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

// validateBufferSize(bufferSize): serialize.go's validateBufferSize, the
// panic text verbatim (serialize.go panicBufferSize). API misuse raises in
// every stream state (DESIGN.md (g)).
local validateBufferSize = function(bufferSize) {
  if (typeof bufferSize != "integer" || bufferSize < 1 || bufferSize > 0x7fffffff) {
    throw PANIC_BUFFER_SIZE;
  }
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

// checkReadHolder(h): a read holder is a table with a value field to assign.
local checkReadHolder = function(h) {
  if (typeof h != "table" || !("value" in h)) {
    throw "serialize: the string holder must be a table { value = ... }";
  }
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

local blobToString = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += (b[i] & 0xff).tochar();
  return s;
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

// decodeRune(b, i): one UTF-8 code point as [cp, next], malformed bytes as
// U+FFFD with size 1, as Go's range does (the wstring writer contract is the
// caller's; a malformed byte becomes the replacement character, never a raise).
local decodeRune = function(b, i) {
  local n = b.len();
  local c = b[i] & 0xff;
  if (c < 0x80) return [ c, i + 1 ];
  if (c >= 0xc2 && c <= 0xdf && i + 1 < n) {
    local c1 = b[i + 1] & 0xff;
    if (c1 >= 0x80 && c1 <= 0xbf) return [ ((c & 0x1f) << 6) | (c1 & 0x3f), i + 2 ];
  } else if (c >= 0xe0 && c <= 0xef && i + 2 < n) {
    local c1 = b[i + 1] & 0xff;
    local c2 = b[i + 2] & 0xff;
    if (c1 >= 0x80 && c1 <= 0xbf && c2 >= 0x80 && c2 <= 0xbf &&
        !(c == 0xe0 && c1 < 0xa0) && !(c == 0xed && c1 > 0x9f)) {
      return [ ((c & 0x0f) << 12) | ((c1 & 0x3f) << 6) | (c2 & 0x3f), i + 3 ];
    }
  } else if (c >= 0xf0 && c <= 0xf4 && i + 3 < n) {
    local c1 = b[i + 1] & 0xff;
    local c2 = b[i + 2] & 0xff;
    local c3 = b[i + 3] & 0xff;
    if (c1 >= 0x80 && c1 <= 0xbf && c2 >= 0x80 && c2 <= 0xbf && c3 >= 0x80 && c3 <= 0xbf &&
        !(c == 0xf0 && c1 < 0x90) && !(c == 0xf4 && c1 > 0x8f)) {
      return [ ((c & 0x07) << 18) | ((c1 & 0x3f) << 12) | ((c2 & 0x3f) << 6) | (c3 & 0x3f), i + 4 ];
    }
  }
  return [ 0xfffd, i + 1 ];
};

local utf8Codepoints = function(str) {
  local b = stringToBlob(str);
  local out = [];
  local i = 0;
  while (i < b.len()) {
    local r = decodeRune(b, i);
    out.append(r[0]);
    i = r[1];
  }
  return out;
};

// appendCodepoint(out, cp): the UTF-8 bytes of one code point, into an array.
local appendCodepoint = function(out, cp) {
  if (cp < 0x80) {
    out.append(cp);
  } else if (cp < 0x800) {
    out.append(0xc0 | (cp >>> 6));
    out.append(0x80 | (cp & 0x3f));
  } else if (cp < 0x10000) {
    out.append(0xe0 | (cp >>> 12));
    out.append(0x80 | ((cp >>> 6) & 0x3f));
    out.append(0x80 | (cp & 0x3f));
  } else {
    out.append(0xf0 | (cp >>> 18));
    out.append(0x80 | ((cp >>> 12) & 0x3f));
    out.append(0x80 | ((cp >>> 6) & 0x3f));
    out.append(0x80 | (cp & 0x3f));
  }
};

local stringFromCodepoints = function(cps) {
  local bytes = [];
  foreach (cp in cps) appendCodepoint(bytes, cp);
  local b = blob(bytes.len());
  for (local i = 0; i < bytes.len(); i++) b[i] = bytes[i];
  return blobToString(b);
};

// putBytes(s, data): bytes on write and measure. alignBits() is the write's
// actual padding and the measure's worst case 7 (STANDARD.md, bytes; The
// Measure Stream). count zero still aligns.
local putBytes = function(s, data) {
  if (!isBlob(data)) throw PANIC_NOT_BLOB;
  local align = s.alignBits();
  if (align > 0) {
    if (!s._room(align)) return false;
    s._put(0, align);
  }
  local n = data.len();
  if (!s._room(8 * n)) return false;
  if (n > 0) s._putBytes(data, 0, n);
  return true;
};

// readBytes(s, data): aligns (verifying zero padding), bounds-checks the whole
// count, then fills the caller's blob (STANDARD.md, bytes; Reader Obligations).
local readBytes = function(s, data) {
  if (!isBlob(data)) throw PANIC_NOT_BLOB;
  local align = s.alignBits();
  if (align > 0) {
    if (!s._room(align)) return false;
    local pad = s._take(align);
    if (pad != 0) return s._fail(E.Align);
  }
  local n = data.len();
  if (!s._room(8 * n)) return false;
  if (n > 0) s._takeBytes(data, 0, n);
  return true;
};

// putString(s, h, bufferSize): the length prefix, the align, then the bytes
// (STANDARD.md, string). A string that does not fit latches ValueOutOfRange,
// as serialize.go's WriteStream does; a write past the buffer latches Overflow.
local putString = function(s, h, bufferSize) {
  validateBufferSize(bufferSize);
  local value = writeString(h);
  if (s.error() != null) return false;
  local length = value.len();
  if (length >= bufferSize) return s._fail(E.ValueOutOfRange);
  if (!writeRange(s, length, 0, bufferSize - 1)) return false;
  local align = s.alignBits();
  if (align > 0) {
    if (!s._room(align)) return false;
    s._put(0, align);
  }
  if (!s._room(8 * length)) return false;
  if (length > 0) s._putBytes(stringToBlob(value), 0, length);
  return true;
};

// readString(s, h, bufferSize): the length prefix, the align, the whole length
// bounds check, the copy, and only then the NUL and UTF-8 refusals, so the
// holder is untouched on failure (STANDARD.md, string; Reader Obligations).
local readString = function(s, h, bufferSize) {
  validateBufferSize(bufferSize);
  checkReadHolder(h);
  if (s.error() != null) return false;
  local r = readRange(s, 0, bufferSize - 1);
  if (!r.ok) return false;
  local length = r.value;
  local align = s.alignBits();
  if (align > 0) {
    if (!s._room(align)) return false;
    local pad = s._take(align);
    if (pad != 0) return s._fail(E.Align);
  }
  if (!s._room(8 * length)) return false;
  local data = blob(length);
  if (length > 0) s._takeBytes(data, 0, length);
  if (hasNul(data) || !utf8Valid(data)) return s._fail(E.ValueOutOfRange);
  h.value = blobToString(data);
  return true;
};

// putWideString(s, h, bufferSize): the length in UTF-16 code units, then each
// unit as a 32-bit group with no alignment anywhere (STANDARD.md, wstring).
// The length is compared with bufferSize at SQInteger width before the length
// field is written, as serialize.go's wideStringFits does before narrowing.
local putWideString = function(s, h, bufferSize) {
  validateBufferSize(bufferSize);
  local value = writeString(h);
  if (s.error() != null) return false;
  local cps = utf8Codepoints(value);
  local units = cps.len();
  foreach (cp in cps) if (cp >= 0x10000) units++;
  if (units >= bufferSize) return s._fail(E.ValueOutOfRange);
  if (!writeRange(s, units, 0, bufferSize - 1)) return false;
  foreach (cp in cps) {
    if (cp >= 0x10000) {
      local v = cp - 0x10000;
      local high = 0xd800 + (v >>> 10);
      local low = 0xdc00 + (v & 0x3ff);
      if (!s._room(32)) return false;
      s._put(high, 32);
      if (!s._room(32)) return false;
      s._put(low, 32);
    } else {
      if (!s._room(32)) return false;
      s._put(cp, 32);
    }
  }
  return true;
};

// readWideString(s, h, bufferSize): the length prefix, the whole length bounds
// check before allocation, then the groups with every STANDARD.md wstring
// refusal: a group above 0xFFFF, an unpaired surrogate and a zero group all
// latch ValueOutOfRange; the holder is assigned only after the last one.
local readWideString = function(s, h, bufferSize) {
  validateBufferSize(bufferSize);
  checkReadHolder(h);
  if (s.error() != null) return false;
  local r = readRange(s, 0, bufferSize - 1);
  if (!r.ok) return false;
  local length = r.value;
  if (!s._room(length * 32)) return false;
  local cps = [];
  local i = 0;
  while (i < length) {
    local unit = s._take(32);
    if (unit == 0) return s._fail(E.ValueOutOfRange);
    if (unit > 0xffff) return s._fail(E.ValueOutOfRange);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      i++;
      if (i == length) return s._fail(E.ValueOutOfRange);
      local low = s._take(32);
      if (low < 0xdc00 || low > 0xdfff) return s._fail(E.ValueOutOfRange);
      cps.append(0x10000 + ((unit - 0xd800) << 10) + (low - 0xdc00));
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      return s._fail(E.ValueOutOfRange);
    } else {
      cps.append(unit);
    }
    i++;
  }
  h.value = stringFromCodepoints(cps);
  return true;
};

_M.write <- {
  serializeBytes = putBytes,
  serializeString = putString,
  serializeWideString = putWideString,
};

_M.read <- {
  serializeBytes = readBytes,
  serializeString = readString,
  serializeWideString = readWideString,
};

_M.measure <- {
  serializeBytes = putBytes,
  serializeString = putString,
  serializeWideString = putWideString,
};
