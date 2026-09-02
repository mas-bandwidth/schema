// Package ir is the fully-resolved form the backends consume (SPEC §7.1).
// Backends never see the AST except through expressions carried for symbolic
// rendering; a target-language divergence must be written into a printer to
// exist at all.
package ir

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
)

// Expr is a schema expression the checker resolved but kept, so a backend can
// render the author's own spelling — `MaxHealth` rather than the folded `100`
// — beside the resolved value in the fields next to it. The concrete node
// types are the parser's, and stay unexported from this module: an expression
// is produced by parsing schema text and is never constructed by a caller, and
// freezing the parse tree under semver buys nothing the functions beside it do
// not already give. A generator outside this module reads the resolved
// numeric fields, renders the author's spelling with [RenderExpr] or
// [RenderExprIdent], and asks [ExprConsts] and [ExprHasEnumMax] the two
// questions a fold-or-render decision turns on — the same doors the built-in
// backends go through.
type Expr = ast.Expr

// Unit is one checked compilation unit — the checker's output and every
// generator's input (SPEC §3.2).
type Unit struct {
	Package    string
	ProtocolId uint64
	Files      []*File // sorted by basename

	// lookups for backends
	DeclFile map[string]string // declaration name -> file base
	Consts   map[string]*Const
	Enums    map[string]*Enum
	Flags    map[string]*Flags
	Structs  map[string]*Struct
	Unions   map[string]*Union

	// Tables holds the unit's `table` declarations (SPEC-TABLES.md), keyed by
	// name. Tables live on the evolution-tolerant TABLE wire, not the packet
	// wire: they are deliberately absent from Structs, from File.Decls and
	// from the wire projection, so the packet backends and the protocol id
	// never see them — packets and tables version independently.
	Tables map[string]*Struct
}

// File is one schema file's declarations, in declaration order.
type File struct {
	Base  string // "Constants"
	Path  string // as given, e.g. "examples/Constants.schema"
	Decls []Decl

	// Tables is the file's `table` declarations in declaration order — kept
	// beside Decls, not inside, so the packet backends' traversals never
	// meet one (SPEC-TABLES.md).
	Tables []*Struct
}

// Decl is one top-level declaration.
type Decl interface{ irDecl() }

// Const is a `const` declaration, its value folded.
type Const struct {
	Name     string
	IsFloat  bool
	Storage  string // schema storage name: explicit type, else "int64" / "float64"
	Explicit bool   // the declaration named its storage — typed in every target (SPEC §4.2)
	Int      *big.Int
	Float    float64
	Expr     Expr // for symbolic rendering in generated code
}

// Enum is an `enum` declaration: None = 0 implicit, variants dense from 1
// (SPEC §4.2).
type Enum struct {
	Name        string
	Variants    []string // implicit None = 0 is not listed; variants pack from 1
	Max         int64    // top wire value: variant count, or the | max = K widening
	StorageBits int      // 8 / 16 / 32 / 64 — smallest unsigned fitting Max
}

// Flags is a `flags` declaration: one bit per variant, consumed as masks
// (SPEC §4.2).
type Flags struct {
	Name     string
	Variants []string // bit i is variant i
	WireBits int      // variant count, or the | max = K widening
}

// Union is a first-class one-of type (SPEC §4.8): the implicit None row at
// tag 0, then each variant in DECLARED order, dense from 1. The tag encodes
// in minimal bits for [0, Max]; the wire then carries the selected variant's
// payload only. The generated tag enum is named <Name>Type.
type Union struct {
	Name        string
	Variants    []UnionVariant // declared order — the tag order
	Max         int64          // = len(Variants); the tag wire range is [0, Max]
	StorageBits int            // 8 / 16 / 32 / 64 — smallest unsigned fitting Max
}

// UnionVariant is one arm of a [Union].
type UnionVariant struct {
	Name string  // declared, field-style lower_snake
	Type string  // the payload type's name
	Ref  *Struct // the payload
}

