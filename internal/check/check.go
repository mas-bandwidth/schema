// Package check resolves and validates a compilation unit and lowers it to IR
// (SPEC §7.1): name resolution across the unit's files, constant folding
// (arbitrary precision with per-use fit checks, §4.2), the shape checks of
// §4.6, the dominance rule of §4.5, and the §3.1 protocol id.
package check

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SourceFile is one *.schema file of the unit.
type SourceFile struct {
	Path  string // as given
	Name  string // basename with extension — the unit's sort key, unique per unit
	Base  string // basename without extension
	Bytes []byte
	AST   *ast.File
}

type checker struct {
	files []SourceFile
	unit  *ir.Unit
	errs  []error

	astDecls map[string]ast.Decl // flat namespace
	declFile map[string]string   // name -> file base
	constant map[string]*constEntry
	enums    map[string]*ir.Enum
	flagsD   map[string]*ir.Flags
	structs  map[string]*ir.Struct
	unions   map[string]*ir.Union
	tables   map[string]*ir.Struct // `table` declarations (SPEC-TABLES.md)

	// the table closure — tables plus every struct one reaches — computed by
	// checkTables and consumed by checkClaimedNames (closure members grow
	// generated Table* symbols)
	tableClosure map[string]bool

	// enums currently being resolved — the cycle guard for | max = E.Max
	// chains (resolveEnum memoizes only on completion, so recursion needs
	// its own in-progress set, exactly as constants have one)
	resolvingEnum map[string]bool

	// enums whose | max = ... failed to resolve — their Max fell back to the
	// variant count, which is NOT what the author wrote, so .Max references
	// to them must fail rather than propagate a fabricated bound
	failedEnum map[string]bool
}

type constEntry struct {
	decl  *ast.ConstDecl
	state int // 0 unresolved, 1 resolving, 2 ok, 3 failed
	out   *ir.Const
}

func (c *checker) errf(pos ast.Pos, format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf("%s: %s", pos, fmt.Sprintf(format, args...)))
}

// Unit checks the files and lowers them to IR. Files may arrive in any order;
// the unit is processed in sorted-basename order, so everything derived from
// it is deterministic.
func Unit(files []SourceFile) (*ir.Unit, []error) {
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	c := &checker{
		files:    files,
		astDecls: map[string]ast.Decl{},
		declFile: map[string]string{},
		constant: map[string]*constEntry{},
		enums:    map[string]*ir.Enum{},
		flagsD:   map[string]*ir.Flags{},
		structs:  map[string]*ir.Struct{},
		unions:   map[string]*ir.Union{},
		tables:   map[string]*ir.Struct{},
		unit: &ir.Unit{
			DeclFile: map[string]string{},
			Consts:   map[string]*ir.Const{},
			Enums:    map[string]*ir.Enum{},
			Flags:    map[string]*ir.Flags{},
			Structs:  map[string]*ir.Struct{},
			Unions:   map[string]*ir.Union{},
			Tables:   map[string]*ir.Struct{},
		},
	}

	c.collectPackage()
	c.collectDecls()
	c.resolveEnumsAndFlags()
	c.resolveAllConsts()
	c.resolveBodies()
	c.checkCycles()
	c.checkTables()
	c.checkTableFileDag()
	c.checkClaimedNames()
	c.checkTargetNames()
	c.assemble()
	if len(c.errs) > 0 {
		return nil, c.errs
	}

	// The id comes LAST, from the assembled unit: the projection describes the
	// resolved wire, so it cannot be computed until every bound, every default
	// and every branch has been resolved. Computing it over a unit that failed
	// checking would be meaningless, so it sits after the error gate.
	c.unit.ProtocolId = protocolIdFromProjection(c.unit)

	return c.unit, nil
}

func (c *checker) collectPackage() {
	for _, f := range c.files {
		if f.AST.Package == "" {
			continue // parser already reported it
		}
		if c.unit.Package == "" {
			c.unit.Package = f.AST.Package
			c.checkPackageName(f.AST.Package, f.AST.PkgPos)
		} else if f.AST.Package != c.unit.Package {
			c.errf(f.AST.PkgPos, "package %q does not match the unit's package %q (exactly one package per unit — SPEC §3.2)",
				f.AST.Package, c.unit.Package)
		}
	}
}

// checkPackageName refuses package names that generate uncompilable code —
// the colliding names get a clear diagnostic. The package ident maps to the
// target's namespace/module/package concept verbatim (SPEC §6.1), which exposes
// it to three collision classes no declaration or field name can hit.
func (c *checker) checkPackageName(name string, pos ast.Pos) {
	if lang, ok := targetReserved[name]; ok {
		c.errf(pos, "package name %q is a reserved word in %s (the package ident becomes the target's namespace/module/package name verbatim) — rename the package; no escaping machinery (SPEC §4.6)",
			name, lang)
		return
	}
	if libcNamespaceScope[name] {
		c.errf(pos, "package name %q collides with a C standard library identifier visible at C++ namespace scope (the generated `namespace %s` cannot be declared beside libc's %s) — rename the package (SPEC §4.6)",
			name, name, name)
		return
	}
	if name == "main" {
		c.errf(pos, "package name \"main\" makes the generated Go a program package that cannot be imported (\"function main is undeclared in the main package\") — rename the package (SPEC §4.6)")
	}
}

func (c *checker) collectDecls() {
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			name := d.DeclName()
			if prev, ok := c.astDecls[name]; ok {
				c.errf(d.DeclPos(), "duplicate declaration %q (first declared at %s; all declaration kinds share one unit-level namespace — SPEC §4.6)",
					name, prev.DeclPos())
				continue
			}
			c.astDecls[name] = d
			c.declFile[name] = f.Base
			if cd, ok := d.(*ast.ConstDecl); ok {
				c.constant[name] = &constEntry{decl: cd}
			}
		}
	}
}

// ---- constant folding (SPEC §4.2: arbitrary precision, fit checks at use) ----

func (c *checker) resolveAllConsts() {
	names := make([]string, 0, len(c.constant))
	for n := range c.constant {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c.resolveConst(n)
	}
}

func (c *checker) resolveConst(name string) *ir.Const {
	e := c.constant[name]
	if e == nil {
		return nil
	}
	switch e.state {
	case 1:
		c.errf(e.decl.Pos, "constant %s is part of a reference cycle (SPEC §4.2: reference cycles are a compile error)", name)
		e.state = 3
		return nil
	case 2:
		return e.out
	case 3:
		return nil
	}
	e.state = 1
	out := &ir.Const{Name: name, Expr: e.decl.Expr}

	isFloatType := e.decl.Type == "float32" || e.decl.Type == "float64"
	kind := c.exprKind(e.decl.Expr)

	if kind == kindFloat || isFloatType {
		v, ok := c.evalFloat(e.decl.Expr)
		if !ok {
			e.state = 3
			return nil
		}
		if math.IsInf(v, 0) || math.IsNaN(v) {
			c.errf(e.decl.Pos, "constant %s is not a finite float", name)
			e.state = 3
			return nil
		}
		out.IsFloat = true
		out.Float = v
		out.Storage = e.decl.Type
		out.Explicit = e.decl.Type != ""
		if out.Storage == "" {
			out.Storage = "float64"
		}
		if e.decl.Type == "float32" && math.Abs(v) > math.MaxFloat32 {
			c.errf(e.decl.Pos, "constant %s value %g does not fit its declared type float32", name, v)
			e.state = 3
			return nil
		}
		if e.decl.Type != "" && !isFloatType {
			// integer-typed constant with a float expression
			c.errf(e.decl.Pos, "constant %s has integer type %s but a float expression", name, e.decl.Type)
			e.state = 3
			return nil
		}
	} else {
		v, ok := c.evalInt(e.decl.Expr)
		if !ok {
			e.state = 3
			return nil
		}
		out.Int = v
		out.Storage = e.decl.Type
		out.Explicit = e.decl.Type != ""
		if out.Storage == "" {
			out.Storage = "int64"
		}
		switch {
		case isFloatType:
			// float-typed constant with an integer expression: converts
			f, _ := new(big.Float).SetInt(v).Float64()
			out.IsFloat = true
			out.Float = f
			out.Int = nil
		case e.decl.Type != "":
			if !fitsStorage(v, e.decl.Type) {
				c.errf(e.decl.Pos, "constant %s value %s does not fit its declared type %s", name, v, e.decl.Type)
				e.state = 3
				return nil
			}
		case !fitsStorage(v, out.Storage):
			// an implicitly-typed constant defaults to int64 storage and was
			// never held to it: a value past the int64 range reached every
			// backend as an int64 constant it cannot represent — an
			// unrepresentable (or constexpr-narrowing) literal in C, C++ and
			// C# (found by FuzzGeneratedCompiles)
			c.errf(e.decl.Pos, "constant %s value %s does not fit int64, the default constant storage — declare an explicit type (const %s uint64 = ...) if a wider range is intended", name, v, name)
			e.state = 3
			return nil
		}
	}
	e.state = 2
	e.out = out
	return out
}

const (
	kindInt = iota
	kindFloat
)

// exprKind infers a constant expression's kind, Go's untyped-constant style:
// float if any float literal or float constant appears in it. The visiting set
// makes it terminate on constant reference cycles (returning int), so the
// resolver's own cycle guard gets to emit the §4.2 diagnostic instead of the
// compiler recursing to a stack overflow.
func (c *checker) exprKind(e ast.Expr) int {
	return c.exprKindV(e, map[string]bool{})
}

func (c *checker) exprKindV(e ast.Expr, visiting map[string]bool) int {
	if e == nil {
		return kindInt // see evalInt — the evaluators reject it with a position
	}
	switch e := e.(type) {
	case *ast.FloatLit:
		return kindFloat
	case *ast.IntLit, *ast.MaxExpr:
		return kindInt
	case *ast.IdentExpr:
		if ce := c.constant[e.Name]; ce != nil && !visiting[e.Name] {
			visiting[e.Name] = true
			// An explicitly float-typed constant is float in EVERY resolution
			// state — the declared type is right there in the AST. Reading it
			// first is what keeps classification order-free (SPEC §4.2: a
			// const "may reference any other const in the unit, order-free
			// across files"). Without it the resolved path honoured the
			// declared type while the shell path re-walked only the
			// expression, so `const Mid float64 = 3` classified as float or
			// int purely by which name sorted first — and a bare referrer
			// then took the integer path and was spuriously REJECTED.
			if ce.decl.Type == "float32" || ce.decl.Type == "float64" {
				return kindFloat
			}
			if ce.state == 2 && ce.out.IsFloat {
				return kindFloat
			}
			if ce.state == 0 {
				if c.exprKindV(ce.decl.Expr, visiting) == kindFloat {
					return kindFloat
				}
			}
		}
		return kindInt
	case *ast.UnaryExpr:
		return c.exprKindV(e.X, visiting)
	case *ast.BinaryExpr:
		if c.exprKindV(e.X, visiting) == kindFloat || c.exprKindV(e.Y, visiting) == kindFloat {
			return kindFloat
		}
		return kindInt
	case *ast.ParenExpr:
		return c.exprKindV(e.X, visiting)
	}
	return kindInt
}

func (c *checker) evalInt(e ast.Expr) (*big.Int, bool) {
	// Defense in depth. Callers are expected to reject a missing expression
	// with a positioned diagnostic before reaching here (see valuedAttr), but
	// a nil that slips through must fail the evaluation, not dereference —
	// the fallthrough below calls e.ExprPos(), which panics on a nil Expr.
	if e == nil {
		return nil, false
	}
	switch e := e.(type) {
	case *ast.IntLit:
		return new(big.Int).Set(e.Value), true
	case *ast.FloatLit:
		c.errf(e.Pos, "float literal in integer context")
		return nil, false
	case *ast.IdentExpr:
		if _, isConst := c.constant[e.Name]; !isConst {
			if _, isDecl := c.astDecls[e.Name]; isDecl {
				c.errf(e.Pos, "%s is not a constant", e.Name)
			} else {
				c.errf(e.Pos, "undefined constant %s", e.Name)
			}
			return nil, false
		}
		out := c.resolveConst(e.Name)
		if out == nil {
			return nil, false
		}
		if out.IsFloat {
			c.errf(e.Pos, "float constant %s in integer context", e.Name)
			return nil, false
		}
		return new(big.Int).Set(out.Int), true
	case *ast.MaxExpr:
		return c.enumMax(e)
	case *ast.UnaryExpr:
		x, ok := c.evalInt(e.X)
		if !ok {
			return nil, false
		}
		return x.Neg(x), true
	case *ast.BinaryExpr:
		x, ok := c.evalInt(e.X)
		if !ok {
			return nil, false
		}
		y, ok := c.evalInt(e.Y)
		if !ok {
			return nil, false
		}
		switch e.Op {
		case "+":
			return x.Add(x, y), true
		case "-":
			return x.Sub(x, y), true
		case "*":
			return x.Mul(x, y), true
		case "/":
			if y.Sign() == 0 {
				c.errf(e.Pos, "division by zero")
				return nil, false
			}
			return x.Quo(x, y), true // truncates toward zero, Go's rule (SPEC §4.2)
		case "%":
			if y.Sign() == 0 {
				c.errf(e.Pos, "division by zero")
				return nil, false
			}
			return x.Rem(x, y), true
		}
	case *ast.ParenExpr:
		return c.evalInt(e.X)
	}
	c.errf(e.ExprPos(), "invalid integer expression")
	return nil, false
}

