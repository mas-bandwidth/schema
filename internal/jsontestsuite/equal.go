// Equal is the semantic-equal oracle for JSON texts, by docs/SPEC-TABLES.md
// §16.12: two JSON LAYER texts hold the same value when they read as one value
// each under §16.12's grammar and the values are equal. Whitespace between
// tokens is insignificant, a string spelling an ASCII character as its \u
// escape is the same string, object members are compared positionally (key
// order is part of the text), and a number is compared by its value without
// being expanded (1e999999999 compares in constant memory). The walk is bounded
// by §16.12's caps: depth 128. Standard library only.
package jsontestsuite

import (
	"bytes"
	"fmt"
	"math/big"
	"strconv"
)

// Equal reports whether the two JSON texts hold the same value and, if not,
// where they first differ (a path into the text).
func Equal(a, b []byte) (bool, string) {
	ra := &reader{data: a}
	rb := &reader{data: b}
	skipBOM(ra)
	skipBOM(rb)
	ok, why := eqValue(ra, rb, 0, "$")
	if !ok {
		return false, why
	}
	ra.skipWS()
	rb.skipWS()
	if ra.i != len(ra.data) || rb.i != len(rb.data) {
		return false, fmt.Sprintf("$: trailing input differs (byte %d)", ra.i)
	}
	return true, ""
}

// reader walks one JSON text.
type reader struct {
	data []byte
	i    int
}

// skipWS advances over whitespace and, as the §16.2 dialect accepts on read
// (§16.12), comments: `/* */` and `//` to end of line or input.
func (r *reader) skipWS() {
	for r.i < len(r.data) {
		switch r.data[r.i] {
		case ' ', '\t', '\n', '\r':
			r.i++
		case '/':
			if r.i+1 < len(r.data) && r.data[r.i+1] == '*' {
				r.i += 2
				for r.i+1 < len(r.data) && !(r.data[r.i] == '*' && r.data[r.i+1] == '/') {
					r.i++
				}
				r.i += 2
			} else if r.i+1 < len(r.data) && r.data[r.i+1] == '/' {
				r.i += 2
				for r.i < len(r.data) && r.data[r.i] != '\n' {
					r.i++
				}
			} else {
				return
			}
		default:
			return
		}
	}
}

// skipBOM skips a leading UTF-8 byte-order mark (RFC 8259 §8.1; §16.12), the
// only place those bytes are not data.
func skipBOM(r *reader) {
	if r.i+3 <= len(r.data) && r.data[r.i] == 0xEF && r.data[r.i+1] == 0xBB && r.data[r.i+2] == 0xBF {
		r.i += 3
	}
}

type kind int

const (
	kNum kind = iota
	kStr
	kObj
	kArr
	kLit
)

func kindOf(c byte) kind {
	switch c {
	case '{':
		return kObj
	case '[':
		return kArr
	case '"':
		return kStr
	case 't', 'f', 'n':
		return kLit
	default:
		return kNum
	}
}

func kindName(c byte) string {
	switch c {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f', 'n':
		return "literal"
	default:
		return "number"
	}
}

func eqValue(ra, rb *reader, depth int, path string) (bool, string) {
	if depth > 128 {
		return false, path + ": past the depth cap of 128 (§16.12)"
	}
	ra.skipWS()
	rb.skipWS()
	if ra.i >= len(ra.data) || rb.i >= len(rb.data) {
		return false, path + ": one text ends early"
	}
	ca, cb := ra.data[ra.i], rb.data[rb.i]
	ka, kb := kindOf(ca), kindOf(cb)
	if ka != kb {
		return false, fmt.Sprintf("%s: %s vs %s (byte %d)", path, kindName(ca), kindName(cb), ra.i)
	}
	switch ka {
	case kObj:
		return eqObject(ra, rb, depth, path)
	case kArr:
		return eqArray(ra, rb, depth, path)
	case kStr:
		return eqString(ra, rb, path)
	case kLit:
		return eqLiteral(ra, rb, path)
	default:
		return eqNumber(ra, rb, path)
	}
}

