package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// The Go operators, each one a fault a test should notice. They are found by
// walking the file's syntax tree and applied as an edit to the bytes the node
// covers, so the rest of the file keeps its formatting and a mutant's diff is
// the fault and nothing else.
const (
	opCmpFlip      = "cmp-flip"       // < to <=, > to >=, == to !=, and back
	opOffByOne     = "off-by-one"     // an integer in a comparison, an index or a slice bound, plus one
	opBitWidth     = "bit-width"      // a shift count or a bits/width argument, plus one
	opDeleteRefuse = "delete-refusal" // `return <error>` under a guard becomes continue (in a loop) or nothing
	opNilError     = "nil-error"      // the error a return carries becomes nil: the cause of a test's Fatal is removed
	opZeroValue    = "zero-value"     // a default (a nonzero integer, a nonempty string, true) becomes its zero
	opDropCheck    = "drop-check"     // a guard's range condition becomes false
	opNegateCond   = "negate-cond"    // a guard's condition is negated
	opDropCall     = "drop-call"      // one write/put/emit/report statement is deleted: a field skipped in a writer
	opDropOperand  = "drop-operand"   // one side of an && or || is dropped: a compared attribute nobody tests
)

// writerCall names the statements drop-call deletes: the calls that put
// something somewhere, which is what a writer skipping a field stops doing.
var writerCall = regexp.MustCompile(`(?i)^(write|put|emit|append|add|encode|serialize|print|fprint|pf$|hf$|p$|u8$|u16$|u32$|u64$|i8$|i16$|i32$|i64$|leb|bits|byte|str|field|errorf|refuse|fail|report|line$)`)

// errorCall names the calls whose result is an error a return hands back.
var errorCall = regexp.MustCompile(`(?i)(errorf$|^new$|error|refus|fail)`)

// widthCall names the calls whose integer argument is a width in bits.
var widthCall = regexp.MustCompile(`(?i)(bits|width)`)

// enumerateGo lists every mutant the Go operators have in one non-test source
// file. With templates set it adds the text operators over the file's string
// literals, which in a generator package are the emitted code.
func enumerateGo(file string, src []byte, templates *textRules) ([]Mutant, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	text := func(n ast.Node) string { return string(src[off(n.Pos()):off(n.End())]) }
	var muts []Mutant
	add := func(op string, n ast.Node, replacement string) {
		muts = append(muts, Mutant{Op: op, Start: off(n.Pos()), End: off(n.End()), Old: text(n), New: replacement})
	}
	addAt := func(op string, start int, old, replacement string) {
		muts = append(muts, Mutant{Op: op, Start: start, End: start + len(old), Old: old, New: replacement})
	}

	skipLiteral := map[*ast.BasicLit]bool{}
	var stack []ast.Node
	parent := func() ast.Node {
		if len(stack) < 2 {
			return nil
		}
		return stack[len(stack)-2]
	}
	inLoop := func() bool {
		for i := len(stack) - 2; i >= 0; i-- {
			switch stack[i].(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				return true
			case *ast.FuncDecl, *ast.FuncLit:
				return false // a loop outside the function is not this return's loop
			}
		}
		return false
	}

	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *ast.ImportSpec:
			skipLiteral[n.Path] = true
		case *ast.Field:
			if n.Tag != nil {
				skipLiteral[n.Tag] = true
			}
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				add(opDropOperand, n, text(n.X))
				add(opDropOperand, n, text(n.Y))
			}
			if flipped, ok := cmpFlip[n.Op]; ok {
				addAt(opCmpFlip, off(n.OpPos), n.Op.String(), flipped)
			}
			shift := n.Op == token.SHL || n.Op == token.SHR
			if lit, ok := n.Y.(*ast.BasicLit); ok {
				if shift {
					addInt(add, opBitWidth, lit)
				} else {
					addInt(add, opOffByOne, lit)
				}
			}
			if lit, ok := n.X.(*ast.BasicLit); ok {
				addInt(add, opOffByOne, lit)
			}
		case *ast.IndexExpr:
			if lit, ok := n.Index.(*ast.BasicLit); ok {
				addInt(add, opOffByOne, lit)
			}
		case *ast.SliceExpr:
			for _, e := range []ast.Expr{n.Low, n.High, n.Max} {
				if lit, ok := e.(*ast.BasicLit); ok {
					addInt(add, opOffByOne, lit)
				}
			}
		case *ast.CallExpr:
			if widthCall.MatchString(calleeName(n)) {
				for _, a := range n.Args {
					if lit, ok := a.(*ast.BasicLit); ok {
						addInt(add, opBitWidth, lit)
					}
				}
			}
		case *ast.ExprStmt:
			if call, ok := n.X.(*ast.CallExpr); ok && writerCall.MatchString(calleeName(call)) {
				if _, labeled := parent().(*ast.LabeledStmt); !labeled {
					add(opDropCall, n, "")
				}
			}
		case *ast.IfStmt:
			if guards(n.Body) {
				add(opNegateCond, n.Cond, "!("+text(n.Cond)+")")
				if relational(n.Cond) {
					add(opDropCheck, n.Cond, "false")
				}
				if ret, ok := n.Body.List[len(n.Body.List)-1].(*ast.ReturnStmt); ok && firstError(ret) != nil {
					if inLoop() {
						add(opDeleteRefuse, ret, "continue")
					} else {
						add(opDeleteRefuse, ret, "{}")
					}
				}
			}
		case *ast.ReturnStmt:
			if e := firstError(n); e != nil {
				add(opNilError, e, "nil")
			}
			for _, r := range n.Results {
				addZero(add, r)
			}
		case *ast.AssignStmt:
			for _, r := range n.Rhs {
				addZero(add, r)
			}
		case *ast.ValueSpec:
			for _, r := range n.Values {
				addZero(add, r)
			}
		case *ast.KeyValueExpr:
			addZero(add, n.Value)
		}
		return true
	})

	if templates != nil {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || skipLiteral[lit] || len(lit.Value) < 8 {
				return true
			}
			start := off(lit.Pos()) + 1
			inner := lit.Value[1 : len(lit.Value)-1]
			muts = append(muts, templates.enumerate(start, inner, lit.Value[0] == '"', false)...)
			return true
		})
	}
	return finish(file, src, muts), nil
}

