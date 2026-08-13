// Package check resolves and validates a compilation unit and lowers it to IR
// (SPEC §7.1): name resolution across the unit's files, constant folding
// (arbitrary precision with per-use fit checks, §4.2), the shape checks of
// §4.6, the dominance rule of §4.5, the message-set and object-set
// extractions (§4.8), and the §3.1 protocol id.
package check

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ast"
	"github.com/mas-bandwidth/schema/internal/ir"
)

// SourceFile is one *.schema file of the unit.
type SourceFile struct {
	Path  string // as given
	Name  string // basename with extension — the §3.1 hash label
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
	objects  map[string]*ir.Object
	ctxDecl  *ast.ContextsDecl

	// enums currently being resolved — the cycle guard for [max = E.Max]
	// chains (resolveEnum memoizes only on completion, so recursion needs
	// its own in-progress set, exactly as constants have one)
	resolvingEnum map[string]bool

	// enums whose [max = ...] failed to resolve — their Max fell back to the
	// variant count, which is NOT what the author wrote, so .Max references
	// to them must fail rather than propagate a fabricated bound
	failedEnum map[string]bool

	// composite-quantize checks deferred until every body is resolved: the
	// rule 2 / rule 2b classification READS the referenced composite's
	// component list, and that composite may be declared in a file that
	// sorts after the object's (§3.1 order) — at field-resolution time only
	// its SHELL exists. Running the classification against a half-built
	// shell mis-classifies by file order (found the day rule 2b landed: the
	// game's Objects.schema sorts before Types.schema).
	deferredQuantize []func()
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
// the unit is processed and hashed in sorted-basename order (SPEC §3.1).
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
		objects:  map[string]*ir.Object{},
		unit: &ir.Unit{
			DeclFile: map[string]string{},
			Consts:   map[string]*ir.Const{},
			Enums:    map[string]*ir.Enum{},
			Flags:    map[string]*ir.Flags{},
			Structs:  map[string]*ir.Struct{},
			Objects:  map[string]*ir.Object{},
		},
	}

	c.collectPackage()
	c.collectDecls()
	c.resolveEnumsAndFlags()
	c.resolveAllConsts()
	c.resolveBodies()
	// deferred composite-quantize checks: every body is resolved now, so
	// rule 2/2b classification sees complete component lists in any file order
	for _, check := range c.deferredQuantize {
		check()
	}
	c.checkCycles()
	c.checkClaimedNames()
	c.checkTargetNames()
	c.assemble()
	c.checkTables()
	c.unit.ProtocolId = protocolId(files)

	if len(c.errs) > 0 {
		return nil, c.errs
	}
	return c.unit, nil
}

func (c *checker) collectPackage() {
	for _, f := range c.files {
		if f.AST.Package == "" {
			continue // parser already reported it
		}
		if c.unit.Package == "" {
			c.unit.Package = f.AST.Package
		} else if f.AST.Package != c.unit.Package {
			c.errf(f.AST.PkgPos, "package %q does not match the unit's package %q (exactly one package per unit — SPEC §3.2)",
				f.AST.Package, c.unit.Package)
		}
	}
}

