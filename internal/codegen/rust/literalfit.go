package rust

import (
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/ast"
)

// literalFits reports whether a literal-only expression evaluates in the Rust
// type typ without any step leaving typ's range. A literal-only expression
// carries no i64 constant, so its literals infer typ from the context it lands
// in and every operation evaluates in typ: `300 - 100` for a u8 is the u8
// literal 300 — out of range, and a const-evaluation error — although the
// checked value 200 fits. Where any step leaves typ (or typ is not a
// fixed-width integer this function knows) the caller folds, and the folded
// literal is the checked value. usize is taken as 32-bit: folding is always
// correct, symbolic rendering is the luxury.
func literalFits(e ast.Expr, typ string) bool {
	lo, hi, ok := rustIntRange(typ)
	if !ok {
		return false
	}
	_, ok = rangeEval(e, lo, hi)
	return ok
}

// rustIntRange is the value range of a Rust fixed-width integer type.
func rustIntRange(typ string) (*big.Int, *big.Int, bool) {
	bits := 0
	signed := false
	switch {
	case typ == "usize":
		bits = 32
	case strings.HasPrefix(typ, "i"):
		signed = true
		bits = rustWidth(typ[1:])
	case strings.HasPrefix(typ, "u"):
		bits = rustWidth(typ[1:])
	}
	if bits == 0 {
		return nil, nil, false
	}
	if signed {
		hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)), big.NewInt(1))
		lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)))
		return lo, hi, true
	}
	hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return big.NewInt(0), hi, true
}

func rustWidth(s string) int {
	switch s {
	case "8":
		return 8
	case "16":
		return 16
	case "32":
		return 32
	case "64":
		return 64
	}
	return 0
}

// rangeEval folds a literal-only expression, reporting whether every literal
// and every intermediate value lies in [lo, hi]. A literal under a unary minus
// is checked as the literal itself, as rustc's overflowing_literals lint does
// for every type but the signed minimum, which this conservatively folds.
func rangeEval(e ast.Expr, lo, hi *big.Int) (*big.Int, bool) {
	in := func(v *big.Int) bool { return v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0 }
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Value, in(e.Value)
	case *ast.ParenExpr:
		return rangeEval(e.X, lo, hi)
	case *ast.UnaryExpr:
		v, ok := rangeEval(e.X, lo, hi)
		if !ok {
			return nil, false
		}
		nv := new(big.Int).Neg(v)
		return nv, in(nv)
	case *ast.BinaryExpr:
		x, ok := rangeEval(e.X, lo, hi)
		if !ok {
			return nil, false
		}
		y, ok := rangeEval(e.Y, lo, hi)
		if !ok {
			return nil, false
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
				return nil, false
			}
			v.Quo(x, y)
		case "%":
			if y.Sign() == 0 {
				return nil, false
			}
			v.Rem(x, y)
			if !in(new(big.Int).Quo(x, y)) {
				return nil, false // MIN % -1: the implied quotient overflows, a const-evaluation error
			}
		default:
			return nil, false
		}
		return v, in(v)
	}
	return nil, false
}
