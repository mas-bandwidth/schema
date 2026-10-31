// THE TEXT FORM (docs/SPEC-TABLES.md §16): one table instance as one JSON
// text and back, driven by the descriptors internal/codegen/squirreltable
// emits (§8.1) and the text facts its json surface adds (frac, flagNames,
// fmin/fmax, jsonArray: internal/codegen/squirreltable/json.go). The walk is
// a port of internal/tabletext (write.go, read.go, wide.go), the oracle the
// goldens come from, in the shape internal/codegen/jstable/variableruntime.go's
// THE TEXT FORM gives a descriptor walk (docs/PORT-SQUIRREL.md, DEEPEN).
//
//   _M.ToJson(T, root, value)        the text (a string) or null: refused (§16.3)
//   _M.ToJsonMeasure(T, root, value) its byte length, or -1 for the same refusals
//   _M.FromJson(T, root, text, report)  a fresh root instance holding what the
//       text placed (§16.1); report.malformed says the text was not read whole
//
// T is a unit's table module; root is the root table's schema name; text is
// a string or a blob; report is the caller's §4 report, filled in place.
local _M = vargv[0];

local MIN = 0x8000000000000000;
local MAXU = 0xffffffffffffffff;
local MAX_DEPTH = 128;  // §16.2's depth cap, carried by the writer too (§16.7)
local MAX_KEY = 255;
local MAX_NUMBER = 511;

local tableDesc = function(T, name) { return T.Descriptors[name]; };
local unionDesc = function(T, name) { return T[name + "Descriptor"]; };
local has = function(d, k) { return (k in d) && d[k] != null; };

// ---- BYTES AND STRINGS ------------------------------------------------------

// CH[b] is the one-byte string of byte b: a Squirrel string is bytes, and a
// text is built from pieces joined pairwise, so building it is n log n
local CH = array(256);
for (local i = 0; i < 256; i++) CH[i] = format("%c", i);

local join = function(parts) {
  if (parts.len() == 0) return "";
  local a = parts;
  while (a.len() > 1) {
    local b = array((a.len() + 1) / 2);
    for (local i = 0; i < b.len(); i++) {
      b[i] = (2 * i + 1 < a.len()) ? a[2 * i] + a[2 * i + 1] : a[2 * i];
    }
    a = b;
  }
  return a[0];
};

local bytesToString = function(bs) {
  local parts = array(bs.len());
  for (local i = 0; i < bs.len(); i++) parts[i] = CH[bs[i] & 0xff];
  return join(parts);
};

local bytesToBlob = function(bs) {
  local b = blob(bs.len());
  for (local i = 0; i < bs.len(); i++) b[i] = bs[i] & 0xff;
  return b;
};

local ult = function(a, b) { return (a ^ MIN) < (b ^ MIN); };

// unsigned 64-bit decimal (docs/PORT-SQUIRREL.md: text prints a uint64 digit
// by digit, because tostring() reads signed)
local utoa = function(v) {
  if (v >= 0) return v.tostring();
  local q = (v >>> 1) / 5;      // v / 10, unsigned
  local r = v - q * 10;
  return q.tostring() + r.tostring();
};

// an unsigned 64-bit value as the nearest double (Go's float64(uint64)):
// halve with the dropped bit kept sticky, convert, double
local utof = function(m) {
  if (m >= 0) return m.tofloat();
  return ((m >>> 1) | (m & 1)).tofloat() * 2.0;
};

// ---- FLOAT32 BITS (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS) -------

local widenF32 = function(bits) {
  bits = bits & 0xffffffff;
  local sign = bits & 0x80000000;
  local exp = (bits >> 23) & 0xff;
  local mant = bits & 0x007fffff;
  local high = sign != 0 ? MIN : 0;
  if (exp == 0xff) {
    high = high | 0x7ff0000000000000;
    if (mant != 0) high = high | (mant << 29);
  } else if (exp == 0) {
    if (mant != 0) {
      local e = -126;
      while ((mant & 0x800000) == 0) { mant = mant << 1; e--; }
      mant = mant & 0x007fffff;
      high = high | ((e + 1023) << 52) | (mant << 29);
    }
  } else {
    high = high | ((exp - 127 + 1023) << 52) | (mant << 29);
  }
  return casti2f(high);
};

// narrowF32 rounds a double to float32 bits, IEEE round-to-nearest ties-to-
// even (Go's float32(x), save.nut's narrowing), or with dir = 1 / -1 a TIE
// rounds away from / toward zero. It answers [bits, tie]: tie is whether the
// dropped part was exactly half an ulp, the one case where rounding a
// decimal through a double can differ from rounding it once (§16.1, Dart's
// paragraph: num-float32-upper-band).
local narrowF32 = function(x, dir) {
  local b = castf2i(x);
  local sign = (b >>> 63) & 1;
  local exp = (b >>> 52) & 0x7ff;
  local mant = b & 0xfffffffffffff;
  if (exp == 0x7ff) {
    if (mant == 0) return [(sign << 31) | 0x7f800000, false];
    local payload = (mant >>> 29) & 0x007fffff;
    if (payload == 0) payload = 0x00400000;
    return [(sign << 31) | 0x7f800000 | payload, false];
  }
  if (exp == 0) return [sign << 31, false];
  local e = exp - 1023;
  local N = 0x10000000000000 | mant;
  local s = 29;
  local sub = false;
  if (e < -126) { s = -e - 97; sub = true; }
  if (s >= 54) return [sign << 31, false];
  local q = N >>> s;
  local rem = N & ((1 << s) - 1);
  local half = 1 << (s - 1);
  local tie = rem == half;
  if (rem > half || (tie && (dir > 0 || (dir == 0 && (q & 1) != 0)))) q = q + 1;
  if (sub) {
    if (q == 0x800000) return [(sign << 31) | 0x00800000, tie];
    return [(sign << 31) | (q & 0x007fffff), tie];
  }
  if (q >= 0x1000000) { q = q >>> 1; e = e + 1; }
  if (e > 127) return [(sign << 31) | 0x7f800000, tie];
  return [(sign << 31) | ((e + 127) << 23) | (q & 0x007fffff), tie];
};

local isFinite = function(x) { return ((castf2i(x) >>> 52) & 0x7ff) != 0x7ff; };

// ---- NUMBER TEXT ------------------------------------------------------------

// formatG is C's %.*g (internal/tabletext write.go formatG): the pinned
// interpreter's format() is the C library's snprintf, so the spelling is C's
// byte for byte, the tie at the last digit included
local formatG = function(v, prec) { return format("%." + prec + "g", v); };

// a token as a double: the interpreter's tofloat() is strtod only when the
// text carries a point or an exponent, so an integral token gets one
local tokenFloat = function(tok) {
  local dot = false;
  foreach (c in tok) if (c == '.' || c == 'e' || c == 'E') { dot = true; break; }
  return (dot ? tok : tok + "e0").tofloat();
};

// a decimal as [digits, point]: the significant digits with leading and
// trailing zeros stripped, the point after `point` of them (wide.go ParseWide)
local decimalOf = function(tok) {
  local i = 0, n = tok.len();
  if (i < n && (tok[i] == '-' || tok[i] == '+')) i++;
  local ip = [];
  while (i < n && tok[i] >= '0' && tok[i] <= '9') { ip.append(tok[i]); i++; }
  local fp = [];
  if (i < n && tok[i] == '.') {
    i++;
    while (i < n && tok[i] >= '0' && tok[i] <= '9') { fp.append(tok[i]); i++; }
  }
  local exp = 0;
  if (i < n && (tok[i] == 'e' || tok[i] == 'E')) {
    i++;
    local en = false;
    if (i < n && (tok[i] == '-' || tok[i] == '+')) { en = tok[i] == '-'; i++; }
    while (i < n && tok[i] >= '0' && tok[i] <= '9') {
      if (exp < 100000) exp = exp * 10 + (tok[i] - '0');
      i++;
    }
    if (en) exp = -exp;
  }
  local point = ip.len() + exp;
  local digits = ip;
  digits.extend(fp);
  local a = 0;
  while (a < digits.len() && digits[a] == '0') { a++; point--; }
  local z = digits.len();
  while (z > a && digits[z - 1] == '0') z--;
  return [digits.slice(a, z), point];
};

// the order of two decimal magnitudes: -1, 0, 1
local decimalCompare = function(x, y) {
  if (x[0].len() == 0 || y[0].len() == 0) return x[0].len() == y[0].len() ? 0 : (x[0].len() == 0 ? -1 : 1);
  if (x[1] != y[1]) return x[1] < y[1] ? -1 : 1;
  local n = x[0].len() > y[0].len() ? x[0].len() : y[0].len();
  for (local i = 0; i < n; i++) {
    local a = i < x[0].len() ? x[0][i] : '0';
    local b = i < y[0].len() ? y[0][i] : '0';
    if (a != b) return a < b ? -1 : 1;
  }
  return 0;
};

local exactDecimal; // a double's exact decimal, defined with the wide kinds

// THE NEAREST FLOAT32 TO A DECIMAL TOKEN, ROUNDED ONCE (internal/tabletext
// read.go placeFloat: ParseFloat(token, 32)). strtod gives the nearest
// double; narrowing it rounds again, and the two roundings differ only when
// that double is exactly a float32 midpoint. There the token's exact decimal
// decides against the midpoint's (snprintf prints a double's exact digits).
local parseF32 = function(tok) {
  local d = tokenFloat(tok);
  if (!isFinite(d)) return [d, false];
  local r = narrowF32(d, 0);
  if (r[1]) {
    local c = decimalCompare(decimalOf(tok), exactDecimal(d));
    if (c != 0) r = narrowF32(d, c);
  }
  return [widenF32(r[0]), true];
};

// the shortest %.*g that reads back the same value at the field's own width
// (write.go writeFloat): 6..9 digits for a float32, 15..17 for a float64.
// A non-finite value has no JSON spelling: null, and the writer refuses.
local floatText = function(v, single) {
  if (!isFinite(v)) return null;
  // a float32 field's value must stay finite at float32 (docs/SPEC-TABLES.md
  // §3.3): a double past float32's range narrows to an infinity, refused
  if (single && (narrowF32(v, 0)[0] & 0x7f800000) == 0x7f800000) return null;
  local low = single ? 6 : 15, high = single ? 9 : 17;
  for (local digits = low; ; digits++) {
    local text = formatG(v, digits);
    if (digits >= high) return text;
    local back = tokenFloat(text);
    if (single) {
      if (widenF32(narrowF32(back, 0)[0]) == v) return text;
    } else if (back == v) {
      return text;
    }
  }
};

// ---- WIDE KINDS: 128-BIT INTEGERS AND FIXED POINT (wide.go) ----------------
// A magnitude is little-endian limbs of 16 bits; a value is [negative, limbs].
// Nothing here passes through a double (§16.2, the wide kinds convert EXACTLY).

