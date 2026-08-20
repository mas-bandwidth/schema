package c

// Symbolic expression rendering — the C twin of the C++ backend's renderInt
// family, making this the sixth client of ir.RenderExprIdent (issue #92). A
// declared bound or constant reference renders as the author's expression
// (MAX_OBJECTS - 1) wherever that is provably safe C, and folds to the
// computed literal everywhere else. Wire bytes are identical either way:
// expressions fold to the same values, so this changes generated SOURCE TEXT
// only.

import (
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ast"
	"github.com/mas-bandwidth/schema/ir"
)

// renderInt renders an integer expression symbolically where its constant
// references are safe to name in C — #defined earlier in this file or in an
// included file — and folds to the computed value otherwise (an E.Max
// reference always folds; the schema source rides in a comment where one is
// carried).
func (g *gen) renderInt(e ast.Expr, folded *big.Int) string {
	if !g.symbolic(e) {
		return folded.String()
	}
	return renderExpr(e)
}

// renderIntSuffixed is renderInt for the sites that spell a folded bound with
// a literal suffix (-30000LL). A symbolic rendering carries no suffix: the
// expression evaluates in int or long long by C's own literal typing, and the
// consuming parameter or comparison promotes it. The fold keeps the suffixed
// spelling those sites always had.
func (g *gen) renderIntSuffixed(e ast.Expr, folded *big.Int, suffix string) string {
	if !g.symbolic(e) {
		return folded.String() + suffix
	}
	return renderExpr(e)
}

// symbolic gates every symbolic rendering: the expression must exist, contain
// no E.Max reference (no C twin — callers fold those), name only constants
// that are #defined by the time the rendered text is compiled, and evaluate
// without overflowing the C arithmetic it lands in.
func (g *gen) symbolic(e ast.Expr) bool {
	return e != nil && !ir.ExprHasEnumMax(e) && g.renderable(e) && g.overflowSafe(e)
}

// renderExpr renders an expression in C form: every constant reference is
// spelled SCREAMING_SNAKE, the way this target #defines it.
func renderExpr(e ast.Expr) string {
	return ir.RenderExprIdent(e, screaming)
}

// foldComment returns a trailing comment carrying the schema expression when
// the rendered C had to fold it (an E.Max reference has no C twin).
func (g *gen) foldComment(e ast.Expr) string {
	if ir.ExprHasEnumMax(e) {
		return " /* = " + ir.RenderExpr(e) + " */"
	}
	return ""
}

// renderable: the preprocessor expands a #define at USE, so every referenced
// constant's definition must be in scope where the rendered text lands —
// #defined earlier in this file (ir.EmissionOrder makes that the emission
// order; the wire header pre-marks the data header's, which it includes) or
// in an included file (the include graph carries expression edges —
// ir.FileDeps).
func (g *gen) renderable(e ast.Expr) bool {
	for _, name := range ir.ExprConsts(e) {
		base, ok := g.unit.DeclFile[name]
		if !ok {
			return false
		}
		if base == g.file.Base && !g.emitted[name] {
			return false
		}
	}
	return true
}

// overflowSafe reports whether e can render symbolically without the TARGET's
// arithmetic overflowing on the way to the folded value. Schema folding is
// arbitrary-precision; C is not — and unlike C++, a constant reference brings
// no typed carrier with it: constants here are #defines, so a reference
// expands to its definition's own tokens and evaluates by C's literal typing.
// An unsuffixed decimal literal takes the first of int / long / long long
// that holds it, so a subtree evaluates 64-bit exactly where some leaf's
// folded value needs more than int32, and in int — conservatively taken as
// 32-bit — everywhere else. Anything unprovable folds: folding is always
// correct, symbolic rendering is the luxury.
func (g *gen) overflowSafe(e ast.Expr) bool {
	_, _, ok := g.carrierEval(e)
	return ok
}

// carrierEval returns e's folded value, whether the subtree carries 64-bit
// arithmetic (some leaf's value is wider than int32 — integer promotion is
// transitive up the tree), and whether every subexpression's value fits the
// arithmetic type it evaluates in.
func (g *gen) carrierEval(e ast.Expr) (*big.Int, bool, bool) {
	switch e := e.(type) {
	case *ast.IntLit:
		// a literal past INT64_MAX has no unsuffixed spelling in C — the
		// decimal literal ladder ends at long long
		return e.Value, !fitsInt32(e.Value), e.Value.IsInt64()
	case *ast.IdentExpr:
		c, ok := g.unit.Consts[e.Name]
		if !ok || c.IsFloat || c.Int == nil || !c.Int.IsInt64() {
			return nil, true, false
		}
		// the reference expands to the constant's own #define: a folded
		// literal, or a symbolic definition this same analysis already
		// admitted — either way the expansion evaluates at least as wide as
		// the value needs
		return c.Int, !fitsInt32(c.Int), true
	case *ast.ParenExpr:
		return g.carrierEval(e.X)
	case *ast.UnaryExpr:
		v, wide, ok := g.carrierEval(e.X)
		if !ok {
			return nil, wide, false
		}
		nv := new(big.Int).Neg(v)
		return nv, wide, fitsCarrier(nv, wide)
	case *ast.BinaryExpr:
		x, xw, ok := g.carrierEval(e.X)
		if !ok {
			return nil, xw, false
		}
		y, yw, ok := g.carrierEval(e.Y)
		if !ok {
			return nil, yw, false
		}
		wide := xw || yw
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
				return nil, wide, false
			}
			v.Quo(x, y) // truncation toward zero, as C divides
		case "%":
			if y.Sign() == 0 {
				return nil, wide, false
			}
			v.Rem(x, y)
		default:
			return nil, wide, false
		}
		return v, wide, fitsCarrier(v, wide)
	}
	return nil, false, false
}

// fitsCarrier: a subtree with a wider-than-int32 leaf evaluates in (at least)
// long long; everything else evaluates in int, conservatively taken as
// 32-bit.
func fitsCarrier(v *big.Int, wide bool) bool {
	if wide {
		return v.IsInt64()
	}
	return fitsInt32(v)
}

func fitsInt32(v *big.Int) bool {
	return v.IsInt64() && v.Int64() >= math.MinInt32 && v.Int64() <= math.MaxInt32
}