func (c *checker) evalFloat(e ast.Expr) (float64, bool) {
	if e == nil {
		return 0, false // see evalInt — a nil expression fails, never panics
	}
	switch e := e.(type) {
	case *ast.FloatLit:
		return e.Value, true
	case *ast.IntLit:
		return c.bigIntFloat(e.Value, e.Pos)
	case *ast.IdentExpr:
		if _, isConst := c.constant[e.Name]; !isConst {
			c.errf(e.Pos, "undefined constant %s", e.Name)
			return 0, false
		}
		out := c.resolveConst(e.Name)
		if out == nil {
			return 0, false
		}
		if out.IsFloat {
			return out.Float, true
		}
		return c.bigIntFloat(out.Int, e.Pos) // integer constants convert in float positions (SPEC §4.2)
	case *ast.MaxExpr:
		v, ok := c.enumMax(e)
		if !ok {
			return 0, false
		}
		f, _ := new(big.Float).SetInt(v).Float64()
		return f, true
	case *ast.UnaryExpr:
		x, ok := c.evalFloat(e.X)
		return -x, ok
	case *ast.BinaryExpr:
		x, ok := c.evalFloat(e.X)
		if !ok {
			return 0, false
		}
		y, ok := c.evalFloat(e.Y)
		if !ok {
			return 0, false
		}
		var r float64
		switch e.Op {
		case "+":
			r = x + y
		case "-":
			r = x - y
		case "*":
			r = x * y
		case "/":
			if y == 0 {
				c.errf(e.Pos, "division by zero")
				return 0, false
			}
			r = x / y
		case "%":
			c.errf(e.Pos, "%% is not defined for float expressions (SPEC §4.2)")
			return 0, false
		default:
			c.errf(e.Pos, "invalid float operator %q", e.Op)
			return 0, false
		}
		if math.IsInf(r, 0) || math.IsNaN(r) {
			c.errf(e.Pos, "float constant expression overflows float64")
			return 0, false
		}
		return r, true
	case *ast.ParenExpr:
		return c.evalFloat(e.X)
	}
	c.errf(e.ExprPos(), "invalid float expression")
	return 0, false
}

// bigIntFloat converts an integer constant appearing in a float position
// (SPEC §4.2). The conversion must stay finite: big.Float.Float64 silently
// returns ±Inf for magnitudes beyond the double's range, and a non-finite
// value must never impersonate a constant — the compressed-float triple is
// where one would reach the wire (SPEC §4.6, the non-finite rule).
func (c *checker) bigIntFloat(v *big.Int, pos ast.Pos) (float64, bool) {
	f, _ := new(big.Float).SetInt(v).Float64()
	if math.IsInf(f, 0) {
		c.errf(pos, "integer constant %s does not fit float64", v.String())
		return 0, false
	}
	return f, true
}

// valuedAttr lists the field attributes that MUST carry `= value`. Adding a
// valued attribute means adding it here, or a bare spelling of it reaches
// expression evaluation as a nil and panics.
var valuedAttr = map[string]bool{
	"min":        true,
	"max":        true,
	"resolution": true,
	"was":        true,
	"json":       true,
}

func (c *checker) enumMax(e *ast.MaxExpr) (*big.Int, bool) {
	if e.Sel == "Count" {
		return c.flagsCount(e)
	}
	d, ok := c.astDecls[e.Enum]
	if !ok {
		// the generated tag set works in constant expressions too (SPEC §4.2,
		// §4.8): <Union>Type.Max derives from the declared variant count
		// alone, so it resolves during const folding, before any body
		// resolves.
		if max, isGen := c.generatedSetMax(e.Enum); isGen {
			return big.NewInt(max), true
		}
		c.errf(e.Pos, "undefined enum %s in %s.Max", e.Enum, e.Enum)
		return nil, false
	}
	if _, isFlags := d.(*ast.FlagsDecl); isFlags {
		// max-of-what is the exact confusion a flags .Max would invite: the
		// variants are independent bits, not an ordered range with a top
		c.errf(e.Pos, "flags %s has no .Max — a flags declaration is a set of independent bits, not a range with a top; %s.Count names the declared variant count (SPEC §4.2)", e.Enum, e.Enum)
		return nil, false
	}
	ed, ok := d.(*ast.EnumDecl)
	if !ok {
		c.errf(e.Pos, "%s is not an enum — .Max names an enum's max (SPEC §4.2)", e.Enum)
		return nil, false
	}
	en := c.resolveEnum(ed)
	if en == nil {
		return nil, false
	}
	if c.failedEnum[e.Enum] {
		// its own bound never resolved, so en.Max is the fallback variant
		// count, not the declared max — propagating it would fabricate a
		// bound and produce a cascade diagnostic about a value nobody wrote
		return nil, false
	}
	return big.NewInt(en.Max), true
}

// flagsCount resolves F.Count (SPEC §4.2): the DECLARED variant count of a
// flags declaration — not the wire width, which a | max = K widening can
// raise above it.
func (c *checker) flagsCount(e *ast.MaxExpr) (*big.Int, bool) {
	d, ok := c.astDecls[e.Enum]
	if !ok {
		c.errf(e.Pos, "undefined flags %s in %s.Count", e.Enum, e.Enum)
		return nil, false
	}
	fd, ok := d.(*ast.FlagsDecl)
	if !ok {
		c.errf(e.Pos, "%s is not a flags declaration — .Count names a flags declaration's variant count; an enum's extent is %s.Max (SPEC §4.2)", e.Enum, e.Enum)
		return nil, false
	}
	fl := c.resolveFlags(fd)
	if fl == nil {
		return nil, false
	}
	return big.NewInt(int64(len(fl.Variants))), true
}

// generatedSetMax resolves E.Max over the GENERATED tag set (SPEC §4.2,
// §4.8): <Union>Type for a declared union. The max is the member count —
// dense from 1, None = 0.
func (c *checker) generatedSetMax(name string) (int64, bool) {
	if base, ok := strings.CutSuffix(name, "Type"); ok {
		if ud, isUnion := c.astDecls[base].(*ast.UnionDecl); isUnion {
			return int64(len(ud.Variants)), true
		}
	}
	return 0, false
}

// ---- enums and flags ----

func (c *checker) resolveEnumsAndFlags() {
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.EnumDecl:
				c.resolveEnum(d)
			case *ast.FlagsDecl:
				c.resolveFlags(d)
			}
		}
	}
}

func (c *checker) resolveEnum(d *ast.EnumDecl) *ir.Enum {
	if en, ok := c.enums[d.Name]; ok {
		return en
	}
	// In-progress guard, the twin of resolveConst's state machine. An
	// enum's | max = Other.Max resolves Other, which can lead back here —
	// and the memo below is only written at the END, so without this the
	// recursion never terminates. It reached the Go runtime as a raw
	// "fatal error: stack overflow" with no diagnostic and no source
	// position: the compiler dying rather than rejecting bad input.
	if c.resolvingEnum[d.Name] {
		c.errf(d.Pos, "enum %s is part of a reference cycle (SPEC §4.2: reference cycles are a compile error)", d.Name)
		return nil
	}
	if c.resolvingEnum == nil {
		c.resolvingEnum = map[string]bool{}
	}
	c.resolvingEnum[d.Name] = true
	defer delete(c.resolvingEnum, d.Name)

	seen := map[string]bool{}
	var variants []string
	for _, v := range d.Variants {
		if v.Text == "None" {
			c.errf(v.Pos, "variant None is a compile error — every enum has None = 0 implicitly (SPEC §4.2)")
			continue
		}
		if v.Text == "Max" {
			c.errf(v.Pos, "variant Max is a compile error — every generated enum carries its extent as the member Max, the same number E.Max names (SPEC §4.2)")
			continue
		}
		if seen[v.Text] {
			c.errf(v.Pos, "duplicate variant %s", v.Text)
			continue
		}
		seen[v.Text] = true
		variants = append(variants, v.Text)
	}
	// An enum with no variants is LEGAL: it holds only the implicit None = 0,
	// so its wire range is the degenerate [0, 0] and it costs zero bits. That
	// is the same rule a | min = K, max = K field follows, and every runtime
	// supports it — a degenerate range is defined by STANDARD.md and the five
	// serialize ports agree on it.
	//
	// It is useful rather than a curiosity: an enum declared before its
	// variants are known keeps compiling and spends nothing on the wire, and a
	// field typed by it round-trips as None without the schema having to
	// invent a placeholder variant to satisfy the compiler.
	max := int64(len(variants))
	for _, a := range d.Attrs {
		if a.Key != "max" {
			c.errf(a.Pos, "unknown attribute %q on an enum — enums take | max = K only (SPEC §4.6)", a.Key)
			continue
		}
		if a.Value == nil {
			c.errf(a.Pos, "max takes a value")
			continue
		}
		v, ok := c.evalInt(a.Value)
		if !ok {
			// the bound never resolved (a cycle, an undefined name, a bad
			// expression — all already reported). Mark the enum degraded so
			// dependents do not inherit the fallback count and report a
			// CASCADE error naming a max the author never wrote.
			if c.failedEnum == nil {
				c.failedEnum = map[string]bool{}
			}
			c.failedEnum[d.Name] = true
			continue
		}
		if !v.IsInt64() || v.Int64() < max {
			c.errf(a.Pos, "enum %s | max = %s is below its variant count %d (SPEC §4.6)", d.Name, v, max)
			continue
		}
		if v.Int64() > math.MaxInt32 {
			// the enum wire rides the 32-bit ranged call in every target
			c.errf(a.Pos, "enum %s | max = %s exceeds the 32-bit tag wire's ceiling %d (SPEC §4.6)", d.Name, v, int64(math.MaxInt32))
			continue
		}
		max = v.Int64()
	}
	en := &ir.Enum{Name: d.Name, Variants: variants, Max: max, StorageBits: ir.StorageBitsFor(max)}
	c.enums[d.Name] = en
	return en
}

func (c *checker) resolveFlags(d *ast.FlagsDecl) *ir.Flags {
	if fl, ok := c.flagsD[d.Name]; ok {
		return fl
	}
	seen := map[string]bool{}
	var variants []string
	for _, v := range d.Variants {
		if seen[v.Text] {
			c.errf(v.Pos, "duplicate variant %s", v.Text)
			continue
		}
		seen[v.Text] = true
		variants = append(variants, v.Text)
	}
	if len(variants) == 0 {
		c.errf(d.Pos, "flags %s has no variants", d.Name)
	}
	if len(variants) > 64 {
		c.errf(d.Pos, "flags %s has %d variants — one bit per variant, up to 64 (SPEC §4.2)", d.Name, len(variants))
	}
	bits := len(variants)
	for _, a := range d.Attrs {
		if a.Key != "max" {
			c.errf(a.Pos, "unknown attribute %q on flags — flags take | max = K only", a.Key)
			continue
		}
		if a.Value == nil {
			c.errf(a.Pos, "max takes a value")
			continue
		}
		v, ok := c.evalInt(a.Value)
		if !ok {
			continue
		}
		if !v.IsInt64() || v.Int64() < int64(bits) || v.Int64() > 64 {
			c.errf(a.Pos, "flags %s | max = %s must be in [%d, 64]", d.Name, v, bits)
			continue
		}
		bits = int(v.Int64())
	}
	fl := &ir.Flags{Name: d.Name, Variants: variants, WireBits: bits}
	c.flagsD[d.Name] = fl
	return fl
}

// ---- type bodies ----

func (c *checker) resolveBodies() {
	// shells first so composition can reference in any order
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.TypeDecl:
				st := &ir.Struct{Name: d.Name}
				for _, a := range d.Attrs {
					switch a.Key {
					case "cpp_native":
						// C++ native type mapping (SPEC §4.2): generated C++
						// refers to this type by the mapped GLOBAL name, so a
						// hand math type deriving from the generated basis can
						// live in storage directly. The mapping is C++-only;
						// every other target ignores it.
						id, ok := a.Value.(*ast.IdentExpr)
						if !ok {
							c.errf(a.Pos, "cpp_native takes an identifier: the global C++ type name (SPEC §4.2 Native type mapping)")
							continue
						}
						st.CppNative = id.Name
					case "cpp_include":
						lit, ok := a.Value.(*ast.StringLit)
						if !ok {
							c.errf(a.Pos, `cpp_include takes a quoted header path, e.g. cpp_include = "core_vector.h" (SPEC §4.2 Native type mapping)`)
							continue
						}
						st.CppInclude = lit.Value
					default:
						if a.Value != nil {
							c.errf(a.Pos, "a type tag is a bare identifier (SPEC §4.2 Type tags)")
							continue
						}
						st.Tags = append(st.Tags, a.Key)
					}
				}
				if (st.CppNative == "") != (st.CppInclude == "") {
					c.errf(d.Pos, "cpp_native and cpp_include go together: the mapped name needs the header that declares it (SPEC §4.2 Native type mapping)")
				}
				c.structs[d.Name] = st
			case *ast.TableDecl:
				// tables share the struct shape but live beside the packet
				// decls, never among them (SPEC-TABLES.md): the packet wire,
				// the projection and the protocol id do not know they exist
				c.tables[d.Name] = &ir.Struct{Name: d.Name, IsTable: true}
			case *ast.UnionDecl:
				// the shell first, so fields can reference the union in any
				// order; variants resolve in the second pass below. Max and
				// storage come from the declared count alone — zero variants
				// is legal, the empty-enum rule (SPEC §4.8): tag range [0, 0],
				// zero bits.
				c.unions[d.Name] = &ir.Union{
					Name:        d.Name,
					Max:         int64(len(d.Variants)),
					StorageBits: ir.StorageBitsFor(int64(len(d.Variants))),
				}
			}
		}
	}
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.TypeDecl:
				c.structs[d.Name].Fields, c.structs[d.Name].Items = c.resolveBody(d.Name, d.Body, false)
			case *ast.TableDecl:
				c.tables[d.Name].Fields, c.tables[d.Name].Items = c.resolveBody(d.Name, d.Body, true)
			case *ast.UnionDecl:
				c.resolveUnion(d)
			}
		}
	}
}