// Struct is a `type` declaration — or, when IsTable is set, a `table`
// declaration (SPEC-TABLES.md), which shares the resolved body shape but
// lives in Unit.Tables/File.Tables instead of the packet decl stream.
type Struct struct {
	Name    string
	IsTable bool     // declared with `table`: a table-wire root
	Tags    []string // inert in v1 (SPEC §4.2)

	// Block marks a table declared `| block` (SPEC-TABLES.md §2.7): the table
	// has a THIRD projection beside its wire (§3) and its cook (§7), one in
	// which its own bounded arrays are laid out of line at a fixed pitch so a
	// consumer in another language points at their rows (§19). It declares no
	// construct, changes no field and moves no wire byte — the marker selects
	// a FORM, and a form is not a mode (§2.2).
	Block bool
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

// Item is one wire-ordered element of a struct body: a field, an if branch
// whose untaken side a read zeroes (SPEC §5), or one of the storage-less wire
// constructs (const, reserved, align — SPEC §4.3).
type Item interface{ irItem() }

// FieldItem wraps a [Field] as an [Item].
type FieldItem struct {
	F *Field
}

// Branch is an if/else wire branch (SPEC §4.5).
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

func (*Const) irDecl()  {}
func (*Enum) irDecl()   {}
func (*Flags) irDecl()  {}
func (*Struct) irDecl() {}
func (*Union) irDecl()  {}

// ArrayKind classifies a field's array form.
type ArrayKind int

const (
	ArrayNone ArrayKind = iota
	ArrayFixed
	ArrayCounted // [..N]T and [Min..N]T
)

// Field is one storage-carrying member of a struct, with its resolved wire
// refinements.
type Field struct {
	Name  string
	Guard string // "" or "if !at_rest" — branch context, kept as a comment

	// WasName is the `was = "old_name"` rename alias (SPEC-TABLES.md): the
	// field's TABLE-wire id derives from this name instead of Name, so wire
	// identity survives a rename. Table fields only; "" when unset.
	WasName string

	// JsonKey is the `json = "key"` text-form key (SPEC-TABLES.md §16.3): the
	// key the JSON walk reads and writes this field under, so a declaration
	// can meet an existing text. Table fields only; "" means the field's own
	// name is the key. It moves no wire byte — keys are the text's business,
	// ids are the wire's.
	JsonKey string

	Array      ArrayKind
	ArrayBound int64
	ArrayExpr  Expr  // the declared bound expression, for rendering
	ArrayMin   int64 // ArrayCounted range form; 0 otherwise

	// KeyEnum is the enum an ENUM-KEYED array is keyed by — the `[E]T`
	// spelling (SPEC-TABLES.md §2.4). The field is an ArrayFixed of
	// E.Max + 1 elements indexed directly by the enum value; slot 0 is
	// None's and is NEVER VALID, because None is the null key — it names no
	// record, it never rides, and indexing it is an error. On the TABLE wire
	// the slots ride keyed by variant id under their own wire kind, so the
	// keyed and positional bodies can never be decoded as one another. ""
	// when the field is not keyed. On the type wire the spelling is exactly
	// `[E.Max + 1]T` and this field changes nothing: the projection carries
	// the resolved bound, so the two spellings share one protocol id.
	KeyEnum    string
	KeyEnumRef *Enum

	Type FieldType

	// specified default (SPEC §5: zero initialization everywhere unless a
	// specified default overrides it)
	HasDefault bool
	DefBool    bool
	DefInt     *big.Int
	DefFloat   float64
	DefVariant string // enum-typed field: the defaulted variant name
	DefExpr    Expr   // for symbolic rendering

	// resolved wire refinements
	HasIntRange   bool
	IntMin        *big.Int
	IntMax        *big.Int
	IntMinExpr    Expr // for symbolic rendering in generated code
	IntMaxExpr    Expr
	HasFloatRange bool // the compressed-float triple (SPEC §4.3)
	FMin          float64
	FMax          float64
	Resolution    float64
	// Round is "" on non-compressed fields and "nearest" on every
	// compressed float — a FROZEN projection token (`round=nearest`
	// renders on every compressed-float line, and keeping it keeps every
	// such unit's id stable; see ir.WireProjection).
	Round string
	Steps int64 // ceil((FMax-FMin)/Resolution)
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
	Signed   bool   // TInt; TFixed (fixed = true; ufixed = false)
	Width    int    // TInt: 8/16/32/64/128; TBits: N; TFixed: I+F (the storage width)
	IntBits  int    // TFixed: I — integer bits; the sign bit counts when Signed (SPEC §4.3)
	FracBits int    // TFixed: F — fractional bits
	Size     int64  // TString/TBytes: N (max length)
	SizeExpr Expr   // TString/TBytes: the declared N expression
	Name     string // TNamed
	Ref      Decl   // TNamed: *Struct, *Enum, *Flags or *Union

	// Pointer marks a `*T` field: a POINTER to a table, not a by-value
	// nesting (SPEC-TABLES.md). Only ever set on a TNamed field whose Ref is
	// a table, declared inside a table body — the checker refuses every other
	// spelling by name. A pointer's presence is what makes its owner a
	// VARIABLE-LENGTH table (ir.VariableTables).
	Pointer bool

	// Optional marks a `?T` field: an OPTIONAL by-value field carrying a
	// generated `<name>_present` bool beside its value (SPEC-TABLES.md
	// §2.3). Table bodies only, and never on a pointer, an array, a string,
	// bytes or a union — the checker refuses each by name. The holder stays
	// FIXED-SIZE: an optional costs one bool and no allocation.
	Optional bool
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
