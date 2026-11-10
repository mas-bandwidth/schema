// Coverage: the parser paths the package's own tests leave unexercised. Each
// case pins a value or a refusal the standard names, never a mirror of the
// implementation. Statement coverage lives in parser.go; the rules are SPEC
// §4.2 (qualifications, expressions), §4.3 (arrays and fixed point),
// §4.8 (unions) and docs/SPEC-TABLES.md §2.8 (maps), §2.9 (unbounded arrays).
package parser

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
)

// TestUncoveredPaths reaches the zero-coverage productions of the recursive
// descent — maps, array bounds, the retired bracket block, flags defaults —
// and the uncovered branches of the most-used ones.
func TestUncoveredPaths(t *testing.T) {
	t.Parallel()

	t.Run("map types", func(t *testing.T) {
		src := `package p
table T
{
    a map[string(32)]Thing
    b map[uint8]map[uint16]Item
    c map[?uint8]?Thing
    d map[uint8 = 1 | x]Thing
    e map[uint8][..4]Thing
    f map[uint8]?[..4]Thing
    g map[uint16]*Thing
    h map[uint16]string
}
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		fields := f.Decls[0].(*ast.TableDecl).Body.Items
		field := func(i int) *ast.Field { return fields[i].(*ast.Field) }

		a := field(0).Map
		if a == nil || a.Key.Kind != ast.ScalarString || a.Key.Arg == nil || a.Value.Type.Kind != ast.ScalarNamed || a.Value.Type.Name != "Thing" {
			t.Fatalf("a: want map[string(32)]Thing, got %+v", a)
		}
		b := field(1).Map
		if b == nil || b.Value.Map == nil || b.Value.Map.Key.Width != 16 || b.Value.Map.Value.Type.Name != "Item" {
			t.Fatalf("b: want a nested map with a uint16 value key, got %+v", b)
		}
		c := field(2).Map
		if c == nil || !c.Key.Optional || !c.Value.Type.Optional {
			t.Fatalf("c: want an optional key and value, got %+v", c)
		}
		d := field(3).Map
		if d == nil || d.KeyDefault == nil || len(d.KeyAttrs) != 1 || d.KeyAttrs[0].Key != "x" {
			t.Fatalf("d: want the key default and tag, got %+v", d)
		}
		e := field(4).Map
		if e == nil || e.Value.Array == nil || e.Value.Array.Kind != ast.ArrayUpTo {
			t.Fatalf("e: want a bounded array value, got %+v", e)
		}
		g := field(6).Map
		if g == nil || !g.Value.Type.Pointer || g.Value.Type.Name != "Thing" {
			t.Fatalf("g: want a pointer value, got %+v", g)
		}
		h := field(7).Map
		if h == nil || h.Value.Type.Kind != ast.ScalarString || !h.Value.Type.Unbounded {
			t.Fatalf("h: want an unbounded string value, got %+v", h)
		}
	})

	t.Run("array bounds", func(t *testing.T) {
		src := `package p
type T
{
    a []int32
    b [..4]int32
    c [0..4]int32
    d [4]int32
    e [..4]?string(8)
}
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		items := f.Decls[0].(*ast.TypeDecl).Body.Items
		want := []ast.ArrayKind{ast.ArrayList, ast.ArrayUpTo, ast.ArrayRange, ast.ArrayFixed, ast.ArrayUpTo}
		for i, k := range want {
			fl := items[i].(*ast.Field)
			if fl.Array == nil || fl.Array.Kind != k {
				t.Errorf("%s: want array kind %v, got %+v", fl.Name, k, fl.Array)
			}
		}
		e := items[4].(*ast.Field)
		if !e.Type.ElemOptional || e.Type.Kind != ast.ScalarString {
			t.Errorf("e: want an optional string element, got %+v", e.Type)
		}
	})

	t.Run("scalars", func(t *testing.T) {
		src := `package p
type T
{
    a fixed(16, 16)
    b ufixed(8, 8)
    c bool
    d float32
    e float64
    f bits(3)
    g string(64)
    i bytes(32)
    j string
    k SomeName
    l *SomeTable
    m *bytes
    n *string
    p int128
    q uint128
}
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		items := f.Decls[0].(*ast.TypeDecl).Body.Items
		sc := func(i int) ast.ScalarType { return items[i].(*ast.Field).Type }
		if s := sc(0); s.Kind != ast.ScalarFixed || !s.Signed || s.Arg == nil || s.Arg2 == nil {
			t.Errorf("a: want signed fixed(16,16), got %+v", s)
		}
		if s := sc(1); s.Kind != ast.ScalarFixed || s.Signed {
			t.Errorf("b: want unsigned fixed, got %+v", s)
		}
		for i, k := range []ast.ScalarKind{ast.ScalarBool, ast.ScalarFloat32, ast.ScalarFloat64, ast.ScalarBits, ast.ScalarString, ast.ScalarBytes} {
			if s := sc(2 + i); s.Kind != k {
				t.Errorf("field %d: want kind %v, got %+v", 2+i, k, s)
			}
		}
		if s := sc(8); s.Kind != ast.ScalarString || !s.Unbounded {
			t.Errorf("j: want unbounded string, got %+v", s)
		}
		if s := sc(9); s.Kind != ast.ScalarNamed || s.Name != "SomeName" {
			t.Errorf("k: want a named type, got %+v", s)
		}
		if s := sc(10); !s.Pointer || s.Kind != ast.ScalarNamed || s.Name != "SomeTable" {
			t.Errorf("l: want a pointer to SomeTable, got %+v", s)
		}
		for i, k := range []ast.ScalarKind{ast.ScalarBytes, ast.ScalarString} {
			if s := sc(11 + i); !s.Pointer || s.Kind != k {
				t.Errorf("pointer field %d: want kind %v, got %+v", 11+i, k, s)
			}
		}
		if s := sc(13); s.Width != 128 || !s.Signed {
			t.Errorf("p: want int128, got %+v", s)
		}
		if s := sc(14); s.Width != 128 || s.Signed {
			t.Errorf("q: want uint128, got %+v", s)
		}
	})

	t.Run("flags default", func(t *testing.T) {
		src := `package p
flags F { Jump, Crouch }
type T
{
    a F = { Jump, Crouch }
    b F = {}
    c F = { Jump, }
}
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		items := f.Decls[1].(*ast.TypeDecl).Body.Items
		got := func(i int) []string {
			lit := items[i].(*ast.Field).Default.(*ast.SetLit)
			var out []string
			for _, n := range lit.Names {
				out = append(out, n.Text)
			}
			return out
		}
		if s := strings.Join(got(0), ","); s != "Jump,Crouch" {
			t.Errorf("a: want Jump,Crouch, got %q", s)
		}
		if s := strings.Join(got(1), ","); s != "" {
			t.Errorf("b: want the zero mask, got %q", s)
		}
		if s := strings.Join(got(2), ","); s != "Jump" {
			t.Errorf("c: want Jump, got %q", s)
		}
	})

	t.Run("expressions", func(t *testing.T) {
		src := `package p
const A = 0x10
const B = 0b101
const C = 1.5
const D = - -3
const E = 1 + 2 - 3
const F = 2 * 3 / 4 % 5
const G = (1 + 2)
const H = En.Max
const I = En.Count
const J = "hi"
const K uint64 = 7
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		decl := func(i int) *ast.ConstDecl { return f.Decls[i].(*ast.ConstDecl) }
		if v := decl(0).Expr.(*ast.IntLit).Value.Int64(); v != 16 {
			t.Errorf("A: want 16, got %d", v)
		}
		if v := decl(1).Expr.(*ast.IntLit).Value.Int64(); v != 5 {
			t.Errorf("B: want 5, got %d", v)
		}
		if v := decl(2).Expr.(*ast.FloatLit).Value; v != 1.5 {
			t.Errorf("C: want 1.5, got %v", v)
		}
		if _, ok := decl(3).Expr.(*ast.UnaryExpr); !ok {
			t.Errorf("D: want a unary expression, got %T", decl(3).Expr)
		}
		if b, ok := decl(4).Expr.(*ast.BinaryExpr); !ok || b.Op != "-" {
			t.Errorf("E: want a subtraction, got %+v", decl(4).Expr)
		}
		if b, ok := decl(5).Expr.(*ast.BinaryExpr); !ok || b.Op != "%" {
			t.Errorf("F: want a remainder, got %+v", decl(5).Expr)
		}
		if _, ok := decl(6).Expr.(*ast.ParenExpr); !ok {
			t.Errorf("G: want a parenthesized expression, got %T", decl(6).Expr)
		}
		if m, ok := decl(7).Expr.(*ast.MaxExpr); !ok || m.Sel != "Max" {
			t.Errorf("H: want En.Max, got %+v", decl(7).Expr)
		}
		if m, ok := decl(8).Expr.(*ast.MaxExpr); !ok || m.Sel != "Count" {
			t.Errorf("I: want En.Count, got %+v", decl(8).Expr)
		}
		if s, ok := decl(9).Expr.(*ast.StringLit); !ok || s.Value != "hi" {
			t.Errorf("J: want the string hi, got %+v", decl(9).Expr)
		}
		if decl(10).Type != "uint64" {
			t.Errorf("K: want an explicit uint64 type, got %q", decl(10).Type)
		}
	})

	t.Run("qualifications and retired brackets", func(t *testing.T) {
		// A declaration's | section and its retired [ ... ] block both name
		// their attributes (SPEC §4.2); the body still opens below.
		src := `package p
type A | tag = 1
{
    x int32 | min = 0, max = 1
}
type B [old = true]
{
    y int32 [min = 0, max = 1]
}
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) != 2 {
			t.Fatalf("want the two retired-block refusals, got %v", errs)
		}
		for _, e := range errs {
			if !strings.Contains(e.Error(), "attribute block is retired") {
				t.Errorf("want the retired-block refusal, got %v", e)
			}
		}
		a := f.Decls[0].(*ast.TypeDecl)
		if len(a.Attrs) != 1 || a.Attrs[0].Key != "tag" || a.Attrs[0].Value == nil {
			t.Errorf("A: want the tag attribute, got %+v", a.Attrs)
		}
		if len(a.Body.Items) != 1 {
			t.Fatalf("A: want the body parsed, got %+v", a.Body.Items)
		}
		if attrs := a.Body.Items[0].(*ast.Field).Attrs; len(attrs) != 2 || attrs[1].Key != "max" {
			t.Errorf("x: want min and max, got %+v", attrs)
		}
		b := f.Decls[1].(*ast.TypeDecl)
		if len(b.Attrs) != 1 || b.Attrs[0].Key != "old" {
			t.Errorf("B: want the retired block's old attribute, got %+v", b.Attrs)
		}
		y := b.Body.Items[0].(*ast.Field)
		if len(y.Attrs) != 2 || y.Attrs[0].Key != "min" {
			t.Errorf("y: want the retired block's attributes, got %+v", y.Attrs)
		}
	})

	t.Run("old default after attributes", func(t *testing.T) {
		src := "package p\ntype T\n{\n    a int32 [min = 0] = 3\n}\n"
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "attribute block is retired") {
			t.Fatalf("want the retired-block refusal, got %v", errs)
		}
		fl := f.Decls[0].(*ast.TypeDecl).Body.Items[0].(*ast.Field)
		if v := fl.Default.(*ast.IntLit).Value.Int64(); v != 3 {
			t.Errorf("want default 3, got %d", v)
		}
	})

	t.Run("union arms", func(t *testing.T) {
		src := `package p
type T { x int32 }
union U
{
    none
    tagged | x
    t T
    m map[uint8]T
    arr [..4]int32
    opt ?T
    ptr *T
}
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		u := f.Decls[1].(*ast.UnionDecl)
		if len(u.Variants) != 7 {
			t.Fatalf("want 7 arms, got %d", len(u.Variants))
		}
		if v := u.Variants[0]; v.Arm != nil {
			t.Errorf("none: want a payload-free arm, got %+v", v)
		}
		if v := u.Variants[1]; len(v.Attrs) != 1 || v.Attrs[0].Key != "x" {
			t.Errorf("tagged: want the arm tag, got %+v", v)
		}
		if v := u.Variants[2]; v.Type != "T" || v.Arm == nil {
			t.Errorf("t: want the bare declaration name and its field, got %+v", v)
		}
		if v := u.Variants[3]; v.Arm == nil || v.Arm.Map == nil {
			t.Errorf("m: want a map payload, got %+v", v)
		}
		if v := u.Variants[4]; v.Arm == nil || v.Arm.Array == nil {
			t.Errorf("arr: want an array payload, got %+v", v)
		}
		if v := u.Variants[5]; v.Arm == nil || !v.Arm.Type.Optional {
			t.Errorf("opt: want an optional payload, got %+v", v)
		}
		if v := u.Variants[6]; v.Arm == nil || !v.Arm.Type.Pointer {
			t.Errorf("ptr: want a pointer payload, got %+v", v)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		for _, tc := range []struct{ src, want string }{
			// array-of-arrays, both bracket spellings (SPEC §4.3)
			{"package p\ntype T {\n    a [][]int32\n}\n", "an array of arrays is not supported in v1"},
			{"package p\ntype T {\n    a [..4][]int32\n}\n", "an array of arrays is not supported in v1"},
			{"package p\ntype T {\n    a map[uint8][][..2]T\n}\n", "an array of arrays is not supported in v1"},
			// retired and incomplete bounds (SPEC §4.3, docs/SPEC-TABLES.md §2.9)
			{"package p\ntype T {\n    a [<= 4]int32\n}\n", "the [<= N] bound is retired"},
			{"package p\ntype T {\n    a [..]int32\n}\n", "[..]T is not the unbounded array"},
			{"package p\ntype T {\n    a [0..]int32\n}\n", "[Min..]T is not the unbounded array"},
			// a buffer pointer takes no bound (docs/SPEC-TABLES.md §2.5)
			{"package p\ntype T {\n    a *bytes(4)\n}\n", "*bytes takes no bound"},
			// reserved spellings (SPEC §4.2)
			{"package p\ntype T {\n    a int\n}\n", `"int" is reserved`},
			{"package p\ntype T {\n    a ,\n}\n", "expected a field type"},
			// fixed is a table keyword at file scope (docs/SPEC-TABLES.md §2.2)
			{"package p\nfixed int32 x\n", "`fixed` at file scope qualifies a table declaration"},
			{"package p\nmessage M\n", "messages are not part of the language"},
			{"package p\nobject O\n", "objects are not part of the language"},
			{"package p\nswitch x\n", "switch is cut from v1"},
			{"package p\nint x\n", `"int" is reserved`},
			{"package p\ncontexts x\n", "contexts are not part of the language"},
			{"package p\nbogus\n", `unexpected "bogus" at file scope`},
			{"package p\n}\n", "unexpected"},
			// a qualification section is never empty or valued (SPEC §4.2)
			{"package p\ntype T |\n{\n    a int32\n}\n", "empty qualification section"},
			{"package p\ntype T {\n    a int32 | table, local\n}\n", "table is a reserved word and cannot be a tag"},
			{"package p\ntype T {\n    a int32 | table\n}\n", "table is a reserved word and cannot be a tag"},
			// only .Max and .Count follow a name (SPEC §4.2)
			{"package p\nconst X = En.Min\n", "only .Max"},
			// an expression must stand after = (SPEC §4.2)
			{"package p\nconst X =\n", "expected an expression"},
			// a map's brackets are named in the diagnostic (docs/SPEC-TABLES.md §2.8)
			{"package p\ntype T {\n    a map uint8 T\n}\n", "[ after map"},
			{"package p\ntype T {\n    a map[uint8 T\n}\n", "] after a map key"},
			// an arm cannot carry the tag member's own name (SPEC §4.8)
			{"package p\nunion U\n{\n    type T\n}\n", "variant type is a compile error"},
			// an unterminated body names EOF (SPEC §4.1)
			{"package p\ntype T\n{\n    a int32\n", "unexpected end of file inside block"},
			{"package p\nunion U\n{\n    a int32\n", "unexpected end of file inside union body"},
		} {
			got := errorTexts(tc.src)
			found := false
			for _, g := range got {
				if strings.Contains(g, tc.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("%q: want %q, got %v", tc.src, tc.want, got)
			}
		}
	})

	t.Run("missing package", func(t *testing.T) {
		_, errs := Parse("T.schema", []byte("\n"))
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "missing package declaration") {
			t.Errorf("want the missing-package refusal, got %v", errs)
		}
	})

	t.Run("item refusals and recovery", func(t *testing.T) {
		// const( ), reserved( ), align and if parse inside a body (SPEC §4.5);
		// switch stays reserved, and an unexpected token recovers to the line.
		src := `package p
type T
{
    const(1, 8)
    reserved(8)
    align
    a bool
    if a { x int32 } else { y int32 }
    switch a
    %
}
`
		f, errs := Parse("T.schema", []byte(src))
		if len(errs) != 2 || !strings.Contains(errs[0].Error(), "switch is cut from v1") || !strings.Contains(errs[1].Error(), "unexpected") {
			t.Fatalf("want the switch refusal and the unexpected token, got %v", errs)
		}
		items := f.Decls[0].(*ast.TypeDecl).Body.Items
		if len(items) != 5 {
			t.Fatalf("want 5 items, got %d: %+v", len(items), items)
		}
		if _, ok := items[0].(*ast.ConstField); !ok {
			t.Errorf("const item: got %T", items[0])
		}
		if _, ok := items[1].(*ast.ReservedItem); !ok {
			t.Errorf("reserved item: got %T", items[1])
		}
		if _, ok := items[2].(*ast.AlignItem); !ok {
			t.Errorf("align item: got %T", items[2])
		}
		it, ok := items[4].(*ast.IfItem)
		if !ok || it.Neg || it.Else == nil || len(it.Then.Items) != 1 {
			t.Errorf("if item: got %+v", items[4])
		}
	})
}