// resolveUnion fills a union shell's variants (SPEC §4.8): names checked over
// the EXPORTED spelling (None/Max reserved post-mapping, uniqueness
// post-mapping — box_a and boxA both export BoxA), payloads restricted to
// declared types.
func (c *checker) resolveUnion(d *ast.UnionDecl) {
	un := c.unions[d.Name]
	seen := map[string]ast.Pos{}
	for _, v := range d.Variants {
		exported := ir.GoExportName(v.Name)
		switch exported {
		case "None":
			c.errf(v.Pos, "variant %s is a compile error — every union has None = 0 implicitly, checked over the exported spelling (SPEC §4.8)", v.Name)
			continue
		case "Max":
			c.errf(v.Pos, "variant %s is a compile error — every generated union tag enum carries its extent as the member Max, checked over the exported spelling (SPEC §4.8, §4.2)", v.Name)
			continue
		case "Type":
			c.errf(v.Pos, "variant %s is a compile error — its exported spelling is Type, the tag member's own name in the Go and C# representations; rename at the source (SPEC §4.8)", v.Name)
			continue
		case d.Name:
			// C# refuses a member named after its enclosing class (CS0542)
			c.errf(v.Pos, "variant %s is a compile error — its exported spelling equals the union's own name, which C# refuses (CS0542); rename at the source (SPEC §4.6)", v.Name)
			continue
		}
		if lang, bad := targetReserved[v.Name]; bad {
			c.errf(v.Pos, "variant %s is a reserved word in %s — rename at the source, no escaping machinery (SPEC §4.6)", v.Name, lang)
			continue
		}
		if prev, dup := seen[exported]; dup {
			c.errf(v.Pos, "duplicate variant %s in union %s (first at %s; names are unique AFTER export mapping — both become %s) (SPEC §4.8)",
				v.Name, d.Name, prev, exported)
			continue
		}
		seen[exported] = v.Pos

		pd, ok := c.astDecls[v.Type]
		if !ok {
			c.errf(v.TypePos, "undefined type %s in union %s", v.Type, d.Name)
			continue
		}
		switch pd.(type) {
		case *ast.TypeDecl:
			un.Variants = append(un.Variants, ir.UnionVariant{Name: v.Name, Type: v.Type, Ref: c.structs[v.Type]})
		case *ast.TableDecl:
			c.errf(v.TypePos, "%s is a table, not a union payload — a payload is a declared `type`; tables nest in tables directly (SPEC-TABLES.md)", v.Type)
		case *ast.EnumDecl, *ast.FlagsDecl:
			c.errf(v.TypePos, "%s is not a union payload — a payload is a declared type; wrap the value in a type (SPEC §4.8)", v.Type)
		case *ast.UnionDecl:
			c.errf(v.TypePos, "a union is not a union payload in v1 — wrap it in a type (SPEC §4.8)")
		default:
			c.errf(v.TypePos, "%s is not a type", v.Type)
		}
	}
	// dropped variants (duplicates, bad payloads) already errored; Max and
	// storage stay the DECLARED count so cascade diagnostics do not invent a
	// second wire shape — the unit is refused either way.
}

type scopeFrame struct {
	fields map[string]*ir.Field
}

func (c *checker) resolveBody(owner string, body *ast.Block, inTable bool) ([]*ir.Field, []ir.Item) {
	var out []*ir.Field
	names := map[string]ast.Pos{}
	var walk func(b *ast.Block, guard string, scopes []*scopeFrame) []ir.Item
	walk = func(b *ast.Block, guard string, scopes []*scopeFrame) []ir.Item {
		var items []ir.Item
		frame := scopes[len(scopes)-1]
		for _, item := range b.Items {
			switch item := item.(type) {
			case *ast.Field:
				if prev, dup := names[item.Name]; dup {
					c.errf(item.Pos, "duplicate field %s in %s (first at %s) — one name, one field, including across branch sides (SPEC §4.6)",
						item.Name, owner, prev)
					continue
				}
				names[item.Name] = item.Pos
				f := c.resolveField(owner, item, inTable)
				if f == nil {
					continue
				}
				f.Guard = guard
				out = append(out, f)
				frame.fields[item.Name] = f
				items = append(items, &ir.FieldItem{F: f})
			case *ast.IfItem:
				cond := c.lookupScope(scopes, item.Cond.Text)
				if cond == nil {
					c.errf(item.Cond.Pos, "if condition %s must be a bool field declared earlier in the same or an enclosing block (the dominance rule, SPEC §4.5)", item.Cond.Text)
				} else if cond.Type.Kind != ir.TBool || cond.Array != ir.ArrayNone {
					c.errf(item.Cond.Pos, "if condition %s must be a bool field (SPEC §4.6)", item.Cond.Text)
				}
				neg := ""
				if item.Neg {
					neg = "!"
				}
				g := "if " + neg + item.Cond.Text
				if guard != "" {
					g = guard + " / " + g
				}
				br := &ir.Branch{Neg: item.Neg, Cond: item.Cond.Text}
				br.Then = walk(item.Then, g, append(scopes, &scopeFrame{fields: map[string]*ir.Field{}}))
				if item.Else != nil {
					br.Else = walk(item.Else, g+" else", append(scopes, &scopeFrame{fields: map[string]*ir.Field{}}))
				}
				items = append(items, br)
			case *ast.ConstField:
				if inTable {
					c.errf(item.Pos, "const(value, bits) is a packet-wire construct — a table's wire is field-tagged TLV with no bit positions; remove it from table %s (SPEC-TABLES.md)", owner)
					continue
				}
				bits, ok := c.evalWidth(item.Bits, "const width")
				if !ok {
					continue
				}
				v, ok := c.evalInt(item.Value)
				if !ok {
					continue
				}
				if v.Sign() < 0 || v.BitLen() > int(bits) {
					c.errf(item.Pos, "const value %s does not fit %d bits (SPEC §4.6)", v, bits)
					continue
				}
				items = append(items, &ir.ConstItem{Value: v, Bits: bits}) // wire-only: no storage
			case *ast.ReservedItem:
				if inTable {
					c.errf(item.Pos, "reserved(bits) is a packet-wire construct — a table's wire is field-tagged TLV with no bit positions; remove it from table %s (SPEC-TABLES.md)", owner)
					continue
				}
				if bits, ok := c.evalWidth(item.Bits, "reserved width"); ok {
					items = append(items, &ir.ReservedItem{Bits: bits})
				}
			case *ast.AlignItem:
				if inTable {
					c.errf(item.Pos, "align is a packet-wire construct — a table's wire is field-tagged TLV with no bit positions; remove it from table %s (SPEC-TABLES.md)", owner)
					continue
				}
				items = append(items, &ir.AlignItem{})
			}
		}
		return items
	}
	items := walk(body, "", []*scopeFrame{{fields: map[string]*ir.Field{}}})
	return out, items
}

func (c *checker) lookupScope(scopes []*scopeFrame, name string) *ir.Field {
	for _, scope := range slices.Backward(scopes) {
		if f, ok := scope.fields[name]; ok {
			return f
		}
	}
	return nil
}

func (c *checker) evalWidth(e ast.Expr, what string) (int64, bool) {
	v, ok := c.evalInt(e)
	if !ok {
		return 0, false
	}
	if !v.IsInt64() || v.Int64() < 1 || v.Int64() > 64 {
		c.errf(e.ExprPos(), "%s %s outside [1, 64] (SPEC §4.6)", what, v)
		return 0, false
	}
	return v.Int64(), true
}

func (c *checker) resolveField(owner string, f *ast.Field, inTable bool) *ir.Field {
	out := &ir.Field{Name: f.Name}

	// scalar type
	switch f.Type.Kind {
	case ast.ScalarInt:
		out.Type = ir.FieldType{Kind: ir.TInt, Signed: f.Type.Signed, Width: f.Type.Width}
	case ast.ScalarBool:
		out.Type = ir.FieldType{Kind: ir.TBool}
	case ast.ScalarFloat32:
		out.Type = ir.FieldType{Kind: ir.TFloat32}
	case ast.ScalarFloat64:
		out.Type = ir.FieldType{Kind: ir.TFloat64}
	case ast.ScalarBits:
		w, ok := c.evalWidth(f.Type.Arg, "bits width")
		if !ok {
			return nil
		}
		out.Type = ir.FieldType{Kind: ir.TBits, Width: int(w)}
	case ast.ScalarFixed:
		// fixed(I, F) — SIGNED — and its unsigned sibling
		// ufixed(I, F): the storage is an integer of exactly I+F
		// bits; for fixed the sign bit counts toward I, for ufixed there is no
		// sign bit and the whole-unit domain is [0, 2^I). Both mirror
		// serialize_fixed's static_asserts (SPEC §4.3, §4.6) — I >= 1 is the
		// runtime's own unconditional requirement, unsigned included.
		spelling := "fixed"
		if !f.Type.Signed {
			spelling = "ufixed"
		}
		iv, ok1 := c.evalInt(f.Type.Arg)
		fv, ok2 := c.evalInt(f.Type.Arg2)
		if !ok1 || !ok2 {
			return nil
		}
		if !iv.IsInt64() || iv.Int64() < 1 {
			if f.Type.Signed {
				c.errf(f.Type.Pos, "fixed(%s, %s): at least one integer bit is required — the sign bit counts toward I (SPEC §4.6)", iv, fv)
			} else {
				c.errf(f.Type.Pos, "ufixed(%s, %s): at least one integer bit is required — the runtime's own floor, unsigned included (SPEC §4.6)", iv, fv)
			}
			return nil
		}
		if fv.Sign() < 0 || !fv.IsInt64() {
			c.errf(f.Type.Pos, "%s(%s, %s): fractional bits cannot be negative (SPEC §4.6)", spelling, iv, fv)
			return nil
		}
		total := iv.Int64() + fv.Int64()
		switch total {
		case 8, 16, 32, 64, 128:
		default:
			c.errf(f.Type.Pos, "%s(%s, %s): I + F = %d must equal a storage width — 8, 16, 32, 64 or 128 (SPEC §4.6)", spelling, iv, fv, total)
			return nil
		}
		out.Type = ir.FieldType{Kind: ir.TFixed, Signed: f.Type.Signed, Width: int(total),
			IntBits: int(iv.Int64()), FracBits: int(fv.Int64())}
	case ast.ScalarString, ast.ScalarBytes:
		n, ok := c.evalInt(f.Type.Arg)
		if !ok {
			return nil
		}
		minN := int64(1)
		what := "bytes"
		k := ir.TBytes
		if f.Type.Kind == ast.ScalarString {
			minN = 2
			what = "string"
			k = ir.TString
		}
		if !n.IsInt64() || n.Int64() < minN {
			c.errf(f.Type.Pos, "%s(%s): N below %d (SPEC §4.6)", what, n, minN)
			return nil
		}
		if n.Int64() > math.MaxInt32 {
			c.errf(f.Type.Pos, "%s(%s): N above %d — lengths live in int32 storage and the count's integer range is the bound (SPEC §4.3, §6.1)", what, n, math.MaxInt32)
			return nil
		}
		out.Type = ir.FieldType{Kind: k, Size: n.Int64(), SizeExpr: f.Type.Arg}
	case ast.ScalarNamed:
		d, ok := c.astDecls[f.Type.Name]
		if !ok {
			c.errf(f.Type.Pos, "undefined type %s", f.Type.Name)
			return nil
		}
		if f.Type.Pointer && !c.checkPointerSpelling(f, inTable, d) {
			return nil
		}
		switch d.(type) {
		case *ast.TypeDecl:
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.structs[f.Type.Name]}
		case *ast.TableDecl:
			if !inTable {
				c.errf(f.Type.Pos, "%s is a table, not a wire type — tables live on the TABLE wire and cannot ride in a `type`; declare the field's type with `type`, or move the declaring type to a `table` (SPEC-TABLES.md)", f.Type.Name)
				return nil
			}
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.tables[f.Type.Name], Pointer: f.Type.Pointer}
		case *ast.EnumDecl:
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.enums[f.Type.Name]}
		case *ast.FlagsDecl:
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.flagsD[f.Type.Name]}
		case *ast.UnionDecl:
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.unions[f.Type.Name]}
		default:
			c.errf(f.Type.Pos, "%s is not a type", f.Type.Name)
			return nil
		}
	}

	// the OPTIONAL prefix (SPEC-TABLES.md §2.3)
	if f.Type.Optional {
		if !c.checkOptionalSpelling(f, out, inTable) {
			return nil
		}
		out.Type.Optional = true
	}

	// array bound
	if f.Array != nil {
		switch out.Type.Kind {
		case ir.TString, ir.TBytes, ir.TBits:
			c.errf(f.Pos, "an array of %s is not supported in v1 — wrap the element in a type", scalarName(out.Type.Kind))
			return nil
		}
		// an ENUM-KEYED array: the bound NAMES a declared enum rather than
		// evaluating to a count — `ships [ShipType]ShipConfig`, one slot per
		// variant, indexed by the variant (SPEC-TABLES.md §2.4)
		if !c.resolveKeyBound(f, out) {
			return nil
		}
		switch {
		case out.KeyEnum != "":
			// one slot per named variant, plus None's — which is never valid,
			// because None is the null key (SPEC-TABLES.md §2.4). The bound is
			// E.Max + 1, the same count `[E.Max + 1]T` resolves to, so the two
			// spellings share one projection and one protocol id.
			out.Array = ir.ArrayFixed
			out.ArrayBound = out.KeyEnumRef.Max + 1
			out.ArrayExpr = f.Array.Hi
		default:
			hi, ok := c.evalInt(f.Array.Hi)
			if !ok {
				return nil
			}
			if !hi.IsInt64() || hi.Int64() < 1 {
				c.errf(f.Pos, "array bound %s below 1 (SPEC §4.6)", hi)
				return nil
			}
			if hi.Int64() > math.MaxInt32 {
				c.errf(f.Pos, "array bound %s above %d — counts live in int32 storage (SPEC §4.3, §6.1)", hi, math.MaxInt32)
				return nil
			}
			out.ArrayBound = hi.Int64()
			out.ArrayExpr = f.Array.Hi
			switch f.Array.Kind {
			case ast.ArrayFixed:
				out.Array = ir.ArrayFixed
			case ast.ArrayUpTo:
				out.Array = ir.ArrayCounted
			case ast.ArrayRange:
				out.Array = ir.ArrayCounted
				lo, ok := c.evalInt(f.Array.Lo)
				if !ok {
					return nil
				}
				if lo.Sign() < 0 || !lo.IsInt64() || lo.Int64() >= hi.Int64() {
					c.errf(f.Pos, "array count range [%s..%s] requires 0 <= Min < N (SPEC §4.6)", lo, hi)
					return nil
				}
				out.ArrayMin = lo.Int64()
			}
		}
	}

	c.resolveAttrs(f, out)

	if out.WasName != "" && !inTable {
		c.errf(f.Pos, "field %s: was is a table-wire concept — it aliases a renamed field's wire id, and only table fields have wire ids; a `type`'s wire is positional, so a rename there moves no bit (SPEC-TABLES.md)", f.Name)
		return nil
	}
	// `json` outside a table CLOSURE is refused in checkTables, where the
	// closure is known: a `type` a table reaches has a text form and may
	// carry the attribute, and only membership decides it.

	// the fixed and 128-bit families mirror serialize's own surface exactly
	// (SPEC §4.3, runtime-first): fixed(I, F) and int128 are RANGED — the
	// bounds are part of the wire format — and uint128 is the raw field.
	// A field whose declared bounds failed above was already diagnosed, so
	// the requirement error fires only when no bounds were attempted at all.
	attempted := false
	for _, a := range f.Attrs {
		if a.Key == "min" || a.Key == "max" {
			attempted = true
		}
	}
	if !attempted && out.Type.Kind == ir.TFixed && !out.HasIntRange {
		c.errf(f.Pos, "field %s: %s(%d, %d) requires | min = A, max = B — the whole-unit bounds are part of the wire format, exactly like a ranged integer's (SPEC §4.3)",
			f.Name, fixedSpelling(out.Type.Signed), out.Type.IntBits, out.Type.FracBits)
		return nil
	}
	if !attempted && out.Type.Kind == ir.TInt && out.Type.Width == 128 && out.Type.Signed && !out.HasIntRange {
		c.errf(f.Pos, "field %s: int128 requires | min = A, max = B — serialize_int128 is the only ranged 128-bit operation; a raw 128-bit field is uint128 (SPEC §4.3)",
			f.Name)
		return nil
	}
	if out.Type.Kind == ir.TFixed && !out.HasIntRange {
		return nil // bounds attempted and rejected above — already diagnosed
	}
	if out.Type.Kind == ir.TInt && out.Type.Width == 128 && out.Type.Signed && !out.HasIntRange {
		return nil // bounds attempted and rejected above — already diagnosed
	}

	c.resolveDefault(f, out)
	return out
}

