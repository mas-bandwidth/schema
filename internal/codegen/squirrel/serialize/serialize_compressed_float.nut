// serialize_compressed_float.nut: the compressed float family of serialize
// (serialize-squirrel-compressed-float; DESIGN.md (h)). A module of the closed
// loader (DESIGN.md (f)): vargv[0] is this module's exports table and vargv[1]
// the loader's require.
//
// STANDARD.md compressed_float: a float quantized to a resolution. Let
// delta = max - min and values = delta / res, clamped to [1, 4294967040] (the
// largest float below 2^32). Then max_integer_value = ceil(values) and
// bits = bits_required(0, max_integer_value). The writer clamps
// (value - min) / delta to [0,1], multiplies by max_integer_value, adds 0.5,
// floors, clamps the integer to max_integer_value, and writes it in `bits`
// bits; the reader divides by max_integer_value, multiplies by delta, adds min,
// and refuses an integer above max_integer_value. The arithmetic is float32
// with EVERY STEP ROUNDING (STANDARD.md compressed_float; DESIGN.md (e)):
// the double result of each +, -, *, / of two float32 values goes through
// fround before the next step (two roundings around the 0.5, never one; a
// step is never widened to double before the floor).
local _M = vargv[0];
local require = vargv[1];

// fround (DESIGN.md (e)) narrows a double to the float32 Go's float32() gives.
// DESIGN.md (h): compressed_float reaches serialize_float's fround through
// require, one source of truth. require returns the module's exports table
// (DESIGN.md (f)), so the named export is indexed from it.
local fround = require("serialize_float").fround;

// bitLen(n): the number of bits to represent the positive integer n
// (serialize.go bits.Len32); bits_required(0, n) for n > 0 (STANDARD.md).
local bitLen = function(n) {
  local b = 0;
  while (n > 0) { b++; n = n >>> 1; }
  return b;
};

// isNotFinite(x): x, after fround, is NaN, +inf or -inf (its double exponent
// field is all ones). DESIGN.md (e) raises before any bit moves on such a
// value; the read path is untouched.
local isNotFinite = function(x) {
  return ((castf2i(x) >>> 52) & 0x7FF) == 0x7FF;
};

// compressedFloatParams(min, max, resolution) (serialize.go CompressedFloatParams):
// derive the wire constants. Returns { maxIntegerValue, bits, delta }. Every
// float32 step rounds (DESIGN.md (e)). min >= max or resolution <= 0 is API
// misuse and raises serialize.go's panic text.
local compressedFloatParams = function(min, max, resolution) {
  if (!(min < max) || !(resolution > 0)) {
    throw "serialize: compressed float requires min < max and resolution > 0";
  }
  local delta = fround(max - min);
  local values = fround(delta / resolution);
  if (!(values >= 1.0)) values = 1.0;
  else if (values > 4294967040.0) values = 4294967040.0;
  local maxIntegerValue = ceil(values).tointeger();
  return { maxIntegerValue = maxIntegerValue, bits = bitLen(maxIntegerValue), delta = delta };
};

// validateConstants(maxIntegerValue, bits, delta): API misuse raises serialize.go's
// panic text (serialize.go validateCompressedFloatConstants). The delta-delta
// term catches a non-finite delta; !(delta > 0) catches NaN, zero and negative.
local validateConstants = function(maxIntegerValue, bits, delta) {
  if (maxIntegerValue == 0 || bits != bitLen(maxIntegerValue) || !(delta > 0) || delta - delta != 0) {
    throw "serialize: precomputed compressed float constants must be exactly what CompressedFloatParams derives";
  }
};

// the write arm's audited home (serialize.go compressedFloat32Precomputed)
local writePrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
  validateConstants(maxIntegerValue, bits, delta);
  local value = fround(h.value);
  if (isNotFinite(value)) {
    throw "serialize: compressed float value is not finite";
  }
  local normalizedValue = fround((value - min) / delta);
  if (!(normalizedValue >= 0)) normalizedValue = 0;
  else if (!(normalizedValue <= 1)) normalizedValue = 1;
  local mf = fround(maxIntegerValue);
  local product = fround(normalizedValue * mf);
  local sum = fround(product + 0.5);
  local integerValue = floor(sum).tointeger();
  if (integerValue > maxIntegerValue) integerValue = maxIntegerValue;
  if (!s._room(bits)) return false;
  s._put(integerValue, bits);
  return true;
};

// the read arm's audited home (serialize.go compressedFloat32Precomputed)
local readPrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
  validateConstants(maxIntegerValue, bits, delta);
  if (!s._room(bits)) return false;
  local integerValue = s._take(bits);
  if (integerValue > maxIntegerValue) return s._fail("value_out_of_range");
  local mf = fround(maxIntegerValue);
  local normalizedValue = fround(fround(integerValue) / mf);
  local product = fround(normalizedValue * delta);
  local result = fround(product + min);
  h.value <- result;
  return true;
};

local write = {
  serializeCompressedFloat = function(s, h, min, max, resolution) {
    local p = compressedFloatParams(min, max, resolution);
    return writePrecomputed(s, h, p.maxIntegerValue, p.bits, p.delta, min);
  },
  serializeCompressedFloatPrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
    return writePrecomputed(s, h, maxIntegerValue, bits, delta, min);
  },
};

local read = {
  serializeCompressedFloat = function(s, h, min, max, resolution) {
    local p = compressedFloatParams(min, max, resolution);
    return readPrecomputed(s, h, p.maxIntegerValue, p.bits, p.delta, min);
  },
  serializeCompressedFloatPrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
    return readPrecomputed(s, h, maxIntegerValue, bits, delta, min);
  },
};

local measure = {
  serializeCompressedFloat = function(s, h, min, max, resolution) {
    local p = compressedFloatParams(min, max, resolution);
    local value = fround(h.value);
    if (isNotFinite(value)) throw "serialize: compressed float value is not finite";
    s._put(0, p.bits);
    return true;
  },
  serializeCompressedFloatPrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
    validateConstants(maxIntegerValue, bits, delta);
    local value = fround(h.value);
    if (isNotFinite(value)) throw "serialize: compressed float value is not finite";
    s._put(0, bits);
    return true;
  },
};

_M.compressedFloatParams <- compressedFloatParams;
_M.write <- write;
_M.read <- read;
_M.measure <- measure;
