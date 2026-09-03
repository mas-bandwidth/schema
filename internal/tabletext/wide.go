package tabletext

import (
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The WIDE kinds' text (docs/SPEC-TABLES.md §16.2): a 128-bit integer is a
// decimal integer, and a fixed-point value is a decimal in WHOLE UNITS — the
// spelling SPEC.md §4.6 gives a fixed default (`1.0`, `0.5`) — exact in both
// directions. Nothing here passes through a double: a Q112.16 value has more
// digits than a double carries, and the reader's rule is exactness, not
// nearness. The C++ walk implements the same two conversions over two 64-bit
// lanes; this engine uses big.Int and lands on the same bytes and the same
// counters, which the hostile corpus holds.

// wideLimits is the 128-bit domain of a kind's signedness — the saturation
// bound a text value clamps to before the declared range is applied, exactly
// as an int64 field saturates at INT64_MAX before its `| max` (§16.2). It is
// 128 bits for every wide kind, the narrow fixed widths included: the walk
// converts in 128 bits and the DECLARED RANGE, which every fixed field has, is
// what lands the value inside its storage.
func wideLimits(kind int) (lo, hi *big.Int) {
	bits := uint(128)
	one := big.NewInt(1)
	if ir.TableKindSigned(kind) {
		half := new(big.Int).Lsh(one, bits-1)
		return new(big.Int).Neg(half), new(big.Int).Sub(half, one)
	}
	return new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(one, bits), one)
}

// The decimal-exponent band a token is normalized into before conversion.
// An integer part past 40 digits is above 2^128 whatever the digits are, and
// a value below 10^-40 is finer than 2^-127, the finest fraction any F can
// spell — so outside the band the answer is known without the arithmetic,
// and a token spelling 1e999999999 costs nothing to refuse.
const wideDecimalBand = 40

// ParseWide converts one JSON number token into a wide kind's RAW integer:
// the value × 2^frac. exact is false when the value is not representable in
// Q I.F — a genuinely finer fraction, the same wrong-shape event a fractional
// token is for an integer field — and nothing is placed. saturated is true
// when the magnitude was clamped to the kind's storage domain, which counts
// as a clamp; the declared range is the caller's clamp, applied after.
func ParseWide(token string, kind, frac int) (raw *big.Int, exact, saturated bool) {
	i := 0
	negative := false
	if i < len(token) && (token[i] == '-' || token[i] == '+') {
		negative = token[i] == '-'
		i++
	}
	var intDigits, fracDigits strings.Builder
	for ; i < len(token) && token[i] >= '0' && token[i] <= '9'; i++ {
		intDigits.WriteByte(token[i])
	}
	if i < len(token) && token[i] == '.' {
		i++
		for ; i < len(token) && token[i] >= '0' && token[i] <= '9'; i++ {
			fracDigits.WriteByte(token[i])
		}
	}
	exp := 0
	if i < len(token) && (token[i] == 'e' || token[i] == 'E') {
		i++
		expNegative := false
		if i < len(token) && (token[i] == '-' || token[i] == '+') {
			expNegative = token[i] == '-'
			i++
		}
		for ; i < len(token) && token[i] >= '0' && token[i] <= '9'; i++ {
			if exp < 100000 {
				exp = exp*10 + int(token[i]-'0')
			}
		}
		if expNegative {
			exp = -exp
		}
	}
	// the digits with the point after `point` of them, leading and trailing
	// zeros stripped
	digits := intDigits.String() + fracDigits.String()
	point := intDigits.Len() + exp
	for len(digits) > 0 && digits[0] == '0' {
		digits = digits[1:]
		point--
	}
	digits = strings.TrimRight(digits, "0")
	lo, hi := wideLimits(kind)
	if len(digits) == 0 {
		return new(big.Int), true, false
	}
	if point > wideDecimalBand {
		// above 2^128 whatever the digits say: the storage domain's end
		if negative {
			if lo.Sign() == 0 {
				return new(big.Int), true, true
			}
			return new(big.Int).Set(lo), true, true
		}
		return new(big.Int).Set(hi), true, true
	}
	if point < -wideDecimalBand {
		return nil, false, false // finer than any F can spell
	}
	// value = digits × 10^(point - len(digits)); raw = value × 2^frac
	mantissa, _ := new(big.Int).SetString(digits, 10)
	scale := point - len(digits)
	raw = new(big.Int).Lsh(mantissa, uint(frac))
	if scale >= 0 {
		raw.Mul(raw, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil))
	} else {
		den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-scale)), nil)
		var rem big.Int
		raw.QuoRem(raw, den, &rem)
		if rem.Sign() != 0 {
			return nil, false, false
		}
	}
	if negative {
		raw.Neg(raw)
	}
	if raw.Cmp(lo) < 0 {
		return new(big.Int).Set(lo), true, true
	}
	if raw.Cmp(hi) > 0 {
		return new(big.Int).Set(hi), true, true
	}
	return raw, true, false
}