// resolveDefault validates an optional specified default: zero initialization
// for all types in all generated languages unless a specified default
// overrides it (SPEC §5).
func (c *checker) resolveDefault(f *ast.Field, out *ir.Field) {
	if f.Default == nil {
		return
	}
	if out.Array != ir.ArrayNone {
		c.errf(f.Default.ExprPos(), "field %s: an array takes no specified default — elements zero-initialize", f.Name)
		return
	}
	out.DefExpr = f.Default
	switch out.Type.Kind {
	case ir.TBool:
		id, ok := f.Default.(*ast.IdentExpr)
		if !ok || (id.Name != "true" && id.Name != "false") {
			c.errf(f.Default.ExprPos(), "field %s: a bool default is true or false", f.Name)
			return
		}
		out.HasDefault = true
		out.DefBool = id.Name == "true"
	case ir.TInt, ir.TBits:
		v, ok := c.evalInt(f.Default)
		if !ok {
			return
		}
		if out.Type.Kind == ir.TInt && !fitsStorage(v, intTypeName(out.Type.Signed, out.Type.Width)) {
			c.errf(f.Default.ExprPos(), "field %s: default %s does not fit %s", f.Name, v, intTypeName(out.Type.Signed, out.Type.Width))
			return
		}
		if out.Type.Kind == ir.TBits {
			lim := new(big.Int).Lsh(big.NewInt(1), uint(out.Type.Width))
			if v.Sign() < 0 || v.Cmp(lim) >= 0 {
				c.errf(f.Default.ExprPos(), "field %s: default %s does not fit bits(%d)", f.Name, v, out.Type.Width)
				return
			}
		}
		if out.HasIntRange && (v.Cmp(out.IntMin) < 0 || v.Cmp(out.IntMax) > 0) {
			c.errf(f.Default.ExprPos(), "field %s: default %s is outside its range [%s, %s]", f.Name, v, out.IntMin, out.IntMax)
			return
		}
		out.HasDefault = true
		out.DefInt = v
	case ir.TFloat32, ir.TFloat64:
		v, ok := c.evalFloat(f.Default)
		if !ok {
			return
		}
		out.HasDefault = true
		out.DefFloat = v
	case ir.TFixed:
		// the quaternion identity (w = 1.0) is
		// the real case. A fixed default is declared in WHOLE UNITS — the same
		// domain as the | min, max bounds — and must scale EXACTLY: v * 2^F an
		// integer, so no rounding rule is ever involved in a default.
		v, ok := c.evalFloat(f.Default)
		if !ok {
			return
		}
		scaled := new(big.Float).SetPrec(256).SetFloat64(v)
		scaled.SetMantExp(scaled, out.Type.FracBits) // scaled = v * 2^F (SetMantExp uses its mant argument as-is)
		raw, acc := scaled.Int(nil)
		if acc != big.Exact {
			c.errf(f.Default.ExprPos(), "field %s: default %g is not exactly representable in %s — a fixed default must scale to an integer with no rounding (units × 2^%d)", f.Name, v, qFormatName(out.Type), out.Type.FracBits)
			return
		}
		if out.HasIntRange {
			minUnits := new(big.Float).SetInt(out.IntMin)
			maxUnits := new(big.Float).SetInt(out.IntMax)
			vf := new(big.Float).SetFloat64(v)
			if vf.Cmp(minUnits) < 0 || vf.Cmp(maxUnits) > 0 {
				c.errf(f.Default.ExprPos(), "field %s: default %g is outside its range [%s, %s] (whole units)", f.Name, v, out.IntMin, out.IntMax)
				return
			}
		}
		out.HasDefault = true
		out.DefInt = raw  // the RAW scaled integer — what storage initializes to
		out.DefFloat = v  // the whole-unit value, for comments
		out.DefExpr = nil // never render the units expression as the raw initializer
	case ir.TNamed:
		if en, isEnum := out.Type.Ref.(*ir.Enum); isEnum {
			id, ok := f.Default.(*ast.IdentExpr)
			if !ok || (id.Name != "None" && !contains(en.Variants, id.Name)) {
				c.errf(f.Default.ExprPos(), "field %s: an enum default names a variant of %s", f.Name, en.Name)
				return
			}
			out.HasDefault = true
			out.DefVariant = id.Name
			return
		}
		c.errf(f.Default.ExprPos(), "field %s: defaults in v1 cover bool, integer, float and enum fields", f.Name)
	default:
		c.errf(f.Default.ExprPos(), "field %s: defaults in v1 cover bool, integer, float and enum fields", f.Name)
	}
}

func scalarName(k ir.FieldTypeKind) string {
	switch k {
	case ir.TString:
		return "string(N)"
	case ir.TBytes:
		return "bytes(N)"
	case ir.TBits:
		return "bits(N)"
	}
	return "?"
}

func (c *checker) resolveAttrs(f *ast.Field, out *ir.Field) {
	byKey := map[string]*ast.Attr{}
	for i := range f.Attrs {
		a := &f.Attrs[i]
		if _, dup := byKey[a.Key]; dup {
			c.errf(a.Pos, "attribute %s repeated (SPEC §4.6)", a.Key)
			continue
		}
		// A valued attribute written bare — ` | min, max` for
		// ` | min = 0, max = 10` — used to reach evalInt with a nil
		// expression and panic the compiler. Reject it here, once, for every
		// valued attribute, rather than nil-checking at each use: a typo in a
		// schema must produce a diagnostic, never a stack trace.
		if a.Value == nil && valuedAttr[a.Key] {
			c.errf(a.Pos, "attribute %s requires a value, as %s = ... (SPEC §4.6)", a.Key, a.Key)
			continue
		}
		byKey[a.Key] = a
	}

	for i := range f.Attrs { // declaration order, so diagnostics are deterministic
		a := &f.Attrs[i]
		if byKey[a.Key] != a {
			continue // a repeated key, already reported above
		}
		switch a.Key {
		case "min", "max", "resolution":
			// validated below
		case "was":
			// rename aliasing (SPEC-TABLES.md): the field's TABLE-wire id
			// stays the hash of the OLD name, so identity survives the
			// rename. Table fields only — enforced by resolveField, which
			// knows the owner's kind.
			lit, ok := a.Value.(*ast.StringLit)
			if !ok {
				c.errf(a.Pos, `was takes the field's old name as a quoted string, e.g. was = "velocity" (SPEC-TABLES.md)`)
				continue
			}
			if lit.Value == "" {
				c.errf(a.Pos, "was = \"\" names nothing — was records the field's old name after a rename (SPEC-TABLES.md)")
				continue
			}
			if lit.Value == f.Name {
				c.errf(a.Pos, "field %s: was = %q names the field's own current name — was records the OLD name after a rename; drop the attribute until one happens (SPEC-TABLES.md)", f.Name, lit.Value)
				continue
			}
			out.WasName = lit.Value
		case "json":
			// the text form's key (SPEC-TABLES.md §16.4): the one attribute
			// the JSON walk adds, so a declaration can meet an existing text.
			// Table fields only — enforced below, where the owner's kind is
			// known — and it moves no wire byte.
			lit, ok := a.Value.(*ast.StringLit)
			if !ok {
				c.errf(a.Pos, `json takes the field's text key as a quoted string, e.g. json = "type" (SPEC-TABLES.md §16.4)`)
				continue
			}
			if lit.Value == "" {
				c.errf(a.Pos, "json = \"\" names nothing — json records the key this field reads and writes in the text form (SPEC-TABLES.md §16.4)")
				continue
			}
			out.JsonKey = lit.Value
		case "round":
			// refused by name: rounding is not an attribute — it is the one
			// fixed-point rule, half away from zero, everywhere (SPEC §4.3)
			c.errf(a.Pos, "round is not part of the language — rounding is the one fixed-point rule: half away from zero, everywhere (SPEC §4.3)")
		default:
			c.errf(a.Pos, "unknown attribute %q — the vocabulary is typed and closed per compiler version (SPEC §4.2)", a.Key)
		}
	}

	hasMin, hasMax := byKey["min"] != nil, byKey["max"] != nil
	hasRes := byKey["resolution"] != nil

	isInt := out.Type.Kind == ir.TInt
	isFixed := out.Type.Kind == ir.TFixed
	isFloat := out.Type.Kind == ir.TFloat32 || out.Type.Kind == ir.TFloat64

	if hasRes || (isFloat && (hasMin || hasMax)) {
		// the compressed-float triple (SPEC §4.3) / ranged-int projection (§4.8 rule 4)
		if !isFloat {
			c.errf(byKey["resolution"].Pos, "resolution applies to float fields (SPEC §4.6)")
			return
		}
		if out.Type.Kind == ir.TFloat64 {
			c.errf(f.Pos, "field %s: the compressed float is float32 (SPEC §4.3)", f.Name)
			return
		}
		if !hasMin || !hasMax || !hasRes {
			c.errf(f.Pos, "field %s: a float range is min, max and resolution, all three together (SPEC §4.6)", f.Name)
			return
		}
		fmin, ok1 := c.evalFloat(byKey["min"].Value)
		fmax, ok2 := c.evalFloat(byKey["max"].Value)
		res, ok3 := c.evalFloat(byKey["resolution"].Value)
		if !ok1 || !ok2 || !ok3 {
			return
		}
		// Non-finite triple parameters are rejected HERE, by name, before any
		// derived check can trip over them 		// send NaN or INF or anything else through compressed float is
		// non-conforming and should assert out on write too" — the runtimes
		// carry the write asserts; the compiler's half is refusing the
		// declaration). Both levels matter: non-finite at float64, and finite
		// at float64 but infinite at FLOAT32, where every runtime evaluates
		// the triple — before this check | min = -1e39, max = 1e39,
		// resolution = 1e30 compiled, and the C++ emitter printed -Inf.0f,
		// a token no C++ compiler accepts (SPEC §4.6, the non-finite rule).
		for _, p := range [3]struct {
			key string
			v   float64
		}{{"min", fmin}, {"max", fmax}, {"resolution", res}} {
			if math.IsInf(p.v, 0) || math.IsNaN(p.v) {
				c.errf(byKey[p.key].Pos, "field %s: %s = %g is not finite — NaN and infinity are non-conforming through a compressed float (SPEC §4.6)", f.Name, p.key, p.v)
				return
			}
			if math.IsInf(float64(float32(p.v)), 0) {
				c.errf(byKey[p.key].Pos, "field %s: %s = %g overflows float32, where every runtime evaluates the triple — a non-finite compressed-float parameter is non-conforming (SPEC §4.6)", f.Name, p.key, p.v)
				return
			}
		}
		if res <= 0 {
			c.errf(byKey["resolution"].Pos, "resolution %g must be positive (SPEC §4.6)", res)
			return
		}
		if fmin >= fmax {
			c.errf(byKey["min"].Pos, "degenerate float range [%g, %g] — min must be below max (SPEC §4.6)", fmin, fmax)
			return
		}
		// every runtime narrows the triple to FLOAT32 at the call; a triple
		// that only survives in float64 generates code that throws/panics
		// unconditionally — including on the hostile-read path
		if float32(res) <= 0 {
			c.errf(byKey["resolution"].Pos, "resolution %g collapses to zero at float32, where every runtime evaluates it (SPEC §4.6)", res)
			return
		}
		if float32(fmin) >= float32(fmax) {
			c.errf(byKey["min"].Pos, "float range [%g, %g] is degenerate at float32, where every runtime evaluates it (SPEC §4.6)", fmin, fmax)
			return
		}
		steps := math.Ceil((fmax - fmin) / res)
		if math.IsNaN(steps) || steps < 1 || steps > 4294967295 {
			c.errf(byKey["resolution"].Pos,
				"field %s: step count ceil((%g - %g) / %g) is outside the 32-bit wire range [1, 4294967295] (SPEC §4.3)",
				f.Name, fmax, fmin, res)
			return
		}
		out.HasFloatRange = true
		out.FMin, out.FMax, out.Resolution = fmin, fmax, res
		out.Steps = int64(steps)
		// `nearest` is a FROZEN projection token: every compressed-float
		// line renders `round=nearest`, and keeping the assignment keeps
		// every compressed-float unit's id stable. Changing it is a
		// ProjectionVersion bump, taken deliberately or not at all.
		out.Round = "nearest"
		return
	}

	if hasMin != hasMax {
		c.errf(f.Pos, "field %s: min without max (or vice versa) is a compile error (SPEC §4.6)", f.Name)
		return
	}
	if hasMin && hasMax {
		if !isInt && !isFixed {
			if out.Type.Kind == ir.TNamed {
				c.errf(byKey["min"].Pos, "min/max are not valid on %s — a field that indexes a declared set derives its range from the set (SPEC §4.2)", out.Type.Name)
			} else {
				c.errf(byKey["min"].Pos, "min/max apply to integer fields (SPEC §4.6)")
			}
			return
		}
		if isInt && out.Type.Width == 128 && !out.Type.Signed {
			// serialize's own surface: serialize_uint128 is the RAW 128-bit
			// field; the only ranged 128-bit operation is serialize_int128
			c.errf(byKey["min"].Pos, "min/max are not valid on uint128 — it is the raw 128-bit field, always 128 bits on the wire; a ranged 128-bit integer is int128 (SPEC §4.3)")
			return
		}
		vmin, ok1 := c.evalInt(byKey["min"].Value)
		vmax, ok2 := c.evalInt(byKey["max"].Value)
		if !ok1 || !ok2 {
			return
		}
		// min == max is LEGAL: a degenerate
		// range costs zero bits and the reader recovers the value from the
		// range alone, the same rule STANDARD.md has always stated for
		// ranged integers and the empty enum already exercises. Only an
		// INVERTED range is an error.
		if vmin.Cmp(vmax) > 0 {
			c.errf(byKey["min"].Pos, "inverted range [%s, %s] — min must not exceed max (SPEC §4.6)", vmin, vmax)
			return
		}
		if isFixed {
			// the bounds are WHOLE UNITS and must be representable in the Q
			// format — Q I.F with the sign bit in I for fixed, [0, 2^I) for
			// ufixed — and in int64, where the runtime's compile-time bound
			// parameters live (serialize_fixed's static_asserts, restated as
			// language rules — SPEC §4.6)
			lo, hi := fixedUnitBounds(out.Type.Signed, out.Type.IntBits)
			if vmin.Cmp(lo) < 0 || vmax.Cmp(hi) > 0 {
				c.errf(f.Pos, "field %s: bounds [%s, %s] whole units do not fit %s(%d, %d) — %s holds [%s, %s] (SPEC §4.6)",
					f.Name, vmin, vmax, fixedSpelling(out.Type.Signed), out.Type.IntBits, out.Type.FracBits, qFormatName(out.Type), lo, hi)
				return
			}
		} else {
			lo, hi := storageBounds(out.Type.Signed, out.Type.Width)
			if vmin.Cmp(lo) < 0 || vmax.Cmp(hi) > 0 {
				c.errf(f.Pos, "field %s: range [%s, %s] does not fit its declared storage %s (SPEC §4.6 — a legal wire value the storage truncates would be silent corruption)",
					f.Name, vmin, vmax, intTypeName(out.Type.Signed, out.Type.Width))
				return
			}
		}
		out.HasIntRange = true
		out.IntMin, out.IntMax = vmin, vmax
		out.IntMinExpr, out.IntMaxExpr = byKey["min"].Value, byKey["max"].Value
	}
}

