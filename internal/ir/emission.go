package ir

import (
	"github.com/mas-bandwidth/schema/internal/ast"
)

// EmissionOrder returns the file's declarations in an order where every
// same-file named type precedes its by-value users — schema references are
// order-free (SPEC §4.2), C and C++ are not. A stable topological sort:
// declaration order is preserved wherever no dependency forces otherwise.
//
// Lifted verbatim from the C++ backend, which has ordered this way from the
// start; the C backend emitted in raw declaration order and shipped
// `unknown type name` / implicit-declaration errors for any unit whose
// schema declared a by-value user before its type in one file (found by
// FuzzGeneratedCompiles, the clang -fsyntax-only leg — issue #22).
//
// Constants are nodes too: a const named in an expression a backend renders
// symbolically (a const initializer, an array bound, a range bound, a
// default) must precede its user, or renderInt finds it un-emitted and folds
// to the literal — silently, and only under some declaration orders (the
// defect behind issue #29). The expression list mirrors FileDeps: EVERY
// expression a backend renders symbolically is an ordering edge.
func EmissionOrder(file *File) []Decl {
	decls := file.Decls
	n := len(decls)
	byName := map[string]int{}
	for i, d := range decls {
		switch d := d.(type) {
		case *Const:
			byName[d.Name] = i
		case *Struct:
			byName[d.Name] = i
		case *Enum:
			byName[d.Name] = i
		case *Flags:
			byName[d.Name] = i
		case *Object:
			byName[d.Name] = i
		}
	}
	adj := make([][]int, n)
	indeg := make([]int, n)
	var noteExpr func(i int, e ast.Expr)
	noteExpr = func(i int, e ast.Expr) {
		switch e := e.(type) {
		case *ast.IdentExpr:
			if j, ok := byName[e.Name]; ok && j != i {
				adj[j] = append(adj[j], i)
				indeg[i]++
			}
		case *ast.MaxExpr:
			// E.Max always folds to a literal (no C++ twin) — no edge
		case *ast.UnaryExpr:
			noteExpr(i, e.X)
		case *ast.BinaryExpr:
			noteExpr(i, e.X)
			noteExpr(i, e.Y)
		case *ast.ParenExpr:
			noteExpr(i, e.X)
		}
	}
	for i, d := range decls {
		var fields []*Field
		switch d := d.(type) {
		case *Const:
			if d.Expr != nil {
				noteExpr(i, d.Expr)
			}
		case *Struct:
			fields = d.Fields
		case *Object:
			fields = d.Fields
		}
		for _, f := range fields {
			if f.Type.Kind == TNamed {
				if j, ok := byName[f.Type.Name]; ok && j != i {
					adj[j] = append(adj[j], i)
					indeg[i]++
				}
			}
			for _, e := range []ast.Expr{
				f.ArrayExpr,
				f.Type.SizeExpr,
				f.DefExpr,
				f.IntMinExpr,
				f.IntMaxExpr,
				f.QuantScaleExpr,
				f.QuantMaxExpr,
			} {
				if e != nil {
					noteExpr(i, e)
				}
			}
		}
	}
	order := make([]Decl, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		pick := -1
		for i := 0; i < n; i++ {
			if !done[i] && indeg[i] == 0 {
				pick = i
				break
			}
		}
		if pick == -1 {
			// an intra-file type cycle — the checker already rejected the unit;
			// defensively emit the rest in declaration order
			for i := 0; i < n; i++ {
				if !done[i] {
					pick = i
					break
				}
			}
		}
		done[pick] = true
		order = append(order, decls[pick])
		for _, t := range adj[pick] {
			indeg[t]--
		}
	}
	return order
}
