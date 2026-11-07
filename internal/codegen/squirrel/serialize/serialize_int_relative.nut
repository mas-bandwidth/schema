// serialize_int_relative.nut: the int_relative family (STANDARD.md int_relative;
// DESIGN.md (g), (h)). A module of the closed loader (DESIGN.md (f)): vargv[0]
// is this module's exports table and vargv[1] the loader's require. It encodes an
// increasing sequence of non-negative int32 values as a ladder of one-bit flags,
// with the five bounded tiers' payloads and an absolute 32-bit fallback.
// STANDARD.md int_relative: the writer takes the first tier the difference fits,
// and that is the only encoding, so the reader refuses the absolute tier carrying
// a bounded difference as non_canonical. serialize.go v2.0.0 is the reference;
// the arm shape and the number model are DESIGN.md (c), (d), (g).
local _M = vargv[0];
local require = vargv[1];

local SerializeError = require("serialize_stream").SerializeError;

local INT_MAX = 2147483647;

// the five bounded tiers [min, max] (STANDARD.md int_relative); a difference past
// the last falls back to the absolute 32-bit tier
local TIERS = [
  { min = 2,   max = 6 },
  { min = 7,   max = 23 },
  { min = 24,  max = 280 },
  { min = 281, max = 4377 },
  { min = 4378, max = 69914 },
];

// BitsRequired(min, max): ceil(log2(max - min + 1)), the payload width of a ranged
// int (STANDARD.md int_relative: each bounded tier's payload is a ranged integer)
local bitsRequired = function(min, max) {
  local span = max - min;
  local bits = 0;
  while (span > 0) {
    span = span >>> 1;
    bits++;
  }
  return bits;
};

// previous outside the int_relative domain is the caller's own state gone wrong,
// never wire data, so it is API misuse and raises serialize.go's panicIntRelativeDomain
// in every stream state (STANDARD.md int_relative: the domain binds previous too; a
// 64-bit SQInteger can hold such a value, so both bounds are checked; DESIGN.md (g))
local checkPrevious = function(previous) {
  if (previous < 0 || previous > INT_MAX) {
    throw "serialize: int relative previous must be in [0,2147483647]";
  }
};

local write = {};
write.serializeIntRelative <- function(s, previous, h) {
  checkPrevious(previous);
  local current = h.value;
  // current is held in a 64-bit SQInteger, so the domain's top is checked too:
  // a current past 2^31 - 1 would write 32 bits its own reader refuses
  if (previous >= current || current > INT_MAX) return s._fail(SerializeError.ValueOutOfRange);
  local diff = current - previous;
  local total;
  local chosen = -1;
  if (diff == 1) {
    total = 1;
  } else {
    total = 1;
    for (local i = 0; i < TIERS.len(); i++) {
      total++;
      if (diff <= TIERS[i].max) {
        total += bitsRequired(TIERS[i].min, TIERS[i].max);
        chosen = i;
        break;
      }
    }
    if (chosen < 0) total += 32;
  }
  if (!s._room(total)) return false;
  if (diff == 1) {
    s._put(1, 1);
    return true;
  }
  s._put(0, 1);
  for (local i = 0; i < TIERS.len(); i++) {
    if (diff <= TIERS[i].max) {
      s._put(1, 1);
      s._put(diff - TIERS[i].min, bitsRequired(TIERS[i].min, TIERS[i].max));
      return true;
    }
    s._put(0, 1);
  }
  s._put(current, 32);
  return true;
};

local read = {};
// a previous outside the domain on read is a bad declaration: it refuses
// ValueOutOfRange after the failure state, never a raise (IMPLEMENTING.md U7,
// U16; the reference refuses it on read since 2.1)
read.serializeIntRelative <- function(s, previous, h) {
  if (s.error() != null) return false;
  if (typeof previous != "integer" || previous < 0 || previous > INT_MAX) return s._fail(SerializeError.ValueOutOfRange);
  if (!s._room(1)) return false;
  if (s._take(1)) {
    local c = previous + 1;
    if (c > INT_MAX) return s._fail(SerializeError.ValueOutOfRange);
    h.value = c;
    return true;
  }
  for (local i = 0; i < TIERS.len(); i++) {
    if (!s._room(1)) return false;
    if (s._take(1)) {
      local pb = bitsRequired(TIERS[i].min, TIERS[i].max);
      if (!s._room(pb)) return false;
      local off = s._take(pb);
      if (off > TIERS[i].max - TIERS[i].min) return s._fail(SerializeError.ValueOutOfRange);
      local c = previous + TIERS[i].min + off;
      if (c > INT_MAX || c <= previous) return s._fail(SerializeError.ValueOutOfRange);
      h.value = c;
      return true;
    }
  }
  if (!s._room(32)) return false;
  local v = s._take(32);
  // the domain and ordering rule first (value_out_of_range), then the canonical
  // tier: an absolute current no more than 69914 above previous has a bounded
  // encoding, so this one is a second encoding of it (non_canonical). v is the
  // 32 raw bits in a 64-bit SQInteger, so the difference cannot wrap.
  if (v > INT_MAX || v <= previous) return s._fail(SerializeError.ValueOutOfRange);
  if (v - previous <= TIERS[TIERS.len() - 1].max) return s._fail(SerializeError.NonCanonical);
  h.value = v;
  return true;
};

local measure = {};
measure.serializeIntRelative <- function(s, previous, h) {
  checkPrevious(previous);
  local current = h.value;
  // current is held in a 64-bit SQInteger, so the domain's top is checked too:
  // a current past 2^31 - 1 would write 32 bits its own reader refuses
  if (previous >= current || current > INT_MAX) return s._fail(SerializeError.ValueOutOfRange);
  local diff = current - previous;
  local total = 1;
  if (diff != 1) {
    local matched = false;
    for (local i = 0; i < TIERS.len(); i++) {
      total++;
      if (diff <= TIERS[i].max) {
        total += bitsRequired(TIERS[i].min, TIERS[i].max);
        matched = true;
        break;
      }
    }
    if (!matched) total += 32;
  }
  s._put(0, total);
  return true;
};

_M.write <- write;
_M.read <- read;
_M.measure <- measure;