// fixedSpelling names the source spelling of a fixed-point type: the sign is
// part of the name, the integer family's own int/uint precedent (SPEC §4.3).
func fixedSpelling(signed bool) string {
	if signed {
		return "fixed"
	}
	return "ufixed"
}

// qFormatName is the Q-notation twin of fixedSpelling: Q I.F signed,
// UQ I.F unsigned.
func qFormatName(t ir.FieldType) string {
	if t.Signed {
		return fmt.Sprintf("Q%d.%d", t.IntBits, t.FracBits)
	}
	return fmt.Sprintf("UQ%d.%d", t.IntBits, t.FracBits)
}

// fixedUnitBounds is the whole-unit domain of a Q I.F format — signed
// [-2^(I-1), 2^(I-1) - 1] (the sign bit counts toward I), unsigned
// [0, 2^I - 1] — clamped to int64, where serialize_fixed's compile-time
// MinUnits/MaxUnits parameters live in every runtime (SPEC §4.6).
func fixedUnitBounds(signed bool, intBits int) (*big.Int, *big.Int) {
	lo, hi := storageBounds(signed, intBits)
	i64lo, i64hi := storageBounds(true, 64)
	if lo.Cmp(i64lo) < 0 {
		lo = i64lo
	}
	if hi.Cmp(i64hi) > 0 {
		hi = i64hi
	}
	return lo, hi
}

func intTypeName(signed bool, width int) string {
	if signed {
		return fmt.Sprintf("int%d", width)
	}
	return fmt.Sprintf("uint%d", width)
}

func storageBounds(signed bool, width int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(width-1))
		lo := new(big.Int).Neg(hi)
		return lo, new(big.Int).Sub(hi, one)
	}
	hi := new(big.Int).Lsh(one, uint(width))
	return big.NewInt(0), new(big.Int).Sub(hi, one)
}

func fitsStorage(v *big.Int, storage string) bool {
	signed := strings.HasPrefix(storage, "int")
	width := 64
	_, _ = fmt.Sscanf(strings.TrimPrefix(strings.TrimPrefix(storage, "uint"), "int"), "%d", &width) // no digits: width stays 64
	lo, hi := storageBounds(signed, width)
	return v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}

// ---- composition cycles (SPEC §4.6) ----

func (c *checker) checkCycles() {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var path []string
	var visit func(name string) bool
	visit = func(name string) bool {
		switch color[name] {
		case grey:
			c.errs = append(c.errs, fmt.Errorf("type composition cycle: %s -> %s (SPEC §4.6)",
				strings.Join(path, " -> "), name))
			return false
		case black:
			return true
		}
		color[name] = grey
		path = append(path, name)
		if st := c.tables[name]; st != nil {
			// tables join the composition graph exactly as types do: nesting
			// is by value, so a table holding itself — directly or through a
			// chain — has infinite size (SPEC-TABLES.md, the §4.6 rule).
			// POINTER edges are exempt and carry no size: `next *Node` inside
			// Node is finite, and recursion through pointers is the whole
			// point of the freedom tables were given.
			for _, f := range st.Fields {
				if f.Type.Pointer {
					continue
				}
				if f.Type.Kind == ir.TNamed {
					switch f.Type.Ref.(type) {
					case *ir.Struct, *ir.Union:
						if !visit(f.Type.Name) {
							break
						}
					}
				}
			}
		}
		if st := c.structs[name]; st != nil {
			for _, f := range st.Fields {
				if f.Type.Kind == ir.TNamed {
					// unions join the composition graph: a payload holding
					// its own union has infinite size (SPEC §4.8)
					switch f.Type.Ref.(type) {
					case *ir.Struct, *ir.Union:
						if !visit(f.Type.Name) {
							break
						}
					}
				}
			}
		}
		if un := c.unions[name]; un != nil {
			for _, v := range un.Variants {
				if !visit(v.Type) {
					break
				}
			}
		}
		path = path[:len(path)-1]
		color[name] = black
		return true
	}
	names := make([]string, 0, len(c.structs)+len(c.unions)+len(c.tables))
	for n := range c.structs {
		names = append(names, n)
	}
	for n := range c.unions {
		names = append(names, n)
	}
	for n := range c.tables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		visit(n)
	}
}

// ---- tables (SPEC-TABLES.md) ----

// closureMember resolves a closure name to its resolved struct — a table or
// a plain type.
func (c *checker) closureMember(name string) *ir.Struct {
	if st := c.tables[name]; st != nil {
		return st
	}
	return c.structs[name]
}

// checkTables enforces the table closure's wire capability and the field-id
// uniqueness the TABLE wire's identity scheme requires (SPEC-TABLES.md).
//
// Capability: a `table` and everything it references, transitively, must stay
// on table-wire kinds — plain fixed-width scalars, length-prefixed
// strings/bytes/tables, the neutral encoding a third party could implement
// from a one-page description. int128/uint128 and fixed(I, F) have no
// table-wire kind, string/bytes/array extents ride in uint16, and an array
// of unions is a named follow-on — each is refused HERE, loudly, instead of
// surprising a generated reader later.
//
// Identity: a field's wire id is fold16(fnv1a32(name)) — of the `was` alias
// where one is declared — and two fields of one closure member whose
// effective ids collide would be indistinguishable on the wire, so the
// collision is a compile error.
func (c *checker) checkTables() {
	closure := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if closure[name] {
			return
		}
		st := c.closureMember(name)
		if st == nil {
			return
		}
		closure[name] = true
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				walk(ref.Name)
			case *ir.Union:
				for _, v := range ref.Variants {
					walk(v.Type)
				}
			}
		}
	}
	// SORTED: map iteration order must not shuffle the diagnostics run to run
	roots := make([]string, 0, len(c.tables))
	for name := range c.tables {
		roots = append(roots, name)
	}
	sort.Strings(roots)
	for _, name := range roots {
		walk(name)
	}
	c.tableClosure = closure

	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := c.closureMember(name)
		if st == nil {
			continue
		}
		pos := ast.Pos{}
		if d, ok := c.astDecls[name]; ok {
			pos = d.DeclPos()
		}
		what := "type"
		if st.IsTable {
			what = "table"
		}
		if st.IsTable && slices.Contains(tableBuilderMembers, name) {
			c.errf(pos, "table %s: the name collides with a member of the generated %sBuilder — a member function hides the type name it shares, and the header would not compile; rename the table (SPEC-TABLES.md §6.2)",
				name, name)
		}
		// the TEXT form's keys (SPEC-TABLES.md §16.4): two fields of one
		// closure member whose keys collide are indistinguishable in a JSON
		// object, exactly as colliding ids are on the wire — refused once,
		// naming both, whether the collision comes from a `json` attribute
		// or from an attribute meeting a plain field name.
		// The BLOCK FORM's generated PROLOGUE (SPEC-TABLES.md §19.1): every
		// fixed table's block projection opens with `magic`,
		// `build_version` and `byte_order`, generated exactly as an optional's `_present`
		// companion is — so a field may not be named after either half. The
		// claim is on EVERY table, not only the ones that have the form
		// today: a table gains and loses the form as its closure gains and
		// loses a pointer, and a name that was free yesterday must not become
		// a collision tomorrow (§11).
		if st.IsTable {
			for _, f := range st.Fields {
				if f.Name == "magic" || f.Name == "build_version" || f.Name == "byte_order" {
					c.errf(pos, "table %s: field %s collides with the block form's generated prologue — `magic`, `build_version` and `byte_order` open every block projection, as `<field>_present` is generated beside an optional's value; rename the field (SPEC-TABLES.md §19.1, §11)",
						name, f.Name)
				}
			}
		}

		seenKey := map[string]*ir.Field{}
		for _, f := range st.Fields {
			key := ir.TableFieldJsonKey(f)
			if prev, dup := seenKey[key]; dup {
				c.errf(pos, "%s %s: fields %s and %s collide on the JSON key %q — rename one, or give one a different json key (SPEC-TABLES.md §16.4)",
					what, name, describeTableJsonField(prev), describeTableJsonField(f), key)
				continue
			}
			seenKey[key] = f
		}

		seen := map[uint16]*ir.Field{}
		for _, f := range st.Fields {
			if f.Type.Pointer {
				// a pointer carries no extent of its own: the checks below
				// bound STORAGE, and a pointer's storage is one relocatable
				// u32 slot. Identity still applies — the id check runs below.
				id := ir.TableFieldId(f)
				if prev, dup := seen[id]; dup {
					c.errf(pos, "%s %s: fields %s and %s collide on table-wire id 0x%04x — rename one (SPEC-TABLES.md)",
						what, name, describeTableField(prev), describeTableField(f), id)
					continue
				}
				seen[id] = f
				continue
			}
			var bad string
			switch {
			case f.Type.Kind == ir.TInt && f.Type.Width == 128:
				bad = "int128/uint128"
			case f.Type.Kind == ir.TFixed:
				bad = fixedSpelling(f.Type.Signed) + "(I, F)"
			}
			if bad != "" {
				c.errf(pos, "%s.%s: %s has no table-wire kind, and %s %s is in a table's closure — a `table` and everything it references must stay on table-wire kinds (SPEC-TABLES.md)",
					name, f.Name, bad, what, name)
				continue
			}
			if f.Type.Kind == ir.TNamed {
				if _, isUnion := f.Type.Ref.(*ir.Union); isUnion && f.Array != ir.ArrayNone {
					// a SCALAR union field rides the table wire as kUnion:
					// a u16 arm id, then the selected arm length-prefixed —
					// skippable, elidable (None), kind-mismatch-safe. An
					// ARRAY of unions is the remaining named follow-on.
					c.errf(pos, "%s.%s: an array of unions may not sit on a table-closure path yet — wrap the union in a type, or ask for the pass (SPEC-TABLES.md)",
						name, f.Name)
					continue
				}
			}
			id := ir.TableFieldId(f)
			if prev, dup := seen[id]; dup {
				c.errf(pos, "%s %s: fields %s and %s collide on table-wire id 0x%04x — rename one (SPEC-TABLES.md)",
					what, name, describeTableField(prev), describeTableField(f), id)
				continue
			}
			seen[id] = f
		}
	}
	c.checkTableVariantIdentity(names)
	c.checkJsonKeysInClosure()
}

// checkJsonKeysInClosure refuses `json = "key"` on a field no table closure
// reaches (SPEC-TABLES.md §16.4). The text form is the table closure's — a
// `type` a table nests has one and may carry the attribute; a type nothing in
// a closure reaches has no text form for a key to name.
func (c *checker) checkJsonKeysInClosure() {
	for _, name := range sortedKeys(c.structs) {
		if c.tableClosure[name] {
			continue
		}
		st := c.structs[name]
		pos := ast.Pos{}
		if d, ok := c.astDecls[name]; ok {
			pos = d.DeclPos()
		}
		for _, f := range st.Fields {
			if f.JsonKey != "" {
				c.errf(pos, "type %s: field %s carries json = %q, but no table reaches %s — the text form is the table closure's, and a type outside one has none (SPEC-TABLES.md §16.4)",
					name, f.Name, f.JsonKey, name)
			}
		}
	}
}