local bnNorm = function(a) { while (a.len() > 0 && a[a.len() - 1] == 0) a.pop(); return a; };
local bnZero = function(a) { return a.len() == 0; };
local bnMulSmall = function(a, m, add) {
  local out = [];
  local carry = add;
  foreach (x in a) { local t = x * m + carry; out.append(t & 0xffff); carry = t >> 16; }
  while (carry > 0) { out.append(carry & 0xffff); carry = carry >> 16; }
  return bnNorm(out);
};
// [quotient, remainder] of a / m, m small
local bnDivSmall = function(a, m) {
  local q = array(a.len(), 0);
  local r = 0;
  for (local i = a.len() - 1; i >= 0; i--) {
    local t = (r << 16) | a[i];
    q[i] = t / m;
    r = t % m;
  }
  return [bnNorm(q), r];
};
local bnShl = function(a, n) {
  local out = a;
  while (n >= 16) { local t = [0]; t.extend(out); out = t; n -= 16; }
  if (n > 0) out = bnMulSmall(out, 1 << n, 0);
  return bnNorm(out);
};
local bnShr = function(a, n) {
  local out = a.slice((n / 16) < a.len() ? n / 16 : a.len());
  n = n % 16;
  if (n > 0) {
    local r = array(out.len(), 0);
    for (local i = 0; i < out.len(); i++) {
      local hi = i + 1 < out.len() ? out[i + 1] : 0;
      r[i] = ((out[i] >> n) | (hi << (16 - n))) & 0xffff;
    }
    out = r;
  }
  return bnNorm(out);
};
local bnLowBits = function(a, n) {
  local out = [];
  for (local i = 0; i < a.len() && i * 16 < n; i++) {
    local keep = n - i * 16;
    out.append(keep >= 16 ? a[i] : (a[i] & ((1 << keep) - 1)));
  }
  return bnNorm(out);
};
local bnCmp = function(a, b) {
  if (a.len() != b.len()) return a.len() < b.len() ? -1 : 1;
  for (local i = a.len() - 1; i >= 0; i--) if (a[i] != b[i]) return a[i] < b[i] ? -1 : 1;
  return 0;
};
// a signed value's order
local svCmp = function(x, y) {
  local xz = bnZero(x[1]), yz = bnZero(y[1]);
  local xn = x[0] && !xz, yn = y[0] && !yz;
  if (xn != yn) return xn ? -1 : 1;
  local c = bnCmp(x[1], y[1]);
  return xn ? -c : c;
};
local bnFromU64 = function(v) {
  local out = [];
  for (local i = 0; i < 4; i++) { out.append(v & 0xffff); v = v >>> 16; }
  return bnNorm(out);
};
local bnToU64 = function(a) {
  local v = 0;
  for (local i = 0; i < 4 && i < a.len(); i++) v = v | (a[i] << (16 * i));
  return v;
};
// a two's complement [lo, hi] pair as 128 bits, read signed or not
local bnFromPair = function(lo, hi) {
  local m = [];
  for (local i = 0; i < 4; i++) { m.append(lo & 0xffff); lo = lo >>> 16; }
  for (local i = 0; i < 4; i++) { m.append(hi & 0xffff); hi = hi >>> 16; }
  return bnNorm(m);
};
local svFromPair = function(p, signed) {
  local lo = p[0], hi = p[1];
  if (signed && hi < 0) {
    // negate the 128 bits: invert and add one, the carry crossing halves
    // exactly when the low half is zero
    return [true, bnFromPair(~lo + 1, ~hi + (lo == 0 ? 1 : 0))];
  }
  return [false, bnFromPair(lo, hi)];
};
local svToPair = function(x) {
  local m = x[1];
  local lo = bnToU64(m);
  local hi = bnToU64(m.len() > 4 ? m.slice(4) : []);
  if (x[0] && !bnZero(m)) {
    lo = ~lo + 1;
    hi = ~hi + (lo == 0 ? 1 : 0);
  }
  return [lo, hi];
};
local svFromInt = function(v, signed) {
  if (signed && v < 0) {
    if (v == MIN) return [true, bnFromU64(MIN)];
    return [true, bnFromU64(-v)];
  }
  return [false, bnFromU64(v)];
};
local svToInt = function(x) {
  local v = bnToU64(x[1]);
  return x[0] ? -v : v;
};
local bnDecimal = function(a) {
  if (bnZero(a)) return "0";
  local parts = [];
  local m = a;
  while (!bnZero(m)) {
    local qr = bnDivSmall(m, 10000);
    m = qr[0];
    local s = qr[1].tostring();
    if (!bnZero(m)) while (s.len() < 4) s = "0" + s;
    parts.insert(0, s);
  }
  return join(parts);
};

// a finite double's EXACT decimal as [digits, point]: M x 2^E is
// M x 5^-E x 10^E, so the digits are those of an integer
exactDecimal = function(d) {
  local b = castf2i(d) & 0x7fffffffffffffff;
  local ex = (b >>> 52) & 0x7ff;
  local M = b & 0xfffffffffffff;
  local E = -1074;
  if (ex != 0) { M = M | 0x10000000000000; E = ex - 1075; }
  local m = bnFromU64(M);
  local point;
  local s;
  if (E >= 0) {
    s = bnDecimal(bnShl(m, E));
    point = s.len();
  } else {
    for (local i = 0; i < -E; i++) m = bnMulSmall(m, 5, 0);
    s = bnDecimal(m);
    point = s.len() + E;
  }
  local digits = [];
  foreach (c in s) digits.append(c);
  local z = digits.len();
  while (z > 0 && digits[z - 1] == '0') z--;
  return [digits.slice(0, z), point];
};

local wideSigned = function(kind) { return kind == 18 || (kind >= 20 && kind <= 24); };
local wideKind = function(kind) { return kind >= 18 && kind <= 29; };

// the 128-bit domain of a kind's signedness (wide.go wideLimits)
local SIGNED_LIMITS = [[true, bnShl([1], 127)],
  [false, [0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0x7fff]]];
local UNSIGNED_LIMITS = [[false, []],
  [false, [0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff]]];
local wideLimits = function(kind) { return wideSigned(kind) ? SIGNED_LIMITS : UNSIGNED_LIMITS; };

// ParseWide (wide.go): one token as a raw integer, value x 2^frac. Answers
// [raw, exact, saturated]; not exact is a finer fraction than Q I.F holds.
local parseWide = function(tok, kind, frac) {
  local neg = tok.len() > 0 && tok[0] == '-';
  local dp = decimalOf(tok);
  local digits = dp[0], point = dp[1];
  local lim = wideLimits(kind);
  if (digits.len() == 0) return [[false, []], true, false];
  if (point > 40) {
    if (neg) return [lim[0], true, true];
    return [lim[1], true, true];
  }
  if (point < -40) return [null, false, false];
  local m = [];
  foreach (c in digits) m = bnMulSmall(m, 10, c - '0');
  local scale = point - digits.len();
  local raw = bnShl(m, frac);
  if (scale >= 0) {
    for (local i = 0; i < scale; i++) raw = bnMulSmall(raw, 10, 0);
  } else {
    for (local i = 0; i < -scale; i++) {
      local qr = bnDivSmall(raw, 10);
      if (qr[1] != 0) return [null, false, false];
      raw = qr[0];
    }
  }
  local v = [neg, raw];
  if (svCmp(v, lim[0]) < 0) return [lim[0], true, true];
  if (svCmp(v, lim[1]) > 0) return [lim[1], true, true];
  return [v, true, false];
};

// FormatWide (wide.go): a 128-bit integer as its decimal, a fixed value as
// the shortest exact decimal in whole units with at least one fraction digit
local formatWide = function(v, kind, frac) {
  local neg = v[0] && !bnZero(v[1]);
  if (kind == 18 || kind == 19) return (neg ? "-" : "") + bnDecimal(v[1]);
  local whole = bnShr(v[1], frac);
  local f = bnLowBits(v[1], frac);
  local s = (neg ? "-" : "") + bnDecimal(whole) + ".";
  if (bnZero(f)) return s + "0";
  local parts = [s];
  while (!bnZero(f)) {
    f = bnMulSmall(f, 10, 0);
    local digit = bnShr(f, frac);
    parts.append((digit.len() > 0 ? digit[0] : 0).tostring());
    f = bnLowBits(f, frac);
  }
  return join(parts);
};

// a wide field's storage as a signed value, and back
local wideOf = function(d, val) {
  if (d.width == 16) return svFromPair(val, wideSigned(d.kind));
  return svFromInt(val, wideSigned(d.kind));
};
local wideStore = function(d, v) {
  if (d.width == 16) return svToPair(v);
  return svToInt(v);
};

// ---- TIMESTAMPS (§16.2's timestamp row, docs/SPEC-TABLES.md §2.11) ----------
// RFC 3339 with nanoseconds and a Z offset, Go's time.RFC3339Nano

local daysFromCivil = function(y, m, d) {
  y -= m <= 2 ? 1 : 0;
  local era = (y >= 0 ? y : y - 399) / 400;
  local yoe = y - era * 400;
  local mp = (m + 9) % 12;
  local doy = (153 * mp + 2) / 5 + d - 1;
  local doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
  return era * 146097 + doe - 719468;
};
local civilFromDays = function(z) {
  z += 719468;
  local era = (z >= 0 ? z : z - 146096) / 146097;
  local doe = z - era * 146097;
  local yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
  local y = yoe + era * 400;
  local doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
  local mp = (5 * doy + 2) / 153;
  local d = doy - (153 * mp + 2) / 5 + 1;
  local m = mp < 10 ? mp + 3 : mp - 9;
  return [y + (m <= 2 ? 1 : 0), m, d];
};
local pad = function(v, n) { local s = v.tostring(); while (s.len() < n) s = "0" + s; return s; };

local timestampText = function(ns) {
  // floor division by 10^9, so a negative count names the second before it
  local sec = ns / 1000000000, frac = ns % 1000000000;
  if (frac < 0) { frac += 1000000000; sec -= 1; }
  local days = sec / 86400, rem = sec % 86400;
  if (rem < 0) { rem += 86400; days -= 1; }
  local c = civilFromDays(days);
  local s = pad(c[0], 4) + "-" + pad(c[1], 2) + "-" + pad(c[2], 2) + "T" +
    pad(rem / 3600, 2) + ":" + pad((rem / 60) % 60, 2) + ":" + pad(rem % 60, 2);
  if (frac != 0) {
    local f = pad(frac, 9);
    local z = f.len();
    while (f[z - 1] == '0') z--;
    s += "." + f.slice(0, z);
  }
  return s + "Z";
};

// the UTC nanosecond count an RFC 3339 spelling names, or null when it does
// not parse or names an instant outside an int64 count (read.go readTimestamp)
local timestampParse = function(s) {
  local n = s.len();
  local num = function(at, k) {
    if (at + k > n) return -1;
    local v = 0;
    for (local i = at; i < at + k; i++) {
      if (s[i] < '0' || s[i] > '9') return -1;
      v = v * 10 + (s[i] - '0');
    }
    return v;
  };
  if (n < 20) return null;
  local y = num(0, 4), mo = num(5, 2), d = num(8, 2), h = num(11, 2), mi = num(14, 2), se = num(17, 2);
  if (y < 0 || mo < 1 || mo > 12 || d < 1 || h < 0 || h > 23 || mi < 0 || mi > 59 || se < 0 || se > 59) return null;
  if (s[4] != '-' || s[7] != '-' || s[13] != ':' || s[16] != ':') return null;
  // §5.6: "T" and "Z" may be lower case, exactly as the one parser,
  // internal/rfc3339/rfc3339.go rfc3339Parse, reads them
  // (docs/SPEC-TABLES.md §2.11, §16.2)
  if (s[10] != 'T' && s[10] != 't') return null;
  local mdays = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  local leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
  if (d > mdays[mo - 1] + (mo == 2 && leap ? 1 : 0)) return null;
  local at = 19, nsec = 0;
  if (at < n && s[at] == '.') {
    at++;
    local k = 0;
    local seen = 0;
    while (at < n && s[at] >= '0' && s[at] <= '9') {
      // digits past the ninth are truncated, as Go's parser does
      if (k < 9) { nsec = nsec * 10 + (s[at] - '0'); k++; }
      seen++;
      at++;
    }
    if (seen == 0) return null;
    while (k < 9) { nsec *= 10; k++; }
  }
  local off = 0;
  if (at < n && (s[at] == 'Z' || s[at] == 'z') && at + 1 == n) {
    off = 0;
  } else if (at + 6 == n && (s[at] == '+' || s[at] == '-') && s[at + 3] == ':') {
    local oh = num(at + 1, 2), om = num(at + 4, 2);
    if (oh < 0 || oh > 23 || om < 0 || om > 59) return null;
    off = (oh * 60 + om) * 60;
    if (s[at] == '-') off = -off;
  } else {
    return null;
  }
  local sec = daysFromCivil(y, mo, d) * 86400 + h * 3600 + mi * 60 + se - off;
  if (sec > 9223372036 || (sec == 9223372036 && nsec > 854775807)) return null;
  if (sec < -9223372037 || (sec == -9223372037 && nsec < 145224192)) return null;
  return sec * 1000000000 + nsec;
};