// FormatWide spells a wide kind's raw integer as §16.2's text: a 128-bit
// integer as a decimal integer, a fixed value as the shortest exact decimal
// in whole units with at least one fractional digit — `1.0`, `-0.25`,
// `3.0000152587890625` — the spelling the schema text gives a fixed default.
func FormatWide(raw *big.Int, kind, frac int) string {
	if kind == ir.TableKindI128 || kind == ir.TableKindU128 {
		return raw.String()
	}
	var b strings.Builder
	magnitude := new(big.Int).Abs(raw)
	if raw.Sign() < 0 {
		b.WriteByte('-')
	}
	whole := new(big.Int).Rsh(magnitude, uint(frac))
	fraction := new(big.Int).And(magnitude, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(frac)), big.NewInt(1)))
	b.WriteString(whole.String())
	b.WriteByte('.')
	if fraction.Sign() == 0 {
		b.WriteByte('0')
		return b.String()
	}
	// each ×10 yields one digit above the fraction bits; it terminates
	// because a dyadic fraction has a finite decimal expansion (at most F digits)
	ten := big.NewInt(10)
	for fraction.Sign() != 0 {
		fraction.Mul(fraction, ten)
		digit := new(big.Int).Rsh(fraction, uint(frac))
		b.WriteByte(byte('0' + digit.Int64()))
		fraction.And(fraction, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(frac)), big.NewInt(1)))
	}
	return b.String()
}

// WideBytes renders a raw integer as its two's-complement bytes at a kind's
// width, little-endian — the wire's payload and the little-endian region's
// slot alike.
func WideBytes(raw *big.Int, kind int) []byte {
	width := ir.TableKindWidth(kind)
	v := new(big.Int).Set(raw)
	if v.Sign() < 0 {
		v.Add(v, new(big.Int).Lsh(big.NewInt(1), uint(width*8)))
	}
	out := make([]byte, width)
	v.FillBytes(out) // big-endian
	for i, j := 0, width-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// WideFromBytes is the inverse: little-endian two's-complement bytes at a
// kind's width, sign-extended for a signed kind.
func WideFromBytes(b []byte, kind int) *big.Int {
	width := ir.TableKindWidth(kind)
	be := make([]byte, width)
	for i := range width {
		be[width-1-i] = b[i]
	}
	v := new(big.Int).SetBytes(be)
	if ir.TableKindSigned(kind) && width > 0 && b[width-1]&0x80 != 0 {
		v.Sub(v, new(big.Int).Lsh(big.NewInt(1), uint(width*8)))
	}
	return v
}

// WideClamp applies a field's declared raw range (ir.TableRawRange) to a
// decoded value, the wire's rule for every bounded scalar (docs/SPEC-TABLES.md
// §4): the value lands on the nearer bound and the event is counted.
func WideClamp(raw *big.Int, f *ir.Field) (*big.Int, bool) {
	lo, hi, ok := ir.TableRawRange(f)
	if !ok {
		return raw, false
	}
	if raw.Cmp(lo) < 0 {
		return new(big.Int).Set(lo), true
	}
	if raw.Cmp(hi) > 0 {
		return new(big.Int).Set(hi), true
	}
	return raw, false
}