func eqObject(ra, rb *reader, depth int, path string) (bool, string) {
	ra.i++
	rb.i++
	for {
		ra.skipWS()
		rb.skipWS()
		if ra.i < len(ra.data) && rb.i < len(rb.data) && ra.data[ra.i] == '}' && rb.data[rb.i] == '}' {
			ra.i++
			rb.i++
			return true, ""
		}
		ka, oka := ra.parseString()
		kb, okb := rb.parseString()
		if !oka || !okb {
			return false, path + ": bad key"
		}
		if ka != kb {
			return false, fmt.Sprintf("%s: key %q != %q", path, ka, kb)
		}
		ra.skipWS()
		rb.skipWS()
		if ra.i >= len(ra.data) || rb.i >= len(rb.data) || ra.data[ra.i] != ':' || rb.data[rb.i] != ':' {
			return false, path + ": expected ':'"
		}
		ra.i++
		rb.i++
		if ok, why := eqValue(ra, rb, depth+1, path+"."+ka); !ok {
			return false, why
		}
		ra.skipWS()
		rb.skipWS()
		if ra.i < len(ra.data) && rb.i < len(rb.data) && ra.data[ra.i] == ',' && rb.data[rb.i] == ',' {
			ra.i++
			rb.i++
			continue
		}
		if ra.i < len(ra.data) && rb.i < len(rb.data) && ra.data[ra.i] == '}' && rb.data[rb.i] == '}' {
			ra.i++
			rb.i++
			return true, ""
		}
		return false, path + ": object structure differs"
	}
}

func eqArray(ra, rb *reader, depth int, path string) (bool, string) {
	ra.i++
	rb.i++
	idx := 0
	for {
		ra.skipWS()
		rb.skipWS()
		if ra.i < len(ra.data) && rb.i < len(rb.data) && ra.data[ra.i] == ']' && rb.data[rb.i] == ']' {
			ra.i++
			rb.i++
			return true, ""
		}
		child := fmt.Sprintf("%s[%d]", path, idx)
		if ok, why := eqValue(ra, rb, depth+1, child); !ok {
			return false, why
		}
		idx++
		ra.skipWS()
		rb.skipWS()
		if ra.i < len(ra.data) && rb.i < len(rb.data) && ra.data[ra.i] == ',' && rb.data[rb.i] == ',' {
			ra.i++
			rb.i++
			continue
		}
		if ra.i < len(ra.data) && rb.i < len(rb.data) && ra.data[ra.i] == ']' && rb.data[rb.i] == ']' {
			ra.i++
			rb.i++
			return true, ""
		}
		return false, path + ": array structure differs"
	}
}

func eqString(ra, rb *reader, path string) (bool, string) {
	sa, oka := ra.parseString()
	sb, okb := rb.parseString()
	if !oka || !okb {
		return false, path + ": bad string"
	}
	if sa != sb {
		return false, path + ": string differs"
	}
	return true, ""
}

func eqLiteral(ra, rb *reader, path string) (bool, string) {
	lits := map[byte]string{'t': "true", 'f': "false", 'n': "null"}
	la := lits[ra.data[ra.i]]
	lb := lits[rb.data[rb.i]]
	if la != lb {
		return false, path + ": literal differs"
	}
	if !bytes.Equal(ra.data[ra.i:ra.i+len(la)], []byte(la)) || !bytes.Equal(rb.data[rb.i:rb.i+len(lb)], []byte(lb)) {
		return false, path + ": bad literal"
	}
	ra.i += len(la)
	rb.i += len(lb)
	return true, ""
}

func eqNumber(ra, rb *reader, path string) (bool, string) {
	na, ta, oka := ra.parseNumber()
	nb, tb, okb := rb.parseNumber()
	if !oka || !okb {
		return false, path + ": bad number"
	}
	if !numbersEqual(na, nb) {
		return false, fmt.Sprintf("%s: number %s != %s", path, ta, tb)
	}
	return true, ""
}

// number is a JSON number kept unexpanded: value = ±dig·10^(exp−frac), where
// exp is arbitrary precision (§16.12's number token cap is 511 bytes) so a
// beam like 1e999999999 compares in constant memory.
type number struct {
	neg  bool
	dig  *big.Int
	exp  *big.Int
	frac int
}