var cmpFlip = map[token.Token]string{
	token.LSS: "<=", token.LEQ: "<", token.GTR: ">=", token.GEQ: ">", token.EQL: "!=", token.NEQ: "==",
}

// addInt adds the literal plus one, for a decimal or prefixed integer small
// enough to be a bound or a width rather than a constant like a hash.
func addInt(add func(string, ast.Node, string), op string, lit *ast.BasicLit) {
	if lit.Kind != token.INT {
		return
	}
	v, err := strconv.ParseInt(strings.ReplaceAll(lit.Value, "_", ""), 0, 64)
	if err != nil || v < 0 || v > 1<<20 {
		return
	}
	add(op, lit, strconv.FormatInt(v+1, 10))
}

// addZero swaps a default for its zero: a nonzero integer, a nonempty
// string, true.
func addZero(add func(string, ast.Node, string), e ast.Expr) {
	switch e := e.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT:
			if v, err := strconv.ParseInt(strings.ReplaceAll(e.Value, "_", ""), 0, 64); err == nil && v != 0 {
				add(opZeroValue, e, "0")
			}
		case token.STRING:
			if len(e.Value) > 2 {
				add(opZeroValue, e, `""`)
			}
		}
	case *ast.Ident:
		if e.Name == "true" {
			add(opZeroValue, e, "false")
		}
	}
}

func calleeName(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// guards reports whether a block ends by leaving: the shape of a check.
func guards(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	switch last := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		if call, ok := last.X.(*ast.CallExpr); ok {
			return calleeName(call) == "panic"
		}
	}
	return false
}

// relational reports whether a condition compares magnitudes anywhere in its
// and/or structure: a range check.
func relational(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return relational(e.X)
	case *ast.UnaryExpr:
		return relational(e.X)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
			return true
		case token.LAND, token.LOR:
			return relational(e.X) || relational(e.Y)
		}
	}
	return false
}

// firstError is the first result of a return that is an error by its name or
// by the call that makes it, or nil.
func firstError(r *ast.ReturnStmt) ast.Expr {
	for _, e := range r.Results {
		switch e := e.(type) {
		case *ast.Ident:
			if e.Name == "err" || strings.HasPrefix(e.Name, "Err") || (strings.HasPrefix(e.Name, "err") && len(e.Name) > 3 && e.Name[3] >= 'A' && e.Name[3] <= 'Z') {
				return e
			}
		case *ast.SelectorExpr:
			if strings.HasPrefix(e.Sel.Name, "Err") {
				return e
			}
		case *ast.CallExpr:
			if errorCall.MatchString(calleeName(e)) {
				return e
			}
		}
	}
	return nil
}
