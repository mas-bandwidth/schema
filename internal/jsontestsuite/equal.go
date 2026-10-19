// Equal is the semantic-equal oracle for JSON texts, by docs/SPEC-TABLES.md
// §16.13 EQUAL: each text is read with §16.12's layer plus the construct's
// refusals, and two values are equal when they are the same kind, numbers are
// equal exactly when their token texts are equal byte for byte, strings when
// their decoded UTF-8 bytes are equal, arrays element by element, and objects
// member by member in order (a repeated key compared where it stands).
// Whitespace, a leading byte-order mark, comments and trailing commas are not
// data (§16.2, §16.12). The walk is bounded by §16.12's caps: depth 128, a key
// of 255 decoded bytes, a number token of 511 bytes. No number is ever
// expanded. Standard library only.
package jsontestsuite

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// §16.12's caps, held by both texts before anything is compared.
const (
	maxDepth       = 128
	maxKeyBytes    = 255
	maxNumberBytes = 511
)

// refusedPrefix marks a refusal rather than an inequality: §16.13 says that if
// either text is refused, Equal refuses too. The two-value signature has no
// third return, so a refusal is false with this prefix and the reason.
const refusedPrefix = "refused: "

// Equal reports whether the two JSON texts hold the same value under
// §16.13's EQUAL. When they do not, it returns false and the path where they
// first differ. When either text is not well-formed under §16.12's layer
// (including a key past 255 decoded bytes and a number token past 511 bytes),
// it refuses: false with a reason beginning "refused: ".
func Equal(a, b []byte) (bool, string) {
	// §16.13 requires refusal if either complete input is refused, even when
	// an earlier value difference would otherwise let the comparison stop.
	if why := validateText(a, "a"); why != "" {
		return false, why
	}
	if why := validateText(b, "b"); why != "" {
		return false, why
	}
	ra := &reader{data: a, side: "a"}
	rb := &reader{data: b, side: "b"}
	ra.skipBOM()
	rb.skipBOM()
	ok, why := eqValue(ra, rb, 0, "$")
	if !ok {
		return false, why
	}
	if !ra.skipSpace() {
		return refuse("$", "unterminated comment in %s", ra.side)
	}
	if !rb.skipSpace() {
		return refuse("$", "unterminated comment in %s", rb.side)
	}
	if ra.i != len(ra.data) {
		return refuse("$", "trailing input in %s at byte %d", ra.side, ra.i)
	}
	if rb.i != len(rb.data) {
		return refuse("$", "trailing input in %s at byte %d", rb.side, rb.i)
	}
	return true, ""
}

// validateText consumes one full input before equality can return an early
// difference. It uses the same §16.12 reader and bounds as the comparison.
func validateText(data []byte, side string) string {
	ra := &reader{data: data, side: side}
	rb := &reader{data: data, side: side}
	ra.skipBOM()
	rb.skipBOM()
	ok, why := eqValue(ra, rb, 0, "$")
	if !ok {
		if !strings.HasPrefix(why, refusedPrefix) {
			return refusal("$", "invalid input in %s: %s", side, why)
		}
		return why
	}
	if !ra.skipSpace() || !rb.skipSpace() {
		return refusal("$", "unterminated comment in %s", side)
	}
	if ra.i != len(ra.data) || rb.i != len(rb.data) {
		return refusal("$", "trailing input in %s at byte %d", side, ra.i)
	}
	return ""
}

func refuse(path, format string, args ...any) (bool, string) {
	return false, refusal(path, format, args...)
}

func refusal(path, format string, args ...any) string {
	return refusedPrefix + path + ": " + fmt.Sprintf(format, args...)
}

// reader walks one JSON text.
type reader struct {
	data []byte
	i    int
	side string
}

// skipBOM skips a leading UTF-8 byte-order mark (RFC 8259 §8.1; §16.12), the
// only place those bytes are not data.
func (r *reader) skipBOM() {
	if r.i+3 <= len(r.data) && r.data[r.i] == 0xEF && r.data[r.i+1] == 0xBB && r.data[r.i+2] == 0xBF {
		r.i += 3
	}
}

