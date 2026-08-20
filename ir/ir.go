// Package ir is the fully-resolved form the backends consume (SPEC §7.1).
// Backends never see the AST except through expressions carried for symbolic
// rendering; a target-language divergence must be written into a printer to
// exist at all.
package ir

import (
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ast"
)

// Expr is a schema expression the checker resolved but kept, so a backend can
// render the author's own spelling — `MaxHealth` rather than the folded `100`
// — beside the resolved value in the fields next to it. The concrete node
// types are the parser's, and stay unexported from this module: an expression
// is produced by parsing schema text and is never constructed by a caller, and
// freezing the parse tree under semver buys nothing the numbers beside it do
// not already give. A generator outside this module reads the resolved
// numeric fields and treats a non-nil Expr as the fact that the author wrote
// something symbolic here.
type Expr = ast.Expr

type Unit struct {
	Package    string
	ProtocolId uint64
	Contexts   []string // declared order; empty if the unit declares none
	Files      []*File  // sorted by basename (the §3.1 order)

	// lookups for backends
	DeclFile map[string]string // declaration name -> file base
	Consts   map[string]*Const
	Enums    map[string]*Enum
	Flags    map[string]*Flags
	Structs  map[string]*Struct
	Objects  map[string]*Object

	Messages []string // sorted by name — the MessageType order (SPEC §4.8)
	ObjNames []string // sorted by name — the ObjectType order
}

type File struct {
	Base  string // "Constants"
	Path  string // as given, e.g. "examples/Constants.schema"
	Decls []Decl
}

type Decl interface{ irDecl() }

type Const struct {
	Name     string
	IsFloat  bool
	Storage  string // schema storage name: explicit type, else "int64" / "float64"
	Explicit bool   // the declaration named its storage — typed in every target (SPEC §4.2)
	Int      *big.Int
	Float    float64
	Expr     Expr // for symbolic rendering in generated code
}

type Enum struct {
	Name        string
	Variants    []string // implicit None = 0 is not listed; variants pack from 1
	Max         int64    // top wire value: variant count, or the [max = K] widening
	StorageBits int      // 8 / 16 / 32 / 64 — smallest unsigned fitting Max
}

type Flags struct {
	Name     string
	Variants []string // bit i is variant i
	WireBits int      // variant count, or the [max = K] widening
}

// ContextsMarker records the contexts declaration in its declaring file so the
// generated header can document it; contexts generate no standalone artifacts.
type ContextsMarker struct {
	Names []string
}

// Struct is a `type`, `table` or `message` declaration.
type Struct struct {
	Name      string
	IsMessage bool
	IsTable   bool // declared with `table`: a table-wire/reflection ROOT.
	// The closure (this table plus everything it references, transitively)
	// gets table codecs and field descriptors — see TableClosure.
	Tags []string // inert in v1 (SPEC §4.2)
	// C++ native type mapping (SPEC §4.2, Native type mapping): when set,
	// generated C++ declares fields of this type as ::CppNative (a hand type
	// deriving from the generated basis struct — same layout, plus behavior)
	// and emits #include "CppInclude" in every header that references it.
	// The basis struct still emits in its own header; references inside that
	// header keep the basis name (the native header includes it — a mapped
	// reference there would be circular). C++-only; other targets ignore it.
	CppNative  string
	CppInclude string
	Fields     []*Field // flattened; branch fields carry Guard — storage emission
	Items      []Item   // the wire tree: fields and branches in wire order — function emission
}

// TableClosure is the set of structs that carry table codecs and reflection
// descriptors: every `table` declaration plus every struct reachable from one
// through fields (nested types, array elements), transitively. Plain types
// outside the closure stay dense-wire only.
func TableClosure(u *Unit) map[string]bool {
	closure := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if closure[name] {
			return
		}
		st, ok := u.Structs[name]
		if !ok {
			return
		}
		closure[name] = true
		for _, f := range st.Fields {
			if f.Type.Kind == TNamed {
				if _, isStruct := f.Type.Ref.(*Struct); isStruct {
					walk(f.Type.Name)
				}
			}
		}
	}
	for name, st := range u.Structs {
		if st.IsTable {
			walk(name)
		}
	}
	return closure
}