// sortedKeys gives a map's keys in a stable order, so diagnostics do not
// shuffle run to run.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkTableFileDag refuses a cross-file reference CYCLE among a unit's table
// closure (SPEC-TABLES.md §11): if a declaration in file A reaches one in file
// B, nothing in B may reach back into A.
//
// The rule is LANGUAGE-NEUTRAL and so it lives here, in the front end, not in
// a backend. C++ makes the consequence concrete — the generated <A>Table.h and
// <B>Table.h would have to include each other and neither could compile — but
// a unit that is legal under one target and illegal under another is the trap
// the rule exists to prevent, so every target refuses the same units.
//
// Same-file recursion is untouched: only edges that LEAVE a file are graphed,
// so a pointer chain inside one file is as legal as it ever was, and a
// by-value cycle is already refused as a composition cycle.
func (c *checker) checkTableFileDag() {
	if len(c.tables) == 0 {
		return
	}
	// SORTED throughout: map iteration order must not shuffle which cycle a
	// multi-cycle unit reports, run to run.
	deps := map[string]map[string]bool{}
	// the declaration that first created each cross-file edge, so the cycle
	// reports AT a source line the author can act on rather than at the unit
	type edgeOrigin struct {
		decl string
		pos  ast.Pos
	}
	edgeFrom := map[string]edgeOrigin{}
	closureNames := make([]string, 0, len(c.tableClosure))
	for name := range c.tableClosure {
		closureNames = append(closureNames, name)
	}
	sort.Strings(closureNames)
	for _, name := range closureNames {
		st := c.closureMember(name)
		base, known := c.declFile[name]
		if st == nil || !known {
			continue
		}
		if deps[base] == nil {
			deps[base] = map[string]bool{}
		}
		note := func(target string) {
			to, ok := c.declFile[target]
			if !ok || to == base {
				return
			}
			deps[base][to] = true
			key := base + " -> " + to
			if _, seen := edgeFrom[key]; !seen {
				pos := ast.Pos{}
				if d, known := c.astDecls[name]; known {
					pos = d.DeclPos()
				}
				edgeFrom[key] = edgeOrigin{decl: name, pos: pos}
			}
		}
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			note(f.Type.Name)
			if un, isUnion := f.Type.Ref.(*ir.Union); isUnion {
				for _, v := range un.Variants {
					note(v.Type)
				}
			}
		}
	}

	const (
		unvisited = 0
		onPath    = 1
		done      = 2
	)
	color := map[string]int{}
	var path []string
	var visit func(base string) bool
	visit = func(base string) bool {
		switch color[base] {
		case onPath:
			// name the whole cycle, from where it re-enters
			at := 0
			for i, p := range path {
				if p == base {
					at = i
					break
				}
			}
			closing := edgeFrom[path[len(path)-1]+" -> "+base]
			c.errf(closing.pos, "%s closes a cross-file table reference cycle: %s -> %s — a unit's table files form a DAG by reference, so if a declaration in one file reaches one in another, nothing there may reach back; move a declaration so the cross-file graph is acyclic (SPEC-TABLES.md §11)",
				closing.decl, strings.Join(path[at:], " -> "), base)
			return false
		case done:
			return true
		}
		color[base] = onPath
		path = append(path, base)
		targets := make([]string, 0, len(deps[base]))
		for t := range deps[base] {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			if !visit(t) {
				return false
			}
		}
		path = path[:len(path)-1]
		color[base] = done
		return true
	}
	bases := make([]string, 0, len(deps))
	for b := range deps {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	for _, b := range bases {
		if !visit(b) {
			return // one cycle named is enough; the fix changes the graph
		}
	}
}

// checkTableVariantIdentity enforces the table wire's variant identity
// (SPEC-TABLES.md §5): an enum value rides as the name hash of its variant and
// a union body opens with the name hash of its arm, so the ids within one
// enum, and within one union, must be distinct — and a value with no name has
// no identity to ride under.
//
// Scoped to the TABLE CLOSURE. The packet wire identifies a variant by its
// ordinal and is untouched by any of this: an enum nothing in a table reaches
// keeps every spelling it ever had.
func (c *checker) checkTableVariantIdentity(closureNames []string) {
	enums := map[string]*ir.Enum{}
	unions := map[string]*ir.Union{}
	// the field that pulled each vocabulary into the closure. The refusals
	// below exist BECAUSE of closure membership, so they name the edge that
	// created them — in a unit of many tables, "somewhere in a closure" is a
	// search the compiler can spare the user.
	reachedBy := map[string]string{}
	for _, name := range closureNames {
		st := c.closureMember(name)
		if st == nil {
			continue
		}
		what := "type"
		if st.IsTable {
			what = "table"
		}
		for _, f := range st.Fields {
			// an enum-keyed array reaches its KEY enum without naming it as a
			// field type, and the key rides under a variant hash exactly as a
			// value does (SPEC-TABLES.md §3.2) — so the key is a closure
			// vocabulary, and both §5 refusals are owed to it
			if f.KeyEnumRef != nil {
				if _, seen := enums[f.KeyEnum]; !seen {
					enums[f.KeyEnum] = f.KeyEnumRef
					reachedBy[f.KeyEnum] = fmt.Sprintf("%s %s's field %s, which keys an array by %s,", what, name, f.Name, f.KeyEnum)
				}
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			site := fmt.Sprintf("%s %s's field %s", what, name, f.Name)
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				if _, seen := enums[ref.Name]; !seen {
					enums[ref.Name] = ref
					reachedBy[ref.Name] = site
				}
			case *ir.Union:
				if _, seen := unions[ref.Name]; !seen {
					unions[ref.Name] = ref
					reachedBy[ref.Name] = site
				}
			}
		}
	}
	pos := func(name string) ast.Pos {
		if d, ok := c.astDecls[name]; ok {
			return d.DeclPos()
		}
		return ast.Pos{}
	}
	for _, name := range sortedKeys(enums) {
		e := enums[name]
		if e.Max > int64(len(e.Variants)) {
			c.errf(pos(name), "enum %s: | max = %d reserves values above the declared variants, and %s reaches it, putting %s in a table closure — a headroom value has no NAME, and a table-wire enum value rides as the hash of its variant name; the table wire needs no headroom, because a variant may be added anywhere (SPEC-TABLES.md §5)",
				name, e.Max, reachedBy[name], name)
		}
		seen := map[uint16]string{}
		for _, v := range e.Variants {
			id := ir.VariantId(v)
			if prev, dup := seen[id]; dup {
				c.errf(pos(name), "enum %s: variants %s and %s collide on table-wire id 0x%04x, and %s reaches it, putting %s in a table closure — rename one (SPEC-TABLES.md §5)",
					name, prev, v, id, reachedBy[name], name)
				continue
			}
			seen[id] = v
		}
	}
	for _, name := range sortedKeys(unions) {
		un := unions[name]
		seen := map[uint16]string{}
		for _, v := range un.Variants {
			id := ir.VariantId(v.Name)
			if prev, dup := seen[id]; dup {
				c.errf(pos(name), "union %s: arms %s and %s collide on table-wire id 0x%04x, and %s reaches it, putting %s in a table closure — rename one (SPEC-TABLES.md §5)",
					name, prev, v.Name, id, reachedBy[name], name)
				continue
			}
			seen[id] = v.Name
		}
	}
}

// checkPointerSpelling enforces the `*T` spelling's rules, each refused by
// name (SPEC-TABLES.md §11). The founding line: types remain VALUE semantics;
// tables ALLOW POINTER semantics — so a pointer is a table-to-table edge
// declared inside a table body, and nowhere else.
func (c *checker) checkPointerSpelling(f *ast.Field, inTable bool, d ast.Decl) bool {
	if !inTable {
		c.errf(f.Type.Pos, "field %s: *%s is a pointer, and pointers are a TABLE construct — types remain value semantics, tables allow pointer semantics; nest the field by value, or move the declaring type to a `table` (SPEC-TABLES.md)",
			f.Name, f.Type.Name)
		return false
	}
	if _, isTable := d.(*ast.TableDecl); !isTable {
		c.errf(f.Type.Pos, "field %s: *%s points at a %s, and a pointer may only target a `table` — %s is value-semantics data with no independent identity to point at; nest it by value, or declare %s as a table (SPEC-TABLES.md)",
			f.Name, f.Type.Name, declKindName(d), f.Type.Name, f.Type.Name)
		return false
	}
	if f.Array != nil {
		c.errf(f.Type.Pos, "field %s: an array of pointers is a named follow-on — declare a bounded array of tables by value, or a pointer to a table that holds the array (SPEC-TABLES.md §15)", f.Name)
		return false
	}
	if f.Default != nil {
		c.errf(f.Pos, "field %s: a pointer field takes no specified default — a fresh pointer is null, and null is the only value a default could name (SPEC-TABLES.md)", f.Name)
		return false
	}
	return true
}

// checkOptionalSpelling enforces the `?T` spelling's rules, each refused by
// name (SPEC-TABLES.md §11). An optional is a table-body construct: it costs
// one presence bool beside the value, and PRESENCE — not content — decides
// whether the field rides.
func (c *checker) checkOptionalSpelling(f *ast.Field, out *ir.Field, inTable bool) bool {
	spelling := "?" + scalarSpelling(f.Type)
	if !inTable {
		c.errf(f.Type.Pos, "field %s: %s is an OPTIONAL field, and optionals are a TABLE construct — a `type`'s wire is positional and every field always rides, so there is no absence to express; drop the ?, or move the declaring type to a `table` (SPEC-TABLES.md §2.3)",
			f.Name, spelling)
		return false
	}
	if f.Type.Pointer {
		c.errf(f.Type.Pos, "field %s: %s marks a pointer optional, and a pointer is ALREADY optional — null is its absence, and it rides exactly as an absent optional does; drop the ? (SPEC-TABLES.md §2.3)",
			f.Name, spelling)
		return false
	}
	if f.Array != nil {
		c.errf(f.Type.Pos, "field %s: ? on an ARRAY is a named follow-on — a counted array's count already carries emptiness, and a fixed array's slots are all present by construction; wrap the array in a table and make that optional (SPEC-TABLES.md §15)", f.Name)
		return false
	}
	switch out.Type.Kind {
	case ir.TString, ir.TBytes:
		c.errf(f.Type.Pos, "field %s: ? on %s is a named follow-on — the generated length companion already carries emptiness, and a second presence bit beside it would be two answers to one question; wrap it in a table and make that optional (SPEC-TABLES.md §15)",
			f.Name, scalarName(out.Type.Kind))
		return false
	}
	if _, isUnion := out.Type.Ref.(*ir.Union); isUnion {
		c.errf(f.Type.Pos, "field %s: %s marks a union optional, and a union is ALREADY optional — its None arm IS the absence, and an empty union elides exactly as an absent optional does; drop the ? (SPEC-TABLES.md §2.3)",
			f.Name, spelling)
		return false
	}
	if f.Default != nil {
		c.errf(f.Pos, "field %s: an optional field takes no specified default — PRESENCE is the only default an optional has, and an absent optional reads as absent with its value at the type's own zero (SPEC-TABLES.md §2.3)", f.Name)
		return false
	}
	return true
}

// resolveKeyBound recognises the ENUM-KEYED array bound — a `[Name]T` whose
// Name is a declared ENUM rather than a constant (SPEC-TABLES.md §2.4). The
// two spellings never overlap, because an enum is declared: `[Name]` naming a
// const is the fixed array it has always been, and `[Name]` naming an enum is
// one slot per variant, keyed by the variant. Returns false when the bound is
// refused.
func (c *checker) resolveKeyBound(f *ast.Field, out *ir.Field) bool {
	name, pos, ok := boundIdent(f.Array)
	if !ok {
		return true
	}
	switch c.astDecls[name].(type) {
	case *ast.FlagsDecl:
		c.errf(pos, "field %s: [%s] names a `flags` declaration, and a keyed array is keyed by a VARIANT — a mask holds any set of bits at once, so it names no single slot; key the array by an enum, or size it with a constant (SPEC-TABLES.md §2.4)",
			f.Name, name)
		return false
	case *ast.EnumDecl:
	default:
		return true // a const name, or an undefined one evalInt diagnoses
	}
	if f.Array.Kind != ast.ArrayFixed {
		c.errf(pos, "field %s: a bounded enum-keyed array is refused — [%s] is COMPLETE by construction, one slot per variant, so [..%s] and [A..%s] name a count that cannot vary; spell it [%s] (SPEC-TABLES.md §2.4)",
			f.Name, name, name, name, name)
		return false
	}
	en := c.enums[name]
	if en == nil {
		return true // the enum failed its own checks; it was already diagnosed
	}
	out.KeyEnum = name
	out.KeyEnumRef = en
	return true
}

// boundIdent reports the bare declaration name an array bound spells, if it
// spells one: `[E]`, and the two bounded forms whose either end names one, so
// `[..E]` is refused by name rather than by "undefined constant".
func boundIdent(b *ast.ArrayBound) (string, ast.Pos, bool) {
	for _, e := range []ast.Expr{b.Hi, b.Lo} {
		if id, isIdent := e.(*ast.IdentExpr); isIdent {
			return id.Name, id.Pos, true
		}
	}
	return "", ast.Pos{}, false
}

// scalarSpelling renders a field type as the author wrote it, for a
// diagnostic that quotes the spelling back.
func scalarSpelling(t ast.ScalarType) string {
	switch t.Kind {
	case ast.ScalarNamed:
		if t.Pointer {
			return "*" + t.Name
		}
		return t.Name
	case ast.ScalarBool:
		return "bool"
	case ast.ScalarFloat32:
		return "float32"
	case ast.ScalarFloat64:
		return "float64"
	case ast.ScalarInt:
		return intTypeName(t.Signed, t.Width)
	case ast.ScalarBits:
		return "bits(N)"
	case ast.ScalarString:
		return "string(N)"
	case ast.ScalarBytes:
		return "bytes(N)"
	}
	return "the field type"
}

// declKindName names a declaration's kind for a diagnostic.
func declKindName(d ast.Decl) string {
	switch d.(type) {
	case *ast.TypeDecl:
		return "type"
	case *ast.EnumDecl:
		return "enum"
	case *ast.FlagsDecl:
		return "flags"
	case *ast.UnionDecl:
		return "union"
	case *ast.TableDecl:
		return "table"
	}
	return "declaration"
}

// describeTableField names a field for the id-collision diagnostic, showing
// the was alias when that is where the colliding id comes from.
func describeTableField(f *ir.Field) string {
	if f.WasName != "" {
		return fmt.Sprintf("%s (was %q)", f.Name, f.WasName)
	}
	return f.Name
}

// describeTableJsonField names a field in a text-key diagnostic, showing the
// `json` attribute where one carries the key.
func describeTableJsonField(f *ir.Field) string {
	if f.JsonKey != "" {
		return fmt.Sprintf("%s (json %q)", f.Name, f.JsonKey)
	}
	return f.Name
}

// ---- target-name safety (SPEC §4.6) ----

