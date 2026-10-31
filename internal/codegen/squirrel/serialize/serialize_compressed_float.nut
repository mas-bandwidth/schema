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
local SerializeError = require("serialize_stream").SerializeError;

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
// misuse and raises serialize.go's panic text; the arms reach paramsOrPanic,
// which returns the text instead, so a read can refuse it (IMPLEMENTING.md
// U7, U16).
local PANIC_PARAMS = "serialize: compressed float requires min < max and resolution > 0";
local PANIC_PARAMS_FINITE = "serialize: compressed float declaration has a max - min or (max - min) / resolution that is not finite in float32";
local paramsOrPanic = function(min, max, resolution, strict = false) {
  // the parameters are float32, as Go's are: rounded on entry (DESIGN.md (e))
  min = fround(min);
  max = fround(max);
  resolution = fround(resolution);
  if (!(min < max) || !(resolution > 0)) return PANIC_PARAMS;
  local delta = fround(max - min);
  local values = fround(delta / resolution);
  // a declaration whose delta, or whose delta / resolution, is not finite in
  // float32 is non-conforming (STANDARD.md compressed_float; the reference's
  // serialize_compressed_float_params asserts both): write misuse, never
  // clamped (IMPLEMENTING.md U9). The test is on the bits: Squirrel's float
  // comparison is not IEEE's (NaN > 0 is true on the pin), so a NaN
  // resolution passes the guard above and is caught here as a NaN values.
  // Only the trapping entry (compressedFloatParams: write, measure and the
  // public helper) makes it; the read arm keeps the clamp it always had, so
  // the read path is unchanged.
  if (strict && (isNotFinite(delta) || isNotFinite(values))) return PANIC_PARAMS_FINITE;
  if (!(values >= 1.0)) values = 1.0;
  else if (values > 4294967040.0) values = 4294967040.0;
  local maxIntegerValue = ceil(values).tointeger();
  return { maxIntegerValue = maxIntegerValue, bits = bitLen(maxIntegerValue), delta = delta };
};
local compressedFloatParams = function(min, max, resolution) {
  local p = paramsOrPanic(min, max, resolution, true);
  if (typeof p == "string") throw p;
  return p;
};

// constantsAreValid(maxIntegerValue, bits, delta): the precomputed constants
// are what CompressedFloatParams can derive (serialize.go
// validateCompressedFloatConstants; IMPLEMENTING.md U16): maxIntegerValue an
// integer in [1, 4294967040], bits exactly its bit length, and delta a
// positive finite number. The delta-delta term catches a non-finite delta;
// !(delta > 0) catches NaN, zero and negative. Write and measure raise
// serialize.go's panic text on a false (validateConstants); a read refuses it
// as ValueOutOfRange (U7).
local constantsAreValid = function(maxIntegerValue, bits, delta) {
  return typeof maxIntegerValue == "integer" && maxIntegerValue >= 1 && maxIntegerValue <= 4294967040 &&
    typeof bits == "integer" && bits == bitLen(maxIntegerValue) &&
    (typeof delta == "float" || typeof delta == "integer") && delta > 0 && delta - delta == 0;
};
local validateConstants = function(maxIntegerValue, bits, delta) {
  if (!constantsAreValid(maxIntegerValue, bits, delta)) {
    throw "serialize: precomputed compressed float constants must be exactly what CompressedFloatParams derives";
  }
};

// checkTopDecode(delta, min): the declaration's top integer decodes to
// min + delta, rounded once in float32, and a declaration whose top decode is
// not finite is non-conforming as a whole (STANDARD.md compressed_float, "The
// decode must be finite"), so a write traps on it, even for a value that
// would decode finite. The measure traps too: it makes every check a write
// makes (IMPLEMENTING.md U6, U9; DESIGN.md (g)). A reader refuses the
// non-finite decode itself.
local checkTopDecode = function(delta, min) {
  if (isNotFinite(fround(fround(min) + delta))) {
    throw "serialize: compressed float declaration decodes its top integer to a value that is not finite";
  }
};

// the write arm's audited home (serialize.go compressedFloat32Precomputed)
local writePrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
  validateConstants(maxIntegerValue, bits, delta);
  min = fround(min);
  checkTopDecode(delta, min);
  local value = fround(h.value);
  if (isNotFinite(value)) {
    throw "serialize: compressed float value is not finite";
  }
  // (value - min) / delta: two float32 operations, each rounded; the
  // difference rounds before the quotient (serialize.go's float32 arithmetic)
  local normalizedValue = fround(fround(value - min) / delta);
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
  if (!constantsAreValid(maxIntegerValue, bits, delta)) return s._fail(SerializeError.ValueOutOfRange);
  min = fround(min);
  if (!s._room(bits)) return false;
  local integerValue = s._take(bits);
  if (integerValue > maxIntegerValue) return s._fail(SerializeError.ValueOutOfRange);
  local mf = fround(maxIntegerValue);
  local normalizedValue = fround(fround(integerValue) / mf);
  local product = fround(normalizedValue * delta);
  local result = fround(product + min);
  // STANDARD.md compressed_float, "The decode must be finite": every operand
  // is finite and the integer in range, and still the final add can round
  // past FLT_MAX ([3*2^103, FLT_MAX] at 2^104 decodes its top integer to +inf).
  // Refused as non_finite_float, the holder untouched, whatever the declaration.
  if (isNotFinite(result)) return s._fail(SerializeError.NonFiniteFloat);
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
    local p = paramsOrPanic(min, max, resolution);
    if (typeof p == "string") return s._fail(SerializeError.ValueOutOfRange);
    return readPrecomputed(s, h, p.maxIntegerValue, p.bits, p.delta, min);
  },
  serializeCompressedFloatPrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
    return readPrecomputed(s, h, maxIntegerValue, bits, delta, min);
  },
};

local measure = {
  serializeCompressedFloat = function(s, h, min, max, resolution) {
    local p = compressedFloatParams(min, max, resolution);
    checkTopDecode(p.delta, min);
    local value = fround(h.value);
    if (isNotFinite(value)) throw "serialize: compressed float value is not finite";
    if (!s._room(p.bits)) return false;
    s._put(0, p.bits);
    return true;
  },
  serializeCompressedFloatPrecomputed = function(s, h, maxIntegerValue, bits, delta, min) {
    validateConstants(maxIntegerValue, bits, delta);
    checkTopDecode(delta, min);
    local value = fround(h.value);
    if (isNotFinite(value)) throw "serialize: compressed float value is not finite";
    if (!s._room(bits)) return false;
    s._put(0, bits);
    return true;
  },
};

_M.compressedFloatParams <- compressedFloatParams;
_M.write <- write;
_M.read <- read;
_M.measure <- measure;