// ---- SHAPES (read.go Shape, ElementShape, TakesNull) -------------------------

local isKeyed = function(d) { return d.shape == "arr" && d.kind == 16; };

local shapeOf = function(T, d) {
  switch (d.shape) {
    case "str": case "wstr": case "bytes": case "sblob": case "bblob": case "enum": return 's';
    case "arr": return d.kind == 16 ? 'o' : 'a';
    case "union": case "ptr": case "map": return 'o';
    case "tab": return has(tableDesc(T, d.table), "jsonArray") ? 'a' : 'o';
    case "flags": return 'a';
    case "bool": return 'b';
  }
  if (d.kind == 36) return 's';
  return 'n';
};
local elemShapeOf = function(T, e) {
  switch (e.shape) {
    case "union": case "ptr": case "map": return 'o';
    case "tab": return has(tableDesc(T, e.table), "jsonArray") ? 'a' : 'o';
    case "enum": case "str": case "wstr": case "sblob": case "bblob": return 's';
    case "flags": return 'a';
    case "bool": return 'b';
  }
  if (e.kind == 36) return 's';
  return 'n';
};
local takesNull = function(d) {
  return d.optional || d.shape == "ptr" || d.shape == "sblob" || d.shape == "bblob";
};

// ---- MAPS (docs/SPEC-TABLES.md §2.8, §16.2) ---------------------------------

// THE KEY ORDER (§2.8): integers by VALUE, signed where the declaration is and
// unsigned past the signed half, and strings by BYTES, never a locale. The
// unsigned compare is spelled here because a Squirrel int reads its high half
// as negative (§2.8's "a port whose language has no unsigned compare").
local mapKeyOrder = function(kd, a, b) {
  if (kd.shape == "str") {
    if (a == b) return 0;
    return a < b ? -1 : 1;
  }
  if (a == b) return 0;
  if (kd.signed) return a < b ? -1 : 1;
  return ult(a, b) ? -1 : 1;
};

// ONE INTEGER KEY TOKEN, read by its OWN digits (§2.8, §16.2): a key is an
// identity, so 1.0 and 1e0 are the key 1, and a fractional value or a
// magnitude past 64 bits is not the key kind's identity. This is the exact
// interpretation internal/tabletext's interpretIntegerExact carries; no
// double decides a key.
local mapKeyNumber = function(tok) {
  local n = tok.len(), i = 0;
  if (i < n && tok[i] == '-') i++;
  if (i >= n) return null;
  if (tok[i] == '0') { i++; }
  else if (tok[i] >= '1' && tok[i] <= '9') {
    while (i < n && tok[i] >= '0' && tok[i] <= '9') i++;
  } else return null;
  if (i < n && tok[i] == '.') {
    i++;
    local at = i;
    while (i < n && tok[i] >= '0' && tok[i] <= '9') i++;
    if (i == at) return null;
  }
  if (i < n && (tok[i] == 'e' || tok[i] == 'E')) {
    i++;
    if (i < n && (tok[i] == '-' || tok[i] == '+')) i++;
    local at = i;
    while (i < n && tok[i] >= '0' && tok[i] <= '9') i++;
    if (i == at) return null;
  }
  if (i != n) return null;
  local out = { mag = 0, neg = false, fractional = false, saturated = false };
  local dp = decimalOf(tok);
  local ds = dp[0], point = dp[1];
  if (ds.len() == 0) return out;
  if (point < ds.len()) { out.fractional = true; return out; }
  if (point > 20) { out.mag = MAXU; out.saturated = true; return out; }
  local mag = 0;
  for (local k = 0; k < ds.len(); k++) {
    local dg = ds[k] - '0';
    local lim = ((MAXU - dg) >>> 1) / 5;
    if (ult(lim, mag)) { out.mag = MAXU; out.saturated = true; return out; }
    mag = mag * 10 + dg;
  }
  local lim10 = (MAXU >>> 1) / 5;
  for (local k = ds.len(); k < point; k++) {
    if (ult(lim10, mag)) { out.mag = MAXU; out.saturated = true; return out; }
    mag = mag * 10;
  }
  out.mag = mag;
  out.neg = (tok[0] == '-') && mag != 0;
  return out;
};

// THE KEY'S DOMAIN (§2.8): a key that does not fit its kind drops its WHOLE
// entry as kind_mismatch; it never clamps, because a clamped key merges two
// identities into one.
local mapKeyInDomain = function(kd, num) {
  if (num.saturated || num.fractional) return [false, 0];
  local width = kd.width;
  local mag = num.mag;
  if (kd.signed) {
    local high = width < 8 ? (1 << (width * 8 - 1)) - 1 : 0x7fffffffffffffff;
    if (num.neg) {
      local low = high + 1;
      if (ult(low, mag)) return [false, 0];
      return [true, -mag];
    }
    if (ult(high, mag)) return [false, 0];
    return [true, mag];
  }
  if (num.neg) return [false, 0];
  local high = width < 8 ? (1 << (width * 8)) - 1 : MAXU;
  if (ult(high, mag)) return [false, 0];
  return [true, mag];
};

// ONE KEY AS THE OBJECT'S KEY (§16.2): the bytes for a string key and the
// decimal spelling for an integer, signed where the declaration is.
local mapKeyText = function(kd, v) {
  if (kd.shape == "str") return v;
  return kd.signed ? v.tostring() : utoa(v);
};

// an array element's value-initialized zero (value.go elementZero): a fresh
// instance for a table, None for a union, null for a reference or an enum
local elemZero = function(T, e) {
  switch (e.shape) {
    case "tab": return tableDesc(T, e.table).make();
    case "union": return unionDesc(T, e.table).make();
    case "ptr": case "sblob": case "bblob": case "enum": return null;
    case "str": return "";
    case "wstr": return [];
    case "bytes": return blob(0);
    case "bool": return false;
    case "float": return 0.0;
  }
  if (e.width == 16) return [0, 0];
  return 0;
};

// =============================================================================
// THE WRITE HALF (internal/tabletext write.go)
// =============================================================================

local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
local b64Text = function(data) {
  local out = ["\""];
  local i = 0, n = data.len();
  for (; n - i >= 3; i += 3) {
    local t = ((data[i] & 0xff) << 16) | ((data[i + 1] & 0xff) << 8) | (data[i + 2] & 0xff);
    out.append(CH[B64[(t >> 18) & 63]] + CH[B64[(t >> 12) & 63]] + CH[B64[(t >> 6) & 63]] + CH[B64[t & 63]]);
  }
  if (i < n) {
    local left = n - i;
    local t = (data[i] & 0xff) << 16;
    if (left == 2) t = t | ((data[i + 1] & 0xff) << 8);
    out.append(CH[B64[(t >> 18) & 63]] + CH[B64[(t >> 12) & 63]]);
    out.append(left == 2 ? CH[B64[(t >> 6) & 63]] : "=");
    out.append("=");
  }
  out.append("\"");
  return join(out);
};

// the length of ONE well-formed UTF-8 sequence at s[i], or -1 (write.go
// utf8Sequence)
local utf8Sequence = function(s, i) {
  local lead = s[i] & 0xff;
  local want, code;
  if (lead < 0x80) return 1;
  if (lead >= 0xc2 && lead <= 0xdf) { want = 2; code = lead & 0x1f; }
  else if (lead >= 0xe0 && lead <= 0xef) { want = 3; code = lead & 0x0f; }
  else if (lead >= 0xf0 && lead <= 0xf4) { want = 4; code = lead & 0x07; }
  else return -1;
  if (s.len() - i < want) return -1;
  for (local k = 1; k < want; k++) {
    local c = s[i + k] & 0xff;
    if ((c & 0xc0) != 0x80) return -1;
    code = (code << 6) | (c & 0x3f);
  }
  if (want == 3 && code < 0x800) return -1;
  if (want == 4 && code < 0x10000) return -1;
  if (code >= 0xd800 && code <= 0xdfff) return -1;
  if (code > 0x10ffff) return -1;
  return want;
};

local HEX = "0123456789abcdef";
local FFFD = format("%c%c%c", 0xef, 0xbf, 0xbd);

// ONE JSON STRING from bytes (write.go writeString): the escapes, a control
// byte as \u00XX, and a byte that is not part of a well-formed sequence as
// U+FFFD, so the text is always valid UTF-8 (§16.3)
local jsonString = function(s) {
  local out = ["\""];
  local run = 0;
  local n = s.len();
  local i = 0;
  local isStr = typeof s == "string";
  local flush = function(to) {
    if (to > run) {
      if (isStr) out.append(s.slice(run, to));
      else { local bs = []; for (local k = run; k < to; k++) bs.append(s[k]); out.append(bytesToString(bs)); }
    }
  };
  while (i < n) {
    local c = s[i] & 0xff;
    local esc = null;
    if (c == '"') esc = "\\\"";
    else if (c == '\\') esc = "\\\\";
    else if (c == 8) esc = "\\b";
    else if (c == 12) esc = "\\f";
    else if (c == 10) esc = "\\n";
    else if (c == 13) esc = "\\r";
    else if (c == 9) esc = "\\t";
    else if (c < 0x20) esc = "\\u00" + CH[HEX[c >> 4]] + CH[HEX[c & 0xf]];
    else if (c >= 0x80) {
      local w = utf8Sequence(s, i);
      if (w < 0) esc = FFFD;
      else { i += w; continue; }
    }
    if (esc != null) {
      flush(i);
      out.append(esc);
      i++;
      run = i;
      continue;
    }
    i++;
  }
  flush(n);
  out.append("\"");
  return join(out);
};

// WIDE TEXT IN THE TEXT (docs/SPEC-TABLES.md §16.2): the code units
// TRANSCODED at the boundary into one JSON string. A well-formed pair is its
// astral code point; storage built in code holding an unpaired surrogate
// writes it as U+FFFD (§16.3's maximal-subpart rule, one per unit), and a zero
// unit writes `\u0000` through jsonString. A slot that is not a code unit, an
// integer in [0, 0xFFFF], has no text spelling: null, and the write refuses.
local wideJsonString = function(units) {
  if (typeof units != "array") return null;
  local bs = [];
  local n = units.len();
  for (local i = 0; i < n; i++) {
    local u = units[i];
    if (typeof u != "integer" || u < 0 || u > 0xFFFF) return null;
    local code = u;
    if (u >= 0xD800 && u <= 0xDBFF && i + 1 < n && typeof units[i + 1] == "integer" &&
        units[i + 1] >= 0xDC00 && units[i + 1] <= 0xDFFF) {
      code = 0x10000 + ((u - 0xD800) << 10) + (units[i + 1] - 0xDC00);
      i++;
    } else if (u >= 0xD800 && u <= 0xDFFF) {
      code = 0xFFFD;
    }
    if (code < 0x80) bs.append(code);
    else if (code < 0x800) bs.extend([0xc0 | (code >> 6), 0x80 | (code & 0x3f)]);
    else if (code < 0x10000) bs.extend([0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f)]);
    else bs.extend([0xf0 | (code >> 18), 0x80 | ((code >> 12) & 0x3f), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f)]);
  }
  return jsonString(bytesToString(bs));
};

local indent = function(depth) {
  local s = "\n";
  for (local i = 0; i < depth; i++) s += "  ";
  return s;
};

local enumName = function(d, val) {
  if (val == null) return "None";
  foreach (p in d.variants) if (p[0] == val) return val;
  return null;
};

local armOf = function(u, tag) {
  foreach (a in u.arms) if (a.tag == tag) return a;
  return null;
};

// the number of elements a field writes: a fixed array its bound, a counted
// or listed array its count (write.go writeField)
local writeCount = function(d, val) {
  if (d.list || d.counted) return val == null ? 0 : val.len();
  return d.bound;
};
local elemAt = function(T, e, val, i) {
  if (val != null && i < val.len()) return val[i];
  return elemZero(T, e);
};

