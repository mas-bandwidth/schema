package compiler

// A small evaluator of the integer expressions the C and C++ backends emit,
// with the C and C++ rules for them on an LP64 / LLP64 target: literal typing
// (a decimal is int / long / long long; a hexadecimal literal may be
// unsigned; u / l / ll suffixes), integer promotion of the fixed-width types,
// the usual arithmetic conversions, truncating division, unsigned wrap, and
// signed overflow reported as the error it is. It reads the GENERATED TEXT,
// not the schema expression, so it checks the backends' symbolic-rendering
// gates from outside: whatever a backend chose to emit, the value C computes
// for it must be the value the checker computed (constagree_test.go).

import (
	"fmt"
	"math/big"
	"strings"
)

type cType struct {
	bits   int
	signed bool
}

var (
	cInt   = cType{32, true}
	cUint  = cType{32, false}
	cLong  = cType{64, true}
	cUlong = cType{64, false}
)

func (t cType) lo() *big.Int {
	if !t.signed {
		return big.NewInt(0)
	}
	return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(t.bits-1)))
}

func (t cType) hi() *big.Int {
	if !t.signed {
		return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.bits)), big.NewInt(1))
	}
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.bits-1)), big.NewInt(1))
}

func (t cType) holds(v *big.Int) bool { return v.Cmp(t.lo()) >= 0 && v.Cmp(t.hi()) <= 0 }

// convert is the C conversion of an exact value to t: modulo 2^bits for an
// unsigned type, and two's-complement wrap for a signed one (implementation
// defined in C99, and what every target this repo ships does).
func (t cType) convert(v *big.Int) *big.Int {
	if t.holds(v) {
		return new(big.Int).Set(v)
	}
	m := new(big.Int).Lsh(big.NewInt(1), uint(t.bits))
	r := new(big.Int).Mod(v, m)
	if t.signed && r.Cmp(t.hi()) > 0 {
		r.Sub(r, m)
	}
	return r
}

func promote(t cType) cType {
	if t.bits < 32 {
		return cInt
	}
	return t
}

func usualConversion(a, b cType) cType {
	a, b = promote(a), promote(b)
	if a == b {
		return a
	}
	if a.signed == b.signed {
		if a.bits > b.bits {
			return a
		}
		return b
	}
	u, s := a, b
	if a.signed {
		u, s = b, a
	}
	if u.bits >= s.bits {
		return u
	}
	return s
}

type cValue struct {
	v *big.Int
	t cType
}

// cEval evaluates one emitted expression. ident resolves a name (a C macro's
// expansion, or a C++ constant's declared type and value).
type cEval struct {
	toks  []string
	pos   int
	ident func(name string) (cValue, error)
}

func evalC(src string, ident func(string) (cValue, error)) (cValue, error) {
	toks, err := cTokens(src)
	if err != nil {
		return cValue{}, err
	}
	e := &cEval{toks: toks, ident: ident}
	v, err := e.add()
	if err != nil {
		return cValue{}, err
	}
	if e.pos != len(e.toks) {
		return cValue{}, fmt.Errorf("trailing %q in %q", e.toks[e.pos], src)
	}
	return v, nil
}

func cTokens(s string) ([]string, error) {
	var out []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case strings.IndexByte("()+-*/%", c) >= 0:
			out = append(out, string(c))
			i++
		case c == ':' && i+1 < len(s) && s[i+1] == ':':
			out = append(out, "::")
			i += 2
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9':
			j := i
			for j < len(s) && (s[j] == '_' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
				j++
			}
			out = append(out, s[i:j])
			i = j
		default:
			return nil, fmt.Errorf("unexpected %q in %q", c, s)
		}
	}
	return out, nil
}

func (e *cEval) peek() string {
	if e.pos < len(e.toks) {
		return e.toks[e.pos]
	}
	return ""
}

func (e *cEval) add() (cValue, error) {
	x, err := e.mul()
	if err != nil {
		return x, err
	}
	for e.peek() == "+" || e.peek() == "-" {
		op := e.toks[e.pos]
		e.pos++
		y, err := e.mul()
		if err != nil {
			return y, err
		}
		if x, err = arith(op, x, y); err != nil {
			return x, err
		}
	}
	return x, nil
}