// skipSpace advances over whitespace and, as the §16.2 dialect accepts on
// read (§16.12), comments: `/* */` and `//` to end of line or input. It reports
// false for an unterminated block comment, which is not a comment.
func (r *reader) skipSpace() bool {
	for r.i < len(r.data) {
		switch r.data[r.i] {
		case ' ', '\t', '\n', '\r':
			r.i++
		case '/':
			if r.i+1 >= len(r.data) {
				return true
			}
			switch r.data[r.i+1] {
			case '*':
				r.i += 2
				closed := false
				for r.i+1 < len(r.data) {
					if r.data[r.i] == '*' && r.data[r.i+1] == '/' {
						r.i += 2
						closed = true
						break
					}
					r.i++
				}
				if !closed {
					return false
				}
			case '/':
				r.i += 2
				for r.i < len(r.data) && r.data[r.i] != '\n' {
					r.i++
				}
			default:
				return true
			}
		default:
			return true
		}
	}
	return true
}

type kind int

const (
	kNum kind = iota
	kStr
	kObj
	kArr
	kLit
	kBad
)

// kindOf names the value a first byte starts, or kBad when it starts none.
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
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return kNum
	default:
		return kBad
	}
}

func kindName(c byte) string {
	switch kindOf(c) {
	case kObj:
		return "object"
	case kArr:
		return "array"
	case kStr:
		return "string"
	case kLit:
		return "literal"
	case kNum:
		return "number"
	default:
		return fmt.Sprintf("unexpected byte %q", c)
	}
}

// eqValue compares two values under §16.13's EQUAL. depth is the depth of the
// value's container, the root's container at 0 (§16.12).
func eqValue(ra, rb *reader, depth int, path string) (bool, string) {
	if !ra.skipSpace() {
		return refuse(path, "unterminated comment in %s", ra.side)
	}
	if !rb.skipSpace() {
		return refuse(path, "unterminated comment in %s", rb.side)
	}
	if ra.i >= len(ra.data) {
		return refuse(path, "text %s ends where a value is expected", ra.side)
	}
	if rb.i >= len(rb.data) {
		return refuse(path, "text %s ends where a value is expected", rb.side)
	}
	ca, cb := ra.data[ra.i], rb.data[rb.i]
	ka, kb := kindOf(ca), kindOf(cb)
	if ka == kBad {
		return refuse(path, "%s in %s", kindName(ca), ra.side)
	}
	if kb == kBad {
		return refuse(path, "%s in %s", kindName(cb), rb.side)
	}
	// §16.12 counts containers: the root's container is depth 0 and a
	// container whose depth is past 128 is refused.
	if (ka == kObj || ka == kArr) && depth > maxDepth {
		return refuse(path, "depth past the cap of 128 in %s", ra.side)
	}
	if (kb == kObj || kb == kArr) && depth > maxDepth {
		return refuse(path, "depth past the cap of 128 in %s", rb.side)
	}
	if ka != kb {
		return false, fmt.Sprintf("%s: %s != %s", path, kindName(ca), kindName(cb))
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
		if !ra.skipSpace() {
			return refuse(path, "unterminated comment in %s", ra.side)
		}
		if !rb.skipSpace() {
			return refuse(path, "unterminated comment in %s", rb.side)
		}
		if ra.i >= len(ra.data) {
			return refuse(path, "text %s ends inside an object", ra.side)
		}
		if rb.i >= len(rb.data) {
			return refuse(path, "text %s ends inside an object", rb.side)
		}
		closeA := ra.i < len(ra.data) && ra.data[ra.i] == '}'
		closeB := rb.i < len(rb.data) && rb.data[rb.i] == '}'
		if closeA || closeB {
			if closeA && closeB {
				ra.i++
				rb.i++
				return true, ""
			}
			return false, path + ": object member count differs"
		}
		ka, okA, whyA := ra.parseString(path, maxKeyBytes, "key")
		if !okA {
			return false, whyA
		}
		kb, okB, whyB := rb.parseString(path, maxKeyBytes, "key")
		if !okB {
			return false, whyB
		}
		if !bytes.Equal(ka, kb) {
			return false, fmt.Sprintf("%s: key %q != %q", path, ka, kb)
		}
		member := path + "." + string(ka)
		if !ra.skipSpace() {
			return refuse(member, "unterminated comment in %s", ra.side)
		}
		if !rb.skipSpace() {
			return refuse(member, "unterminated comment in %s", rb.side)
		}
		if ra.i >= len(ra.data) || ra.data[ra.i] != ':' {
			return refuse(member, "expected ':' in %s", ra.side)
		}
		if rb.i >= len(rb.data) || rb.data[rb.i] != ':' {
			return refuse(member, "expected ':' in %s", rb.side)
		}
		ra.i++
		rb.i++
		if ok, why := eqValue(ra, rb, depth+1, member); !ok {
			return false, why
		}
		if !ra.skipSpace() {
			return refuse(member, "unterminated comment in %s", ra.side)
		}
		if !rb.skipSpace() {
			return refuse(member, "unterminated comment in %s", rb.side)
		}
		if ra.i >= len(ra.data) {
			return refuse(member, "text %s ends inside an object", ra.side)
		}
		if rb.i >= len(rb.data) {
			return refuse(member, "text %s ends inside an object", rb.side)
		}
		closeA = ra.i < len(ra.data) && ra.data[ra.i] == '}'
		closeB = rb.i < len(rb.data) && rb.data[rb.i] == '}'
		commaA := ra.i < len(ra.data) && ra.data[ra.i] == ','
		commaB := rb.i < len(rb.data) && rb.data[rb.i] == ','
		if closeA && closeB {
			ra.i++
			rb.i++
			return true, ""
		}
		if closeA || closeB {
			// One side is done; the other must be spelling a trailing
			// comma, or it holds more members and the count differs.
			if closeA && !commaB {
				return false, path + ": object member count differs"
			}
			if closeB && !commaA {
				return false, path + ": object member count differs"
			}
		} else {
			if !commaA {
				return refuse(member, "expected ',' in %s", ra.side)
			}
			if !commaB {
				return refuse(member, "expected ',' in %s", rb.side)
			}
		}
		// A trailing comma before the closing brace is the §16.2 dialect
		// and is not data, so one side may spell it and the other not
		// (§16.13). The next turn's close check decides.
		if commaA {
			ra.i++
		}
		if commaB {
			rb.i++
		}
	}
}