// PASS ONE (§16.7, write.go countInstance): every slot naming each node is
// counted, so a node's first occurrence knows whether it is named again; a
// reference to a node whose descent is still open is a data cycle, refused
// as the wire refuses it. The ROOT is open for the whole pass.
local countInstance, countField, countCell, countNode;

// THE GUARD'S ELISION (§16.2, write.go countInstance and writeFields): a field
// inside an `if` group rides only while every [member, want] pair of its
// composed condition holds on the same instance; the untaken side of a group
// has no text to carry, so neither pass visits it. A field with no guard key
// always rides. The read keeps whatever keys the text names: the walk infers
// nothing from a group's presence.
local guardHolds = function(fd, v) {
  if (!has(fd, "guard")) return true;
  foreach (term in fd.guard) {
    if (term[0] == null) return false;
    if ((v[term[0]] ? true : false) != term[1]) return false;
  }
  return true;
};

// THE `| required` MARKER (§2.3, §16.2): a record that ends without a required
// field whose guard holds is refused, with missing_required naming the missing
// fields in declaration order. A required field under an untaken guard rides
// on nothing, so it is not missing there (the guard's elision, §16.2).
local checkRequired = function(T, c, r, v, seen) {
  local missing = [];
  foreach (fd in c.fields) {
    if (!fd.required) continue;
    if (!guardHolds(fd, v)) continue;
    if (!(fd.name in seen)) missing.append(fd.name);
  }
  if (missing.len() == 0) return true;
  local reason = "missing_required: " + missing[0];
  for (local i = 1; i < missing.len(); i++) reason += ", " + missing[i];
  r.report.reason <- reason;
  return false;
};

local countBlob = function(kind, val, g) {
  if (val == null) return true;
  if (val in g.blobs[kind]) {
    g.reason = "blob_shared";
    return false;
  }
  g.blobs[kind][val] <- true;
  return true;
};

countInstance = function(T, c, v, g) {
  foreach (fd in c.fields) {
    if (!guardHolds(fd, v)) continue;
    if (fd.optional && !v[fd.member + "Present"]) continue;
    if (!countField(T, fd, v[fd.member], g)) return false;
  }
  return true;
};
countField = function(T, d, val, g) {
  switch (d.shape) {
    case "sblob": return countBlob("s", val, g);
    case "bblob": return countBlob("b", val, g);
    case "str": case "wstr": case "bytes": return true;
    case "ptr": return countNode(T, tableDesc(T, d.table), val, g);
    case "arr": {
      local n = isKeyed(d) ? d.bound : writeCount(d, val);
      for (local i = 0; i < n; i++) {
        if (val == null || i >= val.len()) break;
        if (!countCell(T, d.elem, val[i], g)) return false;
      }
      return true;
    }
    case "map": {
      // A MAP'S ENTRIES ARE BY-VALUE RECORDS (§2.8): the value inside one
      // reaches nodes exactly as a nested table's fields do (§16.7)
      local ed = tableDesc(T, d.entry);
      local n = val == null ? 0 : val.len();
      for (local i = 0; i < n; i++) {
        if (!countInstance(T, ed, val[i], g)) return false;
      }
      return true;
    }
  }
  return countCell(T, d, val, g);
};
countCell = function(T, d, val, g) {
  if (d.shape == "sblob") return countBlob("s", val, g);
  if (d.shape == "bblob") return countBlob("b", val, g);
  if (d.shape == "ptr") return countNode(T, tableDesc(T, d.table), val, g);
  if (d.shape == "union") {
    if (val == null || val.Type == null || val.Type == 0) return true;
    local arm = armOf(unionDesc(T, d.table), val.Type);
    if (arm == null || arm.payload == null) return true;
    local p = arm.payload;
    if (p.shape == "tab") {
      local inst = val[p.member];
      if (inst == null) return true;
      return countInstance(T, tableDesc(T, p.table), inst, g);
    }
    return countField(T, p, val[p.member], g);
  }
  if (d.shape == "tab" && val != null) return countInstance(T, tableDesc(T, d.table), val, g);
  return true;
};
countNode = function(T, c, node, g) {
  if (node == null) return true;
  local n = (node in g.count) ? g.count[node] + 1 : 1;
  g.count[node] <- n;
  if ((node in g.open) && g.open[node]) return false;
  if (n > 1) return true;
  g.open[node] <- true;
  if (!countInstance(T, c, node, g)) return false;
  g.open[node] = false;
  return true;
};

local writeValue, writeFields, writePointer, writeField, writeScalar, writeMap;

// one instance's object, every field in declaration order, defaults included,
// a present optional only (write.go writeValue, writeFields)
writeValue = function(T, c, v, depth, g, out) {
  if (depth > MAX_DEPTH) return false;
  if (has(c, "jsonArray")) {
    // THE ARRAY ROOT (§16.10): the table's text IS its one field's array
    if (c.fields.len() != 1) return false;
    return writeField(T, c.fields[0], v[c.fields[0].member], depth, g, out);
  }
  local st = { any = false };
  if (!writeFields(T, c, v, depth, g, out, st)) return false;
  if (!st.any) { out.append("{}"); return true; }
  out.append(indent(depth) + "}");
  return true;
};

writeFields = function(T, c, v, depth, g, out, st) {
  local ind = indent(depth + 1);
  foreach (fd in c.fields) {
    if (!guardHolds(fd, v)) continue;
    if (fd.optional && !v[fd.member + "Present"]) continue;
    // A `| required` field (§2.3) always rides, even at its default, because
    // the text never elides a default (§16.2): every field here is written.
    out.append(st.any ? "," : "{");
    st.any = true;
    out.append(ind + jsonString(fd.key) + ": ");
    if (!writeField(T, fd, v[fd.member], depth + 1, g, out)) return false;
  }
  return true;
};

// a node named once is its object in place; a node named more than once is
// DEFINED at its first occurrence, "&node" first, and REFERENCED by "&node"
// alone after that, labels from 1 in first-write order (§16.7)
writePointer = function(T, c, node, depth, g, out) {
  if (node == null) { out.append("null"); return true; }
  local n = (node in g.count) ? g.count[node] : 0;
  if (n <= 1) return writeValue(T, c, node, depth, g, out);
  if (depth > MAX_DEPTH) return false;
  if (node in g.labels) {
    out.append("{" + indent(depth + 1) + "\"&node\": " + g.labels[node] + indent(depth) + "}");
    return true;
  }
  g.next = g.next + 1;
  g.labels[node] <- g.next;
  out.append("{" + indent(depth + 1) + "\"&node\": " + g.next);
  local before = out.len();
  if (!writeFields(T, c, node, depth, g, out, { any = true })) return false;
  // a definition carries at least one field: a label alone is a reference
  if (out.len() == before) return false;
  out.append(indent(depth) + "}");
  return true;
};

// A MAP as a plain JSON object keyed by the KEY (§2.8, §16.2): ascending key
// order, because the order is the map's and not the projection's, and an
// integer key as its decimal spelling. An ABSENT `map[K]?T` VALUE is `null`,
// the one spelling of an absent value, never its defaults (§2.3, §2.8).
writeMap = function(T, d, val, depth, g, out) {
  local kd = d.mapkey, vd = d.mapvalue;
  local n = val == null ? 0 : val.len();
  if (n == 0) { out.append("{}"); return true; }
  local order = [];
  for (local i = 0; i < n; i++) order.append(i);
  local src = val;
  order.sort(function(x, y) {
    local o = mapKeyOrder(kd, src[x][kd.member], src[y][kd.member]);
    if (o != 0) return o;
    return x < y ? -1 : 1;
  });
  out.append("{");
  for (local i = 0; i < n; i++) {
    local ent = val[order[i]];
    if (i > 0) out.append(",");
    out.append(indent(depth + 1) + jsonString(mapKeyText(kd, ent[kd.member])) + ": ");
    if (vd.optional && !ent[vd.member + "Present"]) { out.append("null"); continue; }
    if (!writeField(T, vd, ent[vd.member], depth + 1, g, out)) return false;
  }
  out.append(indent(depth) + "}");
  return true;
};

// A TEXT LIST's text form (§2.13, §16.2): a plain array of strings, absent
// `?string` elements as null. The storage is the generated element record;
// textlist 1 is the plain spelling and 2 the optional one.
local writeTextArray = function(T, d, val, depth, out) {
  local n = writeCount(d, val);
  if (n == 0) { out.append("[]"); return true; }
  out.append("[");
  for (local i = 0; i < n; i++) {
    if (i > 0) out.append(",");
    out.append(indent(depth + 1));
    local rec = elemAt(T, d.elem, val, i);
    if (d.textlist == 2 && (rec == null || !rec.ValuePresent)) { out.append("null"); continue; }
    out.append(jsonString(rec == null ? "" : rec.Value));
  }
  out.append(indent(depth) + "]");
  return true;
};

writeField = function(T, d, val, depth, g, out) {
  switch (d.shape) {
    case "sblob":
      if (val == null) { out.append("null"); return true; }
      out.append(jsonString(val));
      return true;
    case "bblob":
      if (val == null) { out.append("null"); return true; }
      out.append(b64Text(val));
      return true;
    case "str": out.append(jsonString(val)); return true;
    case "wstr": {
      local w = wideJsonString(val);
      if (w == null) return false;
      out.append(w);
      return true;
    }
    case "bytes": out.append(b64Text(val)); return true;
    case "map": return writeMap(T, d, val, depth, g, out);
    case "arr": {
      if (d.textlist != 0) return writeTextArray(T, d, val, depth, out);
      local e = d.elem;
      if (isKeyed(d)) {
        // an enum-keyed array is an OBJECT keyed by variant name, every slot
        // the array keys written (write.go writeKeyed)
        if (d.bound <= 0) { out.append("{}"); return true; }
        out.append("{");
        for (local slot = 0; slot < d.bound; slot++) {
          if (slot > 0) out.append(",");
          if (d.keyvariants == null || slot >= d.keyvariants.len()) return false;
          out.append(indent(depth + 1) + jsonString(d.keyvariants[slot][0]) + ": ");
          if (!writeScalar(T, e, elemAt(T, e, val, slot), depth + 1, g, out)) return false;
        }
        out.append(indent(depth) + "}");
        return true;
      }
      local n = writeCount(d, val);
      if (n == 0) { out.append("[]"); return true; }
      out.append("[");
      for (local i = 0; i < n; i++) {
        if (i > 0) out.append(",");
        out.append(indent(depth + 1));
        local x = elemAt(T, e, val, i);
        if (e.shape == "str") { out.append(jsonString(x)); continue; }
        if (e.shape == "sblob" || e.shape == "bblob") {
          if (!writeField(T, e, x, depth + 1, g, out)) return false;
          continue;
        }
        if (!writeScalar(T, e, x, depth + 1, g, out)) return false;
      }
      out.append(indent(depth) + "]");
      return true;
    }
  }
  return writeScalar(T, d, val, depth, g, out);
};