func (e *cEval) mul() (cValue, error) {
	x, err := e.unary()
	if err != nil {
		return x, err
	}
	for e.peek() == "*" || e.peek() == "/" || e.peek() == "%" {
		op := e.toks[e.pos]
		e.pos++
		y, err := e.unary()
		if err != nil {
			return y, err
		}
		if x, err = arith(op, x, y); err != nil {
			return x, err
		}
	}
	return x, nil
}

func (e *cEval) unary() (cValue, error) {
	if e.peek() == "-" {
		e.pos++
		x, err := e.unary()
		if err != nil {
			return x, err
		}
		t := promote(x.t)
		return wrapOrOverflow(new(big.Int).Neg(t.convert(x.v)), t, "-")
	}
	return e.primary()
}

func (e *cEval) primary() (cValue, error) {
	tok := e.peek()
	if tok == "" {
		return cValue{}, fmt.Errorf("unexpected end of expression")
	}
	e.pos++
	switch {
	case tok == "(":
		v, err := e.add()
		if err != nil {
			return v, err
		}
		if e.peek() != ")" {
			return v, fmt.Errorf("missing )")
		}
		e.pos++
		return v, nil
	case tok[0] >= '0' && tok[0] <= '9':
		return cLiteral(tok)
	default:
		return e.ident(tok)
	}
}

// cLiteral types an integer literal the way C99 6.4.4.1 and C++ [lex.icon]
// do on a target with 32-bit int and 64-bit long long.
func cLiteral(tok string) (cValue, error) {
	body := strings.TrimRight(tok, "uUlL")
	suffix := strings.ToLower(tok[len(body):])
	hex := strings.HasPrefix(body, "0x") || strings.HasPrefix(body, "0X")
	v := new(big.Int)
	var ok bool
	if hex {
		_, ok = v.SetString(body[2:], 16)
	} else {
		if len(body) > 1 && body[0] == '0' {
			return cValue{}, fmt.Errorf("octal literal %q", tok)
		}
		_, ok = v.SetString(body, 10)
	}
	if !ok {
		return cValue{}, fmt.Errorf("bad literal %q", tok)
	}
	unsigned := strings.Contains(suffix, "u")
	long := strings.Contains(suffix, "l")
	var ladder []cType
	switch {
	case unsigned && long:
		ladder = []cType{cUlong}
	case unsigned:
		ladder = []cType{cUint, cUlong}
	case long && hex:
		ladder = []cType{cLong, cUlong}
	case long:
		ladder = []cType{cLong}
	case hex:
		ladder = []cType{cInt, cUint, cLong, cUlong}
	default:
		ladder = []cType{cInt, cLong}
	}
	for _, t := range ladder {
		if t.holds(v) {
			return cValue{v, t}, nil
		}
	}
	return cValue{}, fmt.Errorf("literal %q has no type", tok)
}

func arith(op string, x, y cValue) (cValue, error) {
	t := usualConversion(x.t, y.t)
	a, b := t.convert(x.v), t.convert(y.v)
	r := new(big.Int)
	switch op {
	case "+":
		r.Add(a, b)
	case "-":
		r.Sub(a, b)
	case "*":
		r.Mul(a, b)
	case "/", "%":
		if b.Sign() == 0 {
			return cValue{}, fmt.Errorf("division by zero")
		}
		if op == "/" {
			r.Quo(a, b)
		} else {
			r.Rem(a, b)
		}
	}
	return wrapOrOverflow(r, t, op)
}

func wrapOrOverflow(r *big.Int, t cType, op string) (cValue, error) {
	if t.signed && !t.holds(r) {
		return cValue{}, fmt.Errorf("signed overflow in %s: %s does not fit int%d", op, r, t.bits)
	}
	return cValue{t.convert(r), t}, nil
}

// cppDeclType parses a C++ fixed-width integer type name (int64_t, uint8_t).
func cppDeclType(s string) (cType, bool) {
	signed := !strings.HasPrefix(s, "u")
	switch strings.TrimPrefix(s, "u") {
	case "int8_t":
		return cType{8, signed}, true
	case "int16_t":
		return cType{16, signed}, true
	case "int32_t":
		return cType{32, signed}, true
	case "int64_t":
		return cType{64, signed}, true
	}
	return cType{}, false
}