// targetReserved is the union of the four target languages' reserved words.
// A declaration or field using one is rejected: no escaping machinery, rename
// at the source.
var targetReserved = func() map[string]string {
	m := map[string]string{}
	add := func(lang, words string) {
		for w := range strings.FieldsSeq(words) {
			if _, ok := m[w]; !ok {
				m[w] = lang
			}
		}
	}
	add("C++", `alignas alignof and and_eq asm auto bitand bitor bool break case catch char
		char8_t char16_t char32_t class compl concept const consteval constexpr constinit
		const_cast continue co_await co_return co_yield decltype default delete do double
		dynamic_cast else enum explicit export extern false float for friend goto if inline
		int long mutable namespace new noexcept not not_eq nullptr operator or or_eq private
		protected public register reinterpret_cast requires return short signed sizeof static
		static_assert static_cast struct switch template this thread_local throw true try
		typedef typeid typename union unsigned using virtual void volatile wchar_t while xor xor_eq`)
	add("C#", `abstract as base bool break byte case catch char checked class const continue
		decimal default delegate do double else enum event explicit extern false finally fixed
		float for foreach goto if implicit in int interface internal is lock long namespace
		new null object operator out override params private protected public readonly ref
		return sbyte sealed short sizeof stackalloc static string struct switch this throw
		true try typeof uint ulong unchecked unsafe ushort using virtual void volatile while`)
	add("Go", `break case chan const continue default defer else fallthrough for func go goto
		if import interface map package range return select struct switch type var`)
	add("Rust", `as async await break const continue crate dyn else enum extern false fn for
		if impl in let loop match mod move mut pub ref return self Self static struct super
		trait true type unsafe use where while`)
	return m
}()

// libcNamespaceScope is the C standard library's identifier set as a generated
// C++ translation unit sees it at namespace scope: the functions, types and
// object-like macros of the C11 library headers, which implementations also
// declare in the global namespace via the .h headers the runtime's own
// includes eventually pull in. A package named after one generates
// `namespace <name>` beside libc's declaration of the same name — proven with
// the compile fuzzer's specimen: clang rejects the generated `namespace exit`
// with "redefinition of 'exit' as different kind of symbol" (the compile
// fuzzer's run). Curated by walking the C11 header list, one block per
// header; additions are cheap — one word here. Omitted on purpose: function-like
// macros (assert, offsetof, va_arg, ...), which expand only before a '(' and
// cannot bite a namespace declaration; names that are target reserved words,
// refused by the check above this one; and the uppercase macro constants,
// which no schema ident collides with in practice — add any of them the day a
// specimen proves otherwise. Exact, case-sensitive match is correct: C++
// namespaces are case-sensitive.
var libcNamespaceScope = func() map[string]bool {
	m := map[string]bool{}
	for _, words := range []string{
		// <ctype.h>
		`isalnum isalpha isblank iscntrl isdigit isgraph islower isprint ispunct
		 isspace isupper isxdigit tolower toupper`,
		// <errno.h> — errno is an object-like macro, expands anywhere
		`errno`,
		// <fenv.h>
		`feclearexcept fegetexceptflag feraiseexcept fesetexceptflag fetestexcept
		 fegetround fesetround fegetenv feholdexcept fesetenv feupdateenv fenv_t
		 fexcept_t`,
		// <inttypes.h>
		`imaxabs imaxdiv strtoimax strtoumax wcstoimax wcstoumax imaxdiv_t`,
		// <locale.h>
		`localeconv setlocale lconv`,
		// <math.h> — base names; the f/l-suffixed variants join when a specimen asks
		`acos acosh asin asinh atan atan2 atanh cbrt ceil copysign cos cosh erf
		 erfc exp exp2 expm1 fabs fdim floor fma fmax fmin fmod frexp hypot ilogb
		 ldexp lgamma log log10 log1p log2 logb lrint llrint lround llround modf
		 nan nearbyint nextafter nexttoward pow remainder remquo rint round
		 scalbln scalbn sin sinh sqrt tan tanh tgamma trunc float_t double_t`,
		// <setjmp.h> — setjmp itself is a function-like macro
		`longjmp jmp_buf`,
		// <signal.h>
		`signal raise sig_atomic_t`,
		// <stdarg.h>
		`va_list`,
		// <stddef.h>
		`ptrdiff_t size_t max_align_t nullptr_t`,
		// <stdint.h>
		`intmax_t uintmax_t intptr_t uintptr_t
		 int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t
		 int_least8_t int_least16_t int_least32_t int_least64_t
		 uint_least8_t uint_least16_t uint_least32_t uint_least64_t
		 int_fast8_t int_fast16_t int_fast32_t int_fast64_t
		 uint_fast8_t uint_fast16_t uint_fast32_t uint_fast64_t`,
		// <stdio.h> — stdin/stdout/stderr are object-like macros; FILE is the one
		// uppercase name a package could realistically reach for
		`clearerr fclose feof ferror fflush fgetc fgetpos fgets fopen fprintf
		 fputc fputs fread freopen fscanf fseek fsetpos ftell fwrite getc getchar
		 gets perror printf putc putchar puts remove rename rewind scanf setbuf
		 setvbuf snprintf sprintf sscanf tmpfile tmpnam ungetc vfprintf vfscanf
		 vprintf vscanf vsnprintf vsprintf vsscanf fpos_t stdin stdout stderr FILE`,
		// <stdlib.h> — the specimen's home
		`abort abs aligned_alloc atexit atof atoi atol atoll at_quick_exit
		 bsearch calloc div exit free getenv labs ldiv llabs lldiv malloc mblen
		 mbstowcs mbtowc qsort quick_exit rand realloc srand strtod strtof strtol
		 strtold strtoll strtoul strtoull system wcstombs wctomb div_t ldiv_t
		 lldiv_t`,
		// <string.h>
		`memchr memcmp memcpy memmove memset strcat strchr strcmp strcoll strcpy
		 strcspn strerror strlen strncat strncmp strncpy strpbrk strrchr strspn
		 strstr strtok strxfrm`,
		// <time.h>
		`asctime clock ctime difftime gmtime localtime mktime strftime time
		 timespec_get clock_t time_t timespec tm`,
		// <uchar.h> — char16_t/char32_t are C++ keywords, refused above
		`c16rtomb c32rtomb mbrtoc16 mbrtoc32`,
		// <wchar.h> — wchar_t is a C++ keyword, refused above
		`btowc fgetwc fgetws fputwc fputws fwide fwprintf fwscanf getwc getwchar
		 mbrlen mbrtowc mbsinit mbsrtowcs mbstate_t putwc putwchar swprintf
		 swscanf ungetwc vfwprintf vfwscanf vswprintf vswscanf vwprintf vwscanf
		 wcrtomb wcscat wcschr wcscmp wcscoll wcscpy wcscspn wcsftime wcslen
		 wcsncat wcsncmp wcsncpy wcspbrk wcsrchr wcsrtombs wcsspn wcsstr wcstod
		 wcstof wcstok wcstol wcstold wcstoll wcstombs wcstoul wcstoull wcsxfrm
		 wctob wint_t wmemchr wmemcmp wmemcpy wmemmove wmemset wprintf wscanf`,
		// <wctype.h>
		`iswalnum iswalpha iswblank iswcntrl iswdigit iswgraph iswlower iswprint
		 iswpunct iswspace iswupper iswxdigit iswctype towctrans towlower towupper
		 wctrans wctrans_t wctype wctype_t`,
	} {
		for w := range strings.FieldsSeq(words) {
			m[w] = true
		}
	}
	return m
}()

// goExportName delegates to the one true mapping (ir.GoExportName), which
// the Go backend also emits with — two names that collide under it cannot
// coexist in one type (SPEC §4.6).
func goExportName(name string) string {
	return ir.GoExportName(name)
}

func (c *checker) checkTargetNames() {
	checkName := func(name string, pos ast.Pos, what string) {
		if lang, ok := targetReserved[name]; ok {
			c.errf(pos, "%s %q is a reserved word in %s — rename at the source; no escaping machinery (SPEC §4.6)",
				what, name, lang)
		}
	}
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			if d.DeclName() != "" {
				checkName(d.DeclName(), d.DeclPos(), "declaration name")
			}
			// each generated name records which field claims it and AS WHAT —
			// its own export, a length/count companion (SPEC §6.1), or a
			// claimed dispatch name — so a collision names the mechanism
			type claim struct {
				field string
				as    string // "" = the field's own name; else a description
			}
			var walkBlock func(b *ast.Block, owner string, export map[string]claim)
			walkBlock = func(b *ast.Block, owner string, export map[string]claim) {
				register := func(pos ast.Pos, fieldName, exp, as string) {
					prev, ok := export[exp]
					if !ok {
						export[exp] = claim{field: fieldName, as: as}
						return
					}
					if prev.field == fieldName {
						return
					}
					describe := func(cl claim) string {
						if cl.field == "" {
							return cl.as // a claimed generated name, no field behind it
						}
						if cl.as == "" {
							return "field " + cl.field
						}
						return cl.as + " of field " + cl.field
					}
					c.errf(pos, "%s collides with %s (both become %s in generated code) — rename at the source (SPEC §4.6)",
						describe(claim{field: fieldName, as: as}), describe(prev), exp)
				}
				for _, item := range b.Items {
					switch item := item.(type) {
					case *ast.Field:
						checkName(item.Name, item.Pos, "field name")
						register(item.Pos, item.Name, goExportName(item.Name), "")
						if owner != "" && goExportName(item.Name) == owner {
							c.errf(item.Pos, "field %s's exported name equals its declaring type %s — C# forbids a member sharing its enclosing type's name; rename at the source (SPEC §4.6)", item.Name, owner)
						}
						// generated companion storage claims names too: the
						// used length beside string/bytes, the used count
						// beside a counted array (SPEC §6.1)
						if item.Type.Kind == ast.ScalarString || item.Type.Kind == ast.ScalarBytes {
							register(item.Pos, item.Name, goExportName(item.Name)+"Length", "the generated length companion")
						}
						if item.Array != nil && item.Array.Kind != ast.ArrayFixed {
							register(item.Pos, item.Name, goExportName(item.Name)+"Count", "the generated count companion")
						}
						// an optional's presence bool is storage too, and it
						// claims its name (SPEC-TABLES.md §2.3)
						if item.Type.Optional {
							register(item.Pos, item.Name, goExportName(item.Name)+"Present", "the generated presence companion")
						}
					case *ast.IfItem:
						walkBlock(item.Then, owner, export)
						if item.Else != nil {
							walkBlock(item.Else, owner, export)
						}
					}
				}
			}
			switch d := d.(type) {
			case *ast.TypeDecl:
				walkBlock(d.Body, d.Name, map[string]claim{})
			case *ast.TableDecl:
				walkBlock(d.Body, d.Name, map[string]claim{})
			case *ast.EnumDecl:
				for _, v := range d.Variants {
					checkName(v.Text, v.Pos, "enum variant")
				}
			case *ast.FlagsDecl:
				for _, v := range d.Variants {
					checkName(v.Text, v.Pos, "flags variant")
				}
			}
		}
	}
}

// ---- claimed names (SPEC §4.6) ----