writeScalar = function(T, d, val, depth, g, out) {
  switch (d.shape) {
    case "union": {
      // a union is an object with ONE key, the arm's name; None is {}
      if (val == null || val.Type == null || val.Type == 0) { out.append("{}"); return true; }
      local arm = armOf(unionDesc(T, d.table), val.Type);
      if (arm == null) return false; // a tag outside the arm range (§16.3)
      out.append("{" + indent(depth + 1) + jsonString(arm.name) + ": ");
      local p = arm.payload;
      if (p == null) {
        out.append("null" + indent(depth) + "}"); // a payload-free arm
        return true;
      }
      if (p.shape == "tab") {
        local pc = tableDesc(T, p.table);
        local inst = val[p.member];
        if (inst == null) inst = pc.make();
        if (!writeValue(T, pc, inst, depth + 1, g, out)) return false;
      } else if (!writeField(T, p, val[p.member], depth + 1, g, out)) {
        return false;
      }
      out.append(indent(depth) + "}");
      return true;
    }
    case "ptr": return writePointer(T, tableDesc(T, d.table), val, depth, g, out);
    case "tab": {
      local c = tableDesc(T, d.table);
      return writeValue(T, c, val == null ? c.make() : val, depth, g, out);
    }
    case "enum": {
      // a value no variant names has no text spelling: refused (§16.3)
      local name = enumName(d, val);
      if (name == null) return false;
      // A STRICT WRITE REFUSES A VALUE HOLDING A DEPRECATED VARIANT
      // (docs/SPEC-TABLES.md §2.15): the value is the caller's.
      foreach (p in d.variants) {
        if (p[0] == val && p.len() > 2 && p[2]) return false;
      }
      out.append(jsonString(name));
      return true;
    }
    case "flags": {
      local bits = val;
      if (bits == 0) { out.append("[]"); return true; }
      local names = has(d, "flagNames") ? d.flagNames : [];
      out.append("[");
      local first = true;
      for (local bit = 0; bit < 64; bit++) {
        if ((bits & (1 << bit)) == 0) continue;
        // a bit no variant names has no text spelling: refused (§16.3)
        if (bit >= names.len()) return false;
        if (!first) out.append(",");
        first = false;
        out.append(indent(depth + 1) + jsonString(names[bit]));
      }
      out.append(indent(depth) + "]");
      return true;
    }
    case "bool": out.append(val ? "true" : "false"); return true;
    case "float": {
      local t = floatText(val, d.f32);
      if (t == null) return false; // a non-finite float (§16.3)
      out.append(t);
      return true;
    }
    case "str": out.append(jsonString(val)); return true;
    case "wstr": {
      local w = wideJsonString(val);
      if (w == null) return false;
      out.append(w);
      return true;
    }
  }
  if (d.kind == 36) { out.append(jsonString(timestampText(val))); return true; }
  if (wideKind(d.kind)) {
    out.append(formatWide(wideOf(d, val), d.kind, has(d, "frac") ? d.frac : 0));
    return true;
  }
  if (d.kind >= 2 && d.kind <= 5) { out.append(val.tostring()); return true; }
  out.append(utoa(val));
  return true;
};

local writeText = function(T, rootName, value) {
  _M.ToJsonReason <- null;
  local c = tableDesc(T, rootName);
  local g = { count = {}, open = {}, labels = {}, next = 0, blobs = { s = {}, b = {} }, reason = null };
  g.open[value] <- true;
  if (!countInstance(T, c, value, g)) {
    _M.ToJsonReason <- g.reason;
    return null;
  }
  local out = [];
  if (!writeValue(T, c, value, 0, g, out)) return null;
  // THE CANONICAL TEXT ENDS WITH EXACTLY ONE NEWLINE (§16.1)
  out.append("\n");
  return join(out);
};

_M.ToJson <- function(T, rootName, value) { return writeText(T, rootName, value); };

// measure and write agree byte for byte (§16.1): the measure is the write's
_M.ToJsonMeasure <- function(T, rootName, value) {
  local t = writeText(T, rootName, value);
  return t == null ? -1 : t.len();
};

// =============================================================================
// THE READ HALF (internal/tabletext read.go)
// =============================================================================

local newIn = function(text, report, graph) {
  local t = text;
  if (typeof t == "string") {
    local b = blob(t.len());
    for (local i = 0; i < t.len(); i++) b[i] = t[i] & 0xff;
    t = b;
  }
  return { t = t, n = t.len(), p = 0, bad = false, report = report, graph = graph };
};

// whitespace and comments: accepted on read, never written (§16.2); an
// unclosed /* is malformed, and a lone slash is not JSON
local space = function(r) {
  local t = r.t, n = r.n;
  while (r.p < n) {
    local c = t[r.p];
    if (c == ' ' || c == '\t' || c == '\n' || c == '\r') { r.p++; continue; }
    if (c == '/' && r.p + 1 < n && t[r.p + 1] == '/') {
      r.p += 2;
      while (r.p < n && t[r.p] != '\n') r.p++;
      continue;
    }
    if (c == '/' && r.p + 1 < n && t[r.p + 1] == '*') {
      local q = r.p + 2;
      local found = false;
      while (q + 1 < n) {
        if (t[q] == '*' && t[q + 1] == '/') { found = true; break; }
        q++;
      }
      if (!found) { r.bad = true; r.p = n; return; }
      r.p = q + 2;
      continue;
    }
    if (c == '/') r.bad = true;
    return;
  }
};

local peek = function(r) { space(r); return r.p < r.n ? r.t[r.p] : 0; };

local valueShape = function(r) {
  local c = peek(r);
  if (c == '{') return 'o';
  if (c == '[') return 'a';
  if (c == '"') return 's';
  if (c == 't' || c == 'f') return 'b';
  if (c == 'n') return 'z';
  if (c == 0) return 0;
  return 'n';
};

local literal = function(r, word) {
  if (r.p + word.len() > r.n) { r.bad = true; return false; }
  for (local i = 0; i < word.len(); i++) {
    if (r.t[r.p + i] != word[i]) { r.bad = true; return false; }
  }
  r.p += word.len();
  return true;
};

local hex4 = function(r) {
  if (r.p + 4 > r.n) return -1;
  local v = 0;
  for (local i = 0; i < 4; i++) {
    local c = r.t[r.p + i];
    local dg;
    if (c >= '0' && c <= '9') dg = c - '0';
    else if (c >= 'a' && c <= 'f') dg = c - 'a' + 10;
    else if (c >= 'A' && c <= 'F') dg = c - 'A' + 10;
    else return -1;
    v = (v << 4) | dg;
  }
  r.p += 4;
  return v;
};

local encodeUTF8 = function(code) {
  if (code < 0x80) return [code];
  if (code < 0x800) return [0xc0 | (code >> 6), 0x80 | (code & 0x3f)];
  if (code < 0x10000) return [0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f)];
  return [0xf0 | (code >> 18), 0x80 | ((code >> 12) & 0x3f), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f)];
};

local validUnit = function(u) {
  if (u.len() == 1) return u[0] < 0x80;
  return utf8Sequence(u, 0) == u.len();
};

// ONE STRING BODY CHARACTER as UTF-8 bytes (read.go scanUnit): an escape's
// code point, or a UTF-8 sequence read whole; a lone surrogate escape and a
// sequence that is not a code point read as U+FFFD (§16.3). Answers the
// bytes, "done" at the closing quote, or null where the text is not JSON.
local scanUnit = function(r) {
  local t = r.t;
  if (r.p >= r.n) { r.bad = true; return null; }
  local c = t[r.p];
  if (c == '"') { r.p++; return "done"; }
  if (c == '\\') {
    r.p++;
    if (r.p >= r.n) { r.bad = true; return null; }
    local e = t[r.p];
    r.p++;
    switch (e) {
      case '"': return ['"'];
      case '\\': return ['\\'];
      case '/': return ['/'];
      case 'b': return [8];
      case 'f': return [12];
      case 'n': return [10];
      case 'r': return [13];
      case 't': return [9];
      case 'u': {
        local high = hex4(r);
        if (high < 0) { r.bad = true; return null; }
        local code = high;
        if (high >= 0xd800 && high <= 0xdbff && r.p + 2 <= r.n && t[r.p] == '\\' && t[r.p + 1] == 'u') {
          local mark = r.p;
          r.p += 2;
          local low = hex4(r);
          if (low >= 0xdc00 && low <= 0xdfff) code = 0x10000 + ((high - 0xd800) << 10) + (low - 0xdc00);
          else r.p = mark;
        }
        if (code >= 0xd800 && code <= 0xdfff) code = 0xfffd;
        return encodeUTF8(code);
      }
    }
    r.bad = true;
    return null;
  }
  if (c < 0x20) { r.bad = true; return null; }
  local want = 1;
  if ((c & 0xe0) == 0xc0) want = 2;
  else if ((c & 0xf0) == 0xe0) want = 3;
  else if ((c & 0xf8) == 0xf0) want = 4;
  local unit = [c];
  r.p++;
  while (unit.len() < want && r.p < r.n && (t[r.p] & 0xc0) == 0x80) { unit.append(t[r.p]); r.p++; }
  // A SEQUENCE THAT IS NOT A CODE POINT IS REFUSED BY NAME (§16.3): RFC 8259
  // requires a JSON text to be valid UTF-8, and a silent U+FFFD would make two
  // different texts one value. This runs before any bound clamp.
  if (!validUnit(unit)) {
    if (!("reason" in r.report) || r.report.reason == null) r.report.reason <- "string_not_utf8: <root>";
    r.bad = true;
    return null;
  }
  return unit;
};

// one JSON string as bytes, clamped at a code point boundary (read.go
// scanString): capacity < 0 is unbounded. Answers [bytes, clamped] or null.
local scanBytes = function(r, capacity) {
  if (peek(r) != '"') { r.bad = true; return null; }
  r.p++;
  local out = [];
  local clamped = false;
  for (;;) {
    local u = scanUnit(r);
    if (u == null) return null;
    if (u == "done") break;
    // A CLAMP IS A PREFIX: once one code point does not fit, nothing after it
    // is placed
    if (clamped || (capacity >= 0 && out.len() + u.len() > capacity)) { clamped = true; continue; }
    out.extend(u);
  }
  return [out, clamped];
};

// a key, compared as a string
local scanKey = function(r) {
  local s = scanBytes(r, MAX_KEY);
  if (s == null) return null;
  return bytesToString(s[0]);
};

// docs/SPEC-TABLES.md 16.3: a JSON string the read would place in text
// storage that decodes to U+0000 refuses the read, its reason naming the
// innermost field. A string the read only SKIPS stores nothing and is not
// this event, so the check lives at the storage call sites and never in
// scanBytes or scanKey.
local hasZero = function(b) {
  for (local i = 0; i < b.len(); i++) if (b[i] == 0) return true;
  return false;
};
local textZero = function(r, name) {
  if (!("reason" in r.report) || r.report.reason == null) r.report.reason <- "text_holds_zero: " + name;
  r.bad = true;
  return false;
};

// ONE JSON STRING INTO WIDE TEXT (docs/SPEC-TABLES.md §16.2, §16.3;
// read.go scanWString): the string is scanned as UTF-8 first, so ill-formed
// text refuses by name before anything is placed; a zero refuses the read as
// text_holds_zero for `where`; then the code points are TRANSCODED to UTF-16
// code units, and a value longer than `bound` units is CLAMPED as a prefix of
// whole code points, so a pair is never split. Answers [units, clamped], or
// null when the read refused.
local scanWide = function(r, bound, where) {
  local s = scanBytes(r, -1);
  if (s == null) return null;
  local b = s[0];
  if (hasZero(b)) { textZero(r, where); return null; }
  local units = [];
  local clamped = false;
  local i = 0, n = b.len();
  while (i < n) {
    local c = b[i] & 0xff;
    local code, want;
    if (c < 0x80) { code = c; want = 1; }
    else if ((c & 0xe0) == 0xc0) { code = c & 0x1f; want = 2; }
    else if ((c & 0xf0) == 0xe0) { code = c & 0x0f; want = 3; }
    else { code = c & 0x07; want = 4; }
    for (local k = 1; k < want; k++) code = (code << 6) | (b[i + k] & 0x3f);
    i += want;
    local need = code >= 0x10000 ? 2 : 1;
    if (units.len() + need > bound) { clamped = true; break; }
    if (need == 1) units.append(code);
    else {
      code -= 0x10000;
      units.append(0xD800 + (code >> 10));
      units.append(0xDC00 + (code & 0x3ff));
    }
  }
  return [units, clamped];
};

