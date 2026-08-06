// Package ir is the fully-resolved form the backends consume (SPEC §7.1).
// Backends never see the AST except through expressions carried for symbolic
// rendering; a target-language divergence must be written into a printer to
// exist at all.
package ir

import (
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ast"
)

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
	Name    string
	IsFloat bool
	Storage string // schema storage name: explicit type, else "int64" / "float64"
	Int     *big.Int
	Float   float64
	Expr    ast.Expr // for symbolic rendering in generated code
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

// Struct is a `type` or `message` declaration.
type Struct struct {
	Name      string
	IsMessage bool
	Tags      []string // inert in v1 (SPEC §4.2)
	Fields    []*Field // flattened; branch fields carry Guard
}

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
	ArrayExpr  ast.Expr // the declared bound expression, for rendering
	ArrayMin   int64    // ArrayCounted range form; 0 otherwise

	Type FieldType

	// specified default (SPEC: zero initialization everywhere unless a
	// specified default overrides it — Glenn, 2026-08-05)
	HasDefault bool
	DefBool    bool
	DefInt     *big.Int
	DefFloat   float64
	DefVariant string   // enum-typed field: the defaulted variant name
	DefExpr    ast.Expr // for symbolic rendering

	// resolved wire refinements
	HasIntRange    bool
	IntMin         *big.Int
	IntMax         *big.Int
	HasFloatRange  bool // the compressed-float triple (SPEC §4.3) / projection (§4.8 rule 4)
	FMin           float64
	FMax           float64
	Resolution     float64
	Round          string // "", "nearest", "up", "down"
	Steps          int64  // ceil((FMax-FMin)/Resolution)
	HasQuantize    bool   // composite quantization (SPEC §4.8 rule 2)
	QuantScale     int64
	QuantScaleExpr ast.Expr
	QuantMax       float64
	QuantMaxExpr   ast.Expr
	QuantBound     int64 // round(QuantScale * QuantMax) — per-component wire range is [-QuantBound, +QuantBound]
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
	TNamed
)

type FieldType struct {
	Kind     FieldTypeKind
	Signed   bool     // TInt
	Width    int      // TInt: 8/16/32/64; TBits: N
	Size     int64    // TString/TBytes: N (max length)
	SizeExpr ast.Expr // TString/TBytes: the declared N expression
	Name     string   // TNamed
	Ref      Decl     // TNamed: *Struct, *Enum or *Flags
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