func (c *checker) collectDecls() {
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			if ctx, ok := d.(*ast.ContextsDecl); ok {
				if c.ctxDecl != nil {
					c.errf(ctx.Pos, "duplicate contexts declaration (one list per unit — SPEC §4.2)")
					continue
				}
				c.ctxDecl = ctx
				seen := map[string]bool{}
				for _, n := range ctx.Names {
					if n.Text == "all" {
						c.errf(n.Pos, "context name \"all\" is reserved — it is the default scope and cannot be declared")
						continue
					}
					if seen[n.Text] {
						c.errf(n.Pos, "duplicate context name %q", n.Text)
						continue
					}
					seen[n.Text] = true
					c.unit.Contexts = append(c.unit.Contexts, n.Text)
				}
				continue
			}
			name := d.DeclName()
			if prev, ok := c.astDecls[name]; ok {
				c.errf(d.DeclPos(), "duplicate declaration %q (first declared at %s; all five declaration kinds share one unit-level namespace — SPEC §4.6)",
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
		c.resolveConst(n, ast.Pos{})
	}
}

func (c *checker) resolveConst(name string, usePos ast.Pos) *ir.Const {
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
		if isFloatType {
			// float-typed constant with an integer expression: converts
			f, _ := new(big.Float).SetInt(v).Float64()
			out.IsFloat = true
			out.Float = f
			out.Int = nil
		} else if e.decl.Type != "" {
			if !fitsStorage(v, e.decl.Type) {
				c.errf(e.decl.Pos, "constant %s value %s does not fit its declared type %s", name, v, e.decl.Type)
				e.state = 3
				return nil
			}
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
		out := c.resolveConst(e.Name, e.Pos)
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
		f, _ := new(big.Float).SetInt(e.Value).Float64()
		return f, true
	case *ast.IdentExpr:
		if _, isConst := c.constant[e.Name]; !isConst {
			c.errf(e.Pos, "undefined constant %s", e.Name)
			return 0, false
		}
		out := c.resolveConst(e.Name, e.Pos)
		if out == nil {
			return 0, false
		}
		if out.IsFloat {
			return out.Float, true
		}
		f, _ := new(big.Float).SetInt(out.Int).Float64()
		return f, true // integer constants convert in float positions (SPEC §4.2)
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

// valuedAttr lists the field attributes that MUST carry `= value`. The bare
// markers ([interpolate], [local], and the context words) are absent by
// design. Adding a valued attribute means adding it here, or a bare spelling
// of it reaches expression evaluation as a nil and panics.
var valuedAttr = map[string]bool{
	"min":        true,
	"max":        true,
	"resolution": true,
	"quantize":   true,
	"round":      true,
}

func (c *checker) enumMax(e *ast.MaxExpr) (*big.Int, bool) {
	d, ok := c.astDecls[e.Enum]
	if !ok {
		c.errf(e.Pos, "undefined enum %s in %s.Max", e.Enum, e.Enum)
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
	// enum's [max = Other.Max] resolves Other, which can lead back here —
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
		if seen[v.Text] {
			c.errf(v.Pos, "duplicate variant %s", v.Text)
			continue
		}
		seen[v.Text] = true
		variants = append(variants, v.Text)
	}
	// An enum with no variants is LEGAL: it holds only the implicit None = 0,
	// so its wire range is the degenerate [0, 0] and it costs zero bits. That
	// is the same rule a [min = K, max = K] field follows, and every runtime
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
			c.errf(a.Pos, "unknown attribute %q on an enum — enums take [max = K] only (SPEC §4.6)", a.Key)
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
			c.errf(a.Pos, "enum %s [max = %s] is below its variant count %d (SPEC §4.6)", d.Name, v, max)
			continue
		}
		if v.Int64() > math.MaxInt32 {
			// the enum wire rides the 32-bit ranged call in every target
			c.errf(a.Pos, "enum %s [max = %s] exceeds the 32-bit tag wire's ceiling %d (SPEC §4.6)", d.Name, v, int64(math.MaxInt32))
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
			c.errf(a.Pos, "unknown attribute %q on flags — flags take [max = K] only", a.Key)
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
			c.errf(a.Pos, "flags %s [max = %s] must be in [%d, 64]", d.Name, v, bits)
			continue
		}
		bits = int(v.Int64())
	}
	fl := &ir.Flags{Name: d.Name, Variants: variants, WireBits: bits}
	c.flagsD[d.Name] = fl
	return fl
}

// ---- type / message / object bodies ----

type declKind int

const (
	typeD declKind = iota
	messageD
	objectD
)

func (c *checker) resolveBodies() {
	// shells first so composition can reference in any order
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.TypeDecl:
				st := &ir.Struct{Name: d.Name, IsTable: d.IsTable}
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
			case *ast.MessageDecl:
				c.structs[d.Name] = &ir.Struct{Name: d.Name, IsMessage: true}
			case *ast.ObjectDecl:
				c.objects[d.Name] = &ir.Object{Name: d.Name}
			}
		}
	}
	for _, f := range c.files {
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.TypeDecl:
				c.structs[d.Name].Fields, c.structs[d.Name].Items = c.resolveBody(typeD, d.Name, d.Body)
			case *ast.MessageDecl:
				c.structs[d.Name].Fields, c.structs[d.Name].Items = c.resolveBody(messageD, d.Name, d.Body)
			case *ast.ObjectDecl:
				fields, _ := c.resolveBody(objectD, d.Name, d.Body)
				if len(fields) == 0 {
					c.errf(d.Pos, "empty object body — it generates a meaningless view family (SPEC §4.6)")
				}
				c.objects[d.Name].Fields = fields
			}
		}
	}
}

type scopeFrame struct {
	fields map[string]*ir.Field
}