// Item is one wire-ordered element of a struct body: a field, an if branch
// whose untaken side a read zeroes (SPEC §5), or one of the storage-less wire
// constructs (const, reserved, align — SPEC §4.3).
type Item interface{ irItem() }

type FieldItem struct {
	F *Field
}

type Branch struct {
	Neg  bool   // if !cond
	Cond string // the back-referenced bool field's name (SPEC §4.5)
	Then []Item
	Else []Item // nil when no else block
}

// ConstItem is const(value, bits): the constant on the wire; a read REJECTS
// any other value (SPEC §4.3).
type ConstItem struct {
	Value *big.Int
	Bits  int64
}

// ReservedItem is reserved(bits): zeros on the wire; a read rejects nonzero.
type ReservedItem struct {
	Bits int64
}

// AlignItem is align: zero-pad to the next byte boundary; a read rejects
// nonzero padding.
type AlignItem struct{}

func (*FieldItem) irItem()    {}
func (*Branch) irItem()       {}
func (*ConstItem) irItem()    {}
func (*ReservedItem) irItem() {}
func (*AlignItem) irItem()    {}

type Object struct {
	Name   string
	Fields []*Field
}

func (*Const) irDecl()          {}
func (*Enum) irDecl()           {}
func (*Flags) irDecl()          {}
func (*ContextsMarker) irDecl() {}
func (*Struct) irDecl()         {}
func (*Object) irDecl()         {}

type ArrayKind int

const (
	ArrayNone ArrayKind = iota
	ArrayFixed
	ArrayCounted // [<= N]T and [Min..N]T
)

type Field struct {
	Name  string
	Guard string // "" or "if !at_rest" — branch context, kept as a comment

	// object view markers (SPEC §4.8)
	Local       bool
	Interpolate bool
	Context     string // "" = all contexts

	Array      ArrayKind
	ArrayBound int64
	ArrayExpr  Expr  // the declared bound expression, for rendering
	ArrayMin   int64 // ArrayCounted range form; 0 otherwise

	Type FieldType

	// specified default (SPEC: zero initialization everywhere unless a
	// specified default overrides it — Glenn, 2026-08-05)
	HasDefault bool
	DefBool    bool
	DefInt     *big.Int
	DefFloat   float64
	DefVariant string // enum-typed field: the defaulted variant name
	DefExpr    Expr   // for symbolic rendering

	// resolved wire refinements
	HasIntRange    bool
	IntMin         *big.Int
	IntMax         *big.Int
	IntMinExpr     Expr // for symbolic rendering in generated code
	IntMaxExpr     Expr
	HasFloatRange  bool // the compressed-float triple (SPEC §4.3) / projection (§4.8 rule 4)
	FMin           float64
	FMax           float64
	Resolution     float64
	Round          string // "", "nearest", "up", "down"
	Steps          int64  // ceil((FMax-FMin)/Resolution)
	HasQuantize    bool   // composite quantization (SPEC §4.8 rule 2)
	QuantScale     int64
	QuantScaleExpr Expr
	QuantMax       float64
	QuantMaxExpr   Expr
	QuantBound     int64 // round(QuantScale * QuantMax) — per-component wire range is [-QuantBound, +QuantBound]

	// fixed-composite shallow narrowing (SPEC §4.8 rule 2b): the composite's
	// components are all fixed(I, F); the shallow wire keeps QuantShift =
	// log2(QuantScale) fractional bits. Quantize rounds to nearest with ties
	// HALF AWAY FROM ZERO over (F - QuantShift) dropped bits — the one
	// fixed-point rounding rule (SPEC §4.8, decided 2026-08-15; before the
	// unification the emitters shipped the bare shift, ties toward
	// +infinity, and this comment already said half-away — the code moved to
	// match the words, not the other way). Unquantize is the left shift
	// back; the per-component wire bound is the component's own whole-unit
	// [IntMin, IntMax] times QuantScale. QuantMax/QuantBound are
	// meaningless here — bounds live on the components, not the field.
	FixedShallow bool
	QuantShift   int
}

type FieldTypeKind int