func eqArray(ra, rb *reader, depth int, path string) (bool, string) {
	ra.i++
	rb.i++
	idx := 0
	for {
		if !ra.skipSpace() {
			return refuse(path, "unterminated comment in %s", ra.side)
		}
		if !rb.skipSpace() {
			return refuse(path, "unterminated comment in %s", rb.side)
		}
		if ra.i >= len(ra.data) {
			return refuse(path, "text %s ends inside an array", ra.side)
		}
		if rb.i >= len(rb.data) {
			return refuse(path, "text %s ends inside an array", rb.side)
		}
		closeA := ra.i < len(ra.data) && ra.data[ra.i] == ']'
		closeB := rb.i < len(rb.data) && rb.data[rb.i] == ']'
		if closeA || closeB {
			if closeA && closeB {
				ra.i++
				rb.i++
				return true, ""
			}
			return false, path + ": array length differs"
		}
		child := fmt.Sprintf("%s[%d]", path, idx)
		if ok, why := eqValue(ra, rb, depth+1, child); !ok {
			return false, why
		}
		if !ra.skipSpace() {
			return refuse(child, "unterminated comment in %s", ra.side)
		}
		if !rb.skipSpace() {
			return refuse(child, "unterminated comment in %s", rb.side)
		}
		if ra.i >= len(ra.data) {
			return refuse(child, "text %s ends inside an array", ra.side)
		}
		if rb.i >= len(rb.data) {
			return refuse(child, "text %s ends inside an array", rb.side)
		}
		closeA = ra.i < len(ra.data) && ra.data[ra.i] == ']'
		closeB = rb.i < len(rb.data) && rb.data[rb.i] == ']'
		commaA := ra.i < len(ra.data) && ra.data[ra.i] == ','
		commaB := rb.i < len(rb.data) && rb.data[rb.i] == ','
		if closeA && closeB {
			ra.i++
			rb.i++
			return true, ""
		}
		if closeA || closeB {
			// One side is done; the other must be spelling a trailing
			// comma, or it holds more elements and the length differs.
			if closeA && !commaB {
				return false, path + ": array length differs"
			}
			if closeB && !commaA {
				return false, path + ": array length differs"
			}
		} else {
			if !commaA {
				return refuse(child, "expected ',' in %s", ra.side)
			}
			if !commaB {
				return refuse(child, "expected ',' in %s", rb.side)
			}
		}
		// A trailing comma is the §16.2 dialect and is not data. The next
		// turn's close check decides.
		if commaA {
			ra.i++
		}
		if commaB {
			rb.i++
		}
		idx++
	}
}

