package ir

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ast"
)

// RenderExpr renders a kept expression in schema source form: literals keep
// their source spelling (0x10 stays 0x10), constant references keep the
// author's names, an enum max reference renders as E.Max, operators and
// author parentheses are explicit. This is the target-language-neutral form
// the built-in backends carry in comments beside folded values, and the form
// an external generator emits where its target shares schema's spelling.
//
// Anything the parser cannot put in a kept expression — nil included —
// renders as "?": a spelling no target compiles, so a defect is a loud build
// failure in the consumer's tree, never a silently wrong bound.
func RenderExpr(e Expr) string {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Text
	case *ast.FloatLit:
		return e.Text
	case *ast.IdentExpr:
		return e.Name
	case *ast.MaxExpr:
		return e.Enum + "." + e.Sel
	case *ast.UnaryExpr:
		return "-" + RenderExpr(e.X)
	case *ast.BinaryExpr:
		return fmt.Sprintf("%s %s %s", RenderExpr(e.X), e.Op, RenderExpr(e.Y))
	case *ast.ParenExpr:
		return "(" + RenderExpr(e.X) + ")"
	}
	return "?"
}

// RenderExprIdent renders e for a generated target, spelling every constant
// reference through ident — the one point where the targets differ (Go and
// C++ keep the schema spelling, Rust wants [RustConstName], a namespaced
// target qualifies). A nil ident keeps the schema spelling. Two rules the
// neutral [RenderExpr] form does not need:
//
//   - Doubled unary minus parenthesizes ("-(-x)"): "--" is a decrement
//     operator in the C family and a parse error in Go and Rust, so the bare
//     concatenation compiles somewhere as the wrong program.
//   - An enum max reference renders as "?": E.Max has no cross-target
//     spelling. Callers fold expressions containing one to the resolved
//     value first — [ExprHasEnumMax] is that test — as every built-in
//     backend does, carrying the schema form in a comment via [RenderExpr].
//
// Whether a bound should render symbolically at all is the caller's own
// policy, built on [ExprConsts]; the built-in backends disagree with each
// other here (typed constants, same-file emission order), so no single
// answer belongs in this function.
func RenderExprIdent(e Expr, ident func(name string) string) string {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Text
	case *ast.FloatLit:
		return e.Text
	case *ast.IdentExpr:
		if ident == nil {
			return e.Name
		}
		return ident(e.Name)
	case *ast.UnaryExpr:
		inner := RenderExprIdent(e.X, ident)
		if strings.HasPrefix(inner, "-") {
			return "-(" + inner + ")"
		}
		return "-" + inner
	case *ast.BinaryExpr:
		return fmt.Sprintf("%s %s %s", RenderExprIdent(e.X, ident), e.Op, RenderExprIdent(e.Y, ident))
	case *ast.ParenExpr:
		return "(" + RenderExprIdent(e.X, ident) + ")"
	}
	return "?"
}

// ExprConsts returns the constant names e references, in source order, one
// entry per occurrence; nil when the expression is literal. The names index
// [Unit.Consts]. A generator deciding whether to render a bound symbolically
// checks each referenced constant against its own target's rules — an
// expression with no references always renders exactly as its folded value.
func ExprConsts(e Expr) []string {
	var names []string
	var walk func(e Expr)
	walk = func(e Expr) {
		switch e := e.(type) {
		case *ast.IdentExpr:
			names = append(names, e.Name)
		case *ast.UnaryExpr:
			walk(e.X)
		case *ast.BinaryExpr:
			walk(e.X)
			walk(e.Y)
		case *ast.ParenExpr:
			walk(e.X)
		}
	}
	walk(e)
	return names
}

// ExprHasEnumMax reports whether e contains an enum max reference. E.Max has
// no twin in any generated target, so a backend folds any expression
// containing one to the resolved value and keeps the schema form in a
// comment ([RenderExpr]).
func ExprHasEnumMax(e Expr) bool {
	switch e := e.(type) {
	case *ast.MaxExpr:
		return true
	case *ast.UnaryExpr:
		return ExprHasEnumMax(e.X)
	case *ast.BinaryExpr:
		return ExprHasEnumMax(e.X) || ExprHasEnumMax(e.Y)
	case *ast.ParenExpr:
		return ExprHasEnumMax(e.X)
	}
	return false
}