// ONE RFC 8259 number (read.go walkNumber, scanNumber): [token, integral] or
// null. "1-2", "+1", "01", ".5" and "3." are not JSON.
local scanNumber = function(r, ...) {
  local capped = vargv.len() == 0 || vargv[0];
  space(r);
  local t = r.t, n = r.n, start = r.p;
  local digit = function(i) { return i < n && t[i] >= '0' && t[i] <= '9'; };
  local integral = true;
  if (r.p < n && t[r.p] == '-') r.p++;
  if (r.p >= n) return null;
  if (t[r.p] == '0') r.p++;
  else if (t[r.p] >= '1' && t[r.p] <= '9') { while (digit(r.p)) r.p++; }
  else return null;
  if (r.p < n && t[r.p] == '.') {
    r.p++;
    if (!digit(r.p)) return null;
    while (digit(r.p)) r.p++;
    integral = false;
  }
  if (r.p < n && (t[r.p] == 'e' || t[r.p] == 'E')) {
    r.p++;
    if (r.p < n && (t[r.p] == '-' || t[r.p] == '+')) r.p++;
    if (!digit(r.p)) return null;
    while (digit(r.p)) r.p++;
    integral = false;
  }
  local len = r.p - start;
  if (len <= 0 || (capped && len > MAX_NUMBER)) return null;
  local bs = [];
  for (local i = start; i < r.p; i++) bs.append(t[i]);
  return [bytesToString(bs), integral];
};

// the label: a positive integer spelled as one, no sign, fraction, exponent
// or leading zero, within a u64 (§16.7, read.go scanLabel)
local scanLabel = function(r) {
  space(r);
  local t = r.t;
  if (r.p >= r.n || t[r.p] < '1' || t[r.p] > '9') { r.bad = true; return null; }
  local v = 0;
  while (r.p < r.n && t[r.p] >= '0' && t[r.p] <= '9') {
    local dg = t[r.p] - '0';
    // value > (MaxUint64 - digit) / 10, unsigned
    local lim = ((MAXU - dg) >>> 1) / 5;
    if (ult(lim, v)) { r.bad = true; return null; }
    v = v * 10 + dg;
    r.p++;
  }
  return v;
};

local skipValue, skipContainer;

// a skipped OBJECT may still be a pointer's: an "&node" opening it names a
// node the storage could not hold, and the label still exists (§16.7). A
// FIXED root skips the value whole: what it does not place it does not police.
local skippedAmpersand = function(r, key) {
  if (r.graph == null || key != "&node") return false;
  local label = scanLabel(r);
  if (label == null) return false;
  if (!(label in r.graph)) r.graph[label] <- { node = null, c = null, open = false };
  return true;
};

skipValue = function(r, depth) {
  local c = peek(r);
  if (c == '{') return skipContainer(r, '}', depth);
  if (c == '[') return skipContainer(r, ']', depth);
  if (c == '"') return scanBytes(r, 0) != null;
  if (c == 't') return literal(r, "true");
  if (c == 'f') return literal(r, "false");
  if (c == 'n') return literal(r, "null");
  if (c == 0) { r.bad = true; return false; }
  // a skipped number is walked, with no length cap (read.go skipValue)
  if (scanNumber(r, false) == null) { r.bad = true; return false; }
  return true;
};

skipContainer = function(r, close, depth) {
  if (depth > MAX_DEPTH) { r.bad = true; return false; }
  r.p++;
  local first = true;
  for (;;) {
    local c = peek(r);
    if (c == close) { r.p++; return true; }
    if (c == 0) { r.bad = true; return false; }
    if (close == '}') {
      local key = scanKey(r);
      if (key == null) return false;
      if (peek(r) != ':') { r.bad = true; return false; }
      r.p++;
      if (key.len() > 0 && key[0] == '&' && r.graph != null) {
        if (!first || !skippedAmpersand(r, key)) { r.bad = true; return false; }
        first = false;
        c = peek(r);
        if (c == ',') { r.p++; continue; }
        if (c == close) { r.p++; return true; }
        r.bad = true;
        return false;
      }
    }
    first = false;
    if (!skipValue(r, depth + 1)) return false;
    c = peek(r);
    if (c == ',') { r.p++; continue; }
    if (c == close) { r.p++; return true; }
    r.bad = true;
    return false;
  }
};

local readTable, readKeys, readField, readScalar, readPointerCell, readArray, readTextArray, readKeyed, readUnion, readMap;

// one table object (read.go readTable): keys are field keys; the instance is
// already at its declared defaults, so a key the text never mentions keeps
// the default an absent field takes on the wire (§4)
readTable = function(T, c, r, v, depth) {
  if (depth > MAX_DEPTH) { r.bad = true; return false; }
  if (has(c, "jsonArray")) {
    // THE ARRAY ROOT (§16.10)
    if (c.fields.len() != 1) { r.bad = true; return false; }
    if (valueShape(r) != 'a') {
      if (!("reason" in r.report) || r.report.reason == null) r.report.reason <- "json_root_not_array: " + c.name;
      r.bad = true;
      return false;
    }
    return readField(T, c, c.fields[0], r, v, depth);
  }
  if (peek(r) != '{') { r.bad = true; return false; }
  r.p++;
  return readKeys(T, c, r, v, depth, null);
};

// the keys of an object whose brace is consumed (read.go readTableKeys): an
// unknown key skipped and counted, a repeat last-wins and counted, null the
// absence for a ?T and a pointer, a value of the wrong shape skipped and
// counted, never coerced (§16.2). firstKey is a pointer's first key, which
// its reader scanned to see whether it was "&node".
readKeys = function(T, c, r, v, depth, firstKey) {
  local seen = {};
  for (;;) {
    local key;
    if (firstKey != null) {
      key = firstKey;
      firstKey = null;
    } else {
      local ch = peek(r);
      if (ch == '}') { r.p++; return checkRequired(T, c, r, v, seen); }
      if (ch == 0) { r.bad = true; return false; }
      key = scanKey(r);
      if (key == null) return false;
      if (peek(r) != ':') { r.bad = true; return false; }
      r.p++;
    }
    // THE AMPERSAND PREFIX IS RESERVED TO THE FORM (§16.7): malformed among
    // the keys a reader places, never unknown
    if (key.len() > 0 && key[0] == '&') { r.bad = true; return false; }
    if (!(key in c.byKey)) {
      r.report.unknown++;
      if (!skipValue(r, depth + 1)) return false;
    } else {
      local fd = c.byKey[key];
      if (fd.name in seen) r.report.duplicate++;
      seen[fd.name] <- true;
      local shape = valueShape(r);
      if (shape == 'z' && takesNull(fd)) {
        // null is the absence: a ?T reads it ABSENT and a pointer null, the
        // field back at its declared default
        if (!literal(r, "null")) return false;
        v[fd.member] = c.make()[fd.member];
        if (fd.optional) v[fd.member + "Present"] = false;
      } else {
        if (shape != shapeOf(T, fd)) {
          r.report.kindMismatch++;
          if (!skipValue(r, depth + 1)) return false;
        } else if (!readField(T, c, fd, r, v, depth)) {
          return false;
        }
        // PRESENCE OF THE KEY IS PRESENCE (§16.2)
        if (fd.optional) v[fd.member + "Present"] = true;
      }
    }
    local ch = peek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == '}') { r.p++; return checkRequired(T, c, r, v, seen); }
    r.bad = true;
    return false;
  }
};

// a bytes(N) or *bytes body six bits at a time (read.go readBase64): '='
// anywhere is padding, a byte outside the alphabet makes the body the wrong
// shape (one kind mismatch), and past the bound the excess is clamped.
// Answers [blob or null when not base64] or null where the text is not JSON.
local b64val = function(c) {
  if (c >= 'A' && c <= 'Z') return c - 'A';
  if (c >= 'a' && c <= 'z') return c - 'a' + 26;
  if (c >= '0' && c <= '9') return c - '0' + 52;
  if (c == '+') return 62;
  if (c == '/') return 63;
  return -1;
};
local readBase64 = function(r, bound) {
  if (peek(r) != '"') { r.bad = true; return null; }
  r.p++;
  local out = [];
  local acc = 0, held = 0, clamped = false, bad = false;
  for (;;) {
    if (r.p >= r.n) { r.bad = true; return null; }
    local c = r.t[r.p];
    r.p++;
    if (c == '"') break;
    if (c == '=' || bad) continue;
    local at = b64val(c);
    if (at < 0) { bad = true; continue; }
    acc = ((acc << 6) | at) & 0xffffff;
    held += 6;
    if (held >= 8) {
      held -= 8;
      if (bound < 0 || out.len() < bound) out.append((acc >> held) & 0xff);
      else clamped = true;
    }
  }
  if (bad) { r.report.kindMismatch++; return [null]; }
  if (clamped) r.report.clamped++;
  return [bytesToBlob(out)];
};

readField = function(T, c, fd, r, v, depth) {
  switch (fd.shape) {
    case "sblob": {
      v[fd.member] = null;
      local s = scanBytes(r, -1);
      if (s == null) return false;
      if (hasZero(s[0])) return textZero(r, c.name + "." + fd.name);
      v[fd.member] = bytesToString(s[0]);
      return true;
    }
    case "bblob": {
      v[fd.member] = null;
      local b = readBase64(r, -1);
      if (b == null) return false;
      v[fd.member] = b[0]; // not base64: counted, the reference left null
      return true;
    }
    case "ptr": return readPointerCell(T, tableDesc(T, fd.table), r, v, fd.member, depth);
    case "str": {
      local s = scanBytes(r, fd.bound > 0 ? fd.bound : -1);
      if (s == null) return false;
      if (hasZero(s[0])) return textZero(r, c.name + "." + fd.name);
      if (s[1]) r.report.clamped++;
      v[fd.member] = bytesToString(s[0]);
      return true;
    }
    case "wstr": {
      local w = scanWide(r, fd.bound, c.name + "." + fd.name);
      if (w == null) return false;
      if (w[1]) r.report.clamped++;
      v[fd.member] = w[0];
      return true;
    }
    case "bytes": {
      // bytes RE-ESTABLISH before decoding: a repeated key places a whole value
      v[fd.member] = blob(0);
      local b = readBase64(r, fd.bound);
      if (b == null) return false;
      if (b[0] != null) v[fd.member] = b[0];
      return true;
    }
    case "map": return readMap(T, c, fd, r, v, depth);
    case "arr":
      if (fd.textlist != 0) return readTextArray(T, c, fd, r, v, depth);
      if (isKeyed(fd)) return readKeyed(T, c, fd, r, v, depth);
      return readArray(T, c, fd, r, v, depth);
  }
  // a SCALAR is not re-established: it is written only when a value is
  // placed, so a refused repeat leaves the first occurrence standing
  return readScalar(T, fd, r, v, fd.member, depth, c.name + "." + fd.name);
};

// an array re-established before placing (read.go readArray, readTextList):
// a fixed array keeps its bound and its tail's zeros, a bounded one counts
// the excess clamped, a listed one reads every element; a null element of
// an array of pointers is a null slot
readArray = function(T, c, fd, r, v, depth) {
  if (peek(r) != '[') { r.bad = true; return false; }
  r.p++;
  local e = fd.elem;
  local fixed = !fd.list && !fd.counted;
  local out = [];
  if (fixed) {
    out = array(fd.bound);
    for (local i = 0; i < fd.bound; i++) out[i] = elemZero(T, e);
  }
  v[fd.member] = out;
  local bound = fd.list ? 0x7fffffff : fd.bound;
  local shape = elemShapeOf(T, e);
  local placed = 0;
  local text = e.shape == "str" || e.shape == "sblob" || e.shape == "bblob";
  for (;;) {
    local ch = peek(r);
    if (ch == ']') { r.p++; break; }
    if (ch == 0) { r.bad = true; return false; }
    if (!fixed && placed == out.len() && placed < bound) out.append(elemZero(T, e));
    local vs = valueShape(r);
    if (placed >= bound) {
      r.report.clamped++;
      if (!skipValue(r, depth + 1)) return false;
    } else if (vs == 'z' && (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob")) {
      if (!literal(r, "null")) return false;
      out[placed] = null;
      placed++;
    } else if (vs != shape) {
      r.report.kindMismatch++;
      if (!skipValue(r, depth + 1)) return false;
      placed++;
    } else if (text) {
      if (e.shape == "bblob") {
        local b = readBase64(r, -1);
        if (b == null) return false;
        out[placed] = b[0];
      } else {
        local s = scanBytes(r, e.shape == "str" && e.bound > 0 ? e.bound : -1);
        if (s == null) return false;
        if (hasZero(s[0])) return textZero(r, c.name + "." + fd.name);
        if (s[1]) r.report.clamped++;
        out[placed] = bytesToString(s[0]);
      }
      placed++;
    } else {
      if (!readScalar(T, e, r, out, placed, depth + 1, c.name + "." + fd.name)) return false;
      placed++;
    }
    ch = peek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == ']') { r.p++; break; }
    r.bad = true;
    return false;
  }
  if (!fixed && out.len() > placed) out.resize(placed);
  return true;
};