func eqString(ra, rb *reader, path string) (bool, string) {
	sa, okA, whyA := ra.parseString(path, -1, "string")
	if !okA {
		return false, whyA
	}
	sb, okB, whyB := rb.parseString(path, -1, "string")
	if !okB {
		return false, whyB
	}
	if !bytes.Equal(sa, sb) {
		return false, fmt.Sprintf("%s: string %q != %q", path, sa, sb)
	}
	return true, ""
}

func eqLiteral(ra, rb *reader, path string) (bool, string) {
	la, okA, whyA := ra.parseLiteral(path)
	if !okA {
		return false, whyA
	}
	lb, okB, whyB := rb.parseLiteral(path)
	if !okB {
		return false, whyB
	}
	if la != lb {
		return false, fmt.Sprintf("%s: literal %s != %s", path, la, lb)
	}
	return true, ""
}

// parseLiteral reads one of RFC 8259's three literals.
func (r *reader) parseLiteral(path string) (string, bool, string) {
	lits := map[byte]string{'t': "true", 'f': "false", 'n': "null"}
	lit, ok := lits[r.data[r.i]]
	if !ok {
		return "", false, refusal(path, "bad literal")
	}
	if r.i+len(lit) > len(r.data) || !bytes.Equal(r.data[r.i:r.i+len(lit)], []byte(lit)) {
		return "", false, refusal(path, "bad literal")
	}
	r.i += len(lit)
	return lit, true, ""
}

func eqNumber(ra, rb *reader, path string) (bool, string) {
	ta, okA, whyA := ra.parseNumber(path)
	if !okA {
		return false, whyA
	}
	tb, okB, whyB := rb.parseNumber(path)
	if !okB {
		return false, whyB
	}
	// §16.13 EQUAL: two numbers are equal exactly when their token texts are
	// equal byte for byte, so 1.0, 1e0 and 10e-1 are three different numbers
	// and -0 is not 0.
	if !bytes.Equal(ta, tb) {
		return false, fmt.Sprintf("%s: number %s != %s", path, ta, tb)
	}
	return true, ""
}

// parseNumber reads RFC 8259's number production and returns its token, never
// a value (§16.13: the token is copied, never parsed), refusing a token past
// 511 bytes.
func (r *reader) parseNumber(path string) ([]byte, bool, string) {
	start := r.i
	if r.i < len(r.data) && r.data[r.i] == '-' {
		r.i++
	}
	if r.i >= len(r.data) {
		return nil, false, refusal(path, "number has no integer part")
	}
	switch c := r.data[r.i]; {
	case c == '0':
		r.i++
	case c >= '1' && c <= '9':
		for r.i < len(r.data) && r.data[r.i] >= '0' && r.data[r.i] <= '9' {
			r.i++
		}
	default:
		return nil, false, refusal(path, "number has no integer part")
	}
	if r.i < len(r.data) && r.data[r.i] == '.' {
		r.i++
		if r.i >= len(r.data) || r.data[r.i] < '0' || r.data[r.i] > '9' {
			return nil, false, refusal(path, "number has no fraction digits")
		}
		for r.i < len(r.data) && r.data[r.i] >= '0' && r.data[r.i] <= '9' {
			r.i++
		}
	}
	if r.i < len(r.data) && (r.data[r.i] == 'e' || r.data[r.i] == 'E') {
		r.i++
		if r.i < len(r.data) && (r.data[r.i] == '+' || r.data[r.i] == '-') {
			r.i++
		}
		if r.i >= len(r.data) || r.data[r.i] < '0' || r.data[r.i] > '9' {
			return nil, false, refusal(path, "number has no exponent digits")
		}
		for r.i < len(r.data) && r.data[r.i] >= '0' && r.data[r.i] <= '9' {
			r.i++
		}
	}
	if r.i-start > maxNumberBytes {
		return nil, false, refusal(path, "number token past the cap of 511 bytes")
	}
	return r.data[start:r.i], true, ""
}

