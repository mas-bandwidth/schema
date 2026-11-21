package cpp

// The C++ arithmetic a symbolic rendering evaluates in. Schema folding is
// arbitrary-precision (the checker's value is the truth every target must
// produce); C++ is not, and it is not uniform either: a constant reference
// carries its declared constexpr type, so `Start - 2` over a uint32 Start
// evaluates in unsigned int and wraps to 4294967295 where the checker says
// -1. carrierEval models the C++ types each subexpression evaluates in —
// literal typing, integer promotion, the usual arithmetic conversions — and
// admits a symbolic rendering only where every operand and every result is
// exactly representable in the type it evaluates in. There the C++ value is
// the checker's value; everywhere else the caller folds.

import (
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
)

// carrier is the C++ type a subexpression evaluates in after integer
// promotion: int (taken as 32-bit), unsigned int, and the two 64-bit types.
// Narrower types never appear — int8/int16/uint8/uint16 promote to int.
type carrier int

const (
	carrierI32 carrier = iota
	carrierU32
	carrierI64
	carrierU64
)

var (
	minInt32Big  = big.NewInt(math.MinInt32)
	maxInt32Big  = big.NewInt(math.MaxInt32)
	maxUint32Big = new(big.Int).SetUint64(math.MaxUint32)
	minInt64Big  = big.NewInt(math.MinInt64)
	maxInt64Big  = big.NewInt(math.MaxInt64)
)

// holds reports whether v is exactly representable in c.
func (c carrier) holds(v *big.Int) bool {
	switch c {
	case carrierI32:
		return v.Cmp(minInt32Big) >= 0 && v.Cmp(maxInt32Big) <= 0
	case carrierU32:
		return v.Sign() >= 0 && v.Cmp(maxUint32Big) <= 0
	case carrierI64:
		return v.Cmp(minInt64Big) >= 0 && v.Cmp(maxInt64Big) <= 0
	case carrierU64:
		return v.Sign() >= 0 && v.Cmp(maxUint64) <= 0
	}
	return false
}

func (c carrier) wide() bool     { return c == carrierI64 || c == carrierU64 }
func (c carrier) unsigned() bool { return c == carrierU32 || c == carrierU64 }

// common is the usual arithmetic conversion of two promoted operands: the
// wider type wins; at equal width unsigned wins; and a signed 64-bit operand
// beside an unsigned 32-bit one stays signed, since int64 holds every uint32.
func common(a, b carrier) carrier {
	if a == b {
		return a
	}
	if a.wide() != b.wide() {
		if a.wide() {
			return a
		}
		return b
	}
	if a.unsigned() {
		return a
	}
	return b
}

// literalCarrier is the type C++ gives an integer literal as the shared
// renderer spells it: a decimal takes the first of int / long / long long
// that holds it; a hexadecimal literal may also take an unsigned type, so
// 0xffffffff is unsigned int. A literal past INT64_MAX reports false: as a
// decimal it deduces unsigned (-Wimplicitly-unsigned-literal, a -Werror
// break in the consumer's tree), so it never rides symbolically.
func literalCarrier(e *ast.IntLit) (carrier, bool) {
	if !e.Value.IsInt64() {
		return 0, false
	}
	if carrierI32.holds(e.Value) {
		return carrierI32, true
	}
	if strings.HasPrefix(strings.ToLower(e.Text), "0x") && carrierU32.holds(e.Value) {
		return carrierU32, true
	}
	return carrierI64, true
}

// storageCarrier is the promoted type of a reference to a constant emitted as
// `inline constexpr <cppConstType(storage)>`.
func storageCarrier(storage string) (carrier, bool) {
	switch storage {
	case "int8", "int16", "int32", "uint8", "uint16":
		return carrierI32, true
	case "uint32":
		return carrierU32, true
	case "", "int64":
		return carrierI64, true
	case "uint64":
		return carrierU64, true
	}
	return 0, false
}

// overflowSafe reports whether e can render symbolically and evaluate in C++
// to exactly the value the checker folded. Anything unprovable folds —
// folding is always correct, symbolic rendering is the luxury.
func (g *gen) overflowSafe(e ast.Expr) bool {
	_, _, ok := g.carrierEval(e)
	return ok
}

// carrierEval returns e's folded value, the C++ type it evaluates in, and
// whether every operand and result along the way is exactly representable in
// the type its operation evaluates in (so no step wraps, overflows or
// converts a negative into an unsigned type).
func (g *gen) carrierEval(e ast.Expr) (*big.Int, carrier, bool) {
	switch e := e.(type) {
	case *ast.IntLit:
		c, ok := literalCarrier(e)
		return e.Value, c, ok
	case *ast.IdentExpr:
		k, ok := g.unit.Consts[e.Name]
		if !ok || k.IsFloat || k.Int == nil {
			return nil, 0, false
		}
		c, ok := storageCarrier(k.Storage)
		if !ok || !c.holds(k.Int) {
			return nil, 0, false
		}
		return k.Int, c, true
	case *ast.ParenExpr:
		return g.carrierEval(e.X)
	case *ast.UnaryExpr:
		v, c, ok := g.carrierEval(e.X)
		if !ok {
			return nil, c, false
		}
		nv := new(big.Int).Neg(v)
		return nv, c, c.holds(nv)
	case *ast.BinaryExpr:
		x, xc, ok := g.carrierEval(e.X)
		if !ok {
			return nil, xc, false
		}
		y, yc, ok := g.carrierEval(e.Y)
		if !ok {
			return nil, yc, false
		}
		c := common(xc, yc)
		if !c.holds(x) || !c.holds(y) {
			return nil, c, false // an operand changes value converting to c
		}
		v := new(big.Int)
		switch e.Op {
		case "+":
			v.Add(x, y)
		case "-":
			v.Sub(x, y)
		case "*":
			v.Mul(x, y)
		case "/":
			if y.Sign() == 0 {
				return nil, c, false
			}
			v.Quo(x, y) // truncation toward zero, as C++ divides
		case "%":
			if y.Sign() == 0 {
				return nil, c, false
			}
			v.Rem(x, y)
			if !c.holds(new(big.Int).Quo(x, y)) {
				return nil, c, false // the quotient is computed too: MIN % -1 overflows though the remainder is 0
			}
		default:
			return nil, c, false
		}
		return v, c, c.holds(v)
	}
	return nil, 0, false
}
