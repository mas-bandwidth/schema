package luatable

import (
	"fmt"
	"strings"
	"testing"
)

// FLOAT32 CELLS AND THE FINITE-ONLY RULE (docs/PORTING.md M21, retired here).
// The table wire reads a kind-10 cell through string.unpack("<f"), a C float
// conversion, and writes one through string.pack("<f"). The method exists for
// the one class of values a conversion rewrites: a NaN's payload and the
// signalling bit. Under the finite-only rule (docs/SPEC-TABLES.md §3.3) a NaN
// and an infinity never reach the conversion, because every read refuses them
// on their raw bits first (TestLuaPlainFloatSpecialValuesAgreeAcrossForms, and
// its reversed witness TestLuaFiniteFloatNegativeControl, hold each read) and
// every writer refuses them; what is left for the conversion is the finite
// float32, exact in both directions. This test holds that last claim on the
// pinned interpreter: every finite float32 bit pattern in a sweep that takes
// all 254 exponents of both signs with the mantissa corners, every subnormal
// at a stride, and two hundred thousand pseudo-random patterns, goes
// bits -> double -> bits without moving one bit, and widening to a double and
// narrowing back is the identity.

const float32ConversionProbe = `local bad, n = 0, 0
local function check(bits)
  n = n + 1
  local x = string.unpack("<f", string.pack("<I4", bits))
  if x ~= x or x - x ~= 0 then bad = bad + 1 return end
  if string.unpack("<I4", string.pack("<f", x)) ~= bits then bad = bad + 1 end
  -- a value that is already a float32 narrows to itself
  if string.unpack("<f", string.pack("<f", x)) ~= x and x == x then bad = bad + 1 end
end
local corners = { 0, 1, 2, 0x3fffff, 0x400000, 0x400001, 0x7ffffe, 0x7fffff }
for sign = 0, 1 do
  for exp = 0, 254 do
    for _, m in ipairs(corners) do check((sign << 31) | (exp << 23) | m) end
  end
end
for m = 1, 0x7fffff, 7 do check(m); check(0x80000000 | m) end
local state = 0x2545f4914f6cdd1d
for _ = 1, 200000 do
  state = state ~ (state << 13); state = state ~ (state >> 7); state = state ~ (state << 17)
  local bits = state & 0xffffffff
  if (bits >> 23) & 0xff ~= 0xff then check(bits) end
end
print(n, bad)
`

func TestLuaFloat32ConversionIsExactForEveryFinitePattern(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "tabledemo")
	out, stderr, err := try(t, b.files, float32ConversionProbe, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	var n, bad int
	if _, err := fmt.Sscan(strings.TrimSpace(out), &n, &bad); err != nil || n < 1000000 {
		t.Fatalf("the sweep did not run: %q (%v)", out, err)
	}
	if bad != 0 {
		t.Errorf("%d of %d finite float32 patterns moved a bit through the conversion: the finite-only rule no longer retires M21", bad, n)
	}
}