// checkClaimedNames builds the FULL top-level symbol table generation will
// produce — every declaration plus every derived name any target emits (the
// split functions, constructors, size constants, tag/variant constants, the
// dispatch surface, the object families) — and refuses ANY duplicate. The
// registry is one map, so declared-vs-generated AND generated-vs-generated
// collisions are both caught (a unit whose generated symbols collide with
// each other cannot compile in any target, whatever the checker thinks).
func (c *checker) checkClaimedNames() {
	type origin struct {
		what string
		pos  ast.Pos
	}
	registry := map[string]origin{}
	add := func(name, what string, pos ast.Pos) {
		if prev, ok := registry[name]; ok {
			c.errf(pos, "%s collides with %s — both generate the symbol %s; rename at the source (SPEC §4.6)",
				what, prev.what, name)
			return
		}
		registry[name] = origin{what: what, pos: pos}
	}

	// unit-level symbols first, so every collision reports at the DECL side
	unitPos := ast.Pos{}
	add("ProtocolId", "the unit's generated ProtocolId", unitPos)
	// names the generated Rust references unqualified: the serialize imports
	// (use serialize::{Stream, ReadStream, WriteStream}) and the prelude
	// items its emitted impls and expressions resolve through — a declaration
	// with any of these names shadows them and the crate cannot compile
	for _, gen := range []string{"serialize", "Stream", "ReadStream", "WriteStream", "Default", "From", "Ok", "Err"} {
		add(gen, "a name the generated Rust references unqualified (imports and prelude)", unitPos)
	}
	add("ErrValidation", "the unit's generated ErrValidation (Go)", unitPos)
	// the C# target's one namespace-level home for functions and constants:
	// a declaration named Schema would collide with the static class itself
	add("Schema", "the generated C# Schema class", unitPos)
	add("PROTOCOL_ID", "the unit's generated PROTOCOL_ID (Rust form)", unitPos)
	add("Error", "the unit's generated Error type (Rust form)", unitPos)
	add("Result", "the unit's generated Result alias (Rust form)", unitPos)
	if len(c.tables) > 0 {
		// The TABLE-wire runtime the generated table sources define once per
		// package (SPEC-TABLES.md) — claimed only when a unit declares a
		// table, so table-free units keep their whole namespace.
		//
		// The list is not written here: internal/tablenames is the ONE
		// registry, read by this claim and held honest against what the
		// emitters actually emit. A second copy of it in this file is exactly
		// how the C# runtime's names came to be unclaimed in the first place.
		for _, gen := range tablenames.Claimed() {
			add(gen, "the generated TABLE-wire runtime (SPEC-TABLES.md)", unitPos)
		}
	}

	declNames := make([]string, 0, len(c.astDecls))
	for name := range c.astDecls {
		declNames = append(declNames, name)
	}
	sort.Strings(declNames)
	// addRust registers a derived Rust spelling beside its Go/C++ siblings,
	// skipping a spelling that coincides with one already registered for the
	// same declaration (identical names would self-collide, not collide).
	addRust := func(name, what string, pos ast.Pos, siblings ...string) {
		if slices.Contains(siblings, name) {
			return
		}
		add(name, what, pos)
	}

	for _, name := range declNames {
		d := c.astDecls[name]
		add(name, fmt.Sprintf("declaration %s", name), d.DeclPos())
		switch d := d.(type) {
		case *ast.ConstDecl:
			// the Rust target spells constants SCREAMING_SNAKE in the flat
			// crate namespace
			addRust(ir.RustConstName(name), fmt.Sprintf("const %s's generated constant (Rust/C form)", name), d.DeclPos(), name)
		case *ast.EnumDecl:
			// the Go target flattens variants into the package namespace;
			// the Rust target scopes them as associated consts, so their
			// SCREAMING_SNAKE spellings need only be unique WITHIN the enum —
			// including against the implicit NONE
			add(name+"None", fmt.Sprintf("enum %s's generated None constant", name), d.Pos)
			add(name+"Max", fmt.Sprintf("enum %s's generated Max extent (Go form)", name), d.Pos)
			assoc := map[string]string{"NONE": "the implicit None variant", "MAX": "the generated Max extent"}
			for _, v := range d.Variants {
				add(name+v.Text, fmt.Sprintf("enum %s's generated variant constant", name), v.Pos)
				rv := ir.RustConstName(v.Text)
				if prev, dup := assoc[rv]; dup {
					c.errf(v.Pos, "variant %s collides with %s inside enum %s (both become the associated constant %s in Rust) — rename at the source (SPEC §4.6)",
						v.Text, prev, name, rv)
				} else {
					assoc[rv] = "variant " + v.Text
				}
			}
			// the C target flattens variants as #define ENUM_VARIANT into the
			// one preprocessor namespace, plus _NONE, _MAX and the debug-name
			// function — spellings no other claim covers (they were
			// emitted and never registered, so a const like DriveModeLudicrous
			// beside enum DriveMode { Ludicrous } produced a silent duplicate
			// #define in C while every other target compiled)
			whyC := fmt.Sprintf("enum %s's generated variant constants (C form)", name)
			addRust(ir.RustConstName(name)+"_NONE", whyC, d.Pos, name+"None")
			addRust(ir.RustConstName(name)+"_MAX", whyC, d.Pos)
			addRust("enum_name_"+ir.RustSnake(name), fmt.Sprintf("enum %s's generated debug-name function (C form)", name), d.Pos)
			for _, v := range d.Variants {
				addRust(ir.RustConstName(name)+"_"+ir.RustConstName(v.Text), whyC, v.Pos, name+v.Text)
			}
		case *ast.UnionDecl:
			// the union's generated surface (SPEC §4.8): the <Name>Type tag
			// enum with None/Max and one constant per variant (flat in Go),
			// the wire pair and the bounds — plus the C forms: the flat
			// #define family and the tag debug-name function. Registering
			// them here is what refuses a union named Message in a unit with
			// messages (its MessageType/WriteMessage/ReadMessage collide
			// with the dispatch surface) — generated-vs-generated, one map.
			whyTag := fmt.Sprintf("union %s's generated tag enum", name)
			add(name+"Type", whyTag, d.Pos)
			add(name+"TypeNone", whyTag, d.Pos)
			add(name+"TypeMax", whyTag, d.Pos)
			for _, v := range d.Variants {
				add(name+"Type"+ir.GoExportName(v.Name), fmt.Sprintf("union %s's generated tag constant", name), v.Pos)
			}
			whyFn := fmt.Sprintf("union %s's generated functions and constants", name)
			add("Write"+name, whyFn, d.Pos)
			add("Read"+name, whyFn, d.Pos)
			add("Zero"+name, whyFn, d.Pos) // the C#/JS §5 zero-form helper
			add(name+"MaxBits", whyFn, d.Pos)
			add(name+"MaxBytes", whyFn, d.Pos)
			whyRustU := fmt.Sprintf("union %s's generated functions and constants (Rust/C form)", name)
			addRust("write_"+ir.RustSnake(name), whyRustU, d.Pos, "Write"+name)
			addRust("read_"+ir.RustSnake(name), whyRustU, d.Pos, "Read"+name)
			addRust(ir.RustConstName(name+"MaxBits"), whyRustU, d.Pos, name+"MaxBits")
			addRust(ir.RustConstName(name+"MaxBytes"), whyRustU, d.Pos, name+"MaxBytes")
			whyCTag := fmt.Sprintf("union %s's generated tag constants (C form)", name)
			addRust(ir.RustConstName(name+"Type")+"_NONE", whyCTag, d.Pos, name+"TypeNone")
			addRust(ir.RustConstName(name+"Type")+"_MAX", whyCTag, d.Pos, name+"TypeMax")
			for _, v := range d.Variants {
				addRust(ir.RustConstName(name+"Type")+"_"+ir.RustConstName(v.Name), whyCTag, v.Pos, name+"Type"+ir.GoExportName(v.Name))
			}
			addRust("enum_name_"+ir.RustSnake(name+"Type"), fmt.Sprintf("union %s's generated tag debug-name function (C form)", name), d.Pos)
		case *ast.FlagsDecl:
			add(name+"Count", fmt.Sprintf("flags %s's generated Count constant", name), d.Pos)
			addRust(ir.RustConstName(name+"Count"), fmt.Sprintf("flags %s's generated Count constant (Rust/C form)", name), d.Pos, name+"Count")
			whyName := fmt.Sprintf("flags %s's generated name functions", name)
			add("FlagName"+name, whyName, d.Pos)
			add("FlagNames"+name, whyName, d.Pos)
			addRust("flag_name_"+ir.RustSnake(name), whyName+" (Rust/C form)", d.Pos)
			addRust("flag_names_"+ir.RustSnake(name), whyName+" (Rust/C form)", d.Pos)
			for _, v := range d.Variants {
				add(name+v.Text, fmt.Sprintf("flags %s's generated mask constant (Go form)", name), v.Pos)
				add(name+"_"+v.Text, fmt.Sprintf("flags %s's generated mask constant (C++ form)", name), v.Pos)
				addRust(ir.RustConstName(name)+"_"+ir.RustConstName(v.Text),
					fmt.Sprintf("flags %s's generated mask constant (Rust/C form)", name), v.Pos,
					name+v.Text, name+"_"+v.Text)
			}
		case *ast.TypeDecl:
			c.addStructSymbols(add, addRust, name, d.DeclPos())
			if c.tableClosure[name] {
				c.addTableSymbols(add, name, d.DeclPos())
			}
		case *ast.TableDecl:
			// a table generates its storage struct plus the Table codec and
			// descriptor family — no packet-wire symbols (SPEC-TABLES.md)
			c.addTableSymbols(add, name, d.DeclPos())
			// A BLOCK-FORM table claims two more per out-of-line array,
			// because its row accessors are named after its fields: <Table>
			// followed by the PascalCase of the field's name hands back that
			// field's rows, and the same name with `Span` appended is the
			// contiguous view (§11, §19.2). This part of the set moves with
			// the declaration, which is why §11 states it as a rule.
			if st := c.tables[name]; st != nil {
				why := fmt.Sprintf("%s's generated block row accessors (SPEC-TABLES.md §11, §19.2)", name)
				for _, f := range st.Fields {
					if !ir.BlockOutOfLine(f) {
						continue
					}
					accessor := name + ir.GoExportName(f.Name)
					add(accessor, why, d.DeclPos())
					add(accessor+"Span", why, d.DeclPos())
				}
			}
		}
	}
}

// addTableSymbols registers the TABLE-wire generated names of one closure
// member. The table surface is NAME-FIRST — <Name>Measure, <Name>Save,
// <Name>Load, <Name>Builder — so a table's whole surface autocompletes under
// its own name, while the TYPE wire stays verb-first (WriteX/ReadX): the verb
// position tells a reader which wire the call site is on (SPEC-TABLES.md).
// Tables and types share ONE symbol table, and that is exactly what makes the
// unprefixed surface collision-free: a declaration colliding with any of
// these spellings is refused at the source.
// The C++ and C# table backends both spell this surface CamelCase, so one set
// of claims covers both; a port that spells it otherwise adds its spellings to
// the unit-level list in checkClaimedNames, which is where target-specific
// runtime names are reconciled.
func (c *checker) addTableSymbols(add func(name, what string, pos ast.Pos), name string, pos ast.Pos) {
	why := fmt.Sprintf("%s's generated TABLE-wire functions", name)
	for _, verb := range tableGeneratedVerbs {
		add(name+verb, why, pos)
	}
}

// tableGeneratedVerbs is the full name-first suffix set a closure member
// claims. The mutable-life suffixes (Builder, LoadMeasure, At) are
// claimed for EVERY closure member, not only pointer-bearing ones: a table
// gains or loses pointers as an edit, and a name that was free yesterday must
// not become a collision tomorrow (SPEC-TABLES.md).
// The BLOCK spellings are claimed on the same terms: nothing declares the
// block form, every fixed table has one, and a table gains and loses the form
// as its closure gains and loses a pointer — so a name that was free yesterday
// must not become a collision tomorrow (§11).
// Cook, CookMeasure, Open and OpenWalk are the COOK's spellings and no
// backend emits them: the cook is wire v2's (SPEC-TABLES.md §7, schema#251)
// and is not built. The claim is held while the emitter is absent, on the same
// rule — freeing a name now is a collision the day it lands.
var tableGeneratedVerbs = []string{
	"Measure", "MeasureBody", "Save", "SaveBody", "Load", "LoadBody",
	"Reset", "LoadMeasure", "LoadMeasureBody", "LoadBuilder", "TableType", "Builder",
	"At", "Emplace", "Pack", "PackMeasure", "OpenWalk",
	"Cook", "CookMeasure", "Open", "TableFields", "TableInfo",
	"FromJson", "ToJson", "ToJsonMeasure",
	"Block", "BlockStorage", "BlockBegin", "BlockBytes", "BlockMaxBytes", "BlockOpen", "Counts",
	// the C# BLITTABLE records take claimed suffixes in the package namespace
	// rather than a nested namespace of their own: a generated namespace named
	// by a common noun is a collision class no refusal can close, because it
	// collides with declarations in OTHER units of the same assembly and this
	// compiler sees one unit (SPEC-TABLES.md §19.2).
	"Row", "BlockProjection",
}

// tableBuilderMembers are the member names of a generated <Name>Builder. A
// member function hides a type name it shares, so a table whose NAME is one of
// these produces a header that cannot compile — silently, at the user's build
// rather than ours. The two accessors that a real schema would plausibly hit
// were renamed instead (Root -> GetRoot, Const -> AsConst, because `table Root`
// is this spec's own canonical example); the rest are refused here, so no
// legal schema can reach a non-compiling header through this door.
var tableBuilderMembers = []string{
	"Alloc", "AsConst", "GetRoot", "Lock", "Locked", "Region", "RegionBytes",
	"Worker", "arena", "main", "region", "region_bytes", "root_ref",
}

// addStructSymbols registers the per-type generated names: the split
// functions, the constructor, and the size constants — plus their flat Rust
// spellings (new() is associated and claims nothing).
func (c *checker) addStructSymbols(add func(name, what string, pos ast.Pos), addRust func(name, what string, pos ast.Pos, siblings ...string), name string, pos ast.Pos) {
	why := fmt.Sprintf("type %s's generated functions and constants", name)
	add("Write"+name, why, pos)
	add("Read"+name, why, pos)
	add("New"+name, why, pos)
	add("Zero"+name, why, pos) // the C# §5 zero-form helper (branch zeroing, storage reset)
	add(name+"MaxBits", why, pos)
	add(name+"MaxBytes", why, pos)
	whyRust := fmt.Sprintf("type %s's generated functions and constants (Rust/C form)", name)
	addRust("write_"+ir.RustSnake(name), whyRust, pos, "Write"+name)
	addRust("read_"+ir.RustSnake(name), whyRust, pos, "Read"+name)
	addRust(ir.RustConstName(name+"MaxBits"), whyRust, pos, name+"MaxBits")
	addRust(ir.RustConstName(name+"MaxBytes"), whyRust, pos, name+"MaxBytes")
}

// ---- assembly ----

func (c *checker) assemble() {
	u := c.unit
	for _, f := range c.files {
		irf := &ir.File{Base: f.Base, Path: f.Path}
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.ConstDecl:
				if e := c.constant[d.Name]; e != nil && e.state == 2 {
					irf.Decls = append(irf.Decls, e.out)
					u.Consts[d.Name] = e.out
				}
			case *ast.EnumDecl:
				if en := c.enums[d.Name]; en != nil {
					irf.Decls = append(irf.Decls, en)
					u.Enums[d.Name] = en
				}
			case *ast.FlagsDecl:
				if fl := c.flagsD[d.Name]; fl != nil {
					irf.Decls = append(irf.Decls, fl)
					u.Flags[d.Name] = fl
				}
			case *ast.TypeDecl:
				st := c.structs[d.Name]
				irf.Decls = append(irf.Decls, st)
				u.Structs[d.Name] = st
			case *ast.TableDecl:
				// tables assemble BESIDE the decl stream, never into it
				// (SPEC-TABLES.md): File.Decls and Unit.Structs feed the
				// packet backends and the wire projection, and a table must
				// move neither a generated packet byte nor the protocol id
				tbl := c.tables[d.Name]
				irf.Tables = append(irf.Tables, tbl)
				u.Tables[d.Name] = tbl
			case *ast.UnionDecl:
				un := c.unions[d.Name]
				irf.Decls = append(irf.Decls, un)
				u.Unions[d.Name] = un
			}
		}
		u.Files = append(u.Files, irf)
	}
	maps.Copy(u.DeclFile, c.declFile)
}

// ---- the protocol id (SPEC §3.1) ----

// protocolIdFromProjection is the unit's identity: the low 64 bits of SHA-256
// over the WIRE SHAPE PROJECTION (ir.WireProjection), not over the schema
// source.
//
// The source hash it replaced was safe in the direction that matters — it
// could produce a spurious mismatch but never a spurious match — and it moved
// the id for a comment, a blank line, a renamed file, or a field that costs
// zero wire bits. Each of those bought a coordinated redeploy for nothing.
//
// The projection is text, so what the id depends on can be printed and read
// (`schema projection`). A wire-affecting fact missing from it would be the
// dangerous kind of bug, which is why it is a reviewable artifact rather than
// a walk over the IR structs.
func protocolIdFromProjection(u *ir.Unit) uint64 {
	h := sha256.New()
	h.Write([]byte(ir.WireProjection(u)))
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[24:32])
}