func (r *reader) parseNumber() (number, string, bool) {
	start := r.i
	neg := false
	if r.i < len(r.data) && r.data[r.i] == '-' {
		neg = true
		r.i++
	}
	intStart := r.i
	for r.i < len(r.data) && r.data[r.i] >= '0' && r.data[r.i] <= '9' {
		r.i++
	}
	intPart := string(r.data[intStart:r.i])
	fracPart := ""
	if r.i < len(r.data) && r.data[r.i] == '.' {
		r.i++
		fStart := r.i
		for r.i < len(r.data) && r.data[r.i] >= '0' && r.data[r.i] <= '9' {
			r.i++
		}
		fracPart = string(r.data[fStart:r.i])
	}
	e := new(big.Int)
	if r.i < len(r.data) && (r.data[r.i] == 'e' || r.data[r.i] == 'E') {
		r.i++
		esign := 1
		if r.i < len(r.data) && (r.data[r.i] == '+' || r.data[r.i] == '-') {
			if r.data[r.i] == '-' {
				esign = -1
			}
			r.i++
		}
		eStart := r.i
		for r.i < len(r.data) && r.data[r.i] >= '0' && r.data[r.i] <= '9' {
			r.i++
		}
		if _, ok := e.SetString(string(r.data[eStart:r.i]), 10); !ok {
			return number{}, "", false
		}
		if esign < 0 {
			e.Neg(e)
		}
	}
	allDigits := intPart + fracPart
	d := new(big.Int)
	if allDigits == "" {
		d.SetInt64(0)
	} else if _, ok := d.SetString(allDigits, 10); !ok {
		return number{}, "", false
	}
	tok := string(r.data[start:r.i])
	return number{neg: neg, dig: d, exp: e, frac: len(fracPart)}, tok, true
}

func numbersEqual(a, b number) bool {
	za := a.dig.Sign() == 0
	zb := b.dig.Sign() == 0
	if za && zb {
		return true
	}
	if za != zb {
		return false
	}
	if a.neg != b.neg {
		return false
	}
	return cmpMagnitude(a, b) == 0
}

func cmpMagnitude(a, b number) int {
	lenA := len(a.dig.Text(10))
	lenB := len(b.dig.Text(10))
	cA := new(big.Int).Sub(new(big.Int).Set(a.exp), big.NewInt(int64(a.frac)))
	cA.Add(cA, big.NewInt(int64(lenA)))
	cB := new(big.Int).Sub(new(big.Int).Set(b.exp), big.NewInt(int64(b.frac)))
	cB.Add(cB, big.NewInt(int64(lenB)))
	if cmp := cA.Cmp(cB); cmp != 0 {
		return cmp
	}
	p := lenA
	if lenB > p {
		p = lenB
	}
	p--
	x := new(big.Int).Mul(new(big.Int).Set(a.dig), pow10(p-lenA+1))
	y := new(big.Int).Mul(new(big.Int).Set(b.dig), pow10(p-lenB+1))
	return x.Cmp(y)
}

func pow10(e int) *big.Int {
	if e < 0 {
		e = 0
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(e)), nil)
}

// parseString reads a JSON string token from the open quote and returns its
// decoded value (docs/SPEC-TABLES.md §16.3: \u escapes decode; a surrogate
// pair combines; the text is well-formed UTF-8).
func (r *reader) parseString() (string, bool) {
	if r.i >= len(r.data) || r.data[r.i] != '"' {
		return "", false
	}
	r.i++
	var b bytes.Buffer
	for r.i < len(r.data) {
		c := r.data[r.i]
		if c == '"' {
			r.i++
			return b.String(), true
		}
		if c == '\\' {
			r.i++
			if r.i >= len(r.data) {
				return "", false
			}
			switch e := r.data[r.i]; e {
			case '"', '\\', '/':
				b.WriteByte(e)
				r.i++
			case 'b':
				b.WriteByte('\b')
				r.i++
			case 'f':
				b.WriteByte('\f')
				r.i++
			case 'n':
				b.WriteByte('\n')
				r.i++
			case 'r':
				b.WriteByte('\r')
				r.i++
			case 't':
				b.WriteByte('\t')
				r.i++
			case 'u':
				r.i++
				if r.i+4 > len(r.data) {
					return "", false
				}
				n, err := strconv.ParseUint(string(r.data[r.i:r.i+4]), 16, 32)
				if err != nil {
					return "", false
				}
				r.i += 4
				if n >= 0xD800 && n <= 0xDBFF && r.i+6 <= len(r.data) && r.data[r.i] == '\\' && r.data[r.i+1] == 'u' {
					lo, err := strconv.ParseUint(string(r.data[r.i+2:r.i+6]), 16, 32)
					if err == nil && lo >= 0xDC00 && lo <= 0xDFFF {
						b.WriteRune(rune(0x10000 + (n-0xD800)<<10 + (lo - 0xDC00)))
						r.i += 6
						continue
					}
				}
				b.WriteRune(rune(n))
			default:
				return "", false
			}
			continue
		}
		b.WriteByte(c)
		r.i++
	}
	return "", false
}