const (
	TInt FieldTypeKind = iota
	TBits
	TBool
	TFloat32
	TFloat64
	TString
	TBytes
	TFixed // fixed(I, F) / ufixed(I, F) — fixed point per Signed, storage I+F bits (SPEC §4.3)
	TNamed
)

type FieldType struct {
	Kind     FieldTypeKind
	Signed   bool   // TInt; TFixed (fixed = true; ufixed = false — Glenn 2026-08-15, §9 q17 closed)
	Width    int    // TInt: 8/16/32/64/128; TBits: N; TFixed: I+F (the storage width)
	IntBits  int    // TFixed: I — integer bits; the sign bit counts when Signed (SPEC §4.3)
	FracBits int    // TFixed: F — fractional bits
	Size     int64  // TString/TBytes: N (max length)
	SizeExpr Expr   // TString/TBytes: the declared N expression
	Name     string // TNamed
	Ref      Decl   // TNamed: *Struct, *Enum or *Flags
}

// GoExportName is the one true mapping from a schema field name to its
// exported Go identifier: lower_snake_case -> UpperCamelCase. The checker's
// collision detection and the Go backend must share it, or the check lies.
func GoExportName(name string) string {
	out := make([]byte, 0, len(name))
	upper := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' {
			upper = true
			continue
		}
		if upper && c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		upper = false
		out = append(out, c)
	}
	return string(out)
}

// RustSnake is the one true mapping from an UpperCamelCase declaration name
// to its snake_case Rust spelling (functions, modules): ShipCreate ->
// ship_create, ABTest -> ab_test, ShipData_Deep -> ship_data_deep (an
// explicit underscore collapses with the derived one). The checker's
// claimed-name registry and the Rust backend must share it, or the check lies.
func RustSnake(name string) string {
	out := make([]byte, 0, len(name)+4)
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			prevLower := i > 0 && name[i-1] >= 'a' && name[i-1] <= 'z'
			nextLower := i+1 < len(name) && name[i+1] >= 'a' && name[i+1] <= 'z'
			if i > 0 && (prevLower || nextLower) && len(out) > 0 && out[len(out)-1] != '_' {
				out = append(out, '_')
			}
			out = append(out, c-'A'+'a')
			continue
		}
		if c == '_' && len(out) > 0 && out[len(out)-1] == '_' {
			continue // collapse doubled separators (ShipData_Deep)
		}
		out = append(out, c)
	}
	return string(out)
}

// RustConstName is the SCREAMING_SNAKE Rust constant spelling of a
// declaration name: MaxHealth -> MAX_HEALTH, ChatMaxBits -> CHAT_MAX_BITS.
func RustConstName(name string) string {
	s := RustSnake(name)
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		out[i] = c
	}
	return string(out)
}

// StorageBitsFor returns the smallest unsigned storage width holding max.
func StorageBitsFor(max int64) int {
	switch {
	case max <= 0xFF:
		return 8
	case max <= 0xFFFF:
		return 16
	case max <= 0xFFFFFFFF:
		return 32
	default:
		return 64
	}
}

// FixedShallowBounds is the per-component shallow wire range of a narrowed
// fixed composite (SPEC §4.8 rule 2b): the component's own whole-unit
// [IntMin, IntMax] scaled by QuantScale. Every backend derives storage and
// wire bounds from this one function, or the five wires disagree.
func FixedShallowBounds(f *Field, cf *Field) (lo, hi *big.Int) {
	k := big.NewInt(f.QuantScale)
	lo = new(big.Int).Mul(cf.IntMin, k)
	hi = new(big.Int).Mul(cf.IntMax, k)
	return lo, hi
}

// ObjectNeedsQuantize reports whether an object's Quantize/Unquantize pair
// does any work: true when some [interpolate] field quantizes a composite
// (HasQuantize) or projects a float (HasFloatRange). When false the pair
// would be a pure member copy — fixed-point components are their own
// quantization (SPEC §4.8) — and the backends do not emit it at all.
func ObjectNeedsQuantize(d *Object) bool {
	for _, f := range d.Fields {
		if f.Interpolate && (f.HasQuantize || f.HasFloatRange) {
			return true
		}
	}
	return false
}
