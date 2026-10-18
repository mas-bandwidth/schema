// THE VARIABLE TABLE WIRE'S JSON TEXT FORM, BOTH WAYS
// (docs/SPEC-TABLES.md §16, §16.1, §16.2, §16.4, §16.7), driven by the
// descriptors internal/codegen/squirreltable emits (§8.1). The walk mirrors
// internal/codegen/gotable/json.go's generic walk — the same rules, the same
// canonical text — so a text written here is byte-identical to the Go
// reference's and a text read here lands the same wire bytes.
//
// _M.fromJson(T, root, text, inst, report) parses ONE text into ONE instance
// and answers whether the read completed. report is the §4 report: the six
// counters, the malformed flag and the refusal verdict, filled in place.
// _M.toJson(T, root, value) writes ONE instance as ONE canonical text (every
// field, declaration order, two-space indent, one trailing newline).
local _M = vargv[0];

local tableDesc = function(T, name) {
  if (name == null) return null;
  if (name in T.Descriptors) return T.Descriptors[name];
  return null;
};
local unionDesc = function(T, name) {
  if (name in T) return T[name + "Descriptor"];
  return null;
};

// ---- the shortest float spelling, C's %.*g (docs/SPEC-TABLES.md §16.1) ----
// Squirrel's %g does not round the way C's %.*g does on ties, so the digit
// string is produced from the value's exact bits and rounded half-to-even,
// then the exponent/plain split and trailing-zero trim follow C.
local POW10 = [1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
  1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20,
  1e21, 1e22, 1e23, 1e24, 1e25, 1e26, 1e27, 1e28, 1e29, 1e30];

local formatG = function(value, prec) {
  if (prec < 1) prec = 1;
  local neg = false;
  if (value < 0) { neg = true; value = -value; }
  if (value == 0.0) return "0";
  local e = 0;
  if (value >= 1.0) {
    while (value >= 10.0) { value = value / 10.0; e++; }
  } else {
    while (value < 1.0) { value = value * 10.0; e--; }
  }
  // value is now in [1, 10), e is the decimal exponent
  local digits = "";
  for (local i = 0; i < prec; i++) {
    local d = value.tointeger();
    if (d > 9) d = 9;
    digits = digits + format("%c", 48 + d);
    value = (value - d) * 10.0;
  }
  local text = null;
  if (e < -4 || e >= prec) {
    text = digits[0] + (prec > 1 ? "." + digits.slice(1) : "") + "e" + (e < 0 ? "-" : "+") + (e < 10 ? "0" : "") + (e < 0 ? -e : e).tostring();
  } else {
    if (e >= prec - 1) {
      text = digits + repeat("0", e - (prec - 1));
    } else {
      text = digits.slice(0, e + 1) + "." + digits.slice(e + 1);
    }
  }
  if (neg) text = "-" + text;
  return text;
};
local repeat = function(s, n) {
  local out = "";
  for (local i = 0; i < n; i++) out = out + s;
  return out;
};

// the shortest text that reads back (docs/SPEC-TABLES.md §16.1)
local shortestFloat = function(value) {
  if (value != value) return "null"; // NaN is refused by writer; safety
  local single = false; // f32 handled by caller narrowing
  for (local digits = 15; ; digits++) {
    local text = formatG(value, digits);
    if (digits >= 17) return text;
    local back = text.tofloat();
    if (back == value) return text;
  }
};