// parseString reads a JSON string and returns its decoded UTF-8 bytes
// (§16.3): escapes are resolved, a surrogate pair combines, well-formed UTF-8
// is required, and a lone or out-of-order surrogate is refused rather than
// silently folded to U+FFFD. max is the decoded byte cap, -1 for a value's
// string and 255 for an object key (§16.12, §16.13).
func (r *reader) parseString(path string, max int, what string) ([]byte, bool, string) {
	if r.i >= len(r.data) || r.data[r.i] != '"' {
		return nil, false, refusal(path, "%s does not open with a quote", what)
	}
	r.i++
	out := make([]byte, 0, 16)
	for r.i < len(r.data) {
		c := r.data[r.i]
		if c == '"' {
			r.i++
			if max >= 0 && len(out) > max {
				return nil, false, refusal(path, "%s past the cap of %d decoded bytes", what, max)
			}
			return out, true, ""
		}
		if c == '\\' {
			r.i++
			if r.i >= len(r.data) {
				return nil, false, refusal(path, "escape cut short in %s", what)
			}
			switch e := r.data[r.i]; e {
			case '"', '\\', '/':
				out = append(out, e)
				r.i++
			case 'b':
				out = append(out, '\b')
				r.i++
			case 'f':
				out = append(out, '\f')
				r.i++
			case 'n':
				out = append(out, '\n')
				r.i++
			case 'r':
				out = append(out, '\r')
				r.i++
			case 't':
				out = append(out, '\t')
				r.i++
			case 'u':
				r.i++
				n, ok := r.hex4()
				if !ok {
					return nil, false, refusal(path, "bad \\u escape in %s", what)
				}
				switch {
				case n >= 0xD800 && n <= 0xDBFF:
					// A high half must be followed by a low half.
					if r.i+6 > len(r.data) || r.data[r.i] != '\\' || r.data[r.i+1] != 'u' {
						return nil, false, refusal(path, "string_lone_surrogate in %s", what)
					}
					r.i += 2
					lo, ok := r.hex4()
					if !ok || lo < 0xDC00 || lo > 0xDFFF {
						return nil, false, refusal(path, "string_lone_surrogate in %s", what)
					}
					out = utf8.AppendRune(out, rune(0x10000+(n-0xD800)<<10+(lo-0xDC00)))
				case n >= 0xDC00 && n <= 0xDFFF:
					return nil, false, refusal(path, "string_lone_surrogate in %s", what)
				default:
					out = utf8.AppendRune(out, rune(n))
				}
			default:
				return nil, false, refusal(path, "bad escape \\%c in %s", e, what)
			}
			continue
		}
		if c < 0x20 {
			return nil, false, refusal(path, "raw control byte 0x%02x in %s", c, what)
		}
		if c < utf8.RuneSelf {
			out = append(out, c)
			r.i++
			continue
		}
		run, size := utf8.DecodeRune(r.data[r.i:])
		if run == utf8.RuneError && size == 1 {
			return nil, false, refusal(path, "string_not_utf8 in %s", what)
		}
		out = append(out, r.data[r.i:r.i+size]...)
		r.i += size
	}
	return nil, false, refusal(path, "%s is not closed", what)
}

// hex4 reads four hexadecimal digits and returns their value.
func (r *reader) hex4() (int, bool) {
	if r.i+4 > len(r.data) {
		return 0, false
	}
	n := 0
	for k := 0; k < 4; k++ {
		c := r.data[r.i+k]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, false
		}
		n = n<<4 | d
	}
	r.i += 4
	return n, true
}