// A TEXT LIST's text form read (§2.13, §16.2): a plain array of strings into
// the generated element records; null is an absent `?string` element.
readTextArray = function(T, c, fd, r, v, depth) {
  if (peek(r) != '[') { r.bad = true; return false; }
  r.p++;
  local e = fd.elem;
  local ed = tableDesc(T, e.table);
  local out = [];
  local bound = fd.list ? 0x7fffffff : fd.bound;
  local vf = ed.fields[0];
  local capacity = (!vf.unbounded && vf.bound > 0) ? vf.bound : -1;
  for (;;) {
    local ch = peek(r);
    if (ch == ']') { r.p++; break; }
    if (ch == 0) { r.bad = true; return false; }
    local rec = ed.make();
    local vs = valueShape(r);
    if (out.len() >= bound) {
      r.report.clamped++;
      if (!skipValue(r, depth + 1)) return false;
    } else if (fd.textlist == 2 && vs == 'z') {
      if (!literal(r, "null")) return false;
      rec.ValuePresent = false;
      out.append(rec);
    } else if (vs != 's') {
      r.report.kindMismatch++;
      if (!skipValue(r, depth + 1)) return false;
      out.append(rec);
    } else {
      local s = scanBytes(r, capacity);
      if (s == null) return false;
      if (hasZero(s[0])) return textZero(r, c.name + "." + fd.name);
      if (s[1]) r.report.clamped++;
      rec.Value = bytesToString(s[0]);
      if (fd.textlist == 2) rec.ValuePresent = true;
      out.append(rec);
    }
    ch = peek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == ']') { r.p++; break; }
    r.bad = true;
    return false;
  }
  v[fd.member] = out;
  return true;
};

// an enum-keyed array: an OBJECT keyed by VARIANT NAME (read.go readKeyed).
// Every slot back to its zero once; an absent key keeps it; a key that names
// no slot, "None" included, is unknown each time and never a duplicate; a
// repeated slot is a duplicate, counted before the shape check.
readKeyed = function(T, c, fd, r, v, depth) {
  if (peek(r) != '{') { r.bad = true; return false; }
  r.p++;
  local e = fd.elem;
  local out = array(fd.bound);
  for (local i = 0; i < fd.bound; i++) out[i] = elemZero(T, e);
  v[fd.member] = out;
  local shape = elemShapeOf(T, e);
  local seen = {};
  for (;;) {
    local ch = peek(r);
    if (ch == '}') { r.p++; break; }
    if (ch == 0) { r.bad = true; return false; }
    local key = scanKey(r);
    if (key == null) return false;
    if (peek(r) != ':') { r.bad = true; return false; }
    r.p++;
    local slot = -1;
    if (fd.keyvariants != null) {
      for (local k = 0; k < fd.keyvariants.len() && k < fd.bound; k++) {
        if (fd.keyvariants[k][0] == key) { slot = k; break; }
      }
    }
    if (slot >= 0) {
      if (slot in seen) r.report.duplicate++;
      seen[slot] <- true;
    }
    if (slot < 0) {
      r.report.unknown++;
      if (!skipValue(r, depth + 1)) return false;
    } else if (valueShape(r) != shape) {
      r.report.kindMismatch++;
      if (!skipValue(r, depth + 1)) return false;
    } else if (!readScalar(T, e, r, out, slot, depth + 1, c.name + "." + fd.name)) {
      return false;
    }
    ch = peek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == '}') { r.p++; break; }
    r.bad = true;
    return false;
  }
  return true;
};

// A MAP from a JSON object (docs/SPEC-TABLES.md §2.8, §16.2): the object's KEY
// is the map's key and its value is the entry's value. A string key is its
// bytes; an integer key is its decimal spelling read by the integer rule
// alone. A key that is not a JSON number stops the read as malformed; a
// fractional or out-of-domain key drops the entry as kind_mismatch; a key past
// its storage drops it as clamped, because keys never clamp; a repeated key is
// last-wins and counted. The entries are sorted into ASCENDING KEY ORDER, the
// order the wire and the writer hold (§2.8).
readMap = function(T, c, fd, r, v, depth) {
  if (peek(r) != '{') { r.bad = true; return false; }
  r.p++;
  local kd = fd.mapkey, vd = fd.mapvalue;
  local ed = tableDesc(T, fd.entry);
  local out = [];
  for (;;) {
    local ch = peek(r);
    if (ch == '}') { r.p++; break; }
    if (ch == 0) { r.bad = true; return false; }
    // A STRING KEY IS READ AT ITS OWN DECLARATION'S BOUND (§2.8): a key past
    // its bound drops its entry as clamped, never as a shorter key. An
    // integer key's token is bounded by the number walk's own
    // tableJsonMaxNumber, and a token past it is the wrong shape.
    local kb = scanBytes(r, kd.shape == "str" ? -1 : MAX_NUMBER);
    if (kb == null) return false;
    local key = bytesToString(kb[0]);
    if (peek(r) != ':') { r.bad = true; return false; }
    r.p++;
    local ent = ed.make();
    local placed = false;
    if (kb[1]) {
      // A KEY THE SCAN COULD NOT HOLD WHOLE IS NOT A SHORTER KEY (§2.8)
      if (kd.shape == "str") r.report.clamped++;
      else r.report.kindMismatch++;
    } else if (kd.shape == "str") {
      if (hasZero(kb[0])) return textZero(r, c.name + "." + fd.name);
      if (kd.bound > 0 && key.len() > kd.bound) r.report.clamped++;
      else { ent[kd.member] = key; placed = true; }
    } else {
      local num = mapKeyNumber(key);
      if (num == null) {
        // a token the number rule calls malformed stops the read (§16.1)
        r.report.malformed = true;
        r.bad = true;
        return false;
      }
      local dom = mapKeyInDomain(kd, num);
      if (!dom[0]) r.report.kindMismatch++;
      else { ent[kd.member] = dom[1]; placed = true; }
    }
    local valueOk = false;
    if (!placed) {
      if (!skipValue(r, depth + 1)) return false;
    } else {
      local shape = valueShape(r);
      if (shape == 'z' && takesNull(vd)) {
        // null is the one spelling of an ABSENT map value (§2.8, §16.2)
        if (!literal(r, "null")) return false;
        local fresh = ed.make();
        ent[vd.member] = fresh[vd.member];
        if (vd.optional) ent[vd.member + "Present"] = false;
        valueOk = true;
      } else if (shape != shapeOf(T, vd)) {
        r.report.kindMismatch++;
        if (!skipValue(r, depth + 1)) return false;
      } else if (!readField(T, ed, vd, r, ent, depth)) {
        return false;
      } else {
        if (vd.optional) ent[vd.member + "Present"] = true;
        valueOk = true;
      }
    }
    if (valueOk) {
      local at = -1;
      for (local i = 0; i < out.len(); i++) {
        if (mapKeyOrder(kd, out[i][kd.member], ent[kd.member]) == 0) { at = i; break; }
      }
      if (at >= 0) { r.report.duplicate++; out[at] = ent; }
      else out.append(ent);
    }
    ch = peek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == '}') { r.p++; break; }
    r.bad = true;
    return false;
  }
  local order = [];
  for (local i = 0; i < out.len(); i++) order.append(i);
  local src = out;
  order.sort(function(x, y) {
    local o = mapKeyOrder(kd, src[x][kd.member], src[y][kd.member]);
    if (o != 0) return o;
    return x < y ? -1 : 1;
  });
  local sorted = array(out.len());
  for (local i = 0; i < order.len(); i++) sorted[i] = src[order[i]];
  v[fd.member] = sorted;
  return true;
};

// a pointer's object (§16.7, read.go readPointerCell): its FIRST key decides
// what it is — "&node" naming a label not yet defined, with fields after it,
// DEFINES it; "&node" naming one already defined, alone, REFERS to it; any
// other key is a node named once. A reference inside its own definition is
// the cycle the wire refuses; a reference to a dropped definition reads null.
readPointerCell = function(T, c, r, holder, key, depth) {
  if (r.graph == null) { r.bad = true; return false; }
  if (depth + 1 > MAX_DEPTH) { r.bad = true; return false; }
  if (peek(r) != '{') { r.bad = true; return false; }
  r.p++;
  local ch = peek(r);
  if (ch == '}') { r.p++; holder[key] = c.make(); return true; }
  if (ch == 0) { r.bad = true; return false; }
  local k = scanKey(r);
  if (k == null) return false;
  if (peek(r) != ':') { r.bad = true; return false; }
  r.p++;
  if (k != "&node") {
    local node = c.make();
    holder[key] = node;
    return readKeys(T, c, r, node, depth + 1, k);
  }
  local label = scanLabel(r);
  if (label == null) return false;
  local defined = label in r.graph;
  ch = peek(r);
  if (ch == ',') { r.p++; ch = peek(r); }
  local bare = ch == '}';
  if (bare != defined) { r.bad = true; return false; }
  if (bare) {
    r.p++;
    local entry = r.graph[label];
    if (entry.open) { r.bad = true; return false; }
    holder[key] = null;
    if (entry.c == null) return true;
    if (entry.c != c) { r.report.kindMismatch++; return true; }
    holder[key] = entry.node;
    return true;
  }
  local node = c.make();
  holder[key] = node;
  r.graph[label] <- { node = null, c = null, open = true };
  if (!readKeys(T, c, r, node, depth + 1, null)) return false;
  r.graph[label] = { node = node, c = c, open = false };
  return true;
};

// JSON HAS ONE NUMBER TYPE (read.go interpretInteger): a token's sign, its
// magnitude as 64 bits and whether it is fractional, saturated or finite.
// A field reads an exponent or fraction spelling through the double.
local interpretInteger = function(tok, integral) {
  local out = { mag = 0, neg = false, fractional = false, saturated = false, finite = true };
  if (integral) {
    local i = 0;
    if (tok[0] == '-') { out.neg = true; i = 1; }
    for (; i < tok.len(); i++) {
      local dg = tok[i] - '0';
      local lim = ((MAXU - dg) >>> 1) / 5;
      if (ult(lim, out.mag)) { out.mag = MAXU; out.saturated = true; break; }
      out.mag = out.mag * 10 + dg;
    }
    if (out.mag == 0) out.neg = false;
    return out;
  }
  local d = tokenFloat(tok);
  if (!isFinite(d)) { out.finite = false; return out; }
  out.neg = castf2i(d) < 0;
  local whole = out.neg ? -d : d;
  if (whole >= 18446744073709551616.0) { out.mag = MAXU; out.saturated = true; return out; }
  if (whole != floor(whole)) { out.fractional = true; return out; }
  if (whole >= 9223372036854775808.0) out.mag = (whole - 9223372036854775808.0).tointeger() | MIN;
  else out.mag = whole.tointeger();
  if (out.mag == 0) out.neg = false;
  return out;
};

// a declared bound as the double the descriptors' clamp compares, then back
// to a magnitude (read.go integerOf, bigToFloat)
local integerOf = function(bound) {
  local neg = bound < 0;
  local whole = neg ? -bound : bound;
  local mag;
  if (whole >= 18446744073709551616.0) mag = MAXU;
  else if (whole >= 9223372036854775808.0) mag = (whole - 9223372036854775808.0).tointeger() | MIN;
  else mag = whole.tointeger();
  return { mag = mag, neg = mag == 0 ? false : neg };
};