func (c *checker) resolveBody(kind declKind, owner string, body *ast.Block) ([]*ir.Field, []ir.Item) {
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
				f := c.resolveField(kind, owner, item)
				if f == nil {
					continue
				}
				f.Guard = guard
				out = append(out, f)
				frame.fields[item.Name] = f
				items = append(items, &ir.FieldItem{F: f})
			case *ast.IfItem:
				if kind == objectD {
					c.errf(item.Pos, "an object body admits plain fields only in v1 — no if (SPEC §4.8)")
					continue
				}
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
				if kind == objectD {
					c.errf(item.Pos, "an object body admits plain fields only in v1 — no const() (SPEC §4.8)")
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
				if kind == objectD {
					c.errf(item.Pos, "an object body admits plain fields only in v1 — no reserved (SPEC §4.8)")
					continue
				}
				if bits, ok := c.evalWidth(item.Bits, "reserved width"); ok {
					items = append(items, &ir.ReservedItem{Bits: bits})
				}
			case *ast.AlignItem:
				if kind == objectD {
					c.errf(item.Pos, "an object body admits plain fields only in v1 — no align (SPEC §4.8)")
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
	for i := len(scopes) - 1; i >= 0; i-- {
		if f, ok := scopes[i].fields[name]; ok {
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

func (c *checker) resolveField(kind declKind, owner string, f *ast.Field) *ir.Field {
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
		// fixed(I, F) — SIGNED (Glenn, 2026-08-06: "fixed point is signed");
		// the storage is an integer of exactly I+F bits and the sign bit
		// counts toward I, mirroring serialize_fixed's static_asserts
		// (SPEC §4.3, §4.6). The unsigned spelling is an OPEN question (§9).
		iv, ok1 := c.evalInt(f.Type.Arg)
		fv, ok2 := c.evalInt(f.Type.Arg2)
		if !ok1 || !ok2 {
			return nil
		}
		if !iv.IsInt64() || iv.Int64() < 1 {
			c.errf(f.Type.Pos, "fixed(%s, %s): at least one integer bit is required — the sign bit counts toward I (SPEC §4.6)", iv, fv)
			return nil
		}
		if fv.Sign() < 0 || !fv.IsInt64() {
			c.errf(f.Type.Pos, "fixed(%s, %s): fractional bits cannot be negative (SPEC §4.6)", iv, fv)
			return nil
		}
		total := iv.Int64() + fv.Int64()
		switch total {
		case 8, 16, 32, 64, 128:
		default:
			c.errf(f.Type.Pos, "fixed(%s, %s): I + F = %d must equal a storage width — 8, 16, 32, 64 or 128 (SPEC §4.6)", iv, fv, total)
			return nil
		}
		out.Type = ir.FieldType{Kind: ir.TFixed, Signed: true, Width: int(total),
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
		switch d.(type) {
		case *ast.TypeDecl, *ast.MessageDecl:
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.structs[f.Type.Name]}
		case *ast.EnumDecl:
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.enums[f.Type.Name]}
		case *ast.FlagsDecl:
			out.Type = ir.FieldType{Kind: ir.TNamed, Name: f.Type.Name, Ref: c.flagsD[f.Type.Name]}
		case *ast.ObjectDecl:
			c.errf(f.Type.Pos, "an object name is not a field type in v1 — an object has no single wire form (SPEC §4.8)")
			return nil
		default:
			c.errf(f.Type.Pos, "%s is not a type", f.Type.Name)
			return nil
		}
	}

	// array bound
	if f.Array != nil {
		switch out.Type.Kind {
		case ir.TString, ir.TBytes, ir.TBits:
			c.errf(f.Pos, "an array of %s is not supported in v1 — wrap the element in a type", scalarName(out.Type.Kind))
			return nil
		}
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

	c.resolveAttrs(kind, owner, f, out)

	// the fixed and 128-bit families mirror serialize's own surface exactly
	// (SPEC §4.3, runtime-first): fixed(I, F) and int128 are RANGED — the
	// bounds are part of the wire format — and uint128 is the raw field.
	// [local] fields reach no wire, so they carry no bounds (resolveAttrs
	// already rejects encoding attributes there); a field whose declared
	// bounds failed above was already diagnosed, so the requirement error
	// fires only when no bounds were attempted at all.
	attempted := false
	for _, a := range f.Attrs {
		if a.Key == "min" || a.Key == "max" {
			attempted = true
		}
	}
	if !out.Local && !attempted && out.Type.Kind == ir.TFixed && !out.HasIntRange {
		c.errf(f.Pos, "field %s: fixed(%d, %d) requires [min = A, max = B] — the whole-unit bounds are part of the wire format, exactly like a ranged integer's (SPEC §4.3)",
			f.Name, out.Type.IntBits, out.Type.FracBits)
		return nil
	}
	if !out.Local && !attempted && out.Type.Kind == ir.TInt && out.Type.Width == 128 && out.Type.Signed && !out.HasIntRange {
		c.errf(f.Pos, "field %s: int128 requires [min = A, max = B] — serialize_int128 is the only ranged 128-bit operation; a raw 128-bit field is uint128 (SPEC §4.3)",
			f.Name)
		return nil
	}
	if !out.Local && out.Type.Kind == ir.TFixed && !out.HasIntRange {
		return nil // bounds attempted and rejected above — already diagnosed
	}
	if !out.Local && out.Type.Kind == ir.TInt && out.Type.Width == 128 && out.Type.Signed && !out.HasIntRange {
		return nil // bounds attempted and rejected above — already diagnosed
	}

	c.resolveDefault(f, out)
	return out
}

// resolveDefault validates an optional specified default (Glenn, 2026-08-05:
// zero initialization for all types in all generated languages, "unless we
// allow some specified default, eg. ... = true").
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
		// the door reopened 2026-08-12: the quaternion identity (w = 1.0) is
		// the real case. A fixed default is declared in WHOLE UNITS — the same
		// domain as the [min, max] bounds — and must scale EXACTLY: v * 2^F an
		// integer, so no rounding rule is ever involved in a default.
		v, ok := c.evalFloat(f.Default)
		if !ok {
			return
		}
		scaled := new(big.Float).SetPrec(256).SetFloat64(v)
		scaled.SetMantExp(scaled, out.Type.FracBits) // scaled = v * 2^F (SetMantExp uses its mant argument as-is)
		raw, acc := scaled.Int(nil)
		if acc != big.Exact {
			c.errf(f.Default.ExprPos(), "field %s: default %g is not exactly representable in Q%d.%d — a fixed default must scale to an integer with no rounding (units × 2^%d)", f.Name, v, out.Type.IntBits, out.Type.FracBits, out.Type.FracBits)
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
		out.DefInt = raw    // the RAW scaled integer — what storage initializes to
		out.DefFloat = v    // the whole-unit value, for comments
		out.DefExpr = nil   // never render the units expression as the raw initializer
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

func (c *checker) resolveAttrs(kind declKind, owner string, f *ast.Field, out *ir.Field) {
	byKey := map[string]*ast.Attr{}
	for i := range f.Attrs {
		a := &f.Attrs[i]
		if _, dup := byKey[a.Key]; dup {
			c.errf(a.Pos, "attribute %s repeated (SPEC §4.6)", a.Key)
			continue
		}
		// A valued attribute written bare — `[min, max]` for
		// `[min = 0, max = 10]` — used to reach evalInt with a nil
		// expression and panic the compiler. Reject it here, once, for every
		// valued attribute, rather than nil-checking at each use: a typo in a
		// schema must produce a diagnostic, never a stack trace.
		if a.Value == nil && valuedAttr[a.Key] {
			c.errf(a.Pos, "attribute %s requires a value, as %s = ... (SPEC §4.6)", a.Key, a.Key)
			continue
		}
		byKey[a.Key] = a
	}

	wordValue := func(a *ast.Attr) (string, bool) {
		if id, ok := a.Value.(*ast.IdentExpr); ok {
			return id.Name, true
		}
		c.errf(a.Pos, "%s takes a word value", a.Key)
		return "", false
	}

	for i := range f.Attrs { // declaration order, so diagnostics are deterministic
		a := &f.Attrs[i]
		if byKey[a.Key] != a {
			continue // a repeated key, already reported above
		}
		switch a.Key {
		case "min", "max", "resolution", "quantize", "round", "interpolate", "local", "context":
			// validated below
		default:
			c.errf(a.Pos, "unknown attribute %q — the vocabulary is typed and closed per compiler version (SPEC §4.6)", a.Key)
		}
	}

	// markers first
	if a, ok := byKey["interpolate"]; ok {
		if kind != objectD {
			c.errf(a.Pos, "interpolate is an object view marker (SPEC §4.8) — not valid in a %s", kindName(kind))
		} else if a.Value != nil {
			c.errf(a.Pos, "interpolate is valueless")
		} else {
			out.Interpolate = true
		}
	}
	if a, ok := byKey["local"]; ok {
		if kind != objectD {
			c.errf(a.Pos, "local is an object view marker (SPEC §4.8) — not valid in a %s", kindName(kind))
		} else if a.Value != nil {
			c.errf(a.Pos, "local is valueless")
		} else {
			out.Local = true
		}
	}
	if a, ok := byKey["context"]; ok {
		if kind != objectD {
			c.errf(a.Pos, "context scopes a [local] object field (SPEC §4.2, Contexts)")
		} else if !out.Local {
			c.errf(a.Pos, "context is legal only beside [local] — a wire field must be identical on every side (SPEC §4.2)")
		} else if w, ok := wordValue(a); ok {
			if w == "all" {
				c.errf(a.Pos, "context = all is the default — omit the attribute")
			} else if !contains(c.unit.Contexts, w) {
				declared := "no contexts are declared in this unit"
				if len(c.unit.Contexts) > 0 {
					declared = "declared: " + strings.Join(c.unit.Contexts, ", ")
				}
				c.errf(a.Pos, "context %q is not declared — the unit's contexts declaration closes the legal values (SPEC §4.2); %s",
					w, declared)
			} else {
				out.Context = w
			}
		}
	}

	if out.Local && out.Interpolate {
		c.errf(f.Pos, "field %s is [local, interpolate] — a local field reaches no wire and cannot be in the interpolate view (SPEC §4.8)", f.Name)
	}

	hasMin, hasMax := byKey["min"] != nil, byKey["max"] != nil
	hasRes, hasQuant, hasRound := byKey["resolution"] != nil, byKey["quantize"] != nil, byKey["round"] != nil

	if out.Local && (hasMin || hasMax || hasRes || hasQuant || hasRound) {
		c.errf(f.Pos, "field %s is [local] — it reaches no wire, so encoding attributes are not valid on it", f.Name)
		return
	}

	// composite quantization (SPEC §4.8 rules 2/2b): [interpolate, quantize = K, ...]
	if hasQuant {
		a := byKey["quantize"]
		if kind != objectD || !out.Interpolate {
			c.errf(a.Pos, "quantize belongs to the [interpolate] view-encoding rules of an object body (SPEC §4.8)")
			return
		}
		// classification and validation DEFER until every body is resolved —
		// the referenced composite may live in a later-sorting file and have
		// only its shell here (see the checker's deferredQuantize comment)
		c.deferredQuantize = append(c.deferredQuantize, func() {
			c.checkCompositeQuantize(f, out, byKey, hasMin, hasMax, hasRes)
		})
		return
	}
	if hasRound && !hasRes {
		c.errf(byKey["round"].Pos, "round selects the write rounding of a ranged-int projection — it requires the min/max/resolution triple (SPEC §4.8 rule 4)")
	}

	isInt := out.Type.Kind == ir.TInt
	isFixed := out.Type.Kind == ir.TFixed
	isFloat := out.Type.Kind == ir.TFloat32 || out.Type.Kind == ir.TFloat64

	if hasRes || (isFloat && (hasMin || hasMax)) {
		// the compressed-float triple (SPEC §4.3) / ranged-int projection (§4.8 rule 4)
		if !isFloat {
			c.errf(byKey["resolution"].Pos, "resolution applies to float fields (SPEC §4.6)")
			return
		}
		if out.Type.Kind == ir.TFloat64 && !(kind == objectD && out.Interpolate) {
			c.errf(f.Pos, "field %s: the compressed float is float32 (SPEC §4.3) — float64 takes the triple only as an [interpolate] object-view projection (SPEC §4.8 rule 4)", f.Name)
			return
		}
		if !(hasMin && hasMax && hasRes) {
			c.errf(f.Pos, "field %s: a float range is min, max and resolution, all three together (SPEC §4.6)", f.Name)
			return
		}
		if kind == objectD && !out.Interpolate {
			// deep-only float with a range triple: legal — it is §4.3's compressed float
			_ = kind
		}
		fmin, ok1 := c.evalFloat(byKey["min"].Value)
		fmax, ok2 := c.evalFloat(byKey["max"].Value)
		res, ok3 := c.evalFloat(byKey["resolution"].Value)
		if !ok1 || !ok2 || !ok3 {
			return
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
		out.Round = "nearest"
		if hasRound {
			if w, ok := wordValue(byKey["round"]); ok {
				switch w {
				case "nearest", "up", "down":
					out.Round = w
				default:
					c.errf(byKey["round"].Pos, "round = %s — legal values are nearest, up, down (SPEC §4.8 rule 4)", w)
				}
			}
		}
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
		if vmin.Cmp(vmax) >= 0 {
			c.errf(byKey["min"].Pos, "degenerate range [%s, %s] — for a fixed value use const (SPEC §4.6)", vmin, vmax)
			return
		}
		if isFixed {
			// the bounds are WHOLE UNITS and must be representable in the Q
			// format — Q I.F with the sign bit in I — and in int64, where the
			// runtime's compile-time bound parameters live (serialize_fixed's
			// static_asserts, restated as language rules — SPEC §4.6)
			lo, hi := fixedUnitBounds(out.Type.IntBits)
			if vmin.Cmp(lo) < 0 || vmax.Cmp(hi) > 0 {
				c.errf(f.Pos, "field %s: bounds [%s, %s] whole units do not fit fixed(%d, %d) — Q%d.%d holds [%s, %s] (SPEC §4.6)",
					f.Name, vmin, vmax, out.Type.IntBits, out.Type.FracBits, out.Type.IntBits, out.Type.FracBits, lo, hi)
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

// checkCompositeQuantize is the deferred half of the composite-quantize rule
// (SPEC §4.8 rules 2 and 2b): it runs after EVERY body is resolved, so the
// referenced composite's component list is complete regardless of which file
// declared it (§3.1 file order). resolveAttrs verified [interpolate]-on-object
// and scheduled this; everything that reads component fields lives here.
func (c *checker) checkCompositeQuantize(f *ast.Field, out *ir.Field, byKey map[string]*ast.Attr, hasMin, hasMax, hasRes bool) {
	a := byKey["quantize"]
	st, okT := out.Type.Ref.(*ir.Struct)
	if out.Type.Kind != ir.TNamed || !okT {
		c.errf(a.Pos, "quantize applies component-wise to a composite of float or fixed components (SPEC §4.8 rule 2)")
		return
	}
	allFixed := len(st.Fields) > 0
	for _, cf := range st.Fields {
		if cf.Type.Kind != ir.TFixed || cf.Array != ir.ArrayNone {
			allFixed = false
			break
		}
	}
	if allFixed {
		// fixed-composite shallow narrowing (SPEC §4.8 rule 2b):
		// [interpolate, quantize = K] — the shallow wire keeps log2(K)
		// fractional bits of the component format (K quantized units per
		// whole unit, a power of two no finer than the storage's 2^F).
		// Quantize is a round-to-nearest arithmetic shift, Unquantize a left
		// shift; no max here — the shallow bound derives from each
		// component's own whole-unit [min, max], which every component must
		// therefore declare.
		if hasMax || hasMin || hasRes {
			c.errf(a.Pos, "fixed-composite quantization is [interpolate, quantize = K] alone — the bound comes from the components' own [min, max], not the field (SPEC §4.8 rule 2b)")
			return
		}
		k, ok := c.evalInt(a.Value)
		if !ok {
			return
		}
		if !k.IsInt64() || k.Int64() < 1 || k.Int64()&(k.Int64()-1) != 0 {
			c.errf(a.Pos, "fixed-composite quantize scale %s must be a positive power of two — it is the kept fractional resolution, 2^bits (SPEC §4.8 rule 2b)", k)
			return
		}
		log2k := 0
		for v := k.Int64(); v > 1; v >>= 1 {
			log2k++
		}
		for _, cf := range st.Fields {
			if cf.Type.IntBits+cf.Type.FracBits > 64 {
				c.errf(a.Pos, "fixed-composite quantization narrows through int64 shifts — %s.%s is fixed(%d, %d), wider than 64 bits (SPEC §4.8 rule 2b)",
					st.Name, cf.Name, cf.Type.IntBits, cf.Type.FracBits)
				return
			}
			if log2k > cf.Type.FracBits {
				c.errf(a.Pos, "quantize = %s keeps %d fractional bits but %s.%s is fixed(%d, %d) — the shallow wire cannot be finer than the storage (SPEC §4.8 rule 2b)",
					k, log2k, st.Name, cf.Name, cf.Type.IntBits, cf.Type.FracBits)
				return
			}
			if !cf.HasIntRange {
				c.errf(a.Pos, "fixed-composite quantization derives its wire bound from the components — %s.%s must declare whole-unit [min, max] (SPEC §4.8 rule 2b)", st.Name, cf.Name)
				return
			}
		}
		out.HasQuantize = true
		out.FixedShallow = true
		out.QuantScale = k.Int64()
		out.QuantScaleExpr = a.Value
		out.QuantShift = log2k
		return
	}
	for _, cf := range st.Fields {
		if cf.Type.Kind != ir.TFloat32 && cf.Type.Kind != ir.TFloat64 || cf.Array != ir.ArrayNone {
			c.errf(a.Pos, "quantize requires every component of %s to be a float scalar (%s.%s is not)", st.Name, st.Name, cf.Name)
			return
		}
	}
	if !hasMax || hasMin || hasRes {
		c.errf(a.Pos, "composite quantization is [interpolate, quantize = K, max = B] — max required, min and resolution not valid here (SPEC §4.8 rule 2)")
		return
	}
	k, ok := c.evalInt(a.Value)
	if !ok {
		return
	}
	if !k.IsInt64() || k.Int64() < 1 {
		c.errf(a.Pos, "quantize scale %s must be a positive integer", k)
		return
	}
	b, ok := c.evalFloat(byKey["max"].Value)
	if !ok {
		return
	}
	if b <= 0 {
		c.errf(byKey["max"].Pos, "quantize bound max = %g must be positive", b)
		return
	}
	bound := float64(k.Int64()) * b
	if bound < 1 || bound > math.MaxInt64/2 {
		c.errf(a.Pos, "quantized range [-%g, %g] is out of range", bound, bound)
		return
	}
	out.HasQuantize = true
	out.QuantScale = k.Int64()
	out.QuantScaleExpr = a.Value
	out.QuantMax = b
	out.QuantMaxExpr = byKey["max"].Value
	out.QuantBound = int64(math.Round(bound))
}

// fixedUnitBounds is the whole-unit domain of a signed Q I.F format —
// [-2^(I-1), 2^(I-1) - 1] — clamped to int64, where serialize_fixed's
// compile-time MinUnits/MaxUnits parameters live (SPEC §4.6).
func fixedUnitBounds(intBits int) (*big.Int, *big.Int) {
	lo, hi := storageBounds(true, intBits)
	i64lo, i64hi := storageBounds(true, 64)
	if lo.Cmp(i64lo) < 0 {
		lo = i64lo
	}
	if hi.Cmp(i64hi) > 0 {
		hi = i64hi
	}
	return lo, hi
}

func kindName(k declKind) string {
	switch k {
	case typeD:
		return "type body"
	case messageD:
		return "message body"
	}
	return "object body"
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
	fmt.Sscanf(strings.TrimPrefix(strings.TrimPrefix(storage, "uint"), "int"), "%d", &width)
	lo, hi := storageBounds(signed, width)
	return v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
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
		st := c.structs[name]
		if st != nil {
			for _, f := range st.Fields {
				if f.Type.Kind == ir.TNamed {
					if _, isStruct := f.Type.Ref.(*ir.Struct); isStruct {
						if !visit(f.Type.Name) {
							break
						}
					}
				}
			}
		}
		path = path[:len(path)-1]
		color[name] = black
		return true
	}
	names := make([]string, 0, len(c.structs))
	for n := range c.structs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		visit(n)
	}
}

// ---- target-name safety (SPEC §4.6) ----

// targetReserved is the union of the four target languages' reserved words.
// A declaration or field using one is rejected: no escaping machinery, rename
// at the source.
var targetReserved = func() map[string]string {
	m := map[string]string{}
	add := func(lang, words string) {
		for _, w := range strings.Fields(words) {
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

// goExportName delegates to the one true mapping (ir.GoExportName), which
// the Go backend also emits with — two names that collide under it cannot
// coexist in one type (SPEC §4.6).
func goExportName(name string) string {
	return ir.GoExportName(name)
}

// checkTables enforces the table closure's wire capability: a `table` and
// everything it references, transitively, must stay on table-wire kinds.
// int128/uint128, fixed(I, F) and bits wider than 64 have no table-wire kind
// (notes/table-wire.md), so they are refused HERE, loudly, instead of
// surprising a pack run or a generated reader later.
func (c *checker) checkTables() {
	closure := ir.TableClosure(c.unit)
	// SORTED. Ranging a map here made the ORDER of these diagnostics vary
	// run to run (Go randomizes map iteration), so one input could print its
	// errors three different ways — which defeats golden-diagnostic
	// comparison and makes CI logs irreproducible. The exit code and the
	// error set were never affected, but a compiler whose wire is byte-pinned
	// should not shuffle its own output either.
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := c.unit.Structs[name]
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			var bad string
			switch {
			case f.Type.Kind == ir.TInt && f.Type.Width == 128:
				bad = "int128/uint128"
			case f.Type.Kind == ir.TFixed:
				bad = "fixed(I, F)"
			case f.Type.Kind == ir.TBits && f.Type.Width > 64:
				bad = fmt.Sprintf("bits(%d)", f.Type.Width)
			}
			if bad != "" {
				pos := ast.Pos{}
				if d, ok := c.astDecls[name]; ok {
					pos = d.DeclPos()
				}
				c.errf(pos, "%s.%s: %s has no table-wire kind, and %s is in a table's closure — a `table` and everything it references must stay on table-wire kinds (notes/table-wire.md)",
					name, f.Name, bad, name)
			}
		}
	}
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
			case *ast.MessageDecl:
				// the C# dispatch property is named Type, and a C# member
				// cannot share its enclosing class's name — a message named
				// Type has no compilable C# class
				if d.Name == "Type" {
					c.errf(d.DeclPos(), "message Type collides with its own generated C# Type dispatch property (a member cannot share its enclosing class's name) — rename at the source (SPEC §4.6)")
				}
				// the Go dispatch surface gives every message a MessageType()
				// method, and the C# dispatch gives every message a Type
				// property — a field exporting to either name cannot compile
				walkBlock(d.Body, d.Name, map[string]claim{
					"MessageType": {as: "the generated MessageType dispatch method (SPEC §6.1 item 6)"},
					"Type":        {as: "the generated Type dispatch property (C#, SPEC §6.1 item 6)"},
				})
			case *ast.ObjectDecl:
				// objects generate XState/XData_*/CtxXState classes, never a
				// class named X — but a FIELD exporting to one of those class
				// names lands INSIDE that class and C# forbids a member
				// sharing its enclosing class's name (CS0542), so the family
				// names are seeded as claims
				family := map[string]claim{}
				for _, cls := range []string{d.Name + "State", d.Name + "Data_Deep", d.Name + "Data_Shallow", d.Name + "Data_Interpolate"} {
					family[cls] = claim{as: "the generated " + cls + " class name (a C# member cannot share its enclosing class's name)"}
				}
				if c.ctxDecl != nil {
					for _, ctx := range c.unit.Contexts {
						cls := capitalize(ctx) + d.Name + "State"
						family[cls] = claim{as: "the generated " + cls + " class name (a C# member cannot share its enclosing class's name)"}
					}
				}
				walkBlock(d.Body, "", family)
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
	var add func(name, what string, pos ast.Pos)
	add = func(name, what string, pos ast.Pos) {
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
	hasMessages := countMessages(c.astDecls) > 0
	if hasMessages {
		for _, gen := range []string{"MessageType", "MessageTypeNone", "Message", "MessageStorage",
			"WriteMessage", "ReadMessage", "WriteMessageType", "ReadMessageType", "MessageMaxBits", "MessageMaxBytes"} {
			add(gen, "the generated message dispatch surface", unitPos)
		}
		for _, gen := range []string{"write_message", "read_message", "write_message_type",
			"read_message_type", "MESSAGE_MAX_BITS", "MESSAGE_MAX_BYTES"} {
			add(gen, "the generated message dispatch surface (Rust form)", unitPos)
		}
	}
	if len(c.objects) > 0 {
		for _, gen := range []string{"ObjectType", "ObjectTypeNone", "WriteObjectType", "ReadObjectType"} {
			add(gen, "the generated object tag surface", unitPos)
		}
		for _, gen := range []string{"write_object_type", "read_object_type"} {
			add(gen, "the generated object tag surface (Rust form)", unitPos)
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
		for _, s := range siblings {
			if s == name {
				return
			}
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
			addRust(ir.RustConstName(name), fmt.Sprintf("const %s's generated constant (Rust form)", name), d.DeclPos(), name)
		case *ast.EnumDecl:
			// the Go target flattens variants into the package namespace;
			// the Rust target scopes them as associated consts, so their
			// SCREAMING_SNAKE spellings need only be unique WITHIN the enum —
			// including against the implicit NONE
			add(name+"None", fmt.Sprintf("enum %s's generated None constant", name), d.Pos)
			assoc := map[string]string{"NONE": "the implicit None variant"}
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
		case *ast.FlagsDecl:
			for _, v := range d.Variants {
				add(name+v.Text, fmt.Sprintf("flags %s's generated mask constant (Go form)", name), v.Pos)
				add(name+"_"+v.Text, fmt.Sprintf("flags %s's generated mask constant (C++ form)", name), v.Pos)
				addRust(ir.RustConstName(name)+"_"+ir.RustConstName(v.Text),
					fmt.Sprintf("flags %s's generated mask constant (Rust form)", name), v.Pos,
					name+v.Text, name+"_"+v.Text)
			}
		case *ast.TypeDecl:
			c.addStructSymbols(add, addRust, name, d.DeclPos())
		case *ast.MessageDecl:
			c.addStructSymbols(add, addRust, name, d.DeclPos())
			// the Rust tag constant is associated (MessageType::NAME) — no flat claim
			add("MessageType"+name, fmt.Sprintf("message %s's generated tag constant", name), d.DeclPos())
		case *ast.ObjectDecl:
			pos := d.DeclPos()
			why := fmt.Sprintf("object %s's generated family", name)
			add(name+"State", why, pos)
			add(name+"Data_Deep", why, pos)
			add(name+"Data_Shallow", why, pos)
			add(name+"Data_Interpolate", why, pos)
			add("Quantize"+name, why, pos)
			add("Unquantize"+name, why, pos)
			for _, view := range []string{"Data_Deep", "Data_Shallow"} {
				add("Write"+name+view, why, pos)
				add("Read"+name+view, why, pos)
				add(name+view+"MaxBits", why, pos)
				add(name+view+"MaxBytes", why, pos)
			}
			whyRust := fmt.Sprintf("object %s's generated family (Rust form)", name)
			addRust("quantize_"+ir.RustSnake(name), whyRust, pos, "Quantize"+name)
			addRust("unquantize_"+ir.RustSnake(name), whyRust, pos, "Unquantize"+name)
			for _, view := range []string{"Data_Deep", "Data_Shallow"} {
				addRust("write_"+ir.RustSnake(name+view), whyRust, pos, "Write"+name+view)
				addRust("read_"+ir.RustSnake(name+view), whyRust, pos, "Read"+name+view)
				addRust(ir.RustConstName(name+view+"MaxBits"), whyRust, pos, name+view+"MaxBits")
				addRust(ir.RustConstName(name+view+"MaxBytes"), whyRust, pos, name+view+"MaxBytes")
			}
			add("ObjectType"+name, fmt.Sprintf("object %s's generated tag constant", name), pos)
			for _, ctx := range c.unit.Contexts {
				add(capitalize(ctx)+name+"State", fmt.Sprintf("object %s's generated %s-context state", name, ctx), pos)
			}
		}
	}
}

// addStructSymbols registers the per-type generated names shared by types and
// messages: the split functions, the constructor, and the size constants —
// plus their flat Rust spellings (new() is associated and claims nothing).
func (c *checker) addStructSymbols(add func(name, what string, pos ast.Pos), addRust func(name, what string, pos ast.Pos, siblings ...string), name string, pos ast.Pos) {
	why := fmt.Sprintf("type %s's generated functions and constants", name)
	add("Write"+name, why, pos)
	add("Read"+name, why, pos)
	add("New"+name, why, pos)
	add("Zero"+name, why, pos) // the C# §5 zero-form helper (branch zeroing, storage reset)
	add(name+"MaxBits", why, pos)
	add(name+"MaxBytes", why, pos)
	whyRust := fmt.Sprintf("type %s's generated functions and constants (Rust form)", name)
	addRust("write_"+ir.RustSnake(name), whyRust, pos, "Write"+name)
	addRust("read_"+ir.RustSnake(name), whyRust, pos, "Read"+name)
	addRust(ir.RustConstName(name+"MaxBits"), whyRust, pos, name+"MaxBits")
	addRust(ir.RustConstName(name+"MaxBytes"), whyRust, pos, name+"MaxBytes")
}

func countMessages(decls map[string]ast.Decl) int {
	n := 0
	for _, d := range decls {
		if _, ok := d.(*ast.MessageDecl); ok {
			n++
		}
	}
	return n
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
			case *ast.ContextsDecl:
				irf.Decls = append(irf.Decls, &ir.ContextsMarker{Names: u.Contexts})
			case *ast.TypeDecl:
				st := c.structs[d.Name]
				irf.Decls = append(irf.Decls, st)
				u.Structs[d.Name] = st
			case *ast.MessageDecl:
				st := c.structs[d.Name]
				irf.Decls = append(irf.Decls, st)
				u.Structs[d.Name] = st
				u.Messages = append(u.Messages, d.Name)
			case *ast.ObjectDecl:
				ob := c.objects[d.Name]
				irf.Decls = append(irf.Decls, ob)
				u.Objects[d.Name] = ob
				u.ObjNames = append(u.ObjNames, d.Name)
			}
		}
		u.Files = append(u.Files, irf)
	}
	for name, base := range c.declFile {
		u.DeclFile[name] = base
	}
	// the deterministic tag order: SORTED BY NAME, bytewise (SPEC §4.8)
	sort.Strings(u.Messages)
	sort.Strings(u.ObjNames)
}

// ---- the protocol id (SPEC §3.1) ----

// protocolId hashes the unit's files: sorted by bytewise ascending basename;
// per file, basename bytes, one 0x00, then raw content bytes; the id is the
// final 8 bytes of the SHA-256 digest interpreted big-endian.
func protocolId(files []SourceFile) uint64 {
	sorted := make([]SourceFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	h := sha256.New()
	for _, f := range sorted {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write(f.Bytes)
	}
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[24:32])
}