// an integer field takes any token whose VALUE is integral; a fractional
// value is the wrong shape. THE DECLARED RANGE FIRST, THEN THE STORAGE WIDTH,
// the wire's order (§4, read.go placeInteger).
local placeInteger = function(d, tok, integral, report) {
  local signed = d.kind >= 2 && d.kind <= 5;
  local num = interpretInteger(tok, integral);
  if (!num.finite || num.fractional) { report.kindMismatch++; return null; }
  if (num.saturated) report.clamped++;
  local mag = num.mag, neg = num.neg;
  local lo = null, hi = null;
  if (d.lo != null && typeof d.lo == "integer") {
    lo = d.signed ? d.lo.tofloat() : utof(d.lo);
    hi = d.signed ? d.hi.tofloat() : utof(d.hi);
  } else if (d.bits != null && d.bits > 0 && d.bits < 64) {
    lo = 0.0;
    hi = utof((1 << d.bits) - 1);
  }
  if (lo != null) {
    local scale = neg ? -utof(mag) : utof(mag);
    local b = null;
    if (scale < lo) b = integerOf(lo);
    else if (scale > hi) b = integerOf(hi);
    if (b != null) { mag = b.mag; neg = b.neg; report.clamped++; }
  }
  local bytes = d.width;
  if (d.bits != null && d.bits > 0) bytes = d.bits <= 32 ? 4 : 8;
  if (signed) {
    local high = bytes < 8 ? (1 << (bytes * 8 - 1)) - 1 : 0x7fffffffffffffff;
    if (neg) {
      local low = high + 1;
      if (ult(low, mag)) { mag = low; report.clamped++; }
      return -mag;
    }
    if (ult(high, mag)) { mag = high; report.clamped++; }
    return mag;
  }
  // A NEGATIVE TOKEN IN AN UNSIGNED FIELD CLAMPS TO ZERO
  if (neg) { report.clamped++; return 0; }
  local high = bytes < 8 ? (1 << (bytes * 8)) - 1 : MAXU;
  if (ult(high, mag)) { mag = high; report.clamped++; }
  return mag;
};

// a float field (read.go placeFloat): ONE correctly-rounded conversion at the
// field's own width; a magnitude the format cannot hold is the wrong shape and
// never reaches storage; a compressed float clamps to its range, -0 to +0
local placeFloat = function(d, tok, report) {
  local value;
  if (d.f32) {
    local pr = parseF32(tok);
    value = pr[0];
  } else {
    value = tokenFloat(tok);
  }
  if (!isFinite(value)) { report.kindMismatch++; return null; }
  if (has(d, "fmin")) {
    if (value < d.fmin) { value = d.fmin; report.clamped++; }
    else if (value > d.fmax) { value = d.fmax; report.clamped++; }
    else if (value == 0.0) value = 0.0;
  }
  if (d.f32) {
    local nb = narrowF32(value, 0)[0];
    local back = widenF32(nb);
    if (!isFinite(back)) { report.kindMismatch++; return null; }
    value = back;
  }
  return value;
};

// a wide kind (read.go placeWide): exactly representable or the wrong shape;
// a magnitude past the storage saturates and counts; the declared raw range
// clamps after it
local placeWide = function(d, tok, report) {
  local pw = parseWide(tok, d.kind, has(d, "frac") ? d.frac : 0);
  if (!pw[1]) { report.kindMismatch++; return null; }
  if (pw[2]) report.clamped++;
  local v = pw[0];
  if (d.lo != null) {
    local lo = typeof d.lo == "array" ? svFromPair(d.lo, d.signed) : svFromInt(d.lo, d.signed);
    local hi = typeof d.hi == "array" ? svFromPair(d.hi, d.signed) : svFromInt(d.hi, d.signed);
    if (svCmp(v, lo) < 0) { v = lo; report.clamped++; }
    else if (svCmp(v, hi) > 0) { v = hi; report.clamped++; }
  }
  return wideStore(d, v);
};

local readFlags = function(d, r, depth) {
  if (peek(r) != '[') { r.bad = true; return null; }
  r.p++;
  local names = has(d, "flagNames") ? d.flagNames : [];
  local bits = 0;
  for (;;) {
    local ch = peek(r);
    if (ch == ']') { r.p++; break; }
    if (ch == 0) { r.bad = true; return null; }
    if (ch != '"') {
      r.report.kindMismatch++;
      if (!skipValue(r, depth + 1)) return null;
    } else {
      local name = scanKey(r);
      if (name == null) return null;
      local found = false;
      for (local bit = 0; bit < names.len(); bit++) {
        if (names[bit] == name) { bits = bits | (1 << bit); found = true; break; }
      }
      if (!found) r.report.unknown++;
    }
    ch = peek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == ']') { r.p++; break; }
    r.bad = true;
    return null;
  }
  return [bits];
};

// one value at one cell, holder[key] (read.go readScalar)
readScalar = function(T, d, r, holder, key, depth, where) {
  switch (d.shape) {
    case "union": return readUnion(T, unionDesc(T, d.table), r, holder, key, depth);
    case "ptr": return readPointerCell(T, tableDesc(T, d.table), r, holder, key, depth);
    case "tab": {
      local c = tableDesc(T, d.table);
      local inst = c.make();
      holder[key] = inst;
      return readTable(T, c, r, inst, depth + 1);
    }
    case "enum": {
      local name = scanKey(r);
      if (name == null) return false;
      if (name == "None") { holder[key] = null; return true; }
      foreach (p in d.variants) {
        if (p[0] == name) {
          // a DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, never
          // unknown (docs/SPEC-TABLES.md §2.15); an element's d is its element
          // descriptor, whose default is the element default
          holder[key] = (p.len() > 2 && p[2]) ? d.def : name;
          return true;
        }
      }
      // a name this build cannot name reads None and counts unknown (§4)
      holder[key] = null;
      r.report.unknown++;
      return true;
    }
    case "flags": {
      local f = readFlags(d, r, depth);
      if (f == null) return false;
      holder[key] = f[0];
      return true;
    }
    case "str": {
      local s = scanBytes(r, d.bound > 0 ? d.bound : -1);
      if (s == null) return false;
      if (hasZero(s[0])) return textZero(r, where);
      if (s[1]) r.report.clamped++;
      holder[key] = bytesToString(s[0]);
      return true;
    }
    case "wstr": {
      local w = scanWide(r, d.bound, where);
      if (w == null) return false;
      if (w[1]) r.report.clamped++;
      holder[key] = w[0];
      return true;
    }
    case "bool":
      if (peek(r) == 't') {
        if (!literal(r, "true")) return false;
        holder[key] = true;
        return true;
      }
      if (!literal(r, "false")) return false;
      holder[key] = false;
      return true;
  }
  if (d.kind == 36) {
    // §16.2's timestamp row: RFC 3339, refused by name when it does not parse
    local s = scanBytes(r, -1);
    if (s == null) return false;
    local ns = timestampParse(bytesToString(s[0]));
    if (ns == null) {
      r.report.reason <- "timestamp_not_rfc3339: " + (("name" in d) ? d.name : "element");
      r.bad = true;
      return false;
    }
    holder[key] = ns;
    return true;
  }
  local num = scanNumber(r);
  if (num == null) { r.bad = true; return false; }
  local x;
  if (d.shape == "float") x = placeFloat(d, num[0], r.report);
  else if (wideKind(d.kind)) x = placeWide(d, num[0], r.report);
  else if (d.shape == "int") x = placeInteger(d, num[0], num[1], r.report);
  else { r.bad = true; return false; }
  if (x != null) holder[key] = x;
  return true;
};

// a union (read.go readUnion): an object with ONE key, the arm's name; {}
// is None; two keys is malformed. The arm's value takes the arm's own row;
// a value of the wrong shape is a kind mismatch and the union reads None.
readUnion = function(T, u, r, holder, key, depth) {
  if (peek(r) != '{') { r.bad = true; return false; }
  r.p++;
  local inst = u.make();
  holder[key] = inst;
  if (peek(r) == '}') { r.p++; return true; }
  local name = scanKey(r);
  if (name == null) return false;
  if (peek(r) != ':') { r.bad = true; return false; }
  r.p++;
  local arm = null;
  foreach (a in u.arms) if (a.name == name) { arm = a; break; }
  if (arm == null) {
    r.report.unknown++;
    if (!skipValue(r, depth + 1)) return false;
  } else if (arm.payload == null) {
    // A PAYLOAD-FREE ARM'S VALUE IS null: the name selects it
    if (valueShape(r) != 'z') {
      r.report.kindMismatch++;
      if (!skipValue(r, depth + 1)) return false;
    } else {
      if (!literal(r, "null")) return false;
      inst.Type = arm.tag;
    }
  } else if (arm.payload.shape == "tab") {
    local p = arm.payload;
    if (valueShape(r) != 'o') {
      r.report.kindMismatch++;
      if (!skipValue(r, depth + 1)) return false;
    } else {
      local pc = tableDesc(T, p.table);
      local payload = pc.make();
      if (!readTable(T, pc, r, payload, depth + 1)) return false;
      inst.Type = arm.tag;
      inst[p.member] = payload;
    }
  } else {
    // an arm is a field line: its value reads through the field walk one key
    // down, into a fresh arm
    local p = arm.payload;
    local fresh = u.make();
    local shape = valueShape(r);
    if (shape == 'z' && takesNull(p)) {
      if (!literal(r, "null")) return false;
      inst.Type = arm.tag;
      inst[p.member] = fresh[p.member];
    } else if (shape != shapeOf(T, p)) {
      r.report.kindMismatch++;
      if (!skipValue(r, depth + 1)) return false;
    } else {
      if (!readField(T, u, p, r, fresh, depth + 1)) return false;
      inst.Type = arm.tag;
      inst[p.member] = fresh[p.member];
      if (p.optional) inst[p.member + "Present"] = true;
    }
  }
  local ch = peek(r);
  if (ch == ',') { r.p++; ch = peek(r); }
  if (ch == '}') { r.p++; return true; }
  r.bad = true; // a second key: a one-of with two arms is not a value
  return false;
};

// ONE TEXT (read.go Read): the root's object and nothing after it. The
// instance holds what was placed before any stop; a text that is not JSON,
// or whose labels do not resolve, is malformed. A VARIABLE root reads with
// the label map; a fixed root has none, which is what makes "&node" a
// refusal there (§16.7). This is the TOLERANT read: it keeps the recovered
// value and its counters; the plain FromJson is the strict read (§6.5).
_M.FromJsonTolerant <- function(T, rootName, text, report) {
  foreach (k in ["unknown", "kindMismatch", "widened", "clamped", "duplicate"]) {
    if (!(k in report)) report[k] <- 0;
  }
  if (!("malformed" in report)) report.malformed <- false;
  if (!("refused" in report)) report.refused <- false;
  local c = tableDesc(T, rootName);
  local r = newIn(text, report, c.variable ? {} : null);
  local v = c.make();
  local ok = readTable(T, c, r, v, 0);
  if (ok) {
    space(r);
    if (r.p != r.n) r.bad = true; // trailing rubbish is not one text
  }
  if (r.bad || !ok) report.malformed = true;
  return v;
};

// THE STRICT FromJson (docs/SPEC-TABLES.md §6.5): the plain name is the strict
// read. It runs the tolerant read and refuses whatever ReadStrict refuses,
// leaving the reason and the field in the report and answering null. A
// missing | required field is named as ReadStrict names it: missing_required,
// with the first missing field in report.field (§2.3, §16.2).
_M.FromJson <- function(T, rootName, text, report) {
  if (report == null) report = {};
  local v = _M.FromJsonTolerant(T, rootName, text, report);
  local reason = ("reason" in report) ? report.reason : null;
  local field = null;
  if (reason != null) {
    local p = "missing_required: ";
    if (reason.len() >= p.len() && reason.slice(0, p.len()) == p) {
      local names = reason.slice(p.len());
      local comma = names.find(",");
      field = comma == null ? names : names.slice(0, comma);
      reason = "missing_required";
    }
  }
  if (reason == null && report.malformed) reason = "malformed";
  if (reason == null && report.clamped > 0) reason = "clamped";
  if (reason == null && report.kindMismatch > 0) reason = "kind_mismatch";
  if (reason != null) {
    report.reason <- reason;
    report.field <- field;
    return null;
  }
  return v;
};
